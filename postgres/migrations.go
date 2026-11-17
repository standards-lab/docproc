package postgres

import (
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate/migrate"
)

const (
	// Source is the migration set's name and the prefix of every object the set owns.
	Source = "docproc"

	// Table is the history table the set's migrations are recorded in, apart from a host's own.
	Table = "docproc_schema_version"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the index's migration set for PostgreSQL: the entries' table, their logs'
// table, and the index a retention reads. A host declares it in its migrator and applies it; the
// index never migrates on its own.
func Migrations() (migrate.Set, error) {
	files, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		return migrate.Set{}, fmt.Errorf("docproc/postgres: %w", err)
	}
	return migrate.Set{Name: Source, Table: Table, Migrations: files}, nil
}
