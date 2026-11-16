package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"time"
)

// MemoryIndex is an [Index] in memory, for the dispatches of one process: a waiter wakes the
// moment the claimant records or releases. It holds every entry and its log, and a claim's log,
// in memory, each as the JSON a provider stores, so what it gives back is what a provider's
// round trip gives, never memory a caller shares. Its zero value is not ready; [NewMemoryIndex]
// makes one.
type MemoryIndex struct {
	mu      sync.Mutex
	entries map[Key]*memoryEntry
	claims  claims[[][]byte]
}

// memoryEntry is a recorded entry and its log, never changed once recorded: a later record or a
// drop replaces it, which a reader of its log tells by its pointer.
type memoryEntry struct {
	entry []byte
	log   [][]byte
}

// NewMemoryIndex returns an empty MemoryIndex whose claims last lease from their last Claim or
// Renew, zero for claims that never expire, as [FileIndex]'s. No lease applies by default; a
// claim not renewed within a lease may be taken over.
func NewMemoryIndex(lease time.Duration) *MemoryIndex {
	return &MemoryIndex{
		entries: map[Key]*memoryEntry{},
		claims:  newClaims(lease, func([][]byte) {}),
	}
}

// Lookup returns key's entry and whether there is one.
func (m *MemoryIndex) Lookup(ctx context.Context, key Key) (Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, false, err
	}
	m.mu.Lock()
	me, ok := m.entries[key]
	m.mu.Unlock()
	if !ok {
		return Entry{}, false, nil
	}
	var e Entry
	if err := json.Unmarshal(me.entry, &e); err != nil {
		return Entry{}, false, fmt.Errorf("dispatch: memory index: %w", err)
	}
	return e, true, nil
}

// Items yields key's log, one item at a time, failing when the entry is replaced or dropped
// while it reads.
func (m *MemoryIndex) Items(ctx context.Context, key Key) iter.Seq2[Recorded, error] {
	return func(yield func(Recorded, error) bool) {
		m.mu.Lock()
		me := m.entries[key]
		m.mu.Unlock()
		if me == nil {
			return
		}
		for _, b := range me.log {
			if err := ctx.Err(); err != nil {
				yield(Recorded{}, err)
				return
			}
			m.mu.Lock()
			current := m.entries[key]
			m.mu.Unlock()
			if current != me {
				yield(Recorded{}, errReplaced)
				return
			}
			var r Recorded
			if err := json.Unmarshal(b, &r); err != nil {
				yield(Recorded{}, fmt.Errorf("dispatch: memory index: %w", err))
				return
			}
			if !yield(r, nil) {
				return
			}
		}
	}
}

// Claim claims key unless it is recorded or another holds its claim, taking over a claim whose
// lease ran out.
func (m *MemoryIndex) Claim(ctx context.Context, key Key) (string, time.Duration, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[key]; ok {
		return "", 0, false, nil
	}
	token, ok := m.claims.take(key, nil)
	if !ok {
		return "", 0, false, nil
	}
	return token, m.claims.lease, true, nil
}

// Renew renews token's claim of key for another lease.
func (m *MemoryIndex) Renew(ctx context.Context, key Key, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claims.renew(key, token)
}

// Wait waits for key's claim to end or its lease to run out, then returns its entry if it has
// one.
func (m *MemoryIndex) Wait(ctx context.Context, key Key) (Entry, bool, error) {
	for {
		m.mu.Lock()
		_, recorded := m.entries[key]
		done, lease := m.claims.waitOn(key)
		m.mu.Unlock()
		if recorded || done == nil {
			return m.Lookup(ctx, key)
		}
		if err := wait(ctx, done, lease); err != nil {
			return Entry{}, false, err
		}
	}
}

// Append appends r's JSON to token's claim's log.
func (m *MemoryIndex) Append(ctx context.Context, key Key, token string, r Recorded) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkRecorded(r); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("dispatch: memory index: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.claims.holder(key, token)
	if err != nil {
		return err
	}
	c.log = append(c.log, b)
	return nil
}

// Record records e as key's entry, with token's claim's log, and ends the claim.
func (m *MemoryIndex) Record(ctx context.Context, key Key, token string, e Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("dispatch: memory index: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.claims.holder(key, token)
	if err != nil {
		return err
	}
	m.entries[key] = &memoryEntry{entry: b, log: c.log}
	m.claims.commit(key, c)
	return nil
}

// Release ends token's claim of key, discarding its log.
func (m *MemoryIndex) Release(ctx context.Context, key Key, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims.end(key, token)
	return ctx.Err()
}

// Drop removes key's entry and its log.
func (m *MemoryIndex) Drop(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}
