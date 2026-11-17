# Changelog

All notable changes to `github.com/standards-lab/docproc/postgres`, docproc's index over sqlate on
PostgreSQL, are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `postgres/vX.Y.Z` tag carries, and is the release's notes. Its exported API is its contract: a
change to it is a change here. This changelog covers the postgres module only; the base module and
each system tool under `tools/` keep their own.

## [Unreleased]

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
  whole with the entry and resets the entry's `created_at`, from which a retention counts its age.
  A row of another `dispatch.Version` is refused. `Wait` is not implemented yet, failing matching
  `errors.ErrUnsupported`.
