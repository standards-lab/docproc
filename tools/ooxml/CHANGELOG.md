# Changelog

All notable changes to `docproc-ooxml` are documented here, a section per release, headed by the
version its `tools/ooxml/vX.Y.Z` tag carries; the release's notes are its section. The CLI
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Its JSON Lines, their
`schema` version included, are its output contract: a change to them is a change here.

## v0.1.0

The first release. docproc-ooxml was copied, in one commit, from spike-ai-pipeline-office v0.1.0
(b9bb1a2), its schema `"1"` CLI, renamed, and reworked into docproc's system tool.

### Added

- `docproc-ooxml <input> [--media <dir>]`, `--version`, and `--help`, the arguments parsed by
  hand: a NativeAOT binary on .NET 11 for linux-x64 and linux-arm64, the Open XML SDK its only
  package, its package graph locked. `--version` prints the version, then `schema 2`.
- Schema `"2"` JSON Lines in one run: a `document` line with the format, `properties`, the
  Markdown rendering, and `media`, then, per xlsx sheet in workbook order, a `sheet` line and its
  `rows` lines, in snake_case. Joined in order, the lines' Markdown fragments are the whole
  rendering. Output is byte-identical for the same input, pinned by a `.jsonl` and a `.stderr`
  golden per fixture.
- xlsx is streamed in two forward-only passes: a refusal still comes before any output, and a
  `rows` line stays within 256 KiB or one row.
- `properties`: the package's core, extended, and custom properties, each value typed as stored.
- `embedded` and `ole` media, as stored, with their `prog_id` and placement, linked in the
  Markdown after their preview pictures; a chart's embedded workbook is never media. Every
  media entry's placement is its docx section (from 0), its xlsx sheet's `sheet` line (from 1),
  or its pptx slide (from 0).
- External hyperlinks as `[text](target)`, and a picture's title, where it differs from its
  image text, as its Markdown title.
- One warning for each kind of content present that isn't read: text boxes, equations,
  SmartArt, a pptx's comments, and an xlsx's cell notes.
- The common envelope: stderr lines `docproc-ooxml: warning: <where>: <what>` and
  `docproc-ooxml: error: <reason>`, `<where>` a place by ordinal and escaped, a reason never
  leading with the input path; exit codes 0 read, 1 stdout or media not written (a broken pipe
  included), 2 usage, 3 refused before any output, 4 a fault of its own; UTF-8 under any locale.
- Refusals: not a file; not a docx, xlsx, or pptx package (an xlsb among them); an encrypted
  package, its reason beginning `encrypted`, found by docproc-ooxml's own OLE compound-file
  directory reader; any other OLE compound file, such as a legacy doc, xls, or ppt; a malformed
  package; tables nested more than eight deep.
- Releases: `docproc-ooxml-vX.Y.Z-linux-x64.tar.gz` and `-linux-arm64.tar.gz`, each holding the
  binary, LICENSE, and THIRD-PARTY-NOTICES.txt, with `SHA256SUMS`; a release is refused unless
  the binary's `--version` is the tag's and it requires glibc 2.34 at most.

### Changed from the spike

- One run writes the model and its Markdown together; `--format` is gone.
- The model's fields are snake_case, and the text each holds exists once, in the Markdown.
- A refusal is an `error:` line, no longer `refused: <path>: …`.
- Embedded OLE objects and packages are extracted, no longer only their preview pictures.

### Known limits

- Pictures are listed with no layout, so a covered, off-slide, or cropped-away picture is still
  a media block.
- Text boxes, equations, SmartArt, a pptx's comments, and an xlsx's cell notes are not read.
- No formula is evaluated, and no number format is applied but the dates'.
- Tracked changes are read accepted only.
- Media are written as stored, never converted.
- Linux alone, x64 and arm64, on glibc 2.34 or newer.
