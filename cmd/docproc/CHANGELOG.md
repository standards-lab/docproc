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

- The module, with `main.go` at its root, so
  `go install github.com/standards-lab/docproc/cmd/docproc@<version>` installs `docproc`, and its
  composition root and test fixtures under `internal/`.
- `docproc version`, which prints the module's version, or `(devel)` for a binary that carries
  none, and builds nothing: it reads no configuration and looks up no tool.
- `docproc run <path>... --out <dir>`, which collects its inputs, a file argument as one and a
  directory's regular files in lexical order, each once, following a named symbolic link and no
  link under a directory, and fails before any tool is probed on an argument that is neither a
  file nor a directory or on arguments that yield no input; builds the stages over the host's
  tools under the starting bounds, `--exclude <route>` repeatable, and prints the stage table;
  and dispatches the inputs, `--workers` at a time, at least 1 and the host's CPUs by default,
  into a schema "2" run directory through `rundir`: one record line per event, each input's file
  and time on its own line, a quarantined part's file kept and named. Failed parts and error
  inputs exit 0, the summary's last line counting both; a failure of the index or the content
  store, or a cancellation, ends the run with the error and exits 1.
- Help, for the root and for each command, and every usage error exit 2.
