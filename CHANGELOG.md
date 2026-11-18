# Changelog

All notable changes to `github.com/standards-lab/docproc`, docproc's Go base module, are documented
here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is
headed by the version its `vX.Y.Z` tag carries, and is the release's notes. Its exported API is its
contract: a change to it is a change here. This changelog covers the base module only; each system
tool under `tools/` keeps its own.

## [Unreleased]

## [v0.3.0] - 2026-11-18

This release names a run's report directory, which `cmd/docproc`'s report renders into. The
module still requires golang.org/x/text alone.

### Added

- `rundir.Dir.ReportDir`, a run's report directory, `runs/<run>/report`, relative to the run
  directory, which schema `"2"`'s layout now names. A reader from `Open` answers it, as does a
  writer for the run it is writing; it refuses an id that isn't a run of the directory, and it
  makes nothing: a report's writer makes the directory.

## [v0.2.0] - 2026-11-18

This release adds the run directory's packages, moved from spike-ai-pipeline's
`internal/docproc/rundir` and `internal/docproc/schema` at 6a631f1 and made public, so
`cmd/docproc` writes a run directory and a reader in another module, such as `cmd/docinfer`,
reads it. The module still requires golang.org/x/text alone.

### Added

- `rundir`, the run directory's one handle. `Open` reads a run directory and creates nothing;
  `Create` makes the directory, its `rundir.json`, the index, and the content store. Each refuses
  a run directory of another schema, or one holding `runs/` with no `rundir.json`, which schema
  `"1"` wrote. A reader has `Dir.Runs`; `Latest`, the latest run that ended; `Manifest`; `Record`,
  a run's lines as an `iter.Seq2` that yields `ErrNotEnded` last for a run that hasn't ended;
  `Content`; and `Kept`, a quarantined part's kept file. `ContentPath` returns a content's file
  relative to the run directory. A writer has `Dir.Index`; `Begin`, which begins a `Run` and
  removes the working directories a killed run left; and `Quarantine`, which keeps a quarantined
  part's file once, at `quarantine/<sha256>`. A `Run` appends one record line per `Write`, each
  written through, and `End` records its end and its error. Every method that takes a run's id
  refuses one that isn't a run of the directory. One process writes a run directory.
- `rundir/schema`, the run directory's schema `"2"`, which holds shapes alone. `Event` is a record
  line of kind part, output, warning, or error, with its `Span`. `Manifest` holds a run's docproc
  `Build`, its start and end, its workers, its path arguments and how many inputs they named, the
  tools and the stage table as `dispatch.Tool` and `dispatch.Row`, and the error it ended with.
  `File` holds an input's file metadata and its sniffed type. `Time`, which `TimeOf` makes, is a
  time in UTC. The schema includes `dispatch`'s shapes at `dispatch.Version` `"1"`, aliased, and
  the package imports no docproc package but `dispatch` and `inspect`.

## [v0.1.0] - 2026-11-16

The first release. The base module was copied, in one commit, from spike-ai-pipeline-processing
v0.9.0 (4402f7c), its packages, and from spike-ai-pipeline at 6a631f1, its
`internal/docproc/stages` package, which becomes `stages`; the module path and the import paths
changed and nothing else. Changed says how it differs from the spike. The spike's findings and
bake-off tiers, its corpus, vision, and audio test helpers, its document experiment surface,
media's live sources, silencedetect, every test that imports spike-ai-pipeline-client, and its
fixtures scripts stay in the spike.

### Added

- The packages `route`, `inspect`, `tool`, `office`, `email`, `onenote`, `document`, `picture`,
  `audio`, `media`, `sevenzip`, `dispatch`, `dispatch/dispatchtest`, and `stages`, with
  `internal/cli`, `internal/goldentest`, `internal/mediatest`, and `internal/tooltest`. The
  module requires golang.org/x/text alone, at v0.44.0, and no longer
  spike-ai-pipeline-client; its go directive is 1.27.4. It builds on every platform, and only
  the exec runner needs Linux. No package imports a model client.
- `stages.Build(ctx, lookup, Options)`, the shipped wiring: each route's stage over its format
  package and the tools it requires, found through the host's `Lookup` and checked against each
  package's minimum, a stage whose tool is missing or too old left out; its `Set` holds the
  stages, dispatch's tool table and stage rows, and the inspection options. `Options` carries
  every run bound (Timeout, Idle, Memory, Concurrency, Env), ImageMagick's limits, the
  inspection options, and each stage's request, none with a default but Concurrency, which is one
  when zero.
- One failure classification: a tool's own exit, its own fatal signal, its own time bound,
  output that doesn't parse, and a format package's content errors are the content's, a part's
  matching `dispatch.ErrRefused` and recorded, a unit's a `dispatch.Warning`; anything else is
  the run's, never recorded. A death by signal under a memory bound names both: "killed by
  SIGABRT under a 256 MiB memory bound".
- `dispatch`'s content store, `Content`, with `FileContent` and `MemoryContent`, and its index
  log, `Index.Items` and `Append` over a part's interleaved outputs, warnings, and children,
  with leases, renewals, and takeovers; `dispatch/dispatchtest`'s Index and Content conformance
  suites, which every store passes, and its fixture stage.
- dispatch's shapes as JSON, versioned by `dispatch.Version` "1": events, parts and their
  `ContentType`, outputs with their SHA-256, warnings, the tool table and stage rows, the Meta
  keys, and the record media types.
- `tool`: a run's facts, `ExitError.Signal` and `TimedOut`; `Spec.MemoryLimit`, applied by the
  exec runner through util-linux's `prlimit --as=<bytes> --` before the program runs, and by the
  container runner as `--memory`; `Spec.Timeout`, `Spec.Idle`, and `Spec.Env`; `Tool.AtLeast`
  and `Tool.VersionLine`.
- `internal/cli`, the envelope reader the three readers share: a tool's lines, schema `"2"`
  checked on the first, its stderr warnings parsed as they arrive, its exit codes and refusal
  classes, and its media names resolved to a path and SHA-256.
- `audio.Cutter`, the stream cutter `audio` and `media` share, and the audio and video plan
  records, `audio.PlanRecord` and `media.VideoPlan`.
- The unit tier on `internal/tooltest`'s stand-in tool, which reads no `~/docs` and runs no real
  program but util-linux's `prlimit`, for the memory bound; the readers replay each tool's committed
  goldens in place, skipping, by name, when the tools tree is absent. The integration tier, under
  the `integration` build tag, runs the real tools, docproc's three built from the same checkout,
  through `mise run test:integration`; its whole-fixture test checks every committed fixture's route
  and outcome through `stages`.

### Changed

- Constructors take a Config and panic by name on a nil required field; each package's refused
  request matches its one `ErrRequest`; every time returned or written is UTC.
- No bound, cap, or limit has a library default: inspect's bomb rule, the memory index's lease,
  and tool's kill delay are the host's, and the page and window concurrency is one at a time
  until the host raises it.
- `route` owns the extension table and the encrypted-package detector; a ZIP holding
  `[Content_Types].xml` routes as office whatever its name, and content of no known signature
  routes by a stored audio or video type.
- `inspect`'s per-format archive checks become seven rule names; a link, a device, and a
  traversing or absolute name are active, a bomb and an unreadable directory unsafe. The bomb
  rule weighs each entry and the archive's summed declared sizes, a ZIP whose declared
  compressed sizes pass its own size is a bomb, and a ZIP listing past `Options.Entries` is
  unreadable before it is opened. The XML scans decode a declared charset or a UTF-16 mark.
- `dispatch` walks an input from a path or a spooled stream, its SHA-256 checked, puts each
  output's content in order while the stage works, replays a seen part from its log, resumes an
  input after its cursor, and guards every string it writes. It imports `route` and `inspect`
  alone: the stage adapters move to `stages`, unexported.
- The `office`, `email`, and `onenote` readers decode docproc-ooxml's, docproc-mail's, and
  docproc-onenote's schema `"2"`, each at minimum 0.1.0, through `internal/cli`.
- `document`'s form never fails on a field's value, and a signature field reports Signed;
  `Images` writes each page to a file, a page past MaxPixels rendered at the largest DPI that
  fits.
- `picture` takes the caller's kind and carries ImageMagick's limits on every magick run;
  `Converter.RunLibreOffice` is the one LibreOffice runner, office's legacy and RTF conversion
  among its callers.
- `audio` decodes a source once, its windows cut as the stream arrives, v0.9.0's windows byte
  for byte under its tracker detector; `media` cuts with the same cutter.
- `sevenzip` reads a whole archive in one 7zz run, each item held to its listed size and
  CRC-32, and requires 7zz 24.07 or later.

### Removed

- `internal/textscore`, its speech harness included; the text scoring the document tests need
  is theirs, unexported.
- `NewScript`, the silencedetect and volumedetect detectors, `audio.Join` and the transcript
  join (docinfer's), `picture`'s Corrector, `document`'s region rendering and corrections, the
  stages' own tool table, the schema `"1"` decoders, and the per-stage refusal mappings.

### Known limits

The [README](README.md#limits) states them: each stage's tool's limits, docproc-ooxml's
behavior under a memory bound, the Threader's memory, and the formats no stage reads.

[Unreleased]: https://github.com/standards-lab/docproc/compare/v0.3.0...HEAD
[v0.3.0]: https://github.com/standards-lab/docproc/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/standards-lab/docproc/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/v0.1.0
