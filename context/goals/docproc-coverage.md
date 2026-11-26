# goal · docproc-coverage

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] text-encoding-keys
2. [x] archives-and-mpeg
3. [x] text-last-resort
4. [x] pdf-text-padding
5. [x] office-and-mail

## Task brief · office-and-mail

**Problem**

docproc's last coverage gaps block corpus.docs: ODF, WordPerfect, and Excel binary workbooks end
with no processor; docproc-ooxml warns of text boxes, pptx comments, and xlsx cell notes it
leaves unread; its memory floor is known only on x64; and docproc-mail notes a TNEF part instead
of reading it. This task closes them and cuts the goal's final release.

**Behaviors**
1. The office stage converts ODF text, spreadsheet, and presentation packages (and their
   templates) through soffice to docx, xlsx, and pptx and reads the result through
   docproc-ooxml, as it does legacy documents; ConvertedFrom names the source kind.
2. An ODF drawing (.odg, .otg) converts through soffice's Impress import to a pptx holding its
   pages; the LibreOffice run gains an import-filter field to do it.
3. ODF formula, chart, database, and image packages end with no processor, and docs/limits.md
   names them.
4. An encrypted ODF package ends Encrypted, never as a soffice failure.
5. A WordPerfect document converts to docx and an Excel binary workbook to xlsx; an encrypted
   .xlsb ends Encrypted.
6. The office stage's own configuration hash changes, so an index holding these parts as "no
   processor yet" prepares its office content again, and no other stage's; `adapterVersion`
   stays 7.
7. docproc-ooxml reads docx and xlsx text boxes, each once across a docx's
   `mc:Choice`/`mc:Fallback` copies; its first change corrects generate.py's `slide_objects`
   docstring.
8. docproc-ooxml reads a pptx's comments, legacy and modern, into its Markdown.
9. docproc-ooxml reads an xlsx's legacy cell notes, and its threaded comments with their replies
   from their own and the persons parts, skipping the legacy notes Excel writes as their
   duplicates. Comments and notes follow the model docx comments use, as footnotes.
10. docproc-ooxml warns of no text box, pptx comment, or xlsx note or threaded comment; goldens
    hold the new text.
11. A `workflow_dispatch`-only CI job measures docproc-ooxml's memory floor on `ubuntu-24.04`
    and `ubuntu-24.04-arm` from the task branch's final ooxml build: 20 runs at each RLIMIT_AS
    bound under `prlimit --as`. docs/limits.md states both floors, and docs/acceptance.md
    records the recipe.
12. docproc-mail reads a TNEF part (by content type or the name winmail.dat) with its own
    MS-OXTNEF reader filling its MAPI property bag: the body by its existing precedence (HTML,
    RTF `\fromhtml1`, text, RTF text, a MIME text/plain alternative winning over non-HTML RTF)
    and the attachments as the message's media, with no TNEF note. Tests use Apache POI's
    Apache-2.0 hmef samples wrapped in .eml, their expected bodies and attachments checked by
    MimeKit 4.18.2's `TnefPart` in the generator. No new crate.
13. The docs follow: limits.md, the tool contract, tool-gaps, and the mail and ooxml READMEs drop
    what is now read; the README's tool table and install line move with the tags; CHANGELOGs
    for ooxml, mail, the base module (with one sentence correcting v0.8.0's "every PDF again" to
    every stage but text and vision), storage, postgres, and cmd/docproc.
14. A docproc run over ~/docs prepares its ODF, WordPerfect, and .xlsb inputs (the encrypted
    .xlsb flagged encrypted) and warns of none of behavior 10's kinds.

**Test seams**
- docproc-ooxml's goldens through generate.py; docproc-mail's fixture tests; the office stage
  through the exec runner in the pinned gotenberg image; the CI floor job; a ~/docs run.

**Slices**
1. ooxml: the `slide_objects` docstring; docx and xlsx text boxes.
2. ooxml: pptx comments, legacy and modern.
3. ooxml: xlsx cell notes and threaded comments.
4. ooxml: the floor job, both floors measured on the final ooxml build, limits.md and
   acceptance.md.
5. mail: TNEF body and attachments.
6. Go: the office stage for ODF, WordPerfect, and .xlsb, the import filter, encrypted ODF, and
   the office hash.
7. Docs, CHANGELOGs, and the ~/docs run.

**Out of scope**

Recognizing an .xlsb by content rather than name; a processor for ODF formula, chart, database,
or image packages; a docproc-onenote release; poppler's `-remove-hyphens` (held at 22.02);
raising `OfficeMinimum` or `MailMinimum` (they stay 0.1.0).

**Door**

One-way for the tags, which reach corpus.docs and outside callers; two-way for the rest.

**Release**

`mise run test:integration` before every tag; tag messages without "v", per precedent.
`tools/ooxml/v0.2.0`, `tools/mail/v0.2.0`, then `v0.9.0`, then the `-require` PR's
`storage/v0.1.7`, `postgres/v0.1.7`, `cmd/docproc/v0.2.7`.

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
- office-and-mail: kept `picture.LibreOfficeMinimum` at 24.2, though ODF, WordPerfect, and .xlsb
  conversion is proved only on LibreOffice 26.8 (the pinned image's 26.8.2.2; the host's 26.8.1.1
  can't import the .wpd): raising it would refuse hosts whose legacy conversion already works.
- office-and-mail: the storage, postgres, and cmd/docproc CHANGELOG sections are written and dated
  in the `-require` PR after the v0.9.0 tag, not on the task branch.

## Pending edits

- docs-corpus · once docproc-coverage syncs, tell corpus.docs (context/goals/corpus.docs.md, task
  docproc-final; standards-lab #219) the final release's tags, v0.9.0, storage/v0.1.7,
  postgres/v0.1.7, cmd/docproc/v0.2.7, tools/ooxml/v0.2.0, and tools/mail/v0.2.0, and the
  run-directory schema it writes; that `#!` scripts and other unlisted text route text; that a PDF
  page's text is pdftotext's reading order, with no column padding; that ODF, WordPerfect, and
  .xlsb are prepared through LibreOffice, an encrypted .xlsb ending Encrypted; that a TNEF part's
  bodies and attachments are read; that docproc-ooxml's Markdown holds text boxes, a pptx's
  comments, and an xlsx's notes and threaded comments; and that exec-4's check:polyglot can
  reopen. Intermediate releases go untold.
- docinfer · once docproc-coverage syncs, tell v1.ai.inference (docinfer's goal record) docproc's
  final state: the tags v0.9.0, storage/v0.1.7, postgres/v0.1.7, cmd/docproc/v0.2.7,
  tools/ooxml/v0.2.0, and tools/mail/v0.2.0, the run-directory schema they write, and the coverage
  the docs-corpus edit names.
- coordinator · roadmap/docproc-backlog.toml: a text retry for a part whose stage fails at
  runtime, such as a corrupt PDF.
- coordinator · roadmap/docproc-backlog.toml: control over line-end hyphens in PDF page text
  (pdftotext `-remove-hyphens`, poppler 26.05), once Debian and Ubuntu stable carry 26.05.
- coordinator · roadmap/docproc-backlog.toml: docproc-ooxml exits 255 with nothing on stderr when
  it runs out of memory below its 384 MiB floor, so an operator sees no cause; report one.
