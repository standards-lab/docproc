# goal · docproc-coverage

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] text-encoding-keys
2. [ ] archives-and-mpeg
3. [ ] pdf-text-padding
4. [ ] office-soffice
5. [ ] ooxml-unread
6. [ ] ooxml-arm64-floor
7. [ ] tnef
8. [ ] xps
9. [ ] web-content

## Decisions

- archives-and-mpeg: bzip2 streams through compress/bzip2, beside gzip, not 7zz.
- archives-and-mpeg: split RAR volumes stay unread, an unread part with a reason; docproc-backlog.
- web-content: HTML and EPUB through pandoc, sandboxed, to gfm without raw HTML; rejected a Go
  converter (third-party dependencies in a base module) and a Rust tool on htmd (a release
  pipeline).
- xps: XPS converts to PDF through mutool, then the document stage; rejected libgxps (no release
  since 2021) and GhostXPS (Windows binaries only).
- text-encoding-keys: docs-corpus's tools/coverage, the one reader of the combined value, decodes
  outputs by it and is corpus.docs's to fix (outputs are always UTF-8).
- text-encoding-keys: held MimeKit at 4.18.2: 5.0.0 declined, PstBuilder.Eml 1.3.0 is built on
  MimeKit 4.
- text-encoding-keys: held MsgKit at 3.0.6: 3.1.0 declined, it needs MimeKit.Core 5.
- text-encoding-keys: held typed-path at 0.12.4: 0.13.0 declined, onenote_parser 2.1.0 requires
  ^0.12.3.

## Pending edits

- coordinator · context/data-intelligence.md, "Carried notes": HTML and EPUB are planned in
  docproc-coverage; MHTML and WARC stay unplanned.
- docs-corpus · once text-encoding-keys ships, tell the corpus.docs session the released tags and
  the run-directory schema the release writes ("2").
