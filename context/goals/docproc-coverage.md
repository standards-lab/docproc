# goal · docproc-coverage

- **State:** building
- **Task:** pdf-text-padding
- **Branch:** pdf-text-padding

## Tasks

1. [x] text-encoding-keys
2. [x] archives-and-mpeg
3. [x] text-last-resort
4. [ ] pdf-text-padding
5. [ ] office-soffice
6. [ ] ooxml-unread
7. [ ] ooxml-arm64-floor
8. [ ] tnef

## Task brief · pdf-text-padding

**Problem**

A PDF page's text layer comes from gs's txtwrite device, which pads text out to the page's
physical layout. On multi-column pages the padding swamps the text: ~/docs's olmocr-bench
multi-column pages are 31.7–97.0% spaces, with runs up to 2,626, and cfr-2024-title32-vol1 is
50.9% spaces with runs up to 245. The columns are interleaved line by line, and some fonts lose
the spaces between words. docinfer had to cap chunks by characters to cope. poppler's
`pdftotext`, from poppler-utils, which docproc already requires for pdfdetach, reads the same
pages in reading order with no padding.

**Behaviors**
1. Page text is read by poppler's `pdftotext` in reading-order mode, one run per page:
   - The run is `-enc UTF-8 -f N -l N` on the PDF's absolute path, in the PDF's directory, so it
     works under either runner.
   - The run's text has its trailing form feed dropped.
   - The text holds no run of 2 or more spaces that pdftotext's layout made.
2. A new reader exposes this: `TextReader`, built by `NewTextReader` over Config's `PDFToText`
   tool, which `PDFToTextSpec` declares. `PDFToTextMinimum` is 22.02.
   - A nil tool panics, naming the field.
   - A pdftotext older than 22.02 is refused with a `*tool.VersionError` wrapping `tool.ErrTooOld`.
   - The version is read from `pdftotext -v`, which exits 0.
3. `TextReader.Text(ctx, pdf, page)` refuses bad pages:
   - A page below 1 is refused before any run, wrapping `ErrRequest`.
   - A page past the last (pdftotext exits 99 with `Wrong page range given`) returns an error
     naming the page and the PDF, wrapping `ErrRequest`.
   - Any other nonzero exit returns the tool's `*tool.ExitError`, carrying the first 4 KiB of
     pdftotext's stderr.
4. A page with no text layer, such as a scanned page, gives text that `HasText` reports as none.
   A PDF restricted only by its permissions is read in full.
5. `Preparer.Images` reads each page's text through the `TextReader`, one run per page, before
   the page renders.
   - `NewPreparer` requires the reader in Config and panics naming it when nil.
   - A failed text run fails only its page, at step `text`; the stream carries on.
   - Page order, the concurrency bound, a page's own render processes, and the PNG output are
     unchanged.
   - `ImagesRequest.PagesPerProcess` bounds renders alone.
   - No text scratch directory is made.
6. `Ghostscript.Text` and gs's shared txtwrite run are removed. gs only counts pages, surveys
   them, and renders them. The doc comments of `HasText` and `PageImage.Text` name the
   `TextReader`.
7. The document stage requires `pdftotext` as it requires pdfdetach: built on the host, and a
   missing or too-old pdftotext leaves the document route Missing with the tool's reason. It is
   required even when the request reads no text.
   - The stage's identity includes pdftotext's version line.
   - The stage adapters' version is 7, so an index prepares every PDF again.
8. The docs state what is now built:
   - The README's system-tools table gains `pdftotext` (poppler-utils, 22.02, document).
   - The development tool list and `DOCPROC_UNIT_TIMEOUT`'s list of bounded tools gain pdftotext.
   - The `document` package doc and the API page describe the text run. The gs text-run notes
     go: `-sstdout` for text, the `%` doubled, and a missing text file as a content failure.
   - The limits page's document entry says three things. Page text is in pdftotext's reading
     order, so paragraphs of aligned columns may alternate. Line-end hyphens are joined. The
     shared processes render only.
9. A host integration test reads every page of the committed `clean.pdf` fixture through the
   `TextReader`. No page holds a run of 2 or more spaces, and page 2's words (two columns) equal
   its truth's words as a multiset.
10. The acceptance page gains a section, with a recipe, measuring ~/docs's 6 olmocr-bench
    multi-column pages and cfr-2024-title32-vol1 before (gs txtwrite at v0.7.0) and after.
    For each page it records the share of spaces, the longest space run, the words kept, and the
    olmocr-bench order checks passed.
11. The base module's CHANGELOG holds the [v0.8.0] section, with its intro paragraph:
    - Removed: `Ghostscript.Text`, and the text half of `PagesPerProcess`.
    - Added: `TextReader`, `NewTextReader`, `PDFToTextSpec`, `PDFToTextMinimum`, Config's new
      fields, and pdftotext as a required tool.
    - Changed: the shape of page text, adapters' version 7, and `NewPreparer`'s requirement.
    - It states that no wire value or run-directory schema changes.

**Test seams**
- `document.TextReader.Text` and `document.Preparer.Images`, over a fake `tool.Tool` in unit
  tests and the host's pdftotext in the host integration tier.
- `stages.Build`'s rows and stage identity for the document route.

**Slices**
1. **The pdftotext reader.** `PDFToTextSpec`, `PDFToTextMinimum`, Config's `PDFToText` field,
   `NewTextReader` and `TextReader.Text`, with their refusals and form-feed handling, unit tests,
   and the `clean.pdf` host integration test (behaviors 1–4, 9). Demo: the integration test
   reads `clean.pdf` with no padding; `Ghostscript.Text` still exists and nothing calls the
   new reader yet.
2. **Pages and the stage read text through pdftotext.**
   - The Preparer takes the reader.
   - gs's text run and `Ghostscript.Text` are removed.
   - `PagesPerProcess` bounds renders alone.
   - The document stage requires and wires pdftotext, its identity names pdftotext's version,
     and the adapters' version is 7.
   - The package doc, API page, limits page, README, development page and command page are
     updated, along with the CHANGELOG's Unreleased entries (behaviors 5–8).

   Demo: `stages.Build` wires the document route over gs, qpdf, pdfdetach and pdftotext, and a
   prepared PDF's `page N` text has no column padding.
3. **The measurement and the release notes.** The acceptance section and recipe measuring the 6
   olmocr-bench pages and cfr before and after, and the CHANGELOG's [v0.8.0] section with its
   intro paragraph (behaviors 10–11). Demo: the section's figures show no column-padding space
   runs and the words kept.

**Out of scope**
- docinfer's `chunk_runes` character cap and its docproc requirement.
- Other extractors and modes: `pdftotext -raw` and `-layout`, MuPDF's `mutool`, Apache PDFBox,
  xpdf, PdfPig, pdfminer.six, and docproc ordering pdftotext's `-bbox-layout` blocks itself.
- Hyphen control: `-remove-hyphens` and raising pdftotext's minimum to 26.05.
- Better column order for paragraphs that line up across columns.
- Right-to-left text that a PDF's own glyph runs split.
- A container image for pdftotext.
- Text retries for a page whose stage fails at runtime (a backlog item).

**Door**

One-way: the pushed tags docproc v0.8.0, storage/v0.1.6, postgres/v0.1.6 and cmd/docproc/v0.2.6,
since `Ghostscript.Text`'s removal and the new page text reach their callers once tagged.
Everything before the tags is two-way.

**Release**

docproc v0.8.0, then storage/v0.1.6, postgres/v0.1.6 and cmd/docproc/v0.2.6 through the
`pdf-text-padding-require` PR.

## Progress

slices 3/3 committed · standards ✓ · spec — · editor —

## Decisions

- text-encoding-keys: held MimeKit at 4.18.2: 5.0.0 declined, PstBuilder.Eml 1.3.0 is built on
  MimeKit 4.
- text-encoding-keys: held MsgKit at 3.0.6: 3.1.0 declined, it needs MimeKit.Core 5.
- text-encoding-keys: held typed-path at 0.12.4: 0.13.0 declined, onenote_parser 2.1.0 requires
  ^0.12.3.
- pdf-text-padding: page text is poppler's `pdftotext` in reading-order mode, one run per page; gs
  counts and renders only.
- pdf-text-padding: `Ghostscript.Text` and gs's shared txtwrite run are removed (breaking,
  v0.8.0); `PagesPerProcess` bounds renders alone.
- pdf-text-padding: the stage adapters' version is 7, so an index prepares every PDF's page text
  again.
- pdf-text-padding: pdftotext's minimum is poppler 22.02, as pdfdetach's, with line-end hyphens
  joined. Raising it to 26.05 for `-remove-hyphens` was declined: no stable Debian or Ubuntu
  carries 26.05.
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
  route text; and that exec-4's check:polyglot can reopen. Intermediate releases go untold.
- coordinator · roadmap/docproc-backlog.toml: a text retry for a part whose stage fails at
  runtime, such as a corrupt PDF.
