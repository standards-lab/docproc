//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/postgres"
	"github.com/standards-lab/sqlate"
)

// aged returns fixtureKey under the stage name name, so a test holds several keys apart by name.
func aged(name string) dispatch.Key {
	k := fixtureKey
	k.Stage.Name = name
	return k
}

// recordAged records name's key in idx with a log of n items, then sets its created_at to ago
// before the database's clock.
func recordAged(t *testing.T, db *sqlate.DB, idx *postgres.Index, name string, n int, ago string) {
	t.Helper()
	ctx := t.Context()
	k := aged(name)
	token, _, ok, err := idx.Claim(ctx, k)
	if err != nil || !ok {
		t.Fatalf("claim %s: %v, %v", name, ok, err)
	}
	for i := range n {
		if err := idx.Append(ctx, k, token, warned(string(rune('0'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Record(ctx, k, token, fixture); err != nil {
		t.Fatal(err)
	}
	age(t, db, name, ago)
}

// age sets name's row's created_at to ago before the database's clock.
func age(t *testing.T, db *sqlate.DB, name, ago string) {
	t.Helper()
	exec(t, db, `UPDATE docproc_entry SET created_at = now() - $1::interval WHERE stage_name = $2`, ago, name)
}

// names returns the stage names of the entries left, in name order.
func names(t *testing.T, db *sqlate.DB) []string {
	t.Helper()
	r, err := db.QueryContext(t.Context(), `SELECT stage_name FROM docproc_entry ORDER BY stage_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []string
	for r.Next() {
		var n string
		if err := r.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// logOf counts the log rows of name's key, under any token.
func logOf(t *testing.T, db *sqlate.DB, name string) int {
	t.Helper()
	return scalar[int](t, db, `SELECT count(*) FROM docproc_item WHERE stage_name = $1`, name)
}

// sweep runs one pass and checks what it reports and the entries it leaves.
func sweep(t *testing.T, db *sqlate.DB, r *postgres.Retention, budget int, wantMore bool, wantLeft ...string) {
	t.Helper()
	more, err := r.Pass(t.Context(), budget)
	if err != nil {
		t.Fatalf("pass with budget %d: %v", budget, err)
	}
	if more != wantMore {
		t.Errorf("pass with budget %d: more = %v, want %v", budget, more, wantMore)
	}
	if left := names(t, db); !slices.Equal(left, wantLeft) {
		t.Fatalf("after a pass with budget %d, the index holds %v, want %v", budget, left, wantLeft)
	}
}

// A pass drops the entries created more than the age ago, oldest first, at most a budget at a
// time, with their logs: recorded entries, and a claim whose lease ran out. It keeps an entry
// within the age, a claim under a live lease, and a claim that never expires, though both claims
// are older than every other entry.
func TestLive_RetentionDropsAgedEntriesOldestFirst(t *testing.T) {
	db := migrated(t)
	ctx := t.Context()
	idx := index(t, db, 1)
	recordAged(t, db, idx, "old", 2, "5 hours")
	recordAged(t, db, idx, "older", 0, "3 hours")
	recordAged(t, db, idx, "young", 1, "10 minutes")
	// lapsed is claimed with a flushed log, its lease run out an hour ago.
	lapsed := aged("lapsed")
	token, _, ok, err := idx.Claim(ctx, lapsed)
	if err != nil || !ok {
		t.Fatalf("claim lapsed: %v, %v", ok, err)
	}
	if err := idx.Append(ctx, lapsed, token, warned("lapsed")); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `UPDATE docproc_entry SET expires_at = now() - interval '1 hour' WHERE stage_name = 'lapsed'`)
	age(t, db, "lapsed", "4 hours")
	// live holds an hour's lease; never's claim never expires.
	long := indexLeased(t, db, 1, time.Hour)
	for _, name := range []string{"live", "never"} {
		if _, _, ok, err := long.Claim(ctx, aged(name)); err != nil || !ok {
			t.Fatalf("claim %s: %v, %v", name, ok, err)
		}
	}
	exec(t, db, `UPDATE docproc_entry SET expires_at = NULL WHERE stage_name = 'never'`)
	age(t, db, "live", "9 hours")
	age(t, db, "never", "8 hours")

	r, err := postgres.NewRetention(db, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// old then lapsed are the oldest eligible; a full budget reports more.
	sweep(t, db, r, 2, true, "live", "never", "older", "young")
	for _, name := range []string{"old", "lapsed"} {
		if n := logOf(t, db, name); n != 0 {
			t.Errorf("%s's log holds %d items after its entry was dropped", name, n)
		}
	}
	if n := logOf(t, db, "young"); n != 1 {
		t.Errorf("young's log holds %d items, want 1", n)
	}
	// older is the last eligible; dropping fewer than the budget reports none.
	sweep(t, db, r, 2, false, "live", "never", "young")
	sweep(t, db, r, 2, false, "live", "never", "young")
}

// A pass that drops exactly its budget reports more, though no eligible entry remains: the next
// pass drops none and reports none.
func TestLive_RetentionFullBudgetReportsMore(t *testing.T) {
	db := migrated(t)
	idx := index(t, db, 1)
	recordAged(t, db, idx, "a", 1, "2 hours")
	recordAged(t, db, idx, "b", 1, "2 hours")
	r, err := postgres.NewRetention(db, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sweep(t, db, r, 2, true)
	sweep(t, db, r, 2, false)
}

// A pass notifies each key it drops on docproc_index with the key's payload, as Drop does, so a
// waiter on a swept key wakes.
func TestLive_RetentionNotifies(t *testing.T) {
	a, _, dsn := hosts(t)
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "LISTEN docproc_index"); err != nil {
		t.Fatal(err)
	}
	k := aged("swept")
	h := sha256.Sum256([]byte(strings.Join([]string{k.SHA256, k.Stage.Name, k.Stage.Version, k.Stage.Config}, "\x00")))
	recordAged(t, a, index(t, a, 1), "swept", 0, "2 hours")
	r, err := postgres.NewRetention(a, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, prompt)
	defer cancel()
	// Record notified too; its notification is read first.
	if _, err := conn.WaitForNotification(wctx); err != nil {
		t.Fatalf("record: %v", err)
	}
	if more, err := r.Pass(ctx, 1); err != nil || !more {
		t.Fatalf("pass: %v, %v, want one entry dropped", more, err)
	}
	n, err := conn.WaitForNotification(wctx)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if want := hex.EncodeToString(h[:]); n.Channel != "docproc_index" || n.Payload != want {
		t.Errorf("pass notified %s %q, want docproc_index %q", n.Channel, n.Payload, want)
	}
}
