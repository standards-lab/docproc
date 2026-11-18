# Changelog

All notable changes to `github.com/standards-lab/docproc/cmd/docproc`, the docproc command, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `cmd/docproc/vX.Y.Z` tag carries, and is the release's notes.
Its command line, run directory, and exit codes are its contract: a change to any of them is a
change here. This changelog covers the cmd/docproc module only; the base module, the storage and
postgres modules, and each system tool under `tools/` keep their own.

## [Unreleased]

The command is built from spike-ai-pipeline at 6a631f1, its `cmd/docproc` and its
`internal/docproc/app` and `internal/docproc/apptest` packages, with `internal/app` and
`internal/apptest` started again from go-cli-sdk-template's template/v0.3.0 on go-cli-sdk v0.4.0
and go-core v0.12.0, in place of the spike's port of go-cli-sdk v0.2.0's.

### Added

- The module. `main.go` sits at its root, so
  `go install github.com/standards-lab/docproc/cmd/docproc@<version>` installs `docproc`. Its
  composition root, `internal/app`, its fixtures, `internal/apptest`, and its command package,
  `internal/run`, sit under `internal/`, and `integration` holds the built-binary tier's harness.
  It requires the docproc base module at v0.2.0, go-cli-sdk, go-core, and golang.org/x/sys.
- `docproc version`, which prints the module's version, or `(devel)` for a binary that carries
  none, and builds nothing: it reads no configuration and looks up no tool.
- `docproc run <path>... --out <dir> [--workers N] [--exclude <route>]...`. It collects its
  inputs first: a file argument is one input, and a directory yields its regular files in lexical
  order, each once. It follows a named symbolic link and no link under a directory, and fails
  before any tool is looked up on an argument that is neither a file nor a directory, or on
  arguments that yield no input. It then builds the stages over the host's tools under the run's
  bounds, leaving out each route `--exclude` names, prints the tools and the stage table, and
  dispatches the inputs, `--workers` at a time (at least 1, the host's CPUs by default), into a
  schema "2" run directory through `rundir`. It writes the run's manifest; one record line per
  event, each input's own line carrying its file's metadata, sniffed type, and time; each
  output's content, to the run directory's content store; and each quarantined part's file, kept
  and named on its line. The terminal shows one line per input and a summary.
- The run's bounds, at docproc's starting values, each overridden by its `DOCPROC_` variable: the
  tools' memory bound, unit timeout, and stream idle bound; a dispatch's most parts, bytes, and
  depth; the bomb rule's ratio and size and an archive's most entries; a claim's release bound;
  the pages and windows held at once; a PDF's DPI, text layer, and most pixels; a video frame's
  largest side; and ImageMagick's area, memory, map, and disk limits. A value the command can't
  take fails the run before any tool is looked up, and the error names every such variable.
  `DOCPROC_SHUTDOWN_TIMEOUT` bounds the shutdown. The base module's
  [README](../../README.md#configuration) lists each variable with its bound and starting value.
- Exit statuses: 0 when the run ends, failed parts and inputs stopped with an error included,
  the summary's last line counting both, a failure replayed from the index among the failed
  parts; 1 when it stops or never begins, a failure of the index or the content store or a
  cancellation ending the run's manifest with the error; and 2 for help and every usage error,
  which build nothing.
- The built-binary integration tier, under the `integration` build tag, run by
  `mise run test:integration`: it builds `docproc` and runs it over the repository's committed
  fixtures, asserting its arguments, output, exit status, signals, and the run directory it
  writes, read back through `rundir`.
