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
//   - [Retention], made by [NewRetention] from the session and an age, which drops the entries
//     older than the age, with their logs, a budget per [Retention.Pass]; the host drives it from
//     a sweeper, and the age has no default
//   - [Verify], which prepares the statements against a live schema, and [Statements], which
//     lists them
//
// An Index holds one connection of its own from the host's pool while any Wait has run, its
// listener, until [Index.Close]; a host sizes its pool to allow it, one per Index, beside the
// connections the other methods take, and closes the Index when it is done with it.
//
// # Schema
//
// The migration set's 0001 creates two tables. docproc_entry holds a row per key, keyed by the
// content's SHA-256 and the stage identity's name, version, and config. Its token is the claim's
// while the key is claimed and, once recorded, the token whose log the entry holds; expires_at is
// the lease's expiry, NULL for a claim that never expires and for a recorded entry; created_at is
// the first claim's time, which neither a takeover nor a record changes, from which a retention
// counts the entry's age; dispatch_version is the dispatch.Version whose shapes the row's JSON carries;
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
// # Waiting
//
// Record, Release, and Drop each notify the key's payload in their own statement, through
// pg_notify in its RETURNING, so a notification is sent when, and only when, the statement
// changed the key's row and committed. Wait LISTENs on a connection the Index takes from the
// pool on the first Wait, reached through pgx's database/sql driver, and holds until Close; the
// listener fans each notification out to the waits on its payload. A wait registers with the
// listener, and the listener's LISTEN is in effect, before it reads the key's row, so a claim
// that ends between the two is either read or notified. It wakes at the notification or at the
// lease's expiry, which it times from the row's expires_at against the database's clock, so a
// claimant that died is waited out; on waking it reads the row again, returning the entry when
// one is recorded, none when the key is free or its lease ran out, and waiting on otherwise. A
// listener whose connection fails wakes its waits, which read the row again on a new listener,
// so a notification lost with the connection costs no wait its wakeup. The listener UNLISTENs
// before its connection goes back to the pool, or has the pool discard it.
//
// # Claims and the log
//
// Each of a claim, a renewal, a flush of the log, a record, and a release is one statement
// conditional on the entry's current token, so the database, not the claimant, decides whether
// a token still holds its claim; an old token's renewal, flush, and record fail matching
// dispatch.ErrClaimLost. A claim inserts the key's row, or takes over a claimed row whose lease
// ran out, replacing its token, restarting its lease, and keeping created_at, and deletes the
// old token's log in the same transaction. An Index buffers a claim's appends in memory, Flush at
// a time, and writes each full buffer in one multi-row insert on the token; Record writes the
// remainder with the entry in one statement, so the log commits whole with its entry or not at
// all. Items reads the log in pages of Flush items, by position, under the token the entry
// holds, and fails when the entry is replaced or dropped while it reads. A row written under
// another dispatch.Version is refused rather than read.
//
// # Retention
//
// The index keeps every entry until it is dropped, so a host bounds it with a [Retention], whose
// age is how long a result is worth keeping: a dropped key's content is prepared again on its
// next dispatch. Each pass is one statement that deletes at most its budget of entries created
// more than the age ago, oldest first by docproc_ix_entry_created, their logs going with them,
// and notifies each key's waiters as Drop does. An entry's age runs from its first claim, which
// neither a takeover nor a record refreshes; a recorded entry, and a claim whose lease ran out,
// are eligible once old enough, and a claim under a live lease, or one that never expires, is
// never dropped. The candidates are locked FOR UPDATE SKIP LOCKED, so a pass skips a
// row a claim is taking over, and passes on several hosts skip each other's. A pass reports more
// exactly when it dropped its whole budget, so a sweeper passes again until one reports none.
//
// # Statements
//
// The statements are authored files compiled through sqlate's query package. [Verify], which
// prepares them all, is for a host's verify stage, after its migrations. The listener's LISTEN
// and UNLISTEN take no parameters and can't be prepared, so they run on its pgx connection
// outside the catalog, and Verify doesn't check them.
//
// A test that needs a throwaway database with the docproc set applied makes one with
// github.com/standards-lab/sqlate/postgres/pgtest, passing [Migrations] to its Migrated, as this
// module's integration suite does.
package postgres
