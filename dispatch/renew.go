package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// renewer is a dispatch's one renewer: a goroutine that holds the claims in flight, the chain of
// parts being prepared, and renews each at a third of its lease, whatever the walk's goroutine is
// doing, the caller's loop body included. A renewal the index refuses calls lost, once, with an
// error matching [ErrClaimLost], which ends the dispatch. A claim of no lease is never renewed.
type renewer struct {
	index Index
	ctx   context.Context
	lost  func(error)

	// mu guards claims and started; renewing is held across a round of renewals, so a claim
	// dropped is never renewed after drop returns.
	mu       sync.Mutex
	renewing sync.Mutex
	claims   map[Key]*renewal
	started  bool
	wake     chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
}

// renewal is one claim the renewer holds: its token, its lease, and when it is next renewed.
type renewal struct {
	token string
	lease time.Duration
	next  time.Time
}

// newRenewer returns a renewer of claims in index, renewing under ctx, which calls lost when a
// renewal is refused.
func newRenewer(ctx context.Context, index Index, lost func(error)) *renewer {
	return &renewer{
		index: index, ctx: ctx, lost: lost,
		claims: map[Key]*renewal{}, wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
}

// add holds key's claim, token's, of lease, starting the goroutine at the first claim with a
// lease.
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

// drop stops renewing key's claim, before its Record or Release, waiting for a renewal in flight.
func (r *renewer) drop(key Key) {
	r.renewing.Lock()
	defer r.renewing.Unlock()
	r.mu.Lock()
	delete(r.claims, key)
	r.mu.Unlock()
}

// stop ends the goroutine, once the dispatch is done.
func (r *renewer) stop() {
	close(r.done)
	r.wg.Wait()
}

// poke wakes the goroutine to take a new claim's time.
func (r *renewer) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// loop renews each claim when it is due, until stop.
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
			// The dispatch is ending: its own error stands.
			return
		}
		if err != nil {
			r.lost(err)
			return
		}
	}
}

// due returns how long until the next claim is due, an hour when none is held.
func (r *renewer) due() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	wait := time.Hour
	for _, c := range r.claims {
		wait = min(wait, time.Until(c.next))
	}
	return max(wait, 0)
}

// renewDue renews every claim that is due, returning the first refusal.
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
