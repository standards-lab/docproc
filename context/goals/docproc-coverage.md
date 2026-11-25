# goal · docproc-coverage

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] text-encoding-keys
2. [x] archives-and-mpeg
3. [x] text-last-resort
4. [x] pdf-text-padding
5. [ ] office-soffice
6. [ ] ooxml-unread
7. [ ] ooxml-arm64-floor
8. [ ] tnef

## Decisions

- text-encoding-keys: held MimeKit at 4.18.2: 5.0.0 declined, PstBuilder.Eml 1.3.0 is built on
  MimeKit 4.
- text-encoding-keys: held MsgKit at 3.0.6: 3.1.0 declined, it needs MimeKit.Core 5.
- text-encoding-keys: held typed-path at 0.12.4: 0.13.0 declined, onenote_parser 2.1.0 requires
  ^0.12.3.
- pdf-text-padding: held pdftotext's minimum at poppler 22.02: 26.05 (`-remove-hyphens`)
  declined; Debian 12/13 and Ubuntu 24.04 carry 22.12–25.03, so their users would need Homebrew,
  conda-forge, or a source build, and `none` or `soft` would leave hyphenated words split.
- pdf-text-padding: rejected collapsing gs's space runs in Go. The columns stay interleaved and
  merged words stay merged, and a 2-space column gap can't be told from ordinary spacing.
- pdf-text-padding: rejected `pdftotext -layout`: it still pads (up to 57.8% spaces).
- pdf-text-padding: rejected `pdftotext -raw`: it merges words (55% kept on one page) and breaks
  right-to-left order.
- pdf-text-padding: rejected MuPDF `mutool`: it fragments Type 3 fonts' words (64% kept) and adds
  an AGPL system tool.
- pdf-text-padding: rejected Apache PDFBox: it needs a new JVM toolchain, costs 0.48 s per process,
  and reverses right-to-left lines.
- pdf-text-padding: rejected xpdf 4.06 (0 words on Type 3 fonts; only the vendor's binary
  provides it), and PdfPig and pdfminer.six (Arabic lost).

## Pending edits

- docs-corpus · once docproc-coverage syncs, tell corpus.docs (context/goals/corpus.docs.md, task
  docproc-final; standards-lab #219) the final release's tags, docproc, storage, postgres, and
  cmd/docproc, and the run-directory schema it writes; that `#!` scripts and other unlisted text
  route text; that a PDF page's text is pdftotext's reading order, with no column padding; and
  that exec-4's check:polyglot can reopen. Intermediate releases go untold.
- coordinator · roadmap/docproc-backlog.toml: a text retry for a part whose stage fails at
  runtime, such as a corrupt PDF.
- coordinator · roadmap/docproc-backlog.toml: control over line-end hyphens in PDF page text
  (pdftotext `-remove-hyphens`, poppler 26.05), once Debian and Ubuntu stable carry 26.05.
