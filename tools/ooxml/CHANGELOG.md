# Changelog

All notable changes to `spike-ai-pipeline-office` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the CLI adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). The JSON document model, its `schema`
version included, is office's output contract: a change to it is a change here.

## [Unreleased]

## [v0.1.0] - 2026-10-31

### Added

- `spike-ai-pipeline-office <input> [--format json|markdown] [--media <dir>]`, which turns a docx,
  xlsx, or pptx file into a JSON document model, or into that model's Markdown rendering, on
  stdout, which carries the rendering and nothing else. Exit codes: 0 converted, 1 media not
  written, 2 usage (usage on stderr), 3 refused (the reason on stderr, before any output), and 4
  failed, a fault of office's own. A single invalid reference, such as a header reference of a
  type no header has or a sheet whose relationship names no part, is skipped with one stderr
  line, `spike-ai-pipeline-office: warning: <path>: <what was skipped>`, and the rest converts.
  `--version` prints the version.
- The initial output contract, schema `"1"`: one tree for all three formats, a document's
  `schema`, `format`, and `blocks`, each block a `heading`, `paragraph`, `list`, `table`,
  `chart`, `media`, `sheet`, or `slide`. An optional property with no value is left out, never
  written as `null`, so an absent property means null or none: a formula cell with no cached
  value has no `value` and no `valueType`. Only an array keeps a `null` in place, for a chart
  point the cache lacks. A docx document also holds its `sections`, each with the headers and
  footers its pages show (`default`, `first`, `even`), its `footnotes` and `endnotes`, and its
  `comments`, each with its author, anchor text, resolved state, and replies, referred to from
  the text by `footnote`, `endnote`, and `comment` inlines. Its tracked changes are read
  accepted.
- Sheets: in workbook order, hidden ones marked, each a sparse table of the cells that have a
  value or a formula, by row number and reference, then its charts and pictures. A cell's
  `value` is typed by its `valueType` (`number`, `text`, `boolean`, `error`, `date`), a number in
  a date or time format as ISO 8601; a formula's cell has its `formula` text and its cached
  value, nothing evaluated, and a shared formula's other cells name its anchor in `sharedFrom`.
- Slides: in presentation order, hidden ones marked, each with its `number`, its shapes' text,
  lists, tables, charts, and pictures in shape-tree order, and its `notes`.
- Charts as their cached data, never an image: type, title, and series of names, categories, and
  values, from a Transitional or a Strict document, and Office 2016 chartEx charts by their
  layout; a chartEx chart that caches no data is its fallback picture instead, never both.
- Media: each embedded picture written once to `--media`, as stored, named by the lowercase hex
  SHA-256 of its bytes and its stored extension, only after the whole conversion has succeeded;
  a linked picture is its `target`, never fetched.
- The Markdown rendering of the model: headings, nested lists counting from their start, GFM
  tables with a merged cell's text in its top-left position, charts as their title and a table
  of their series, media as image links, sheets and slides under headings of their own, slide
  notes after a bold `**Notes**` label, headers before the body and footers after it, notes as
  `[^n]` and comments as `[^cN]` footnotes.
- Macro-enabled files, templates, and slide shows read as their base format, by their main
  part's content type; macros are ignored, never run.
- Refusals: legacy doc, xls, and ppt files and encrypted packages, anything that isn't a docx,
  xlsx, or pptx package, a malformed package, and tables nested more than eight deep.
- Releases: `spike-ai-pipeline-office-vX.Y.Z-linux-x64.tar.gz` and `-linux-arm64.tar.gz`, each a
  NativeAOT binary and LICENSE, and `SHA256SUMS`.

### Known limits

- No formula is evaluated: a formula's value is the one the file caches, or none.
- No number format is applied but the dates': a number is written as stored.
- Tracked changes are read accepted only, never as stored.
- No page images are rendered.
- Legacy doc, xls, and ppt files are refused, never read, as are tables nested more than eight
  deep.
- Not extracted: equations; a docx's or an xlsx's text boxes and WordArt; SmartArt; embedded OLE
  objects, of which only the picture a docx shows in their place comes out, as media; a pptx's
  comments; an xlsx's cell notes and comments; and a docx comment range that starts or ends
  between a table's rows or cells.
- Media are written as stored, never converted: an EMF, WMF, or TIFF stays one.
- office runs no inference: it calls no model.

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-office/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/JaimeStill/spike-ai-pipeline-office/releases/tag/v0.1.0
