# goal · docproc-coverage

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] text-encoding-keys
2. [x] archives-and-mpeg
3. [ ] pdf-text-padding
4. [ ] office-soffice
5. [ ] ooxml-unread
6. [ ] ooxml-arm64-floor
7. [ ] tnef
8. [ ] xps
9. [ ] web-content

## Decisions

- goal: XPS (mutool) and web-content (pandoc) moved to docproc-backlog; the
  text-as-the-last-resort task replaces web-content, right after archives-and-mpeg. Under it,
  content with no signature, message shape, text extension, or byte order mark whose head passes
  route's UTF-8 text check routes Text, the HTML-is-not-text rule goes, and a `#!` script routes
  Text. Whether the text-extension list can go entirely (a Windows-1252 sniff; precedence over
  mbox and header-sniffed messages) is that task's first planning question.
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
- docs-corpus · tell corpus.docs the new kind values (RAR4, RAR5, xz, bzip2, MPEG program
  stream, Ogg video), that `.ogv` records change kind from `ogg`, and the released tags.
- coordinator · context/data-intelligence.md, "Carried notes": supersedes the line above; HTML
  and EPUB are read as raw text under text-as-the-last-resort; MHTML and WARC stay unplanned.
