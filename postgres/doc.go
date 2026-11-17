// Package postgres is docproc's index over sqlate on PostgreSQL: a [dispatch.Index] that keeps
// each key's entry, claim, and log in the database a sqlate session names, so several hosts
// dispatching into one store share one index.
//
// The package exports:
//
//   - [Migrations], the index's tables and the index a retention reads, as a migration set,
//     named [Source] and recorded in the history table [Table]
//   - [Verify], which prepares the statements against a live schema, and [Statements], which
//     lists them
//
// The index itself is not yet exported.
//
// # Schema
//
// The migration set's 0001 creates two tables. docproc_entry holds a row per key, keyed by the
// content's SHA-256 and the stage identity's name, version, and config. Its token is the claim's
// while the key is claimed and, once recorded, the token whose log the entry holds; expires_at is
// the lease's expiry, NULL for a claim that never expires and for a recorded entry; created_at is
// the claim's time; dispatch_version is the dispatch.Version whose shapes the row's JSON carries;
// and entry is the dispatch.Entry as jsonb, NULL until recorded, so a row with an entry is
// recorded and one without is claimed. docproc_item holds the log, a row per dispatch.Recorded as
// jsonb, keyed by its entry, the token that appended it, and its position, seq, so a takeover's
// old items stay apart from the new claimant's until they are deleted, and deleting an entry
// deletes its items. docproc_ix_entry_created orders the entries by created_at for a retention,
// which drops the oldest first. Leases and ages are on the database's clock, now(). Every object
// the set creates is prefixed docproc_, and a host declares the set in its migrator and applies
// it; the index never migrates. A released migration never changes; a golden-hash test pins each
// one.
//
// A waiter learns that a claim ended through LISTEN/NOTIFY on one channel, docproc_index, whose
// payload names the key as the lowercase hex SHA-256 of its SHA-256, name, version, and config
// joined by NUL bytes: one channel keeps clear of PostgreSQL's 63-byte identifier limit, which a
// channel per key would pass, and the digest keeps the payload short whatever the identity holds.
//
// # Statements
//
// The statements are authored files compiled through sqlate's query package. [Verify], which
// prepares them all, is for a host's verify stage, after its migrations.
//
// A test that needs a throwaway database with the docproc set applied makes one with
// github.com/standards-lab/sqlate/postgres/pgtest, passing [Migrations] to its Migrated, as this
// module's integration suite does.
package postgres
