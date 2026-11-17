//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/standards-lab/sqlate/postgres/pgtest"
)

// dsnEnv names the PostgreSQL the live tier creates its databases on, which the root mise.toml
// sets for the compose harness's server.
const dsnEnv = "DOCPROC_DSN"

// TestLive_Ping proves the harness: pgtest creates a database of the test's own on the server
// DOCPROC_DSN names, and the pool over it answers a ping. pgtest drops the database when the
// test ends.
func TestLive_Ping(t *testing.T) {
	db := pgtest.Create(t, dsnEnv, "docproc")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := db.Pool.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
