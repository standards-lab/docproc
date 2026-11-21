//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/postgres"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/postgres/pgtest"
	"github.com/standards-lab/sqlate/query"
)

const (
	dsnEnv = "DOCPROC_DSN"
	prefix = "docproc"
)

func TestLive_Ping(t *testing.T) {
	db := pgtest.Create(t, dsnEnv, prefix)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := db.Pool.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func bare(t *testing.T) *sqlate.DB {
	t.Helper()
	return pgtest.Create(t, dsnEnv, prefix).Session()
}

func migrated(t *testing.T) *sqlate.DB {
	t.Helper()
	return pgtest.Migrated(t, dsnEnv, prefix, docprocSet(t)).Session()
}

func docprocSet(t *testing.T) migrate.Set {
	t.Helper()
	s, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func statement(t *testing.T, name string) query.Statement {
	t.Helper()
	for _, st := range postgres.Statements() {
		if st.Name() == name {
			return st
		}
	}
	t.Fatalf("no statement %s", name)
	return query.Statement{}
}

func exec(t *testing.T, db *sqlate.DB, sql string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func scalar[T any](t *testing.T, db *sqlate.DB, sql string, args ...any) T {
	t.Helper()
	r, err := db.QueryContext(t.Context(), sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	defer func() { _ = r.Close() }()
	var v T
	if !r.Next() {
		t.Fatalf("%s: no row: %v", sql, r.Err())
	}
	if err := r.Scan(&v); err != nil {
		t.Fatalf("%s: scan: %v", sql, err)
	}
	return v
}

func exists(t *testing.T, db *sqlate.DB, name string) bool {
	t.Helper()
	return scalar[bool](t, db, `SELECT to_regclass($1) IS NOT NULL`, name)
}

var key = query.Args{
	"sha256":        strings.Repeat("ab", 32),
	"stage_name":    "fixture",
	"stage_version": "1",
	"stage_config":  "fixture",
}

var keyCols = []any{key["sha256"], key["stage_name"], key["stage_version"], key["stage_config"]}

func TestLive_MigrationsUpAndDown(t *testing.T) {
	db := bare(t)
	m, err := migrate.New(db, []migrate.Set{docprocSet(t)}, migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(t.Context()); err != nil {
		t.Fatalf("up: %v", err)
	}
	for _, name := range []string{"docproc_entry", "docproc_item", "docproc_ix_entry_created", postgres.Table} {
		if !exists(t, db, name) {
			t.Errorf("after up, %s is missing", name)
		}
	}
	if def := scalar[string](t, db, `SELECT pg_get_indexdef('docproc_ix_entry_created'::regclass)`); !strings.HasSuffix(def, "docproc_entry USING btree (created_at)") {
		t.Errorf("the retention's index is %s, want it on created_at", def)
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down: %v", err)
	}
	for _, name := range []string{"docproc_entry", "docproc_item"} {
		if exists(t, db, name) {
			t.Errorf("after down, %s remains", name)
		}
	}
}

func TestLive_VerifyNeedsTheMigratedSchema(t *testing.T) {
	if err := postgres.Verify(t.Context(), bare(t)); err == nil {
		t.Fatal("Verify passed on a database without the docproc set")
	}
	if err := postgres.Verify(t.Context(), migrated(t)); err != nil {
		t.Fatalf("Verify on the migrated schema: %v", err)
	}
}

func TestLive_SchemaHoldsTheModel(t *testing.T) {
	db := migrated(t)
	ctx := t.Context()
	lookup := statement(t, "lookup_entry").Scan(func(r query.Row) (string, error) {
		var version, entry string
		err := r.Scan(&version, &entry)
		return version + " " + entry, err
	})
	items := statement(t, "list_items").Scan(func(r query.Row) (string, error) {
		var seq int
		var item string
		err := r.Scan(&seq, &item)
		return item, err
	})
	page := func(token string) query.Args { return key.With("token", token).With("after", -1).With("size", 10) }

	exec(t, db, `INSERT INTO docproc_entry (sha256, stage_name, stage_version, stage_config, token, expires_at, dispatch_version)
		VALUES ($1, $2, $3, $4, 'old', now() + interval '1 minute', '1')`, keyCols...)
	exec(t, db, `INSERT INTO docproc_item (sha256, stage_name, stage_version, stage_config, token, seq, item)
		VALUES ($1, $2, $3, $4, 'old', 0, '{"warning":{"tool":"t","where":"w","what":"stale"}}')`, keyCols...)
	if _, err := lookup.One(ctx, db, key); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("lookup of a claimed key: %v, want no rows", err)
	}
	if got, err := items.All(ctx, db, page("old")); err != nil || len(got) != 0 {
		t.Errorf("items of a claimed key = %v, %v, want none", got, err)
	}

	exec(t, db, `UPDATE docproc_entry SET token = 'new' WHERE sha256 = $1 AND stage_name = $2 AND stage_version = $3 AND stage_config = $4`, keyCols...)
	for _, seq := range []int{1, 0} {
		exec(t, db, `INSERT INTO docproc_item (sha256, stage_name, stage_version, stage_config, token, seq, item)
			VALUES ($1, $2, $3, $4, 'new', $5, jsonb_build_object('after', $5::int))`, append(keyCols, seq)...)
	}
	if _, err := db.ExecContext(ctx, `UPDATE docproc_entry SET entry = '{"outcome":"prepared"}' WHERE sha256 = $1`, key["sha256"]); err == nil {
		t.Error("a row held an entry and a lease at once")
	}
	exec(t, db, `UPDATE docproc_entry SET entry = '{"outcome":"prepared","stages":[]}', expires_at = NULL WHERE sha256 = $1`, key["sha256"])
	if got, err := lookup.One(ctx, db, key); err != nil || got != `1 {"stages": [], "outcome": "prepared"}` {
		t.Errorf("lookup = %q, %v", got, err)
	}
	if got, err := items.All(ctx, db, page("new")); err != nil || strings.Join(got, ",") != `{"after": 0},{"after": 1}` {
		t.Errorf("items = %v, %v, want the new token's two in order", got, err)
	}
	if got, err := items.All(ctx, db, page("old")); err != nil || len(got) != 0 {
		t.Errorf("items under the old token = %v, %v, want none", got, err)
	}

	other := append([]any{strings.Repeat("cd", 32)}, keyCols[1:]...)
	if _, err := db.ExecContext(ctx, `INSERT INTO docproc_item (sha256, stage_name, stage_version, stage_config, token, seq, item)
		VALUES ($1, $2, $3, $4, 't', 0, '{}')`, other...); err == nil {
		t.Error("an item was appended without its entry")
	}
	exec(t, db, `DELETE FROM docproc_entry WHERE sha256 = $1`, key["sha256"])
	if n := scalar[int](t, db, `SELECT count(*) FROM docproc_item`); n != 0 {
		t.Errorf("after the entry's delete, %d items remain", n)
	}
}
