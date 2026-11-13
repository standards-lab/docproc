# spike-ai-pipeline-office

The office-document tool of [spike-ai-pipeline](https://github.com/JaimeStill/spike-ai-pipeline):
a command-line utility on Microsoft's [Open XML SDK](https://github.com/dotnet/Open-XML-SDK) that
turns docx, xlsx, and pptx files into a JSON document model (headings, paragraphs, lists, tables
and their cells, sheets, slides in order with their notes, chart series, and embedded media
written out as files), with Markdown as a second rendering of the same model.

It is published as one NativeAOT binary each for linux-x64 and linux-arm64, with no .NET runtime
needed on the host, for spike-ai-pipeline-processing to run like any other system tool. It
renders no page images: chart data and embedded pictures take their place. It refuses the legacy
binary formats (doc, xls, ppt) and never reads them; converting them to OOXML first, with
LibreOffice in a container, is the caller's part. It runs no inference: it calls no model, and
the text it extracts is complete without one.

## Usage

```sh
spike-ai-pipeline-office <input> [--format json|markdown] [--media <dir>]
```

It writes the chosen rendering of the input to stdout, JSON when `--format` is left out, and
nothing else there. Embedded media go to the `--media` directory, created if missing, each
distinct content once, as stored, named by the lowercase hex SHA-256 of its bytes and its stored
extension. Without `--media` nothing is written to disk. A linked picture is never fetched.
`--version` prints the version.

| Exit code | Meaning |
| --- | --- |
| 0 | Converted. |
| 1 | The media could not be written. |
| 2 | A usage error; usage goes to stderr. |
| 3 | Refused: the document is missing or unreadable, not a docx, xlsx, or pptx package, encrypted or a legacy binary format, malformed at the ZIP or part level, or past a limit of office's. |
| 4 | Failed in a way office expects of no document: a fault of office's own, never reported as any other code. |

office refuses a document before writing any output, with its reason on stderr. Its one limit is
tables nested more than eight deep, and that refusal names where:
`refused: <path>: tables nested 9 deep (limit 8), at body paragraph 112`. office writes media only
once the whole conversion has succeeded, and stdout after them.

A single invalid reference, such as a header reference of a type no header has or a sheet whose
relationship names no part, is skipped and the rest converts with exit code 0. Each skip writes
one line to stderr: `spike-ai-pipeline-office: warning: <path>: <what was skipped>`.

A macro-enabled document, a template, or a slide show (docm, dotx, dotm, xlsm, xltx, xltm, pptm,
potx, potm, ppsx) is read as its base format, by its main part's content type whatever the file
is named. Its macros are ignored, never run.

## The JSON model

The JSON model is office's output contract, one tree for all three formats: a document's
`schema`, `"1"` in this version, its `format` (`docx`, `xlsx`, `pptx`), and its `blocks` in
document order, each a `heading`, `paragraph`, `list`, `table`, `chart`, `media`, `sheet`, or
`slide` by its `type`. The schema is versioned with office's tags: a change to the model's shape
changes the `schema` version and is recorded in [`CHANGELOG.md`](CHANGELOG.md). The model grows
by adding kinds and optional properties, never by reshaping these.

An optional property with no value is left out, never written as `null`: an absent property
means null or none. So a formula cell with no cached value has no `value` and no `valueType`, and
a chart with no title has no `title`. Only an array keeps a `null` in place, as a chart series
does for a point it caches no category or value for, so its positions still line up.

## docx

A docx document's blocks are its body's in order, its tracked changes accepted. An ordered list
has its `start`, its first number, when that isn't 1: its numbering's start, or, for a list a
paragraph interrupted, the number its count resumes at, as Word numbers it. Markdown numbers its
items from there. A cell of a row that starts late (`w:gridBefore`) has the grid columns it skips
as `skipBefore`, so Markdown places it in its own column.

Content controls, custom XML, and smart tags are read through wherever they wrap text, around
runs, paragraphs, rows, or cells. A control showing its placeholder ("Choose an item.") holds a
prompt, not text, and is left out. A ruby's base text is the text, its annotation left out. An
embedded OLE object is never extracted, but the picture Word shows in its place is a media block.

A docx document's `sections` hold, for each section in body order, the headers and footers its
pages show (`default`, `first`, `even`): its own, or the previous section's where it has none of a
kind. A first-page one counts only with a title page, and an even-page one only when the document
turns even-page headers on. Its `footnotes` and `endnotes` are the notes the text references, in
the order it first references them. Its `comments` are its comment threads, each with its author,
its anchor (the text of its range), whether it is resolved, its blocks, and its replies. The text
refers to them by `footnote`, `endnote`, and `comment` inlines. A media block's `section` is the
section that shows it.

In Markdown a heading is `#` by its level, and a list nests and counts from its start. A table is
a GFM table whose merged cell's text sits in its top-left position, the positions it covers left
empty. Each distinct header comes before the body and each distinct footer after it. The notes
then follow as GFM footnotes, footnotes and endnotes counted together as `[^1]`, `[^2]`, and the
comments after them as `[^c1]`, `[^c2]`, each with its author, its state, and the text it is
anchored to.

## xlsx

An xlsx document's blocks are its sheets in workbook order, hidden ones included and marked
`hidden`. A sheet holds a table of the cells that have a value or a formula, each row with its
number and each cell with its reference (`B2`), so the table is as sparse as the sheet. Its charts
and pictures follow, in the order its drawing anchors them, each picture with its `sheet`.

A cell's `value` is typed by its `valueType`:

- `number`: as stored, no number format applied.
- `text`: rich text flattened.
- `boolean`.
- `error`, such as `#DIV/0!`.
- `date`: a number in a date or time format, as ISO 8601, in the workbook's 1900 or 1904 date
  system. It is a date alone when the format shows no time, a time alone when the format shows no
  date and the number is under a day, and both otherwise.

A formula's cell has its `formula` text and its cached value, or no value when the file caches
none; nothing is evaluated. A shared formula's other cells name its anchor in `sharedFrom`; an
array formula's text is on its anchor. A merged range's anchor has its spans, and the cells it
covers are left out.

In Markdown each sheet is a heading of its name, with `(hidden)` after a hidden one's. Its table
is a GFM table whose header row is the column letters its cells use and whose first column is the
row numbers. A cell shows its value, or with none its formula as code (`` `=SUM(A1:A3)` ``), or a
shared formula's anchor.

## pptx

A pptx document's blocks are its slides in presentation order (the presentation's slide list, not
the parts' names), hidden ones included and marked `hidden`, each with its `number` from 1, its
blocks, and its `notes`. A slide's shapes are read in its shape tree's order, back to front, never
re-sorted by position, and a group's shapes in place. A title placeholder's text is a level-1
heading, its paragraphs its lines. Any other shape's paragraphs are paragraphs, or list items
where they have a bullet, nested by their level: a character or picture bullet is unordered, an
auto-number ordered, and the list's `start` is its first item's `startAt`.

A paragraph's bullet is its own, or else the first one its level's properties give, looked up in
this order:

1. the shape's list style;
2. for a placeholder, its layout placeholder's (by index, or else type), that one's master
   placeholder's, and the master's title, body, or other text style; for any other shape, the
   master's other text style;
3. the presentation's default text style.

A table keeps its merges as spans, the cells they cover left out. A chart is its cached data. A
picture is a media block with its `slide`, the slide's index in the document's blocks. An empty
paragraph or placeholder is left out, and a layout's prompt text never appears. A slide's notes
are its notes page's body placeholder's text, never its slide image or slide number.

In Markdown each slide is a heading, `# Slide 3`, with `(hidden)` after a hidden one's, its title
a level-2 heading below it, then its content, then its notes after a bold `**Notes**` label.

## Charts and media

In every format a chart is the data its chart part caches, never an image of it: its type (the
first plot's kind: `bar`, `line`, `pie`), its title, and its series, each with its name,
categories, and values, in a Transitional or an ISO 29500 Strict document alike. An Office 2016
chart (waterfall, histogram, pareto, box and whisker, sunburst, treemap, funnel, region map) sits
in `mc:AlternateContent` beside a picture for applications that can't draw it. office reads it the
same way, its type its layout (`boxWhisker`, `sunburst`), each point of a hierarchy labeled with
the innermost level that names it. One that caches no data comes out as that picture instead, a
media block, and never both.

In Markdown a chart is its title, then a GFM table of its series, a column each and a row for each
category. A picture is an image link to its file in the media directory, or to a linked picture's
target.

## What office does not do

- It evaluates no formula: a formula's value is the one the file caches, or none.
- It applies no number format but the dates': a number is written as stored.
- It reads tracked changes accepted only, never as stored.
- It renders no page images.
- It refuses legacy doc, xls, and ppt files, and tables nested more than eight deep; it reads
  neither.
- It extracts nothing outside what this page describes: equations, SmartArt, a docx's or an
  xlsx's text boxes and WordArt, embedded OLE objects (of which only the picture a docx shows in
  their place comes out, as media), an xlsx's cell notes and comments, a pptx's comments, and a
  docx comment range that starts or ends between a table's rows or cells.
- It writes media as stored, never converted: an EMF, WMF, or TIFF stays one.
- It runs no inference.

## CI and release

CI (`.github/workflows/ci.yml`) runs `mise run check`, the check a developer runs, on every pull
request into main and every push to main, on `ubuntu-24.04` and `ubuntu-24.04-arm`, so each
publishes and tests its own NativeAOT binary. A newer run on a ref cancels the one it supersedes.
A pull request merges once both pass. main's ruleset, `.github/rulesets/main.json`, requires a
pull request and forbids deleting main or force-pushing to it. It is applied once, by hand:
`gh api repos/JaimeStill/spike-ai-pipeline-office/rulesets --method POST --input
.github/rulesets/main.json`.

office is released by tag, `vX.Y.Z`, with a [CHANGELOG](CHANGELOG.md), the tag pushed once main's
CI has passed. The release workflow (`.github/workflows/release.yml`) builds the binary natively
for linux-x64 and linux-arm64 and refuses one whose `--version` isn't the tag's version. So the
version the csproj records must be the tag's: a release binary prints it alone, and other builds
append `+` and the commit. The GitHub release, its notes the tag's CHANGELOG section, carries
`spike-ai-pipeline-office-vX.Y.Z-linux-x64.tar.gz` and `-linux-arm64.tar.gz`, each holding the
binary and LICENSE, and `SHA256SUMS` covering both.

## The baseline

The baseline tier, `mise run baseline`, runs on a laptop and never in CI. It runs office's
NativeAOT binary over the committed pool, scores its Markdown element by element against the ground
truth (`bench/SpikeAiPipeline.Office.Baseline`, with Markdig, never in office's binary), and
measures each run's time and peak memory and the binary's size. Its scoring rules have tests of
their own, `test/SpikeAiPipeline.Office.Baseline.Tests`, which the check runs. Its findings are
[`docs/findings/office-cli.md`](docs/findings/office-cli.md), from the runs kept in
`docs/findings/office-cli/runs/`; that page is the evidence the root's evidence 12 asks of the Open
XML CLI. A plain run keeps nothing: it prints its summary and writes only
`.state/baseline/results.json`. `mise run baseline -- --record [name]` records a run in a new
`docs/findings/office-cli/runs/<name>/`, `<date>-<n>` by default, and never overwrites one. Its comparison with markitdown and xberg was measured once and is closed; the page keeps
it as measured.

The spike's question, evidence, and answer are in the root repository's README.

Apache-2.0.
