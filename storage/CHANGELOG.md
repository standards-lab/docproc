# Changelog

All notable changes to `github.com/standards-lab/docproc/storage`, docproc's content store over
go-storage, are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `storage/vX.Y.Z` tag carries, and is the release's notes. Its exported API is its contract: a
change to it is a change here. This changelog covers the storage module only; the base module and
each system tool under `tools/` keep their own.

## [Unreleased]

## [v0.1.0] - 2026-11-17

The first release. It requires `github.com/standards-lab/docproc` v0.1.0, whose
`dispatch.Content` it implements and whose `dispatchtest.Content` suite it passes, and
`github.com/standards-lab/go-storage` v0.8.0; its live tier runs over go-storage's `s3` v0.3.0.

### Added

- `Content`, a `dispatch.Content` over a go-storage `Client`, made by `New(Config)` from the
  `Client`, the host's `Prefix`, and the `Renew` age; a nil `Client` panics naming the field, and
  a negative renew age is refused, matching `dispatch.ErrRequest`. A content's key is the prefix,
  its SHA-256's first two hex digits, a slash, and the whole SHA-256, a stable layout. A put
  stats the key and writes nothing when the content is present and no older than the renew age
  by its `ModifiedAt`, zero never rewriting; otherwise it puts all or nothing.
- The errors a dispatcher reads: go-storage's `ErrTooLarge` matches `dispatch.ErrTooLarge`, an
  open's `ErrNotFound` matches `fs.ErrNotExist`, a missing container never does, and a name that
  isn't a SHA-256 matches `dispatch.ErrRequest`.
- No sweeper: the package doc names the host's object-store lifecycle rule on age, at least the
  index entries' retention plus the renew age plus the job stream's retention.
- The tiers: the unit tier runs `dispatchtest.Content` over a go-storage `Store` wrapping
  storagetest's `Fake`, with the renew age on the Fake's clock, a `Store`'s `MaxObjectSize`, and
  a missing container; the live tier, under `-tags integration`, runs it over go-storage's S3
  provider against the compose harness's SeaweedFS.

[Unreleased]: https://github.com/standards-lab/docproc/compare/storage/v0.1.0...HEAD
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/storage/v0.1.0
