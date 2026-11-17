package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/standards-lab/docproc/dispatch"
)

// channel is the one channel Release, Record, and Drop notify a key's waiters on, each with the
// key's payload.
const channel = "docproc_index"

// The listener's commands, which it runs on its pgx connection, not through the catalog: LISTEN
// and UNLISTEN take no parameters and can't be prepared, so Verify could not check them.
const (
	listenSQL   = "LISTEN " + channel
	unlistenSQL = "UNLISTEN " + channel
)

// unlistenTimeout bounds the UNLISTEN that readies the listener's connection for its pool again.
const unlistenTimeout = 5 * time.Second

// errClosed refuses a Wait on an Index that was closed, and ends the waits in flight at Close.
var errClosed = errors.New("postgres: index: closed")

// payload is key's notification payload: the lowercase hex SHA-256 of its SHA-256, name, version,
// and config joined by NUL bytes, as the statements compute it in SQL.
func payload(key dispatch.Key) string {
	h := sha256.New()
	for i, s := range []string{key.SHA256, key.Stage.Name, key.Stage.Version, key.Stage.Config} {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// listener holds one pooled connection LISTENing on channel and fans each notification out to
// the waiters registered for its payload. ready closes once the LISTEN is in effect, and done
// once the listener has ended, its connection back in the pool, with err saying why.
type listener struct {
	cancel context.CancelFunc
	ready  chan struct{}
	done   chan struct{}

	mu      sync.Mutex
	waiters map[string]map[*waiter]struct{}
	err     error
}

// waiter is one Wait's registration with a listener: its key's payload and the channel that
// wakes it, buffered so a notification that arrives while it reads the row is kept.
type waiter struct {
	l       *listener
	payload string
	wake    chan struct{}
}

// subscribe registers a waiter for key with the Index's listener, starting one when there is
// none or the last has ended, and returns once the listener's LISTEN is in effect, so a
// notification sent after it returns reaches the waiter. A waiter subscribes before it reads
// the key's row: a claim that ends between the two is either seen by the read or notified.
func (x *Index) subscribe(ctx context.Context, key dispatch.Key) (*waiter, error) {
	x.lmu.Lock()
	if x.closed {
		x.lmu.Unlock()
		return nil, errClosed
	}
	l := x.listener
	if l == nil || isDone(l.done) {
		l = x.listen()
		x.listener = l
	}
	w := &waiter{l: l, payload: payload(key), wake: make(chan struct{}, 1)}
	l.add(w)
	x.lmu.Unlock()
	select {
	case <-l.ready:
		return w, nil
	case <-l.done:
		if isDone(l.ready) {
			// It ended after its LISTEN; the waiter sees done and subscribes again.
			return w, nil
		}
		l.remove(w)
		return nil, l.failure()
	case <-ctx.Done():
		l.remove(w)
		return nil, ctx.Err()
	}
}

// listen starts a listener on a connection of its own from the pool, held until Close, and
// until its connection fails, which ends it: its waiters, woken by done, subscribe again, and
// the first of them starts the next listener. It runs on a context of its own, since it
// outlives the Wait that starts it.
func (x *Index) listen() *listener {
	ctx, cancel := context.WithCancel(context.Background())
	l := &listener{cancel: cancel, ready: make(chan struct{}), done: make(chan struct{}), waiters: map[string]map[*waiter]struct{}{}}
	go func() {
		err := x.hold(ctx, l)
		if ctx.Err() != nil {
			err = errClosed
		}
		l.mu.Lock()
		l.err = fmt.Errorf("postgres: index: wait: the listener's connection: %w", err)
		l.mu.Unlock()
		close(l.done)
	}()
	return l
}

// hold takes the listener's connection, LISTENs on it, and delivers its notifications until ctx
// ends or the connection fails. pgx's connection is reached through Raw, the one way through
// database/sql, and used only inside it, as Raw requires; New checked the driver. Before the
// connection goes back to the pool it is UNLISTENed, or, if that fails, discarded, so no other
// user of the pool receives the channel's notifications.
func (x *Index) hold(ctx context.Context, l *listener) error {
	conn, err := x.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	var held error
	err = conn.Raw(func(dc any) error {
		pc := dc.(*stdlib.Conn).Conn()
		if _, err := pc.Exec(ctx, listenSQL); err != nil {
			held = err
			return driver.ErrBadConn
		}
		close(l.ready)
		for {
			n, err := pc.WaitForNotification(ctx)
			if n != nil {
				l.notify(n.Payload)
			}
			if err != nil {
				held = err
				break
			}
		}
		uctx, cancel := context.WithTimeout(context.Background(), unlistenTimeout)
		defer cancel()
		if _, err := pc.Exec(uctx, unlistenSQL); err != nil {
			return driver.ErrBadConn
		}
		return nil
	})
	if held != nil {
		return held
	}
	return err
}

// add registers w for its payload's notifications.
func (l *listener) add(w *waiter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.waiters[w.payload]
	if set == nil {
		set = map[*waiter]struct{}{}
		l.waiters[w.payload] = set
	}
	set[w] = struct{}{}
}

// remove ends w's registration.
func (l *listener) remove(w *waiter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if set := l.waiters[w.payload]; set != nil {
		delete(set, w)
		if len(set) == 0 {
			delete(l.waiters, w.payload)
		}
	}
}

// notify wakes every waiter registered for payload; a waiter already woken stays so.
func (l *listener) notify(payload string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for w := range l.waiters[payload] {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
}

// failure is why the listener ended.
func (l *listener) failure() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// await blocks until w is notified, expired fires, w's listener ends, or ctx ends, returning the
// context's error in the last case alone.
func (w *waiter) await(ctx context.Context, expired <-chan time.Time) error {
	select {
	case <-w.wake:
	case <-expired:
	case <-w.l.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// cancel ends w's registration.
func (w *waiter) cancel() { w.l.remove(w) }

// isDone reports whether ch is closed.
func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Close stops the listener Wait holds, if one is running, and returns its connection to the
// pool, ending the waits in flight with an error; a Wait after Close fails. The Index's other
// methods are unaffected. Close is safe to call more than once, and always returns nil.
func (x *Index) Close() error {
	x.lmu.Lock()
	x.closed = true
	l := x.listener
	x.lmu.Unlock()
	if l != nil {
		l.cancel()
		<-l.done
	}
	return nil
}
