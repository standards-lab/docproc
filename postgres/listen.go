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

const channel = "docproc_index"

const (
	listenSQL   = "LISTEN " + channel
	unlistenSQL = "UNLISTEN " + channel
)

const unlistenTimeout = 5 * time.Second

var errClosed = errors.New("postgres: index: closed")

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

type listener struct {
	cancel context.CancelFunc
	ready  chan struct{}
	done   chan struct{}

	mu      sync.Mutex
	waiters map[string]map[*waiter]struct{}
	err     error
}

type waiter struct {
	l       *listener
	payload string
	wake    chan struct{}
}

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
			return w, nil
		}
		l.remove(w)
		return nil, l.failure()
	case <-ctx.Done():
		l.remove(w)
		return nil, ctx.Err()
	}
}

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

func (l *listener) failure() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

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

func (w *waiter) cancel() { w.l.remove(w) }

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
