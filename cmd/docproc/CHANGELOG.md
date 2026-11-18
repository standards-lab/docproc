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
- Help, for the root and for each command, and every usage error exit 2.
