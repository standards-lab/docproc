# Changelog

All notable changes to `github.com/standards-lab/docproc/cmd/docproc`, the docproc command, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `cmd/docproc/vX.Y.Z` tag carries, and is the release's notes.
Its command line, run directory, and exit codes are its contract: a change to any of them is a
change here. This changelog covers the cmd/docproc module only; the base module, the storage and
postgres modules, and each system tool under `tools/` keep their own.

## [Unreleased]

## [v0.2.2] - 2026-11-23

This release requires the docproc base module at v0.4.2, whose default container images reach
`docproc run` as Changed says. Its command line, run directory, and exit codes are unchanged.

### Changed

- The module requires the docproc base module at v0.4.2, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full: soffice runs in
  `gotenberg/gotenberg:9.9.3-libreoffice`, which carries the same LibreOffice, 26.8.2.2, and the
  ffmpeg default is `linuxserver/ffmpeg:9.0.2-cli-ls90`, each pinned by its multi-arch index
  digest.

## [v0.2.1] - 2026-11-20

This release requires the docproc base module at v0.4.1, whose fixes reach `docproc run` as
Changed says. Its command line, run directory, and exit codes are unchanged.

### Changed

- The module requires the docproc base module at v0.4.1, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - Each tool run keeps its temporary files in a directory of its own under the host's `TMPDIR`,
    removed once the run's program is reaped, so a gs ended early leaves nothing beside its input.
    A host with a small tmpfs `/tmp` points `TMPDIR` at room enough.
  - A PDF portfolio's page is the portfolio's own page, one PNG and that page's text, where before
    it held each embedded document's pages. The stage adapters' version is 6, so a run over an
    index prepares its content again.
  - A ZIP holding an XPS package's `FixedDocumentSequence.fdseq`, whatever its name, is recorded
    as no processor yet, rather than failing in docproc-ooxml.
  - soffice's exit 77, a fatal error at its start such as a full disk, fails the part unrecorded,
    so a later run tries it again, rather than recording it failed.
  - docproc builds on 64-bit platforms only.

## [v0.2.0] - 2026-11-19

This release adds the audio window's variable and requires the docproc base module at v0.4.0,
whose changes reach `docproc run` as Changed says.

### Added

- `DOCPROC_AUDIO_LIMIT`, the longest audio window, its overlaps included, which an audio part's
  windows are cut under: `stages.Options.Audio`'s `Limit`. It starts at 30s, and a host sets it
  to its model's request size, the longest audio one request takes; a source no longer than it is
  one window. A value of 4s or less, which is not over twice the 2 s overlap between windows, zero
  among them, fails the run before any tool is looked up, as a value that doesn't parse does, the
  error naming the variable. The base module's
  [command page](../../docs/command.md#configuration) lists it.

### Changed

- The module requires the docproc base module at v0.4.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - `DOCPROC_MAX_BYTES` bounds what the archive and document stages write, not only what a
    dispatch admits: an archive's entry, or a PDF's attachment, is stopped at the write that would
    pass it, its partial file removed, and is recorded as an unread child, `limited`, `past the
    bytes cap, <cap>`, with no size or SHA-256, where before it was written whole and then
    limited.
  - A PDF's pages are rendered, and their text layers read, in gs processes shared up to 8 pages
    a process, at `DOCPROC_CONCURRENCY` lanes; a shared process reads `DOCPROC_UNIT_TIMEOUT` as an
    idle bound between its pages. Every page's PNG and text, and the run directory's records of
    them, are as before.
  - A page gs renders with a message of its own, such as one whose `/Annots` is null, is
    rendered, where before it failed with "no PNG on stdout".
  - Each tool's version is checked in the environment its runs get, so a version manager's shim
    that answers only under the host's `HOME` is listed as unavailable in the tools table, and
    the stages that need it are `missing`, where before its runs each failed.

## [v0.1.0] - 2026-11-18

The first release. The command is built from spike-ai-pipeline at 6a631f1, its `cmd/docproc` and
its `internal/docproc/app` and `internal/docproc/apptest` packages, with `internal/app` and
`internal/apptest` started again from go-cli-sdk-template's template/v0.3.0 on go-cli-sdk v0.4.0
and go-core v0.12.0, in place of the spike's port of go-cli-sdk v0.2.0's.

### Added

- The module. `main.go` sits at its root, so
  `go install github.com/standards-lab/docproc/cmd/docproc@<version>` installs `docproc`. Its
  composition root, `internal/app`, its fixtures, `internal/apptest`, and its command packages,
  `internal/run` and `internal/report`, sit under `internal/`, and `integration` holds the
  built-binary tier's harness. It requires the docproc base module at v0.3.0, go-cli-sdk,
  go-core, and golang.org/x/sys.
- `docproc version`, which prints the module's version, or `(devel)` for a binary that carries
  none, and builds nothing: it reads no configuration and looks up no tool.
- `docproc run <path>... --out <dir> [--workers N] [--exclude <route>]... [--report]`. It collects
  its inputs first: a file argument is one input, and a directory yields its regular files in
  lexical order, each once. It follows a named symbolic link and no link under a directory, and
  fails before any tool is looked up on an argument that is neither a file nor a directory, or on
  arguments that yield no input. It then builds the stages over the host's tools under the run's
  bounds, leaving out each route `--exclude` names, prints the tools and the stage table, and
  dispatches the inputs, `--workers` at a time (at least 1, the host's CPUs by default), into a
  schema "2" run directory through `rundir`. It writes the run's manifest; one record line per
  event, each input's own line carrying its file's metadata, sniffed type, and time; each output's
  content, to the run directory's content store; and each quarantined part's file, kept and named
  on its line. The terminal shows one line per input and a summary.
- `docproc report <run-dir> [--run <id>]`, which renders a run's report and prints its index
  page's path: static HTML pages in the run's report directory, `runs/<run>/report/`, replacing
  earlier ones whole. It renders the latest run that ended, or the run `--run` names, one that
  never ended among them, its pages marked not ended. It reads the run directory alone: it reads
  no configuration and looks up no tool. An index page, `index.html`, has the run's header, from
  its manifest, a summary counting the inputs and their parts by route and outcome, and every
  input, sorted by path, with its route, outcome, and time, linking its content's page. A page
  per distinct content, `<sha256>.html`, lists the inputs holding it, then its part tree, each
  part's record metadata, and its outputs: a text, Markdown, or record output inline, a PNG or
  JPEG image and a WAV window shown at its content's file, any other output linked, and a
  quarantined part's kept file named. Every link into the run directory is relative, so the run
  directory moves or archives whole, and the pages load nothing from elsewhere. Every time is the
  record's, in UTC, so the same run directory renders the same bytes in any zone.
- `docproc run --report`, which, once the summary is printed, renders the run's report and prints
  its index page's path, as `docproc report` does: after a run that ended, and after a failure of
  the index or the content store, which still exits 1; never after a cancellation.
- The run's bounds, at docproc's starting values, each overridden by its `DOCPROC_` variable: the
  tools' memory bound, unit timeout, and stream idle bound; a dispatch's most parts, bytes, and
  depth; the bomb rule's ratio and size and an archive's most entries; a claim's release bound;
  the pages and windows held at once; a PDF's DPI, text layer, and most pixels; a video frame's
  largest side; and ImageMagick's area, memory, map, and disk limits. A value the command can't
  take fails the run before any tool is looked up, and the error names every such variable.
  `DOCPROC_SHUTDOWN_TIMEOUT` bounds the shutdown. The base module's
  [command page](../../docs/command.md#configuration) lists each variable with its bound and
  starting value.
- `docproc run`'s exit statuses: 0 when the run ends, failed parts and inputs stopped with an
  error included, the summary's last line counting both, a failure replayed from the index among
  the failed parts; 1 when it stops or never begins, a failure of the index or the content store
  or a cancellation ending the run's manifest with the error, or, with `--report`, a run that
  ended whose pages couldn't be rendered, which `docproc report` renders again; and 2 for help and
  every usage error, which build nothing. `docproc report`'s: 0 when the report is rendered; 1
  when nothing is: a run directory that doesn't exist or isn't one, a `--run` that isn't one of
  its runs, no `--run` and no run that ended, or a page that couldn't be read or written; and 2
  for help and every usage error.
- The built-binary integration tier, under the `integration` build tag, run by
  `mise run test:integration`: it builds `docproc` and runs it over the repository's committed
  fixtures, asserting its arguments, output, exit status, signals, and the run directory it
  writes, read back through `rundir`, and the report it renders of a fixture run and of a killed
  run.

[Unreleased]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.2...HEAD
[v0.2.2]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.1...cmd/docproc/v0.2.2
[v0.2.1]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.0...cmd/docproc/v0.2.1
[v0.2.0]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.1.0...cmd/docproc/v0.2.0
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/cmd/docproc/v0.1.0
