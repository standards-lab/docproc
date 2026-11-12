# Evaluation · docproc-ooxml (spike-ai-pipeline-office v0.1.0)

The spike is sound to promote: its readers are careful, nothing is suppressed to quiet a NativeAOT
or trim warning, no enumeration order leaks into the output, and its fixtures carry independent
ground truth. The findings below are where it meets schema "2". The three most consequential:

1. The contract gives a sheet's charts and pictures no line to carry their Markdown, so an xlsx's
   lines can't rejoin into the rendering as layers.md promises (OX-1).
2. The tool reads a whole workbook into memory before it writes anything: a 17 MB workbook took
   8.2 GiB and 25 s. Splitting the output into `rows` lines bounds the reader, not the tool (OX-2).
3. stdout is encoded by the process's locale, so the same input gives different bytes under a
   non-UTF-8 `LANG`, with exit code 0 (OX-3).

Probes ran on the binary already published in the spike's `.state/aot/` (0.1.0+6ed0d0e, linux-x64),
one run each on this laptop, with inputs built in the session's scratch directory. Line numbers
are at tag v0.1.0. Paths are relative to the spike root, with `src/SpikeAiPipeline.Office/`
shortened to `src/…/`.

## Findings

### OX-1 · A sheet's charts and pictures have no line to carry their Markdown
- **Kind:** issue
- **Weight:** high
- **Where:** `src/…/Rendering/MarkdownRenderer.cs:258-263`, `:40`;
  `src/…/Reading/XlsxReader.cs:57-81`;
  `test/SpikeAiPipeline.Office.Tests/Expected/drawings.openpyxl.md`;
  `fixtures/realworld/xlsx/chart-sheet.xlsx`
- **Against:** layers.md, "docproc-ooxml": the `sheet` and `rows` lines, and "The lines' Markdown,
  joined in order, is office's whole rendering"
- **Finding:** `RenderSheet` writes a sheet's heading, its cell table, then its charts' tables and
  its pictures' image links. In `drawings.openpyxl.md` the `# Sales` table is followed by the
  chart title "Revenue by quarter" and two image links. `chart-sheet.xlsx` renders `# Chart1` as a
  chart table alone, with no cells. The contract places the heading and the table's header on the
  `sheet` line, the table rows on `rows` lines, and no sheet's Markdown on the `document` line, so
  the chart tables and image links belong to no line. "Joined in order" also leaves the separator
  open: the renderer joins chunks with a blank line, table rows with a newline, and ends with one
  newline (line 40), so a byte-exact rejoin needs each fragment to carry its own separators.
- **Proposed design:** State in layers.md that the fragments concatenate with nothing between
  them, each ending in its own newlines. Carry a sheet's drawing Markdown on a closing `rows` line
  of that sheet that has `markdown` and no `rows` (the absence rule already allows it), bounded as
  any `rows` line is. A chart sheet is then a `sheet` line with its heading and that one closing
  line. The alternative is a new line kind for a sheet's drawing, which is clearer to a reader
  and costs one more kind.
- **Carried by:** ooxml, with the layers.md edit in design and the `office.Rows` reader in library

### OX-2 · The tool holds the whole workbook in memory, which the `rows` lines don't change
- **Kind:** issue
- **Weight:** high
- **Where:** `src/…/Reading/XlsxReader.cs:64`, `:87-152`, `:155-169`;
  `src/…/Reading/MediaStore.cs:12-33`; `src/…/Rendering/MarkdownRenderer.cs:330-353`;
  `src/…/Cli.cs:110`, `:127`
- **Against:** layers.md, "docproc-ooxml" ("no line holds a whole sheet, so a reader streams it");
  the envelope's exit code 3, "refused before any output"; roadmap.toml, the ooxml task's backlog
  comment
- **Finding:** A generated workbook of one sheet, 500,000 rows by 10 numeric cells (17 MB on disk,
  173 MB of XML), took 25.5 s and 8,201 MiB peak for JSON (791 MB written) and 23.2 s and
  4,116 MiB for Markdown. The spike's pool never showed this: its largest peak is 44 MiB. The
  tool builds the SDK's element tree of the sheet, then the model, then the whole output as one
  string. Writing compact lines shrinks only the last of the three. A process killed for memory
  exits 137, which is none of the contract's codes. The roadmap lists sheet-by-sheet writing as
  backlog, but two things in the code stand in its way and neither is in the change list: a
  refusal must come before any output, and a bad cell anywhere throws
  (`XlsxReader.cs:104`, `:112`, `:196`, `:204`, `:217`); and a sheet's header row needs every
  column the sheet uses (`MarkdownRenderer.cs:332-333`). Media are held as byte arrays until the
  end, which grows once embedded packages and OLE payloads are media. docproc's zip-bomb check
  bounds a hostile file, not an honest large one.
- **Proposed design:** Read each worksheet part twice with the SDK's forward-only part reader.
  The first pass, over the whole workbook, validates every cell and collects per sheet its used
  columns, its shared-formula anchors (already a separate full pass, lines 155-169), and its
  merges, and writes nothing. The second writes the `sheet` line and each `rows` line as its rows
  are read. Memory is then the shared strings and one line. Media are hashed from their part
  streams in the first pass and copied to files in the second. docx and pptx keep the whole-model
  path, since their `document` line is whole by contract. If the review defers this, the README
  states the measured ceiling and docproc treats a killed tool as a failure of the part.
- **Carried by:** ooxml; roadmap if deferred; acceptance measures it over ~/docs either way

### OX-3 · stdout's bytes depend on the process's locale
- **Kind:** issue
- **Weight:** high
- **Where:** `src/…/Program.cs:4`; `src/…/Cli.cs:127`
- **Against:** layers.md, "Common envelope", Determinism; system-tools.md ("promises byte-identical
  output under test")
- **Finding:** Output goes through `Console.Out`, which .NET encodes by the charset the locale
  variables name. `fixtures/realworld/docx/ruby.docx --format markdown` writes `東京\n` (7 bytes)
  under a UTF-8 locale, under `LANG=C`, and under an empty environment, and `??\n` (3 bytes) under
  `LANG=en_US.ISO-8859-1 LC_ALL=en_US.ISO-8859-1`, with exit code 0 and nothing on stderr. Every
  test runs under the developer's or the runner's UTF-8 locale, so no golden can catch it.
- **Proposed design:** Write UTF-8 bytes to the standard output stream directly, never through
  `Console.Out`, and stderr the same way. One CLI test runs a non-ASCII fixture under
  `LC_ALL=en_US.ISO-8859-1` and compares it with its golden.
- **Carried by:** ooxml

### OX-4 · stderr lines and exit codes differ from the envelope, and the change list doesn't list them
- **Kind:** issue
- **Weight:** medium
- **Where:** `src/…/Cli.cs:16-18`, `:60-69`, `:82-85`, `:103`, `:108`, `:123`, `:127`;
  `src/…/Program.cs:4`; `test/SpikeAiPipeline.Office.Tests/CliTests.cs:418-432`
- **Against:** layers.md, "Common envelope", stderr and Exit codes; the change list's Envelope
  bullet, which names `line`, `schema`, snake_case, and media names only
- **Finding:**
  - Failures are labelled `refused:`, `failed:`, and `media not written:`, where the envelope has
    one label, `error:`. processing's `internal/cli` carries a `Labels` list for office alone.
  - A usage error prints System.CommandLine's messages with no tool name or label (lines 60-69).
  - A warning's `<where>` is the input path (line 108). A path can hold `": "`, which the
    envelope forbids, and it differs by machine, so a `.stderr` golden can't be compared byte for
    byte. mail's `<where>` is a place inside the input.
  - Exit code 1 means media only. A stdout that can't be written exits 4: `> /dev/full` gave
    `failed: … System.IO.IOException: No space left on device`, exit 4. The test at
    `CliTests.cs:418-432` asserts that mapping.
  - A reader that closes the pipe early gets exit 0 over truncated output (`| head -c 10`, exit
    0), since .NET's console stream drops a broken-pipe error.
  - With stdout closed (`>&-`) the process aborts with a stack trace and exit 134, before
    `Cli.Run`, where `Program.cs` first touches `Console.Out`.
- **Proposed design:** One stderr writer with the tool's name as one constant (it is spelled out
  five times today). `error:` for codes 1 to 4, usage included. `<where>` is a place in the
  document: `section 3`, or `sheet 2` by ordinal, since a sheet's name can hold `": "`. An I/O
  error on stdout is exit 1, written through the raw stream so a broken pipe surfaces. The entry
  point catches everything, so no fault leaves as 134. The test at lines 418-432 changes its
  expectation to 1.
- **Carried by:** ooxml; library drops `Labels` from `internal/cli`

### OX-5 · Embedded and OLE media are a larger change than "Added" reads
- **Kind:** issue
- **Weight:** medium
- **Where:** `src/…/Reading/DocxReader.cs:505-507`, `:566-575`;
  `src/…/Reading/PptxReader.cs:82-97`, `:155-165`; `src/…/Reading/XlsxReader.cs:57-81`;
  `src/…/Reading/ChartReader.cs:10-13`; `src/…/Reading/MediaStore.cs:40-60`
- **Against:** layers.md, "docproc-ooxml" (media: "`ole`, the native payload") against its change
  list ("`ole` payloads … written as stored"); "Common envelope", Media ("the Markdown links it by
  that name")
- **Finding:**
  - The two passages disagree on what an `ole` file is. The native payload sits inside the part's
    OLE compound file, which the Open XML SDK doesn't read, so that reading needs a compound-file
    reader the tool doesn't have. The roadmap summary says "as stored".
  - Nothing reads these objects today except a docx's preview picture (lines 505-507). xlsx reads
    only the drawing part, never the worksheet's `oleObjects`. pptx's `ReadGraphic` takes tables
    and charts, so an OLE graphic frame is dropped, its picture included, and pptx skips
    `mc:AlternateContent` unless it is a chartEx chart (line 88), which is where PowerPoint often
    wraps an OLE object. `prog_id` is an attribute of three different elements across the formats.
  - Every chart part has its source workbook as an embedded package. Nine committed fixtures hold
    one (`chartex.docx` holds six). Listed as `embedded` media, each becomes a child part in
    docproc and the chart's data is extracted twice, against "every piece of text exists once".
    `ChartReader` already says it never reads that workbook.
  - No text says how the Markdown links a payload that isn't a picture.
  - A media's `sheet` index counts the sheets written, not the workbook's, once a sheet is
    skipped with a warning (`XlsxReader.cs:32-36`, `:39`).
- **Proposed design:** `ole` is the part as stored, and layers.md's "native payload" is corrected;
  unwrapping a compound file is docproc's, whose inspection already recognises one. A chart's
  embedded workbook is never media. Payloads are found through each story part's, sheet's, and
  slide's own embedded-package and embedded-object relationships, which gives the xlsx and pptx
  placement directly, and `prog_id` comes from the element that names the same relationship. The
  Markdown links a payload as `[<prog_id, or the part's file name>](<file>)` after its preview
  picture. `sheet` is the ordinal of the `sheet` line. Generated xlsx and pptx fixtures gain an
  embedded package and an OLE object; only `word-with-attachments.docx` has real ones now.
- **Carried by:** ooxml, with layers.md's correction in design

### OX-6 · The real-world files have no golden, and their CLI test compares the tool with itself
- **Kind:** issue
- **Weight:** medium
- **Where:** `test/SpikeAiPipeline.Office.Tests/CliTests.cs:158-173`;
  `test/SpikeAiPipeline.Office.Tests/Fixtures.cs:132-137`;
  `test/SpikeAiPipeline.Office.Tests/ConverterTests.cs:27-65`, `:332-340`;
  `test/SpikeAiPipeline.Office.Tests/GroundTruth.cs`
- **Against:** layers.md, "Common envelope", Determinism, and the change list's Goldens bullet;
  tests-and-docs.md ("A test proves a behavior only if it can fail")
- **Finding:** `Expected/` holds exact output for the 42 generated variants. The 30 real-world
  files, the only ones Word, Excel, and PowerPoint wrote, have none.
  `ARealWorldFileConvertsToStdoutAlone` computes its expected text in the test with the same
  reader and renderer, so it proves the NativeAOT binary agrees with the in-process build and
  can't fail when both are wrong alike. The Goldens bullet names `Expected/*.json` and `*.md`,
  which reads as the 42. The 72 `truth.json` files describe the block tree, which leaves the
  contract, and the change list doesn't say what becomes of them or of the 653 lines of
  `GroundTruth.cs` and `ConverterTests.cs` that compare against them.
- **Proposed design:** One `.jsonl` and one `.stderr` for each of the 72 files, compared on the
  published binary, one golden per file with no shared-file fallback (`Fixtures.Expected`). A
  `golden` task re-records them and the check never does. The truth tests stay as tests of the
  internal model, since the truth is independent of the tool and a golden is only what the tool
  once wrote.
- **Carried by:** ooxml

### OX-7 · With the block tree cut, the model's JSON machinery can go
- **Kind:** simplification
- **Weight:** medium
- **Where:** `src/…/Model/Document.cs:18`, `:67`, `:96-105`, `:114-180`, `:244`, `:270`,
  `:306-311`; `src/…/Model/DocumentJson.cs:1-63`; `src/…/Model/InlineText.cs`;
  `src/…/Reading/OfficeReader.cs:37`; `src/…/Cli.cs:32`
- **Against:** none: simplification
- **Finding:** Once the tree is no longer written, the model is only the renderer's input. What
  serializes it is then unused: the polymorphic type discriminators on `Block` and `Inline`, the
  property-order attributes, the enum converters, and the source-generated context with its depth
  limit of 128 and the comment that derives it. What is written is three flat shapes. Three
  public members exist for the tests alone: `InlineText.Of`, `OfficeReader.Read`, and
  `Cli.Version`.
- **Proposed design:** The model's types lose their JSON attributes and become internal, visible
  to the test project. One writer on `Utf8JsonWriter` writes the `document`, `sheet`, and `rows`
  lines with each name and its order spelled out, taking in the cell-value converter. That leaves
  no naming policy and no source generator, and the field order, which byte-identical output
  depends on, sits in one file. It is also where a `rows` line is measured against 256 KiB.
- **Carried by:** ooxml

### OX-8 · System.CommandLine can be dropped instead of migrated
- **Kind:** simplification
- **Weight:** medium
- **Where:** `src/…/SpikeAiPipeline.Office.csproj:17`; `src/…/Cli.cs:36-71`, `:74-76`
- **Against:** minimal-footprint.md; the change list's Toolchain bullet makes this harder than it
  needs to be
- **Finding:** With `--format` gone the command line is one input, `--media <dir>`, `--version`,
  and `--help`. The code already works around the library for that much: it prints the parse
  errors itself (lines 60-69) and catches every exception so the library's own handler never maps
  one to exit code 1 (lines 74-76). The change list schedules a major-version migration, 2.0.14
  to 3.0.0, as the task's first step.
- **Proposed design:** Parse the arguments by hand, about 30 lines, with the usage text a
  constant. DocumentFormat.OpenXml is then the tool's only package. This reverses a plan decision
  (the goal record's "moves to .NET 11 and System.CommandLine 3.0.0"), so it is the architect's
  call; the .NET 11 move stands either way.
- **Carried by:** ooxml

### OX-9 · A published advisory fails the check and the release without a code change
- **Kind:** issue
- **Weight:** medium
- **Where:** `Directory.Build.props:8`; `mise.toml:19`, `:26`; `.github/workflows/release.yml:48`
- **Against:** release-and-ci.md ("One check per repository": read-only and deterministic;
  "Currency is not a CI gate"); layers.md, "Payload measurement" (`-p:NuGetAudit=false`)
- **Finding:** Restore audits packages against the advisory feed and warns, and warnings are
  errors in every project. layers.md records the result: the v0.1.0 tag stopped building once
  Microsoft.DotNet.ILCompiler 10.0.13 got an advisory. The same commit passes one day and fails
  the next, in the check, in CI, and in the release workflow. The move to .NET 11 clears that
  advisory and leaves the mechanism in place.
- **Proposed design:** The audit is off in the check's and the release's builds and runs in
  `currency`, which already reads the network and is never a gate.
- **Carried by:** ooxml

### OX-10 · What nesting under `tools/ooxml` has to change
- **Kind:** issue
- **Weight:** medium
- **Where:** `.github/workflows/ci.yml:26`; `.github/workflows/release.yml:13`, `:54`, `:63`,
  `:95`; `mise.toml:16-28`; `SpikeAiPipeline.Office.slnx`;
  `src/…/SpikeAiPipeline.Office.csproj:8-9`;
  `test/SpikeAiPipeline.Office.Tests/Fixtures.cs:143`;
  `test/SpikeAiPipeline.Office.Tests/CliTests.cs:15`; `CLAUDE.md`; `STANDARDS.md`
- **Against:** independent-releases.md; release-and-ci.md ("Releases", "One check per repository");
  data-intelligence.md (each tool pinned by its `tools/<format>/v<version>` tag)
- **Finding:**
  - Workflows live only at a repository's root. The release workflow triggers on `v*.*.*`, takes
    the version as `${TAG#v}`, names its archives `spike-ai-pipeline-office-…`, and reads the root
    `CHANGELOG.md`. Under docproc the tag is `tools/ooxml/v0.1.0`, and Go module tags (`v*`,
    `storage/v*`, `cmd/docproc/v*`) must not start a binary build.
  - Go Elemental's CI is one `check` job with no matrix. This tool needs native x64 and arm64
    runners, since a NativeAOT publish can't cross-link (findings, "Footprint"), plus clang and
    zlib.
  - docproc's root `mise.toml` pins Go and loops `GO_MODULES`. The spike's tasks need dotnet, jq,
    shellcheck, and uv, and its `check` starts with `cd "$MISE_PROJECT_ROOT"`, which has to mean
    `tools/ooxml` under a nested config; that is unverified.
  - Names to change: the assembly and binary, the root namespace, the solution and project
    directories, the solution-file marker the tests find their root by, the binary path in
    `CliTests`, and "office" as the tool's name in messages and the README.
  - Repository-level files that don't nest: `CLAUDE.md`, `.claude/`, `.github/rulesets/`,
    `LICENSE`, `STANDARDS.md` (its three lines join docproc's tools part), and
    `context/README.md`.
- **Proposed design:** `tools/ooxml/` holds `src/`, `test/`, `fixtures/`, its own `mise.toml`
  with the toolchain pins and the tasks `check`, `currency`, `fixtures`, and `golden`, its
  `CHANGELOG.md`, `README.md`, and `.gitignore`. Names: `DocprocOoxml.slnx`,
  `src/Docproc.Ooxml`, namespace `Docproc.Ooxml`, binary `docproc-ooxml`. One root workflow
  releases a tool from a `tools/*/v*` tag, deriving the directory, changelog, and version
  (`${TAG##*/v}`) from the tag as Go Elemental's sub-module releases do, and writes
  `docproc-ooxml-v<version>-<rid>.tar.gz` with `SHA256SUMS` and the repository's LICENSE. CI gains
  a `tools` job, a matrix of tool by runner, beside `check`. The review decides whether the
  root's `mise run check` also runs each tool's check or stays Go-only.
- **Carried by:** ooxml for the tool's directory; library for the root's workflows and mise tasks

### OX-11 · Hyperlink targets are not read, and unread content leaves no trace
- **Kind:** issue
- **Weight:** medium
- **Where:** `src/…/Reading/DocxReader.cs:422-425`; `src/…/Reading/PptxReader.cs:271-292`;
  `src/…/Reading/XlsxReader.cs:87-152`; `README.md`, "What office does not do"
- **Against:** layers.md, "Consumers of structured text" ("A link target the Markdown lacks is
  text, rendered as a Markdown link"), which the onenote rows apply
- **Finding:** A docx hyperlink is read through for its text and its target is dropped. A pptx
  run's link and an xlsx cell's are never looked at. The README's list of what is not extracted
  doesn't name links. 4 of the 85 packages in ~/docs hold hyperlink relationships. The kinds the
  README does list (text boxes, in 1 of the 85; equations; SmartArt; pptx comments; xlsx cell
  notes) are skipped silently, and once the tree is cut nothing else shows that a document's text
  layer is partial.
- **Proposed design:** External hyperlinks render as `[text](target)` in all three formats, and an
  internal anchor stays text. Each unread kind a document holds gets one warning, such as
  `slide 4: comments not read`, so docproc records it. Presence is known from the part types for
  pptx comments, xlsx comments, and SmartArt, which the reader already enumerates
  (`OfficeReader.cs:94`). Reading those kinds stays backlog.
- **Carried by:** ooxml; roadmap for the unread kinds

### OX-12 · The baseline harness need not be promoted
- **Kind:** simplification
- **Weight:** medium
- **Where:** `bench/` (1,536 lines of C#, `rusage.c`); `scripts/baseline.sh`;
  `test/SpikeAiPipeline.Office.Baseline.Tests/` (268 lines);
  `SpikeAiPipeline.Office.slnx:3`, `:9`;
  `bench/SpikeAiPipeline.Office.Baseline/SpikeAiPipeline.Office.Baseline.csproj:14`;
  `docs/findings/office-cli/runs/`
- **Against:** none: simplification; minimal-footprint.md for Markdig
- **Finding:** The harness scored office's Markdown against the truth and compared it with
  markitdown and xberg. The README calls the comparison "measured once and is closed". The check
  still builds the harness and runs its tests, and Markdig is a second package to keep current,
  for a measurement that never runs in CI.
- **Proposed design:** `bench/`, its tests, `baseline.sh`, and `docs/findings/` stay in the spike
  as its evidence. docproc-ooxml takes `src/`, the tool's tests, and `fixtures/` with
  `generate.py`. The acceptance task's rerun of ~/docs is the measurement from then on.
- **Carried by:** ooxml

### OX-13 · `properties` "typed as stored" needs its own reading rule
- **Kind:** issue
- **Weight:** medium
- **Where:** new code; `src/…/Reading/OfficeReader.cs:58-84`, `:187-188` for how a parse failure
  is mapped today
- **Against:** layers.md, "docproc-ooxml" (`properties`: "each value typed as stored"); the
  capstone's `metadata` task, its consumer
- **Finding:** The change list treats this as one added field. The SDK's package properties give
  core dates as parsed date values, not the stored text, so writing them means re-formatting.
  The extended properties hold numbers as element text. Custom properties carry typed variants
  (string, integer, real, boolean, file time). A properties part that fails to parse would take
  the existing path and refuse a document whose text is fine. None of this was run; it follows
  from the SDK's types and needs tests to fix the behavior.
- **Proposed design:** Read the three parts as XML, as `ChartReader` reads a chart part. Core
  values are strings as stored, dates included. An extended value is a number when it is all
  digits. A custom value takes its variant's type, a file time as its stored ISO text. Keys keep
  their stored order, and a repeated key keeps its first value with a warning. A properties part
  that can't be parsed is a warning, never a refusal. The thumbnail is not media. layers.md
  states the rule.
- **Carried by:** ooxml

### OX-14 · office's Markdown escaping is not the rule the envelope says the tools share
- **Kind:** issue
- **Weight:** low
- **Where:** `src/…/Rendering/MarkdownRenderer.cs:568-593`
- **Against:** layers.md, "Common envelope", Text layer ("Literal text is escaped by htmd's rules,
  which mail's and onenote's Markdown share")
- **Finding:** office escapes `<` and `>` wherever they occur and leaves `$` alone. onenote's
  `escape` (`spike-ai-pipeline-onenote/src/markdown.rs:451-478`) escapes `$`, escapes `<` only
  before something tag-like, and `>` only at a line's start. Both read back as the literal text,
  but they are two rules, and the ooxml change list names no change to escaping.
- **Proposed design:** layers.md lists the escaped characters and ooxml's `Escape` matches the
  list, which costs nothing extra while every golden is re-recorded. Or the envelope says each
  tool's escaping is its own.
- **Carried by:** ooxml, with the layers.md wording in design

### OX-15 · A chart inside a docx table cell keeps only its title
- **Kind:** issue
- **Weight:** low
- **Where:** `src/…/Rendering/MarkdownRenderer.cs:487-489`
- **Against:** layers.md, "Consumers of structured text" (chart series are cut because "the
  Markdown's table holds them")
- **Finding:** `CollectLines` flattens a cell's blocks to lines and, for a chart, adds its title
  alone. Its series names, categories, and values were in the JSON tree and nowhere else. With
  the tree cut they are lost.
- **Proposed design:** A chart found in a table cell renders as its own chunk after the table, its
  title left in the cell.
- **Carried by:** ooxml

### OX-16 · A private-use character can be replaced by a footnote label
- **Kind:** issue
- **Weight:** low
- **Where:** `src/…/Rendering/MarkdownRenderer.cs:530-536`, `:545-557`
- **Against:** none: correctness
- **Finding:** References stand in the text as U+E000 and up while it is split and escaped. Only
  U+E000 is cleared from the document's own text (line 530). A paragraph that holds U+E001 and
  two references has that character replaced by the second reference's label, and U+E000 in any
  docx or pptx text becomes a space. Found by reading the code, not run.
- **Proposed design:** Render a paragraph's inlines as segments, escaping each text run and
  appending each label, with no placeholder. Failing that, clear the whole range and test it.
- **Carried by:** ooxml

### OX-17 · An elapsed-time cell past a day is written as a date in 1900
- **Kind:** issue
- **Weight:** low
- **Where:** `src/…/Reading/XlsxReader.cs:478-486`, `:567-602`
- **Against:** layers.md, `rows` (`value_type`, `value`), which `athena-mapping` reads as typed
  cells
- **Finding:** A cell of 1.5 in the format `[h]:mm`, 36 hours, comes out as
  `"valueType": "date", "value": "1900-01-01T12:00:00"`. `SerialDate.Iso` writes a time alone only
  under one day and otherwise adds the epoch, whether or not the format shows a date.
- **Proposed design:** A value in an elapsed format (`[h]`, `[m]`, `[s]`) stays a number, as
  stored.
- **Carried by:** ooxml

### OX-18 · SpreadsheetML's `_xHHHH_` escapes are written literally
- **Kind:** issue
- **Weight:** low
- **Where:** `src/…/Reading/XlsxReader.cs:227-245`
- **Against:** none: correctness
- **Finding:** A string cell holding `line1_x000D_` and a line break is written as
  `"line1_x000D_\nline2"`, and in Markdown as `line1\_x000D\_<br>line2`. Excel stores a carriage
  return and other control characters in that form.
- **Proposed design:** Decode `_xHHHH_` in shared and inline strings, `_x005F_` being the escaped
  underscore, with a generated fixture cell for it.
- **Carried by:** ooxml

### OX-19 · Two refusals give the wrong reason
- **Kind:** issue
- **Weight:** low
- **Where:** `src/…/Reading/OfficeReader.cs:44`, `:168`
- **Against:** validation-first.md ("The failure names the cause")
- **Finding:** A directory is refused as `/tmp: no such file`. An xlsb package matches `ms-excel`
  in its content type, is opened as xlsx, and is refused as `a malformed package: Invalid
  character in the given encoding`, not as an unsupported format. Both exit 3, which is right.
- **Proposed design:** A path that isn't a regular file is "not a file". The content-type match
  takes the main parts' exact types, so xlsb falls to "not a docx, xlsx, or pptx package".
- **Carried by:** ooxml

### OX-20 · The binary needs glibc, and no page says which
- **Kind:** issue
- **Weight:** low
- **Where:** `.github/workflows/release.yml:27-29`; `README.md` ("no .NET runtime needed on the
  host")
- **Against:** data-intelligence.md, `tool-packaging` (the tools inside Cerebro's image);
  system-tools.md ("Startup fails, not the first request")
- **Finding:** The published binary links `libc.so.6` and `libm.so.6` dynamically, and the build
  on this laptop references symbols up to GLIBC_2.34. docproc-mail and docproc-onenote are static
  musl binaries, so this tool alone constrains the image's base: it won't start on Alpine or on
  an image with an older glibc.
- **Proposed design:** The README states the glibc floor and the release job checks it on the
  built binary. A static musl publish is the alternative if `tool-packaging` wants a base without
  glibc.
- **Carried by:** ooxml; the capstone's `tool-packaging` reads the floor
