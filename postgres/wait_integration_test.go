//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/postgres"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/postgres/pgtest"
)

const (
	held       = time.Minute
	prompt     = 10 * time.Second
	quiet      = 100 * time.Millisecond
	unprompted = 2 * time.Second
)

func hosts(t *testing.T) (a, b *sqlate.DB, dsn string) {
	t.Helper()
	d := pgtest.Migrated(t, dsnEnv, prefix, docprocSet(t))
	pool, err := sql.Open("pgx", d.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return d.Session(), sqlate.Wrap(pool, pgdialect.Dialect{}), d.DSN
}

type waited struct {
	e   dispatch.Entry
	ok  bool
	err error
}

func waitOn(t *testing.T, ctx context.Context, idx *postgres.Index) <-chan waited {
	t.Helper()
	done := make(chan waited, 1)
	go func() {
		e, ok, err := idx.Wait(ctx, fixtureKey)
		done <- waited{e, ok, err}
	}()
	select {
	case w := <-done:
		t.Fatalf("wait returned while the key was claimed: %+v", w)
	case <-time.After(quiet):
	}
	return done
}

func result(t *testing.T, done <-chan waited) waited {
	t.Helper()
	select {
	case w := <-done:
		return w
	case <-time.After(prompt):
		t.Fatalf("wait didn't return within %s", prompt)
		return waited{}
	}
}

func TestLive_WaitWakesAtTheClaimsEnd(t *testing.T) {
	for _, end := range []string{"record", "release", "drop"} {
		t.Run(end, func(t *testing.T) {
			a, b, _ := hosts(t)
			ctx := t.Context()
			claimant, waiter := indexLeased(t, a, 1, held), indexLeased(t, b, 1, held)
			token, _, ok, err := claimant.Claim(ctx, fixtureKey)
			if err != nil || !ok {
				t.Fatalf("claim: %v, %v", ok, err)
			}
			done := waitOn(t, ctx, waiter)
			switch end {
			case "record":
				err = claimant.Record(ctx, fixtureKey, token, fixture)
			case "release":
				err = claimant.Release(ctx, fixtureKey, token)
			case "drop":
				err = claimant.Drop(ctx, fixtureKey)
			}
			if err != nil {
				t.Fatal(err)
			}
			w := result(t, done)
			record := end == "record"
			if w.err != nil || w.ok != record || (record && !reflect.DeepEqual(w.e, fixture)) {
				t.Errorf("wait after the %s: %+v, %v, %v, want an entry %v", end, w.e, w.ok, w.err, record)
			}
		})
	}
}

func TestLive_WaitOutlastsADeadClaimant(t *testing.T) {
	a, b, _ := hosts(t)
	ctx := t.Context()
	claimant, waiter := index(t, a, 1), indexLeased(t, b, 1, held)
	start := time.Now()
	claimed(t, claimant)
	w := result(t, waitOn(t, ctx, waiter))
	if w.err != nil || w.ok {
		t.Errorf("wait on a dead claimant's key: %v, %v, want no entry", w.ok, w.err)
	}
	if took := time.Since(start); took < lease-50*time.Millisecond {
		t.Errorf("wait returned after %s, before the lease of %s ran out", took, lease)
	}
}

func TestLive_WaitEndsWithItsContext(t *testing.T) {
	a, b, _ := hosts(t)
	claimant, waiter := indexLeased(t, a, 1, held), indexLeased(t, b, 1, held)
	claimAny(t, claimant)
	ctx, cancel := context.WithCancel(t.Context())
	done := waitOn(t, ctx, waiter)
	cancel()
	if w := result(t, done); w.err != ctx.Err() {
		t.Errorf("wait after its context was cancelled: %v, want %v", w.err, ctx.Err())
	}
}

func claimAny(t *testing.T, idx *postgres.Index) string {
	t.Helper()
	token, _, ok, err := idx.Claim(t.Context(), fixtureKey)
	if err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	return token
}

func listenerPID(t *testing.T, db *sqlate.DB, except int) int {
	t.Helper()
	for deadline := time.Now().Add(prompt); ; time.Sleep(quiet / 10) {
		if time.Now().After(deadline) {
			t.Fatalf("no listener but %d within %s", except, prompt)
		}
		if pid := scalar[int](t, db, `SELECT CASE WHEN count(*) = 1 THEN min(pid) ELSE 0 END
			FROM pg_stat_activity WHERE datname = current_database()
			AND query = 'LISTEN docproc_index' AND pid <> $1`, except); pid != 0 {
			return pid
		}
	}
}

func TestLive_WaitSurvivesItsListenersEnd(t *testing.T) {
	a, b, _ := hosts(t)
	ctx := t.Context()
	claimant, waiter := indexLeased(t, a, 1, held), indexLeased(t, b, 1, held)
	token := claimAny(t, claimant)
	done := waitOn(t, ctx, waiter)
	old := listenerPID(t, a, 0)
	if !scalar[bool](t, a, `SELECT pg_terminate_backend($1)`, old) {
		t.Fatalf("listener %d wasn't terminated", old)
	}
	listenerPID(t, a, old)
	if err := claimant.Record(ctx, fixtureKey, token, fixture); err != nil {
		t.Fatal(err)
	}
	if w := result(t, done); w.err != nil || !w.ok {
		t.Errorf("wait after its listener ended and the claim was recorded: %v, %v, want the entry", w.ok, w.err)
	}
}

func TestLive_WaitWakesOnlyWhenNotified(t *testing.T) {
	a, b, _ := hosts(t)
	ctx := t.Context()
	claimant, waiter := indexLeased(t, a, 1, held), indexLeased(t, b, 1, held)
	claimAny(t, claimant)
	done := waitOn(t, ctx, waiter)
	exec(t, a, `DELETE FROM docproc_entry WHERE sha256 = $1`, fixtureKey.SHA256)
	select {
	case w := <-done:
		t.Fatalf("wait returned with no notification: %+v", w)
	case <-time.After(unprompted):
	}
	exec(t, a, `SELECT pg_notify('docproc_index', $1)`, payloadOf(fixtureKey))
	if w := result(t, done); w.err != nil || w.ok {
		t.Errorf("wait after the notification: %v, %v, want no entry", w.ok, w.err)
	}
}

func payloadOf(k dispatch.Key) string {
	h := sha256.Sum256([]byte(strings.Join([]string{k.SHA256, k.Stage.Name, k.Stage.Version, k.Stage.Config}, "\x00")))
	return hex.EncodeToString(h[:])
}

func TestLive_CloseEndsTheWaits(t *testing.T) {
	a, b, _ := hosts(t)
	ctx := t.Context()
	claimant, waiter := indexLeased(t, a, 1, held), indexLeased(t, b, 1, held)
	claimAny(t, claimant)
	done := waitOn(t, ctx, waiter)
	if err := waiter.Close(); err != nil {
		t.Fatal(err)
	}
	if w := result(t, done); w.err == nil || errors.Is(w.err, context.Canceled) {
		t.Errorf("a wait in flight at Close: %v, want the closed index's error", w.err)
	}
	if _, _, err := waiter.Wait(ctx, fixtureKey); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("a wait after Close: %v, want the closed index's error", err)
	}
	if _, ok, err := waiter.Lookup(ctx, fixtureKey); err != nil || ok {
		t.Errorf("lookup after Close: %v, %v, want no entry", ok, err)
	}
	if err := waiter.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
}

func TestLive_NotifyPayload(t *testing.T) {
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
	want := payloadOf(fixtureKey)
	idx := index(t, a, 1)
	next := func(what string) {
		t.Helper()
		wctx, cancel := context.WithTimeout(ctx, prompt)
		defer cancel()
		n, err := conn.WaitForNotification(wctx)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n.Channel != "docproc_index" || n.Payload != want {
			t.Errorf("%s notified %s %q, want docproc_index %q", what, n.Channel, n.Payload, want)
		}
	}
	if err := idx.Release(ctx, fixtureKey, claimed(t, idx)); err != nil {
		t.Fatal(err)
	}
	next("release")
	if err := idx.Record(ctx, fixtureKey, claimed(t, idx), fixture); err != nil {
		t.Fatal(err)
	}
	next("record")
	if err := idx.Drop(ctx, fixtureKey); err != nil {
		t.Fatal(err)
	}
	next("drop")
	if err := idx.Release(ctx, fixtureKey, "absent"); err != nil {
		t.Fatal(err)
	}
	if err := idx.Drop(ctx, fixtureKey); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, quiet)
	defer cancel()
	if n, err := conn.WaitForNotification(wctx); err == nil {
		t.Errorf("a release and a drop that changed nothing notified %q", n.Payload)
	}
}
