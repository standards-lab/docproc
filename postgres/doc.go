// Package postgres is docproc's index over sqlate on PostgreSQL: a [dispatch.Index] that keeps
// each key's entry, claim, and log in the database a sqlate session names, so several hosts
// dispatching into one store share one index.
//
// The package exports:
//
//   - [Index], the dispatch.Index, made by [New] from a [Config]: the sqlate session, whose pool
//     must be on pgx's database/sql driver, every claim's lease, and the log's flush size
//   - [Migrations], the index's tables and the index a retention reads, as a migration set,
//     named [Source] and recorded in the history table [Table]
//   - [Verify], which prepares the statements against a live schema, and [Statements], which
//     lists them
//
// Index's Wait is not yet implemented: it fails matching errors.ErrUnsupported.
//
// # Schema
//
// The migration set's 0001 creates two tables. docproc_entry holds a row per key, keyed by the
// content's SHA-256 and the stage identity's name, version, and config. Its token is the claim's
// while the key is claimed and, once recorded, the token whose log the entry holds; expires_at is
// the lease's expiry, NULL for a claim that never expires and for a recorded entry; created_at is
// the claim's time and, once recorded, the record's, from which a retention counts the entry's
// age; dispatch_version is the dispatch.Version whose shapes the row's JSON carries;
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
// # Claims and the log
//
// Each of a claim, a renewal, a flush of the log, a record, and a release is one statement
// conditional on the entry's current token, so the database, not the claimant, decides whether
// a token still holds its claim; an old token's renewal, flush, and record fail matching
// dispatch.ErrClaimLost. A claim inserts the key's row, or takes over a claimed row whose lease
// ran out, replacing its token, restarting its lease, and resetting created_at, and deletes the
// old token's log in the same transaction. An Index buffers a claim's appends in memory, Flush at
// a time, and writes each full buffer in one multi-row insert on the token; Record writes the
// remainder with the entry in one statement, so the log commits whole with its entry or not at
// all. Items reads the log in pages of Flush items, by position, under the token the entry
// holds, and fails when the entry is replaced or dropped while it reads. A row written under
// another dispatch.Version is refused rather than read.
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
