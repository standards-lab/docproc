# design

The design review: Fable 5.2 evaluated each of the five artifacts v1.ai.processing promotes, and
the architect settled every finding in one review round per artifact, 2026-11-12. `layers.md`
holds the boundaries and the change lists; this note holds each finding's settled design and its
disposition, with the evidence that justified it, since the evaluations themselves are not kept.
An entry names the task that carries it, and the `layers.md` section that states it where one
does; it doesn't restate a signature that section holds.

## docproc-ooxml

From spike-ai-pipeline-office v0.1.0. Probes ran on the spike's published binary
(0.1.0+6ed0d0e, linux-x64). Every finding was accepted; the changes are in `layers.md`,
"docproc-ooxml [ooxml]" unless an entry says otherwise.

- **OX-1 · A sheet's charts and pictures had no line for their Markdown.** Accepted → ooxml. The
  renderer writes a sheet's chart tables and picture links after its cell table, and a chart
  sheet has only those, so no line held them. They ride on a closing `rows` line of that sheet
  with `markdown` and no `rows`, bounded as any `rows` line is; a chart sheet is a `sheet` line
  and that closing line. The lines' Markdown fragments concatenate with nothing between them,
  each ending in its own newlines, since the renderer's separators differ by place (`layers.md`,
  "docproc-ooxml"). Rejected: a new line kind for a sheet's drawing.
- **OX-2 · The tool held the whole workbook in memory.** Accepted → ooxml. One sheet of 500,000
  rows by 10 cells (17 MB on disk) took 25.5 s and 8,201 MiB peak; a killed tool exits 137, none
  of the contract's codes, and `rows` lines bound only the reader. Each worksheet part is read
  twice, forward-only: a validate pass that writes nothing collects each sheet's used columns,
  shared-formula anchors, and merges, and its media's hashes, so a refusal still comes before any
  output; a write pass emits the `sheet` line and each `rows` line as its rows are read. docx and
  pptx keep the whole-model path, their `document` line being whole by contract. Acceptance
  measures peak memory over ~/docs. Rejected: deferring with a stated ceiling.
- **OX-3 · stdout's bytes depended on the locale.** Accepted → ooxml. `ruby.docx` wrote `東京\n`
  under UTF-8, `LANG=C`, and an empty environment, and `??\n` under `LC_ALL=en_US.ISO-8859-1`,
  exit 0. stdout and stderr are written as UTF-8 bytes to the raw streams, never `Console.Out`,
  and a CLI test runs a non-ASCII fixture under that locale against its golden.
- **OX-4 · stderr labels and exit codes differed from the envelope.** Accepted → ooxml, and
  library drops the `Labels` list from `internal/cli` (`layers.md`, "docproc's packages
  [library]"). The spike wrote `refused:`, `failed:`, and `media not written:`; a usage error
  had no tool name; `<where>` was the input path, which can hold `": "`; `> /dev/full` exited 4;
  a closed pipe exited 0 over truncated output; a closed stdout aborted with 134. One stderr
  writer, the tool name one constant, `error:` for codes 1 to 4, usage included; `<where>` a
  place such as `sheet 2`, by ordinal; an I/O error on stdout exit 1, a broken pipe included; no
  fault leaves as 134.
- **OX-5 · Embedded and OLE media were a larger change than "Added" read.** Accepted → ooxml.
  `ole` media is the part as stored; unwrapping its compound file, which the Open XML SDK can't
  read, is docproc's, whose inspection already recognises one (`layers.md`, "docproc-ooxml",
  corrected from "the native payload"). A chart's embedded source workbook is never media: nine
  fixtures hold one, and as media its data would be extracted twice. Payloads are found through
  each story part's, sheet's, and slide's own embedded-package and embedded-object
  relationships, `prog_id` from the element naming that relationship; the Markdown links a
  payload as `[<prog_id or the part's file name>](<file>)` after its preview picture; a medium's
  `sheet` is the ordinal of the `sheet` line, where the spike counted sheets written. Generated
  xlsx and pptx fixtures gain an embedded package and an OLE object. Rejected: the tool
  extracting the native payload.
- **OX-6 · The real-world files had no golden.** Accepted → ooxml. The 30 files Word, Excel, and
  PowerPoint wrote were checked only against the tool's own in-process output. Each of the 72
  fixtures gets one `.jsonl` and `.stderr`, compared on the published binary, with no shared-file
  fallback; a `golden` task re-records them and the check never does; the truth tests stay as
  tests of the internal model.
- **OX-7 · The model's JSON machinery can go.** Accepted → ooxml. The model becomes internal and
  loses its JSON attributes, converters, and source-generated context; one `Utf8JsonWriter`
  writer spells out the `document`, `sheet`, and `rows` lines' fields in order and measures a
  `rows` line against 256 KiB.
- **OX-8 · System.CommandLine dropped.** Accepted → ooxml. The command line is one input,
  `--media`, `--version`, and `--help`, and the spike already printed parse errors and caught
  exceptions around the library. Arguments are parsed by hand, the usage text a constant;
  DocumentFormat.OpenXml is the only package; .NET 11 stands. Reverses the plan decision
  "System.CommandLine 3.0.0".
- **OX-9 · A published advisory failed the check without a code change.** Accepted → ooxml. The
  v0.1.0 tag stopped building when ILCompiler 10.0.13 got an advisory. NuGet's audit is off in
  the check's and the release's builds and runs in `currency`, never a gate.
- **OX-10 · Nesting under `tools/ooxml`.** Accepted → ooxml, with library's part in
  "docproc's packages [library]". `tools/ooxml/` holds `src/`, `test/`, `fixtures/`, its own
  `mise.toml` (toolchain pins; `check`, `currency`, `fixtures`, `golden`), `CHANGELOG.md`,
  `README.md`, and `.gitignore`; names `DocprocOoxml.slnx`, `src/Docproc.Ooxml`, namespace
  `Docproc.Ooxml`, binary `docproc-ooxml`. docproc's root `mise run check` runs each tool's own
  check: the root `mise.toml` pins the tools' toolchains and loops a `TOOLS` list beside
  `GO_MODULES`. CI splits the same checks into a Go job and a tool-by-runner matrix on native
  x64 and arm64 runners, since a NativeAOT publish can't cross-link. The ooxml task builds
  docproc's CI and the one root workflow releasing `tools/*/v*` tags, deriving the directory,
  changelog, and version (`${TAG##*/v}`) from the tag and writing
  `docproc-ooxml-v<version>-<rid>.tar.gz` with `SHA256SUMS` and the repository's LICENSE; mail
  and onenote add a matrix row each; library adds the Go job and the Go module releases.
- **OX-11 · Hyperlink targets unread; unread content left no trace.** Accepted → ooxml. External
  hyperlinks render as `[text](target)` in all three formats, an internal anchor staying text;
  4 of the 85 packages in ~/docs hold hyperlinks. Each unread kind a document holds (text boxes,
  equations, SmartArt, pptx comments, xlsx notes) gets one warning. Reading those kinds stays
  backlog.
- **OX-12 · The baseline harness isn't promoted.** Accepted → ooxml. `bench/`, its tests,
  `baseline.sh`, and `docs/findings/` stay in the spike as its evidence, and Markdig with them;
  the acceptance rerun over ~/docs is the measurement from then on.
- **OX-13 · `properties` typed as stored needs a reading rule.** Accepted → ooxml. The SDK parses
  core dates and hides the stored text, and a properties part that fails to parse would refuse a
  readable document. The rule is in `layers.md`, "docproc-ooxml".
- **OX-14 · office's escaping wasn't the shared rule.** Accepted → ooxml. office escaped `<` and
  `>` everywhere and left `$`; onenote follows htmd. The escaped characters are listed in
  `layers.md`, "Common envelope", and ooxml's escaping matches them while every golden is
  re-recorded.
- **OX-15 · A chart in a docx table cell kept only its title.** Accepted → ooxml. It renders as
  its own chunk after the table, its title left in the cell.
- **OX-16 · A private-use character could become a footnote label.** Accepted → ooxml. Found by
  reading: only U+E000 was cleared from the text, the placeholders running from U+E000 up. A
  paragraph renders as segments, each text run escaped and each label appended, with no
  placeholder.
- **OX-17 · An elapsed time past a day became a date in 1900.** Accepted → ooxml. 1.5 in `[h]:mm`
  came out as `1900-01-01T12:00:00`. A value in an elapsed format (`[h]`, `[m]`, `[s]`) stays a
  number, as stored.
- **OX-18 · `_xHHHH_` escapes were written literally.** Accepted → ooxml. Shared and inline
  strings decode them, `_x005F_` the escaped underscore, with a generated fixture cell.
- **OX-19 · Two refusals gave the wrong reason.** Accepted → ooxml. A directory was refused as "no
  such file" and an xlsb as a malformed package. A path that isn't a regular file is "not a
  file", and the main part's exact content type decides the format, so xlsb is "not a docx,
  xlsx, or pptx package".
- **OX-20 · The binary needs glibc.** Accepted → ooxml. The build links libc and libm
  dynamically, with symbols up to GLIBC_2.34, where mail and onenote are static musl. The README
  states the glibc floor and the release job checks it on the built binary; a static musl
  publish is the capstone's `tool-packaging` call.

The remaining artifacts' reviews follow in their own sections.
