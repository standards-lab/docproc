package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type renewer struct {
	index Index
	ctx   context.Context
	lost  func(error)

	mu       sync.Mutex
	renewing sync.Mutex
	claims   map[Key]*renewal
	started  bool
	wake     chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
}

type renewal struct {
	token string
	lease time.Duration
	next  time.Time
}

func newRenewer(ctx context.Context, index Index, lost func(error)) *renewer {
	return &renewer{
		index: index, ctx: ctx, lost: lost,
		claims: map[Key]*renewal{}, wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
}

func (r *renewer) add(key Key, token string, lease time.Duration) {
	if lease <= 0 {
		return
	}
	r.mu.Lock()
	r.claims[key] = &renewal{token: token, lease: lease, next: time.Now().Add(lease / 3)}
	if !r.started {
		r.started = true
		r.wg.Go(r.loop)
	}
	r.mu.Unlock()
	r.poke()
}

func (r *renewer) drop(key Key) {
	r.renewing.Lock()
	defer r.renewing.Unlock()
	r.mu.Lock()
	delete(r.claims, key)
	r.mu.Unlock()
}

func (r *renewer) stop() {
	close(r.done)
	r.wg.Wait()
}

func (r *renewer) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *renewer) loop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		timer.Reset(r.due())
		select {
		case <-r.done:
			return
		case <-r.wake:
			continue
		case <-timer.C:
		}
		err := r.renewDue()
		if r.ctx.Err() != nil {
			return
		}
		if err != nil {
			r.lost(err)
			return
		}
	}
}

func (r *renewer) due() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	wait := time.Hour
	for _, c := range r.claims {
		wait = min(wait, time.Until(c.next))
	}
	return max(wait, 0)
}

func (r *renewer) renewDue() error {
	r.renewing.Lock()
	defer r.renewing.Unlock()
	r.mu.Lock()
	type due struct {
		key Key
		c   renewal
	}
	var todo []due
	now := time.Now()
	for k, c := range r.claims {
		if !now.Before(c.next) {
			todo = append(todo, due{k, *c})
		}
	}
	r.mu.Unlock()
	for _, d := range todo {
		if err := r.index.Renew(r.ctx, d.key, d.c.token); err != nil {
			if !errors.Is(err, ErrClaimLost) {
				err = fmt.Errorf("%w: %w", ErrClaimLost, err)
			}
			return fmt.Errorf("dispatch: renew the claim of %s under %s: %w", d.key.SHA256, d.key.Stage, err)
		}
		r.mu.Lock()
		if c, ok := r.claims[d.key]; ok && c.token == d.c.token {
			c.next = time.Now().Add(c.lease / 3)
		}
		r.mu.Unlock()
	}
	return nil
}
