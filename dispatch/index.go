package dispatch

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/standards-lab/docproc/inspect"
)

// Index is a content-addressed record of what has been prepared, shared by every dispatch that
// uses it, concurrent ones and other replicas' among them. It records an [Entry] for content by a
// [Key], the content's SHA-256 and the stage's [Identity], with the entry's log: the outputs,
// warnings, and children its stage yielded, as [Recorded] items, one interleaved log in append
// order, so a replay is the preparation's order. An implementation is safe for concurrent use.
//
// A claim makes one claimant the preparer of a key, so replicas sharing an index prepare content
// once. Claim succeeds for one claimant only, while the key is neither recorded nor claimed, and
// returns the claim's token and its lease. A claim lasts its lease from its last Claim or Renew,
// zero being a claim that never expires while its holder lives; it is held from Claim to Record
// or Release. A claim not renewed within its lease may be taken over by another claimant, and
// from then on the first token's Renew, Append, and Record fail. A takeover, or a Release,
// discards the log items the old token appended.
//
// The claimant appends each item to the claim's log by Append, as its stage yields it. An append
// needs no durability before Record: an implementation may buffer the log, flushing in batches
// and at Record, which commits the log whole, with the entry, or fails; a lost claim surfaces at a
// flush, a Renew, or Record. Wait blocks a claimant that lost the race until the claim ends,
// returning the entry if the claimant recorded one. Drop removes an entry and its log, so its
// content is prepared again.
//
// Package dispatchtest holds this contract as conformance suites, which every implementation
// runs.
type Index interface {
	// Lookup returns key's entry and whether there is one.
	Lookup(ctx context.Context, key Key) (Entry, bool, error)
	// Items yields key's log, in append order, streamed: an implementation holds one item at a
	// time. It yields none for a key with no entry, and an error, last, when an item can't be
	// read or the entry is replaced or dropped while it reads.
	Items(ctx context.Context, key Key) iter.Seq2[Recorded, error]
	// Claim claims key, returning the claim's token, its lease, and true; it returns false,
	// claiming nothing, when key is recorded or another holds its claim.
	Claim(ctx context.Context, key Key) (token string, lease time.Duration, ok bool, err error)
	// Renew renews the claim token names for another lease, failing, matching [ErrClaimLost],
	// when token no longer holds key's claim.
	Renew(ctx context.Context, key Key, token string) error
	// Wait waits while key is claimed, then returns its entry and true if the claimant recorded
	// it, or false if the claim ended without a record. It returns at once when key is recorded
	// or unclaimed, and an error matching the context's when ctx ends first.
	Wait(ctx context.Context, key Key) (Entry, bool, error)
	// Append appends r to the log of the claim token names, failing, matching [ErrClaimLost],
	// when token no longer holds key's claim, or, when the log is buffered, at a later flush or
	// Record. Exactly one of r's
	// Output, Warning, and Child is set; an item with none or more is refused, matching
	// [ErrRequest].
	Append(ctx context.Context, key Key, token string, r Recorded) error
	// Record records e as key's entry, with the log appended under token, replacing any entry
	// key had, and ends the claim, failing, matching [ErrClaimLost], when token no longer holds
	// it. It records e as given, its counts among it, so a replay checks the log against them.
	Record(ctx context.Context, key Key, token string, e Entry) error
	// Release ends the claim token names without a record, discarding its log; a claim another
	// now holds is left.
	Release(ctx context.Context, key Key, token string) error
	// Drop removes key's entry and its log, if it has one.
	Drop(ctx context.Context, key Key) error
}

// Key is what an index records an entry under: the content's SHA-256, lowercase hex, and the
// stage's identity.
type Key struct {
	SHA256 string   `json:"sha256"`
	Stage  Identity `json:"stage"`
}

// Entry is what the index holds for a key: the outcome the part came to and why, how many outputs
// and children its log holds, the distinct stages its subtree ran, its own first, and the part's
// content flags, its stage's among them.
type Entry struct {
	Outcome  Outcome        `json:"outcome"`
	Reason   string         `json:"reason,omitzero"`
	Outputs  int            `json:"outputs,omitzero"`
	Children int            `json:"children,omitzero"`
	Stages   []Identity     `json:"stages"`
	Flags    []inspect.Flag `json:"flags,omitempty"`
}

// Recorded is one item of an entry's log: an output, a warning, or a found part, exactly one of
// them set. After is an output's [Item.After], a store's cursor, opaque and compared by equality
// alone.
type Recorded struct {
	Output  *Output  `json:"output,omitempty"`
	Warning *Warning `json:"warning,omitempty"`
	Child   *Child   `json:"child,omitempty"`
	After   uint64   `json:"after,omitzero"`
}

// Child is a part a stage found, as its parent's entry records it: the found [Part] whole, less
// its local fields, and the outcome it came to. The index keeps its Where and ignores its ID,
// Parent, and Depth, which a replaying walk sets, since the same content can sit at another place.
type Child struct {
	Part    Part    `json:"part"`
	Outcome Outcome `json:"outcome"`
}

func checkRecorded(r Recorded) error {
	n := 0
	for _, set := range []bool{r.Output != nil, r.Warning != nil, r.Child != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("dispatch: index: a log item sets %d of output, warning, and child, want 1: %w", n, ErrRequest)
	}
	return nil
}

var (
	errNotClaimed = fmt.Errorf("dispatch: index: the token doesn't hold the key's claim: %w", ErrClaimLost)
	errReplaced   = errors.New("dispatch: index: the entry was replaced or dropped while its log was read")
)

type claims[P any] struct {
	lease   time.Duration
	discard func(P)
	held    map[Key]*claim[P]
}

type claim[P any] struct {
	token   string
	done    chan struct{}
	expires time.Time
	log     P
}

func newClaims[P any](lease time.Duration, discard func(P)) claims[P] {
	return claims[P]{lease: lease, discard: discard, held: map[Key]*claim[P]{}}
}

func (t *claims[P]) expired(c *claim[P]) bool {
	return t.lease > 0 && !time.Now().Before(c.expires)
}

func (t *claims[P]) take(key Key, log P) (string, bool) {
	if c, ok := t.held[key]; ok {
		if !t.expired(c) {
			return "", false
		}
		t.end(key, c.token)
	}
	c := &claim[P]{token: rand.Text(), done: make(chan struct{}), log: log}
	if t.lease > 0 {
		c.expires = time.Now().Add(t.lease)
	}
	t.held[key] = c
	return c.token, true
}

func (t *claims[P]) holder(key Key, token string) (*claim[P], error) {
	c, ok := t.held[key]
	if !ok || c.token != token {
		return nil, errNotClaimed
	}
	return c, nil
}

func (t *claims[P]) renew(key Key, token string) error {
	c, err := t.holder(key, token)
	if err != nil {
		return err
	}
	if t.lease > 0 {
		c.expires = time.Now().Add(t.lease)
	}
	return nil
}

func (t *claims[P]) end(key Key, token string) {
	if c, ok := t.held[key]; ok && c.token == token {
		delete(t.held, key)
		t.discard(c.log)
		close(c.done)
	}
}

func (t *claims[P]) commit(key Key, c *claim[P]) {
	delete(t.held, key)
	close(c.done)
}

func (t *claims[P]) waitOn(key Key) (done <-chan struct{}, lease *time.Timer) {
	c, ok := t.held[key]
	if !ok || t.expired(c) {
		return nil, nil
	}
	if t.lease > 0 {
		lease = time.NewTimer(time.Until(c.expires))
	}
	return c.done, lease
}

func wait(ctx context.Context, done <-chan struct{}, lease *time.Timer) error {
	var expired <-chan time.Time
	if lease != nil {
		defer lease.Stop()
		expired = lease.C
	}
	select {
	case <-done:
	case <-expired:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
