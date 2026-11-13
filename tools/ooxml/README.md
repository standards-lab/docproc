# docproc-ooxml

docproc's system tool for Office Open XML documents: a command-line utility on Microsoft's
[Open XML SDK](https://github.com/dotnet/Open-XML-SDK) that reads one docx, xlsx, or pptx file
and writes it to stdout as JSON Lines, schema `"2"`, with its Markdown rendering inside the lines
as the text layer. Its headings, paragraphs, lists, tables, notes and comments, sheets and their
typed cells, slides and their notes, charts' cached data, document properties, and hyperlinks
are read; its pictures, embedded packages, and OLE objects are written out as media files.

It is one NativeAOT binary each for linux-x64 and linux-arm64, built with .NET 11, needing no
.NET runtime on the host, for docproc to run like any other system tool. It renders no page
images and runs no inference: it calls no model, and the text it extracts is complete without
one.

## Install

Each release, tagged `tools/ooxml/vX.Y.Z`, carries two archives and their checksums:

- `docproc-ooxml-vX.Y.Z-linux-x64.tar.gz`
- `docproc-ooxml-vX.Y.Z-linux-arm64.tar.gz`
- `SHA256SUMS`, covering both.

Each archive holds the `docproc-ooxml` binary, the repository's `LICENSE`, and
`THIRD-PARTY-NOTICES.txt` for the packages and the .NET runtime the binary links in. Check the
archive against `SHA256SUMS` before unpacking it:

```sh
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf docproc-ooxml-v0.1.0-linux-x64.tar.gz
./docproc-ooxml --version
```

The binary links glibc dynamically and needs glibc 2.34 or newer (see [Limits](#limits)).

## Usage

```sh
docproc-ooxml <input> [--media <dir>]
docproc-ooxml --version
docproc-ooxml --help
```

It reads the one `<input>` and writes its lines to stdout, and nothing else there. `--` ends
the options, so an input may begin with `-`. With `--media <dir>`, each distinct picture,
embedded package, and OLE object is written once into `<dir>`, created if missing, as stored,
named by the lowercase hex SHA-256 of its bytes and its stored extension. Without `--media`
nothing is written to disk and the lines name the files all the same. A linked picture is named
by its target, never fetched.

`--version` prints the version on its first line and the schema on its second:

```text
0.1.0
schema 2
```

A macro-enabled document, a template, or a slide show (docm, dotx, dotm, xlsm, xltx, xltm,
pptm, potx, potm, ppsx) is read as its base format, by its main part's content type whatever the
file is named. Its macros are ignored, never run.

## Output

One run writes a `document` line first: the schema, the format (`docx`, `xlsx`, or `pptx`), the
package's core, extended, and custom `properties`, the Markdown rendering, and the `media` it
names, each a `picture`, `embedded`, or `ole` with its file, its `prog_id` where it has one, and
its placement: the docx section (from 0), the xlsx sheet's `sheet` line (from 1), or the pptx
slide (from 0). An xlsx's Markdown is split across its lines so no line holds a whole sheet:
each sheet in workbook order is a `sheet` line, its heading and its table's header, then `rows`
lines, each holding the sheet's next rows and their typed cells, within 256 KiB or one row. A
sheet's charts and pictures ride on its closing `rows` line. Joined in order, the lines'
Markdown fragments are the whole rendering.

```text
{"line":"document","schema":"2","format":"xlsx","properties":{"core":{"creator":"openpyxl",…}}}
{"line":"sheet","name":"Dates","markdown":"# Dates\n\n|  | A |\n| --- | --- |\n"}
{"line":"rows","markdown":"| 1 | 2026-01-15 |\n…","rows":[{"row":1,"cells":[{"ref":"A1","value_type":"date","value":"2026-01-15"}]},…]}
```

A field with no value is left out, never `null`. The same input gives byte-identical stdout and
media. The contract, field by field, is docproc's:
[`context/layers.md`](../../context/layers.md), "The tool contract" and its "docproc-ooxml"
section. From schema `"2"` on, it grows by addition only; any other change moves the schema, and
[`CHANGELOG.md`](CHANGELOG.md) records it.

## Exit codes and stderr

| Exit code | Meaning |
| --- | --- |
| 0 | Read, skipped items included. |
| 1 | stdout or the media could not be written, a broken pipe included. |
| 2 | A usage error. |
| 3 | Refused before any output. |
| 4 | A fault of docproc-ooxml's own, never the document's. |

stderr holds one line each:

- `docproc-ooxml: warning: <where>: <what>` for what was skipped, `<where>` a place by ordinal,
  such as `sheet 2` or `slide 4`, escaped so the line splits at its first three `": "`. An
  invalid reference, such as a sheet whose relationship names no part, a properties part that
  can't be parsed, or a repeated property key, is skipped with a warning, and the rest is read
  with exit code 0.
- `docproc-ooxml: error: <reason>` for exit codes 1 to 4. The reason never names the input path.

A refusal comes before anything is written to stdout or the media directory. docproc-ooxml
refuses an input that is not a file or can't be read; one that is not a docx, xlsx, or pptx
package (an xlsb among them); an encrypted package, its reason beginning `encrypted`; an OLE
compound file, such as a legacy doc, xls, or ppt, which is to be converted to OOXML first; a
package malformed at the ZIP or part level; and a docx with tables nested more than eight deep.

The envelope these follow, its cases stated once for every tool, is
[`tools/envelope/cases.json`](../envelope/cases.json).

## Limits

- Pictures are listed with no layout, so a covered, off-slide, or cropped-away picture is still
  a media block.
- Text boxes, equations, SmartArt, a pptx's comments, and an xlsx's cell notes are not read.
  Each kind present gets one warning, where the document first holds one:
  `docproc-ooxml: warning: slide 1: comments not read`.
- No formula is evaluated: a formula's value is the one the file caches, or none.
- No number format is applied but the dates': a number is written as stored.
- Tracked changes are read accepted only.
- Media are written as stored, never converted: an EMF, WMF, or TIFF stays one. A chart's
  embedded workbook is never media; the chart is its cached data.
- It runs on Linux alone, x64 and arm64, as a .NET 11 NativeAOT binary that links glibc
  dynamically: the host needs glibc 2.34 or newer, the floor `GLIBC_FLOOR` in
  [`mise.toml`](mise.toml) states and the release refuses to exceed.

## Building and testing

The toolchain (the .NET SDK, jq, uv) is pinned in [`mise.toml`](mise.toml) alone. Its tasks run
from this directory, `mise run <task>`, or from the repository root, `mise -C tools/ooxml run
<task>`; docproc's own `mise run check` runs this tool's check.

- `check`: restores the locked package graph, verifies formatting, builds with warnings as
  errors, publishes the NativeAOT binary, and runs the tests, which compare the published
  binary's stdout, stderr, and exit code for every fixture against its committed goldens.
- `golden`: re-records each fixture's `.jsonl` and `.stderr` goldens, and a refused fixture's
  `.exit`, from the published binary. The check never writes them, so a change to the output is
  reviewed as a diff of them.
- `publish`: builds the release binary for this machine into `.state/publish/`;
  `tools/pack.sh ooxml <version>` runs it and packs the archive, refusing a binary whose
  `--version` isn't `<version>` or that requires a glibc above the floor.
- `currency`: reports NuGet packages, the fixture generator's Python packages, and mise tools
  behind their latest, and NuGet packages with a known vulnerability. It reads the network, so
  CI never runs it.
- `upgrade`: bumps what `currency` reports as behind to its latest: the mise tools in
  [`mise.toml`](mise.toml), the direct NuGet packages, their lock files rewritten and restored
  in locked mode, and the fixture generator's pinned packages and its lock. A vulnerable
  transitive package is moved only by a newer direct one. Review the diff and run `check`;
  docproc's own `mise run upgrade` runs it.
- `fixtures`: regenerates the generated fixtures; it needs LibreOffice's `soffice`. The
  fixtures are described in [`fixtures/README.md`](fixtures/README.md).

The version the project records, in `src/Docproc.Ooxml/Docproc.Ooxml.csproj`, must be the
release tag's and that of [`CHANGELOG.md`](CHANGELOG.md)'s newest release section. A release
binary prints it alone; other builds append `+` and the commit.

Apache-2.0.
