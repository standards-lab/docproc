# Development

The toolchain, Go, golangci-lint, actionlint, shellcheck, jq, and the integration tier's 7zz, is
pinned in [`mise.toml`](../mise.toml); each tool under `tools/` pins its own. Tasks run from the
repository root, `mise run <task>`:

- `check`: shellchecks the scripts, lints the workflows, and tests the release workflow's tag
  routing; builds, vets, format-checks, fixes, tidies, tests under the race detector, and lints
  every Go module, the base, both providers, and cmd/docproc, each with the workspace off, then
  sqlints the postgres module's SQL; then runs each tool's check. It is the unit tier: each
  package over its own testdata and a stand-in tool, the readers replaying the tools' committed
  goldens in place. It reads no network and runs no real tool but util-linux's `prlimit`, which
  the memory-bound tests need on PATH.
- `test:integration`: the integration tier, under the `integration` build tag. It builds
  docproc-ooxml, docproc-mail, and docproc-onenote from this checkout through their publish tasks
  and puts those three ahead of everything else on PATH, so no installed release stands in for
  them, and it runs 7zz at the root's pin. It then runs, in turn, the base module's tools tier,
  which runs the real tools and checks every committed fixture's route and outcome through
  `stages`; cmd/docproc's built-binary tier, which builds `docproc` with the race detector, runs
  it over the committed fixtures, and checks its arguments, output, exit status, signals, and the
  run directory it writes, read back through `rundir`; and `integration`, below. Run it on your
  machine before each Go tag; CI doesn't. The other programs come from the host, each at its minimum or later: `gs`, `qpdf`,
  `pdfdetach`, `ffmpeg`, `ffprobe`, `magick`, `soffice`, util-linux's `prlimit`, and `docker`,
  which runs the compose harness and pulls `audio.FFmpegImage` and `picture.LibreOfficeImage` for
  the tools tier.
- `integration`: the providers' live tiers, `integration:postgres` then `integration:storage`, every
  one even after one fails; CI runs it on a push to main that needs it and on demand.
  `integration:postgres` runs the postgres module's suite against the harness's PostgreSQL, and
  `integration:storage` the storage module's against its SeaweedFS. Each starts only its own
  service, runs its module's suite, and stops the service whether the suite passes or fails,
  printing its log on a failure.
- `currency` and `upgrade`: report, then bump, every root pin behind its latest: the root mise pins,
  each Go module's go directive, direct requirements, and tool modules, the workflows' action pins,
  and the compose image pins, then each tool's. A pin the root `HOLDS` list names is held.

The compose harness, [`compose.yml`](../compose.yml), is test-only: PostgreSQL on port 5436 and
SeaweedFS's S3 gateway on port 8334, each built from `compose/<service>/Dockerfile`, which pins
its image, and each keeping its data in tmpfs, so every start is empty. `DOCPROC_POSTGRES_PORT`
and `DOCPROC_SEAWEEDFS_PORT` move the ports, and the variables `mise.toml` sets for the live
tiers follow: `DOCPROC_DSN`, the admin DSN on which each postgres test creates and drops its own
database, and `DOCPROC_S3_ENDPOINT`, `DOCPROC_S3_ACCESS_KEY`, and `DOCPROC_S3_SECRET_KEY`, the
gateway on which each storage test creates its own bucket.

A Go module's release is a `vX.Y.Z` tag (`<module>/vX.Y.Z` for a nested module), a tool's
`tools/<format>/vX.Y.Z`; the release workflow publishes each from its own changelog.
