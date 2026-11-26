# Changelog

All notable changes to `github.com/standards-lab/docproc/storage`, docproc's content store over
go-storage, are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `storage/vX.Y.Z` tag carries, and is the release's notes. Its exported API is its contract: a
change to it is a change here. This changelog covers the storage module only; the base module and
each system tool under `tools/` keep their own.

## [Unreleased]

## [v0.1.7] - 2026-11-25

This release requires the docproc base module at v0.9.0. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.9.0, from v0.8.0, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.
  This module stores an output's bytes and reads none of their text, so what the office stage now
  prepares reaches a caller through the base module, not through this module: the text of an ODF
  text, spreadsheet, presentation, or drawing package or template, a WordPerfect document, or an
  Excel binary workbook, which LibreOffice converts, where each had no processor yet; and, over
  docproc-ooxml 0.2.0 and docproc-mail 0.2.0, a document's text boxes, a pptx's comments, an
  xlsx's notes and threaded comments, and a TNEF part's attachments and bodies.

## [v0.1.6] - 2026-11-25

This release requires the docproc base module at v0.8.0. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.8.0, from v0.7.0, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.
  This module stores an output's bytes and reads none of their text, so a PDF page's text read by
  pdftotext in its reading order, without gs's column padding, reaches a caller through the base
  module, not through this module.

## [v0.1.5] - 2026-11-25

This release requires the docproc base module at v0.7.0. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.7.0, from v0.6.0, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.
  This module stores an output's bytes and reads none of a part's kind or route, so text as the
  last resort, the text-extension list's removal, the stricter mbox claim, and the changed route
  reasons reach a caller through the base module, not through this module.

## [v0.1.4] - 2026-11-25

This release requires the docproc base module at v0.6.0. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.6.0, from v0.5.0, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.
  This module stores an output's bytes and reads none of a part's kind or route, so the new RAR,
  xz, bzip2, MPEG program stream, and Ogg video kinds, an Ogg video's move from audio to video,
  and 7-Zip's 26.06 minimum reach a caller through the base module, not through this module.

## [v0.1.3] - 2026-11-25

This release requires the docproc base module at v0.5.0. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.5.0, from v0.4.2, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.
  This module stores a text output's bytes and reads none of its Meta, so the split of the text
  output's `encoding` into `encoding` and `encoding_by` reaches a caller through the base module,
  not through this module.

## [v0.1.2] - 2026-11-23

This release requires the docproc base module at v0.4.2. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.4.2, from v0.4.1, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes,
  none of which reach this module.

## [v0.1.1] - 2026-11-23

This release requires the docproc base module at v0.4.1. Its exported API is unchanged.

### Changed

- The module requires the docproc base module at v0.4.1, from v0.1.0, and passes its
  `dispatchtest.Content` suite; its [CHANGELOG](../CHANGELOG.md) states the base module's changes.

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

[Unreleased]: https://github.com/standards-lab/docproc/compare/storage/v0.1.7...HEAD
[v0.1.7]: https://github.com/standards-lab/docproc/compare/storage/v0.1.6...storage/v0.1.7
[v0.1.6]: https://github.com/standards-lab/docproc/compare/storage/v0.1.5...storage/v0.1.6
[v0.1.5]: https://github.com/standards-lab/docproc/compare/storage/v0.1.4...storage/v0.1.5
[v0.1.4]: https://github.com/standards-lab/docproc/compare/storage/v0.1.3...storage/v0.1.4
[v0.1.3]: https://github.com/standards-lab/docproc/compare/storage/v0.1.2...storage/v0.1.3
[v0.1.2]: https://github.com/standards-lab/docproc/compare/storage/v0.1.1...storage/v0.1.2
[v0.1.1]: https://github.com/standards-lab/docproc/compare/storage/v0.1.0...storage/v0.1.1
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/storage/v0.1.0
