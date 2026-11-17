package postgres_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/postgres"
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

// Statements lists the compiled statements in name order; none requires a transaction, since
// each reads on its own.
func TestStatementsList(t *testing.T) {
	var names []string
	for _, st := range postgres.Statements() {
		names = append(names, st.Name())
		if st.TransactionRequired() {
			t.Errorf("%s requires a transaction", st.Name())
		}
	}
	if want := []string{"list_items", "lookup_entry"}; !slices.Equal(names, want) {
		t.Fatalf("statements %v, want %v", names, want)
	}
}
