package postgres_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/postgres"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
)

// The set is named for the module and recorded in its own history table, apart from a host's.
func TestMigrationsName(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if set.Name != "docproc" || set.Table != "docproc_schema_version" || len(set.Migrations) != 1 {
		t.Fatalf("set = %s/%s with %d migrations", set.Name, set.Table, len(set.Migrations))
	}
}

// Every object the set creates carries the docproc_ prefix, so it sits beside a host's own
// schema without a clash.
func TestMigrationsPrefixTheirObjects(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range set.Migrations {
		for line := range strings.SplitSeq(m.Up, "\n") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if (f == "TABLE" || f == "INDEX" || f == "CONSTRAINT") && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "docproc_") {
					t.Errorf("%d %s: %q names an object without the docproc_ prefix", m.Version, m.Name, strings.TrimSpace(line))
				}
			}
		}
	}
}

// Statements lists the compiled statements in name order; only discard_items, a takeover's
// second statement, requires a transaction, the claim's.
func TestStatementsList(t *testing.T) {
	var names, tx []string
	for _, st := range postgres.Statements() {
		names = append(names, st.Name())
		if st.TransactionRequired() {
			tx = append(tx, st.Name())
		}
	}
	want := []string{
		"append_items", "claim_entry", "discard_items", "drop_entry", "list_items", "lookup_claim",
		"lookup_entry", "lookup_log", "record_entry", "release_claim", "renew_claim",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("statements %v, want %v", names, want)
	}
	if !slices.Equal(tx, []string{"discard_items"}) {
		t.Errorf("statements requiring a transaction %v, want discard_items alone", tx)
	}
}

// fakeDriver is a database/sql driver other than pgx's, whose connections do nothing.
type fakeDriver struct{}

// fakeConn is fakeDriver's connection.
type fakeConn struct{}

func (fakeDriver) Open(string) (driver.Conn, error)  { return fakeConn{}, nil }
func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no statements") }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("fake: no transactions") }

func init() { sql.Register("docproc-postgres-fake", fakeDriver{}) }

// fake returns a session over a pool on fakeDriver.
func fake(t *testing.T) *sqlate.DB {
	t.Helper()
	pool, err := sql.Open("docproc-postgres-fake", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return sqlate.Wrap(pool, pgdialect.Dialect{})
}

// New refuses a pool on any driver but pgx's, naming the driver it found.
func TestNewRefusesAnotherDriver(t *testing.T) {
	_, err := postgres.New(t.Context(), postgres.Config{DB: fake(t), Lease: time.Second, Flush: 1})
	if !errors.Is(err, dispatch.ErrRequest) || !strings.Contains(err.Error(), "postgres_test.fakeConn") {
		t.Fatalf("New over another driver: %v, want ErrRequest naming postgres_test.fakeConn", err)
	}
}

// New panics on a nil DB, naming the field, and refuses a lease or a flush that isn't positive.
func TestNewRefusesItsConfig(t *testing.T) {
	func() {
		defer func() {
			if p := recover(); p != "postgres: Config.DB is nil" {
				t.Errorf("New with a nil DB: panic %v", p)
			}
		}()
		_, _ = postgres.New(t.Context(), postgres.Config{Lease: time.Second, Flush: 1})
	}()
	for _, cfg := range []postgres.Config{
		{Lease: 0, Flush: 1}, {Lease: -time.Second, Flush: 1}, {Lease: time.Second, Flush: 0}, {Lease: time.Second, Flush: -1},
	} {
		cfg.DB = fake(t)
		if _, err := postgres.New(t.Context(), cfg); !errors.Is(err, dispatch.ErrRequest) {
			t.Errorf("New with lease %s and flush %d: %v, want ErrRequest", cfg.Lease, cfg.Flush, err)
		}
	}
}
