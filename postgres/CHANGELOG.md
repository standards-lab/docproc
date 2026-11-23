# Changelog

All notable changes to `github.com/standards-lab/docproc/postgres`, docproc's index over sqlate on
PostgreSQL, are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `postgres/vX.Y.Z` tag carries, and is the release's notes. Its exported API is its contract: a
change to it is a change here. This changelog covers the postgres module only; the base module and
each system tool under `tools/` keep their own.

## [Unreleased]

## [v0.1.2] - 2026-11-23

This release requires the docproc base module at v0.4.2. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.4.2, from v0.4.1, and passes its
  `dispatchtest.Index` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes,
  none of which reach this module.

## [v0.1.1] - 2026-11-23

This release requires the docproc base module at v0.4.1. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.4.1, from v0.1.0, and passes its
  `dispatchtest.Index` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.

## [v0.1.0] - 2026-11-17

The first release. It requires `github.com/standards-lab/docproc` v0.1.0, whose
`dispatch.Index` it implements and whose `dispatchtest.Index` suite it passes;
`github.com/standards-lab/sqlate` v0.7.0 and `sqlate/postgres` v0.8.0, whose `pgtest` its live
tier runs on; and pgx v5.13.0, whose `database/sql` driver the host's pool must be on.

### Added

- `Migrations`, the index's schema as a sqlate migration set named `Source`, `docproc`, recorded
  in `Table`, `docproc_schema_version`: `docproc_entry`, a row per key with its claim's token,
  lease expiry, creation time, `dispatch.Version`, and `Entry` as jsonb; `docproc_item`, the
  entries' logs, a row per `Recorded` item keyed by its entry, token, and position; and
  `docproc_ix_entry_created`, the retention's index. A host applies the set.
- `Verify`, which prepares every statement against a live schema, and `Statements`, which lists
  them.
- `Index`, the `dispatch.Index` over PostgreSQL, made by `New` from a `Config`: `DB`, the sqlate
  session, whose pool must be on pgx's `database/sql` driver; `Lease`, every claim's lease; and
  `Flush`, the log items a claim buffers before one multi-row insert, and the page `Items` reads.
  None has a default. Claims, renewals, flushes, records, and releases are each one statement
  conditional on the entry's token, on the database's clock; a takeover deletes the old token's
  log, and the old token's calls fail matching `dispatch.ErrClaimLost`. `Record` commits the log
  whole with the entry. Neither `Record` nor a takeover changes the entry's `created_at`, its
  first claim's time, from which a retention counts its age. A row of another `dispatch.Version` is refused. `Wait` waits on LISTEN/NOTIFY: `Record`,
  `Release`, and `Drop` notify the channel `docproc_index` with the key's payload, the lowercase hex
  SHA-256 of its SHA-256, name, version, and config joined by NUL bytes, in their own statement, and
  a waiter wakes at the notification or at the claim's lease expiry, so a claimant that died is
  waited out.
- `Index.Close`, which stops the listener `Wait` starts on its first call: one connection held
  from the host's pool per `Index` until `Close`, which a host's pool must allow. A `Wait` after
  `Close` fails; the other methods are unaffected.
- `Retention`, made by `NewRetention(db, age)`, whose `Pass(ctx, budget)` drops at most `budget`
  entries created more than `age` ago on the database's clock, oldest first, with their logs, in
  one statement that notifies each dropped key's waiters, and reports `more` exactly when it
  dropped its whole budget. A claim under a live lease, or one that never expires, is never
  dropped; a claim whose lease ran out is, once old enough. Rows another transaction holds are
  skipped. The host drives it from a sweeper; the age has no default. A non-positive age and a
  budget below 1 are refused, matching `dispatch.ErrRequest`, and a nil `db` panics.

[Unreleased]: https://github.com/standards-lab/docproc/compare/postgres/v0.1.2...HEAD
[v0.1.2]: https://github.com/standards-lab/docproc/compare/postgres/v0.1.1...postgres/v0.1.2
[v0.1.1]: https://github.com/standards-lab/docproc/compare/postgres/v0.1.0...postgres/v0.1.1
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/postgres/v0.1.0
