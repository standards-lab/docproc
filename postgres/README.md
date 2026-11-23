# docproc/postgres

`github.com/standards-lab/docproc/postgres` is docproc's index in PostgreSQL: an `Index`, the
`dispatch.Index` that keeps each key's entry, claim, and log in the database a sqlate session
names, so several hosts dispatching into one store share one index and prepare each content once.
It is a nested module of [docproc](../README.md), released on its own `postgres/vX.Y.Z` tags, and
requires the base module, sqlate, sqlate's `postgres` dialect, and pgx.

## Install

```sh
go get github.com/standards-lab/docproc/postgres@v0.1.1
```

[`CHANGELOG.md`](CHANGELOG.md) states what each release changes. The package's documentation,
`go doc github.com/standards-lab/docproc/postgres`, is the reference for its exported names, and
its doc comment lists them: `Config`, `New`, and `Index` with its `Close`; `Migrations`, `Source`,
and `Table`; `Retention` and `NewRetention`; and `Verify` and `Statements`.

## Configuration

The pool is the host's, opened on pgx's `database/sql` driver, `github.com/jackc/pgx/v5/stdlib`,
and wrapped as a `*sqlate.DB`. Error handling is elided:

```go
set, err := postgres.Migrations() // declare it in the host's migrator, beside its own sets, and apply it
err = postgres.Verify(ctx, db)    // from the verify stage, after the migrations

index, err := postgres.New(ctx, postgres.Config{
	DB:    db,              // a *sqlate.DB over a pool on pgx's stdlib driver
	Lease: 2 * time.Minute, // every claim's lease, renewed by the dispatcher at a third of it
	Flush: 256,             // log items a claim buffers per multi-row insert, and Items' page
})
defer index.Close()
```

Every `Config` field is required and none has a default. A nil `DB` panics naming the field;
`New` refuses a `Lease` or `Flush` that isn't positive, and a pool on any other driver, naming
the one it found, each matching `dispatch.ErrRequest`. It reads the driver from a connection it
takes from the pool, so a pool it can't reach fails too.

## The contracts

- **Migrations the host applies.** `Migrations` is the `docproc` migration set, recorded in its
  own history table, `docproc_schema_version`: `docproc_entry`, a row per key; `docproc_item`, the
  entries' logs; and `docproc_ix_entry_created`, the index a retention reads. Every object it
  creates is prefixed `docproc_`. The index never migrates; a host declares the set and applies
  it, and `Verify` prepares every statement against the live schema, so a schema without the set,
  or behind it, fails at start. A released migration never changes; a golden-hash test pins each.
- **The pgx stdlib driver.** `Wait` listens through pgx's own connection, reached from the pool's
  `database/sql` connection, so the pool must be on `github.com/jackc/pgx/v5/stdlib`; `New`
  refuses one that isn't.
- **One listener connection per `Index`.** The first `Wait` takes a connection of its own from the
  pool, LISTENs on the channel `docproc_index`, and holds it until `Index.Close`. A host sizes
  its pool to allow one per `Index`, beside the connections the other methods take, and closes
  the `Index` when it is done with it; a `Wait` after `Close` fails, and the other methods are
  unaffected.
- **Fencing on the database.** A claim, a renewal, a flush of the log, a record, and a release are
  each one statement conditional on the entry's current token, on the database's clock, so a
  taken-over claimant's calls fail matching `dispatch.ErrClaimLost`. A record commits the log
  whole with its entry, or nothing does. A row written under another `dispatch.Version` is
  refused rather than read.
- **Retention by the host's sweeper.** The index keeps every entry until it is dropped.
  `NewRetention(db, age)` returns a `Retention` whose `Pass(ctx, budget)` drops at most `budget`
  entries created more than `age` ago on the database's clock, oldest first, with their logs, and
  notifies each dropped key's waiters; it reports `more` exactly when it dropped its whole budget.
  An entry's age runs from its first claim, which neither a takeover nor a record refreshes; a
  claim under a live lease, or one that never expires, is never dropped. The host drives `Pass`
  from its sweeper, and the age is its call: a dropped key's content is prepared again on its
  next dispatch, and the module has no default.

```go
retention, err := postgres.NewRetention(db, 30*24*time.Hour)
more, err := retention.Pass(ctx, 500) // pass again while more
```

## Testing

Tasks run from the repository root, `mise run <task>`:

- `check`: the unit tier, with every other module, then `sqlint` over the module's SQL. It holds
  the migrations' golden hashes, the statement list, and the constructors' refusals; it reads no
  network.
- `integration:postgres`: the live tier, under the `integration` build tag. It starts the compose
  harness's PostgreSQL alone, runs dispatchtest's `Index` suite whole, `Wait` across two
  connections, and the retention, each test on a database of its own made through
  `sqlate/postgres/pgtest`, and stops the service whether the suite passes or fails, printing its
  log on a failure. `integration` runs it before the storage module's, and `test:integration`
  after the base module's integration tier.

PostgreSQL listens on port 5436; `DOCPROC_POSTGRES_PORT` moves it, and `DOCPROC_DSN` in the root
`mise.toml`, the admin DSN the tier reads, follows. A host's own tests make a migrated database
the same way, passing `Migrations` to `pgtest.Migrated`.

## Limits

- A token is honored only by the `Index` that issued it: a claim's appends are buffered in that
  `Index`'s memory, so an `Append` or `Record` through another fails matching `ErrClaimLost`.
- An `Index` holds one pool connection while any `Wait` has run, until `Close`.
- `Items` fails when its entry is replaced or dropped while it reads.
- Notifications share one channel, so every `Index` listening hears every key's, and fans out the
  ones its waits are on.

Apache-2.0.
