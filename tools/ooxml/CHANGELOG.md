# Changelog

All notable changes to `docproc-ooxml` are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the CLI adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `tools/ooxml/vX.Y.Z` tag carries, and is the release's notes. Its JSON Lines, their `schema`
version included, are its output contract: a change to them is a change here. This changelog
covers docproc-ooxml only.

## [Unreleased]

### Added

- Text boxes are read, and no longer warned of as `text boxes not read`. A docx text box's
  paragraphs, lists, and tables follow the paragraph that anchors it, as its pictures do, in the
  Markdown and the model; one Word writes twice, as DrawingML in alternate content with its VML
  copy as the fallback, is read once, and a VML one alone is read too. An xlsx shape's text, a
  text box's among them, is read as paragraphs and lists in its sheet's drawing order, on the
  sheet's closing `rows` line with its charts and pictures. Other alternate content in an xlsx
  drawing is read once, as its first branch that holds anything read, rather than through every
  branch.
- A pptx's comments are read, and no longer warned of as `comments not read`: legacy comments
  (`p:cmLst` with `commentAuthors.xml`), a reply named by PowerPoint 2013's threading extension
  following its thread, and PowerPoint 365's modern threaded comments (`p188:cmLst` with
  `authors.xml`), their replies in order and a resolved thread marked. Each thread is a footnote
  definition as a docx's comment is, `[^cN]: Comment by <author> (open|resolved): <text>`, its
  replies after it; with no anchored text, its marker follows its slide's heading,
  `# Slide 2[^c1]`.

## [v0.1.2] - 2026-11-19

### Changed

- An input that is missing, a directory, or without read permission is refused, still exit 3,
  with a reason that says which and names the input's path as given, after it: `no such file:
  <path>`, `is a directory: <path>`, or `permission denied: <path>`, in place of `not a file` and
  `cannot be read: permission denied`. A dangling link is missing, and a path under a directory
  that can't be searched is without read permission. Any other input that is not a regular file,
  such as a device, is still `not a file`, and every other refusal and warning reads as before.

## [v0.1.1] - 2026-11-16

### Fixed

- A link's or an image's destination reads back as its target. A backslash in it is escaped, so
  `\\server\Team Notes\plan.docx` no longer reads back with one leading backslash, and so is a
  `&` that could begin a character reference. The rule is the one docproc's Rust tools share,
  stated by tools/envelope/cases.json: a target holding whitespace, a control character, `<`, or
  `>` is written within angle brackets, its `<` and `>` backslash-escaped, no longer
  percent-encoded; any other is written bare, its parentheses backslash-escaped, no longer
  angle-bracketed for them. A line ending or a NUL, which no destination can hold, is
  percent-encoded. A `|` is still escaped.
- A picture's title is written by the title rule docproc's tools share, stated by
  tools/envelope/cases.json: each `\` and `"` is escaped, and so is a `&` that could begin a
  character reference; a line ending is written as a character reference, `&#10;` or `&#13;`, so
  a carriage return within a line no longer reads back as a line feed. A title read back as
  written before but for that, through the literal-text escape; other markup, such as `*`, `_`,
  `[`, or a `#` at its start, is no longer backslash-escaped in it, as a title reads it as itself.
  Its lines are still trimmed and joined by spaces, and a `|` is still escaped.

## [v0.1.0] - 2026-11-13

The first release. docproc-ooxml was copied, in one commit, from spike-ai-pipeline-office v0.1.0
(b9bb1a2), its schema `"1"` CLI, renamed, and reworked into docproc's system tool; Changed
says how it differs from the spike.

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

### Changed

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

[Unreleased]: https://github.com/standards-lab/docproc/compare/tools/ooxml/v0.1.2...HEAD
[v0.1.2]: https://github.com/standards-lab/docproc/compare/tools/ooxml/v0.1.1...tools/ooxml/v0.1.2
[v0.1.1]: https://github.com/standards-lab/docproc/compare/tools/ooxml/v0.1.0...tools/ooxml/v0.1.1
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/tools/ooxml/v0.1.0
