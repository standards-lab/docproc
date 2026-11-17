//go:build integration

package postgres_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/postgres"
	"github.com/standards-lab/sqlate"
)

// lease is the live tier's claims' lease, a fraction of a second, so the suite's lease case and
// the takeover tests wait it out quickly.
const lease = 500 * time.Millisecond

// index returns an Index over db whose claims last lease and whose log flushes every flush items,
// closed when the test ends, before its database is dropped.
func index(t *testing.T, db *sqlate.DB, flush int) *postgres.Index {
	t.Helper()
	return indexLeased(t, db, flush, lease)
}

// indexLeased is index with claims that last l.
func indexLeased(t *testing.T, db *sqlate.DB, flush int, l time.Duration) *postgres.Index {
	t.Helper()
	idx, err := postgres.New(t.Context(), postgres.Config{DB: db, Lease: l, Flush: flush})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// TestLive_Index runs dispatchtest's Index suite whole against a migrated database per case,
// with a flush of two, so the suite's three-item log is written in two statements and read in
// two pages.
func TestLive_Index(t *testing.T) {
	dispatchtest.Index(t, func(t *testing.T) dispatch.Index {
		return index(t, migrated(t), 2)
	})
}

// The fixture key and items of the tests below.
var (
	fixtureKey = dispatch.Key{SHA256: strings.Repeat("ab", 32), Stage: dispatch.Identity{Name: "fixture", Version: "1", Config: "fixture"}}
	fixture    = dispatch.Entry{Outcome: dispatch.Prepared, Stages: []dispatch.Identity{fixtureKey.Stage}}
)

func warned(where string) dispatch.Recorded {
	return dispatch.Recorded{Warning: &dispatch.Warning{Tool: "fixture", Where: where, What: "unreadable"}}
}

// claimed claims fixtureKey in idx, failing the test unless it is claimed.
func claimed(t *testing.T, idx *postgres.Index) string {
	t.Helper()
	token, got, ok, err := idx.Claim(t.Context(), fixtureKey)
	if err != nil || !ok || got != lease {
		t.Fatalf("claim: %v, %s, %v, want claimed for %s", ok, got, err, lease)
	}
	return token
}

// takeOver claims fixtureKey in idx once the claim that holds it has lapsed.
func takeOver(t *testing.T, idx *postgres.Index) string {
	t.Helper()
	for deadline := time.Now().Add(10 * lease); ; time.Sleep(lease / 10) {
		if time.Now().After(deadline) {
			t.Fatal("the claim wasn't taken over within ten leases")
		}
		if token, _, ok, err := idx.Claim(t.Context(), fixtureKey); err != nil {
			t.Fatal(err)
		} else if ok {
			return token
		}
	}
}

// itemsOf counts the log rows token appended for fixtureKey, the entry's or not.
func itemsOf(t *testing.T, db *sqlate.DB, token string) int {
	t.Helper()
	return scalar[int](t, db, `SELECT count(*) FROM docproc_item WHERE sha256 = $1 AND token = $2`, fixtureKey.SHA256, token)
}

// firstClaimed is the creation time backdate gives fixtureKey's row, far enough back that a
// statement setting created_at to now() can't leave it.
const firstClaimed = "2001-02-03 04:05:06+00"

// backdate sets fixtureKey's row's created_at to firstClaimed.
func backdate(t *testing.T, db *sqlate.DB) {
	t.Helper()
	exec(t, db, `UPDATE docproc_entry SET created_at = $1 WHERE sha256 = $2`, firstClaimed, fixtureKey.SHA256)
}

// keptFirstClaim reports whether fixtureKey's row's created_at is still firstClaimed: an
// entry's age runs from its first claim, and neither a takeover nor a record refreshes it.
func keptFirstClaim(t *testing.T, db *sqlate.DB) bool {
	t.Helper()
	return scalar[bool](t, db, `SELECT created_at = $1::timestamptz FROM docproc_entry WHERE sha256 = $2`, firstClaimed, fixtureKey.SHA256)
}

// A takeover deletes the log the old token flushed, and from then on the old token's renewal,
// append, and record fail matching ErrClaimLost, while its release leaves the new claim, which
// records its own log alone. The row keeps its first claim's created_at. A claim is two hosts'
// here: the old claimant's Index and the taker's.
func TestLive_TakeoverDiscardsTheOldLog(t *testing.T) {
	db := migrated(t)
	ctx := t.Context()
	idx, taker := index(t, db, 1), index(t, db, 1)
	old := claimed(t, idx)
	for _, w := range []string{"1", "2"} {
		if err := idx.Append(ctx, fixtureKey, old, warned(w)); err != nil {
			t.Fatal(err)
		}
	}
	if n := itemsOf(t, db, old); n != 2 {
		t.Fatalf("before the takeover, %d of the old token's items are written, want 2", n)
	}
	backdate(t, db)
	next := takeOver(t, taker)
	if !keptFirstClaim(t, db) {
		t.Error("the takeover changed the row's created_at, want the first claim's")
	}
	if n := itemsOf(t, db, old); n != 0 {
		t.Errorf("after the takeover, %d of the old token's items remain", n)
	}
	if err := idx.Renew(ctx, fixtureKey, old); !errors.Is(err, dispatch.ErrClaimLost) {
		t.Errorf("the old token's renewal: %v, want ErrClaimLost", err)
	}
	// The renewal's refusal ended the old claim here, so a later call fails without a statement;
	// a fresh claimant's old token, whose buffer remains, is refused by the flush's statement.
	if err := idx.Append(ctx, fixtureKey, old, warned("late")); !errors.Is(err, dispatch.ErrClaimLost) {
		t.Errorf("the old token's append: %v, want ErrClaimLost", err)
	}
	if err := idx.Record(ctx, fixtureKey, old, fixture); !errors.Is(err, dispatch.ErrClaimLost) {
		t.Errorf("the old token's record: %v, want ErrClaimLost", err)
	}
	if err := idx.Release(ctx, fixtureKey, old); err != nil {
		t.Errorf("the old token's release: %v", err)
	}
	if err := taker.Append(ctx, fixtureKey, next, warned("taker")); err != nil {
		t.Fatal(err)
	}
	if err := taker.Record(ctx, fixtureKey, next, fixture); err != nil {
		t.Fatal(err)
	}
	var got []string
	for r, err := range taker.Items(ctx, fixtureKey) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, r.Warning.Where)
	}
	if strings.Join(got, ",") != "taker" {
		t.Errorf("items %v, want the taker's alone", got)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM docproc_item`); n != 1 {
		t.Errorf("%d item rows, want the taker's one", n)
	}
}

// Each of the old token's calls is refused by its own statement, on the token, not by a buffer
// the Index dropped: the flush, the record, and the renewal each fail matching ErrClaimLost.
func TestLive_OldTokenStatementsRefuse(t *testing.T) {
	ctx := t.Context()
	for _, call := range []string{"append", "record", "renew"} {
		t.Run(call, func(t *testing.T) {
			db := migrated(t)
			idx := index(t, db, 1)
			old := claimed(t, idx)
			takeOver(t, index(t, db, 1))
			var err error
			switch call {
			case "append":
				err = idx.Append(ctx, fixtureKey, old, warned("late"))
			case "record":
				err = idx.Record(ctx, fixtureKey, old, fixture)
			case "renew":
				err = idx.Renew(ctx, fixtureKey, old)
			}
			if !errors.Is(err, dispatch.ErrClaimLost) {
				t.Errorf("the old token's %s: %v, want ErrClaimLost", call, err)
			}
			if n := itemsOf(t, db, old); n != 0 {
				t.Errorf("the old token's %s wrote %d items", call, n)
			}
		})
	}
}

// Record commits the log whole with its entry or not at all: a record the database fails, here
// through a trigger that refuses every entry, writes none of the buffered items, and the claim
// survives it, its record retried once the trigger is gone writing the log whole, and leaving
// the row's created_at the first claim's.
func TestLive_RecordIsAtomic(t *testing.T) {
	db := migrated(t)
	ctx := t.Context()
	idx := index(t, db, 3)
	token := claimed(t, idx)
	for _, w := range []string{"1", "2", "3", "4", "5"} {
		if err := idx.Append(ctx, fixtureKey, token, warned(w)); err != nil {
			t.Fatal(err)
		}
	}
	if n := itemsOf(t, db, token); n != 3 {
		t.Fatalf("%d items flushed, want the first three", n)
	}
	exec(t, db, `CREATE FUNCTION docproc_test_refuse() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'refused'; END $$`)
	exec(t, db, `CREATE TRIGGER docproc_test_refuse BEFORE UPDATE OF entry ON docproc_entry
		FOR EACH ROW EXECUTE FUNCTION docproc_test_refuse()`)
	if err := idx.Record(ctx, fixtureKey, token, fixture); err == nil || errors.Is(err, dispatch.ErrClaimLost) {
		t.Fatalf("a refused record: %v, want the database's error", err)
	}
	if n := itemsOf(t, db, token); n != 3 {
		t.Errorf("after a failed record, %d items are written, want the three flushed before it", n)
	}
	if _, ok, err := idx.Lookup(ctx, fixtureKey); err != nil || ok {
		t.Errorf("lookup after a failed record: %v, %v, want no entry", ok, err)
	}
	exec(t, db, `DROP TRIGGER docproc_test_refuse ON docproc_entry`)
	backdate(t, db)
	if err := idx.Record(ctx, fixtureKey, token, fixture); err != nil {
		t.Fatalf("the retried record: %v", err)
	}
	n := 0
	for r, err := range idx.Items(ctx, fixtureKey) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; r.Warning.Where != string(rune('0'+n)) {
			t.Errorf("item %d is %q", n, r.Warning.Where)
		}
	}
	if n != 5 {
		t.Errorf("%d items, want 5", n)
	}
	if err := idx.Renew(ctx, fixtureKey, token); !errors.Is(err, dispatch.ErrClaimLost) {
		t.Errorf("renew after the record: %v, want ErrClaimLost", err)
	}
	if exp := scalar[bool](t, db, `SELECT expires_at IS NULL FROM docproc_entry`); !exp {
		t.Error("the recorded row kept a lease")
	}
	if !keptFirstClaim(t, db) {
		t.Error("the record changed the row's created_at, want the first claim's")
	}
}

// A row written under another dispatch.Version is refused, by Lookup and by Items, rather than
// read as this build's shapes.
func TestLive_AnotherVersionIsRefused(t *testing.T) {
	db := migrated(t)
	ctx := t.Context()
	idx := index(t, db, 1)
	token := claimed(t, idx)
	if err := idx.Append(ctx, fixtureKey, token, warned("1")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Record(ctx, fixtureKey, token, fixture); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `UPDATE docproc_entry SET dispatch_version = '0'`)
	if _, _, err := idx.Lookup(ctx, fixtureKey); err == nil || !strings.Contains(err.Error(), `dispatch version "0"`) {
		t.Errorf("lookup of a version 0 row: %v, want it refused", err)
	}
	for _, err := range idx.Items(ctx, fixtureKey) {
		if err == nil || !strings.Contains(err.Error(), `dispatch version "0"`) {
			t.Errorf("items of a version 0 row: %v, want it refused", err)
		}
	}
}

// Drop of a key with no row is no error.
func TestLive_DropAbsent(t *testing.T) {
	idx := index(t, migrated(t), 1)
	if err := idx.Drop(t.Context(), fixtureKey); err != nil {
		t.Errorf("drop of an absent entry: %v", err)
	}
}
