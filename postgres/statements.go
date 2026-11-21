package postgres

import (
	"context"
	"embed"

	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"
)

//go:embed statements/*.sql
var statementFiles embed.FS

var statements = query.MustCatalog(query.Patterns()).MustCompile(statementFiles, "statements", pgdialect.Dialect{})

// Statements returns the compiled statements in name order, for a host that lists the SQL its
// program runs.
func Statements() []query.Statement { return statements.Statements() }

// Verify prepares every statement against the schema sess reaches, so a schema without the
// docproc set, or a set behind these statements, fails at start rather than at the first lookup
// or claim. A host calls it from its verify stage, after its migrations.
func Verify(ctx context.Context, sess sqlate.Session) error { return statements.Verify(ctx, sess) }
