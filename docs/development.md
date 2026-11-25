# Development

The toolchain, Go, golangci-lint, actionlint, shellcheck, jq, marathon's lint-context and
lint-references, and the integration tier's 7zz, is pinned in [`mise.toml`](../mise.toml); each
tool under `tools/` pins its own. Tasks run from the repository root, `mise run <task>`:

- `check`: shellchecks the scripts, lints the workflows, tests the release workflow's tag
  routing, and runs lint-context and lint-references; builds, vets, format-checks, fixes, tidies,
  tests under the race detector, and lints every Go module, the base, both providers, and
  cmd/docproc, each with the workspace off, then sqlints the postgres module's SQL; then runs each
  tool's check. It is the unit tier: each package over its own testdata and a stand-in tool, the
  readers replaying the tools' committed goldens in place. It reads no network and runs no real
  tool but util-linux's `prlimit`, which the memory-bound tests need on PATH.
- `test:integration`: the integration tier, under the `integration` build tag. It builds
  docproc-ooxml, docproc-mail, and docproc-onenote from this checkout through their publish tasks
  and puts those three ahead of everything else on PATH, so no installed release stands in for
  them, and it runs 7zz at the root's pin. It then runs, in turn, the base module's tools tier,
  which runs the real tools and checks every committed fixture's route and outcome through
  `stages`; cmd/docproc's built-binary tier, which builds `docproc` with the race detector, runs
  it over the committed fixtures, and checks its arguments, output, exit status, signals, and the
  run directory it writes, read back through `rundir`; and `integration`, below. Run it on your
  machine before each Go tag; CI doesn't. The other programs come from the host, each at its
  minimum or later: `gs`, `qpdf`, `pdfdetach`, `pdftotext`, `ffmpeg`, `ffprobe`, `magick`,
  `soffice`, util-linux's `prlimit`, and `docker`, which runs the compose harness and pulls
  `audio.FFmpegImage` and `picture.LibreOfficeImage` for the tools tier.
- `integration`: the providers' live tiers, `integration:postgres` then `integration:storage`, every
  one even after one fails; CI runs it on a push to main that needs it and on demand.
  `integration:postgres` runs the postgres module's suite against the harness's PostgreSQL, and
  `integration:storage` the storage module's against its SeaweedFS. Each starts only its own
  service, runs its module's suite, and stops the service whether the suite passes or fails,
  printing its log on a failure.
- `currency` and `upgrade`: report, then bump, every root pin behind its latest: the root mise pins,
  each Go module's go directive, direct requirements, and tool modules, the workflows' action pins,
  the compose image pins, and the image pins in the Go source, then each tool's. A pin the root
  `HOLDS` list names is held.

A Go module's release is a `vX.Y.Z` tag (`<module>/vX.Y.Z` for a nested module), a tool's
`tools/<format>/vX.Y.Z`; the release workflow publishes each from its own changelog. The
workflows are described in [CI and releases](ci.md), and each system tool's build configuration
in [the tools' builds](tool-builds.md).

## The root mise.toml

[`mise.toml`](../mise.toml) pins the integration tier's 7-Zip, `7zz`, so currency and upgrade keep
it current: `sevenzip` requires 26.06 or later. `test:integration` runs under it; CI never needs
it, so its workflows set `MISE_DISABLE_TOOLS` to it, and no job installs it. The tier's other
system tools are the host's. crane lists an image's tags for currency's scan of the compose
harness's and the Go source's image pins, and reads a tag's index digest for the upgrade; no CI job
installs it either.

Its environment:

- `GO_MODULES`: the Go module directories, each with its own go.mod, which `check:go` checks in
  turn, kept in sync with go.work's use list: the base module,
  `github.com/standards-lab/docproc`, at the root, then the storage and postgres provider modules
  and the cmd/docproc command module, which require it.
- `TOOLS`: the system tools under `tools/`, each with its own mise.toml that pins its toolchain and
  defines its `check`, `currency`, and `upgrade` tasks, which the root's tasks run in turn:
  docproc-ooxml (.NET), and docproc-mail and docproc-onenote (Rust).
- `HOLDS`: the root's held pins, which currency reports as held and upgrade never moves: one line
  each, `<item> <pin>: <reason>`, the item named as currency names it (a Go module by its path, the
  go directive as `go`, a mise tool by its name, an action by its owner/repo, an image by its
  image). A tool's own holds are in its mise.toml.
- `DOCPROC_DSN`, `DOCPROC_S3_ENDPOINT`, `DOCPROC_S3_ACCESS_KEY`, and `DOCPROC_S3_SECRET_KEY`: the
  live tiers' servers, below.

Its tasks:

- `check` is the one check: read-only and deterministic, stopping at the first failing part. It runs
  the lint, then `check:go`, then each tool's check, so CI's lint, Go, and tool jobs run its three
  parts apart. After each tool's check it writes the notices a release would pack for this
  machine to nowhere, so a package the notices script can't give a notice for fails there, not at
  the tag.
- `check:go` builds, tests, and lints each module in `GO_MODULES` under the `.golangci.yml`
  nearest it, outside any go.work, so each resolves its own pins as a consumer does; then it runs
  sqlint over the postgres module's statements and migrations. sqlint is the postgres module's
  tool, and resolves the engine `sqlint.toml` names through the module's own requirements.
- `lint`: shellcheck, actionlint, the tag-routing test, then lint-context and lint-references,
  each run before the task fails if any failed. A lint missing from PATH fails the task with
  `FAIL: <lint> is not on PATH; run mise install`. actionlint shellchecks each workflow's `run:`
  blocks too, with the shellcheck pinned in mise.toml. The release workflow's tag routing is a
  script, so its test runs here, beside the workflow it routes. CI's lint job runs this task; the
  check runs it first.
- `test:integration`: each docproc tool is built from this checkout through its own publish task,
  and the three binaries alone are linked into a directory put ahead of everything else on PATH.
  Packages run in parallel, as the unit tier's do: each container test watches only the containers
  its own runs name. The base module's tier runs over the tools, then cmd/docproc's, which builds
  docproc itself and runs it over the committed fixtures, over the same tools; the storage and
  postgres modules' through their `integration:*` tasks, each against its own compose service,
  every one even after one fails.
- `integration` runs one module at a time, every module even after one fails.
- `currency` is read-only and development-time: it reads the network, so CI never runs it.
  `upgrade` is development-time too.

## Currency and upgrade

[`scripts/currency.sh`](../scripts/currency.sh) is both tasks. `report` prints one line for each pin
behind its latest, a held one with its hold's reason, and exits 1 when any it reports is not held;
it reads the network and writes nothing but the tools' `obj/` and cargo-deny's advisory database.
`upgrade` moves every pin `report` shows trailing to its latest, but a held one, so a report after
it shows none of them but the held. Run it, review the diff, and run the check.

- **Captures fail currency.** Each scan captures its command's output in a variable before
  reading it, and each capture ends in `|| return`, so a scan command that fails (a go list
  missing a go.sum entry, a registry crane can't reach) fails currency instead of reporting
  nothing: a command substitution runs with `set -e` off.
- **Go modules.** A module's direct requirements, its tool modules, and its go directive, each
  read in the module's directory. A tool's module is an indirect requirement, which the direct
  scan leaves out; one the module also requires directly is reported there, so it is skipped
  among the tool modules. Only go list's standard output names modules: its standard error
  carries the warning when no tool directive matches and any download's progress, so it is shown
  only when go list fails. A go directive trails when its minor trails the current Go minor.
- **The root mise tools.** Those the root mise.toml pins; the tools' mise files are their own.
  `--no-global` reads and writes the root mise.toml alone, never the global config, and a held
  tool is excluded from the upgrade by name.
- **Images.** An image's latest follows `architecture/principles/rolling-currency.md`'s rule for a
  container image: the highest version among the tags with a dot that carry the pin's variant
  suffix. A linuxserver image's trailing `-ls<N>` is its build, not its variant: a pin of
  `9.0.2-cli-ls89` is current against the newest `-cli-ls<N>` tag, by version first and build
  second. A service that builds from `compose/<service>/Dockerfile` has its one pin in that file's
  FROM line.
- **Images in the Go source.** The scan reads every Go file the repository tracks, with its
  string concatenations joined, for an `<image>:<tag>@sha256:<digest>` reference:
  `audio.FFmpegImage`, `picture.LibreOfficeImage`, and each test's copy of one. A reference whose
  registry is a reserved name (`.example`, `.invalid`, or `.test`) is a test's stand-in and names
  no registry, so it is skipped. The upgrade moves the tag and the digest together, the digest to
  the new tag's multi-arch index digest, as crane reads it.
- **Each tool's own.** Each tool's currency reports its packages, advisories, and toolchain, each
  line prefixed with the tool's directory. A tool reporting anything exits non-zero, which marks
  the whole report stale without stopping the other scans.
- **The upgrade's order.** It bumps the root mise pins first, so the Go modules then move under
  the Go just pinned: a go directive raised to a Go minor the old toolchain predates would
  otherwise send the go command to fetch a toolchain. Its `go` runs the pinned Go in a new mise
  process, so it is the Go just pinned, not the one the task started with. Then each Go module:
  its go directive, when its minor trails, to the current Go minor with no toolchain line (a
  directive whose minor is current is left as written, patch and all), then each direct
  requirement and tool module with a newer version to its latest, then `go mod tidy`. Then the
  action pins, then the image pins, then each tool's upgrade last, so a tool's pins move after
  the root's.
- **Rewriting a pin.** A pin is matched with its regex characters escaped, under a `#` delimiter.
  An action's or a compose image's is matched only up to a space or the line's end, so v1 never
  matches inside v1.2 and 18.7 never inside 18.70, and every use of a trailing action is
  rewritten. A Go source image's is matched from its opening quote through `@sha256:`, and its
  digest up to its closing quote, in each file that holds it.

## Lint configuration

Each Go module has its own [`.golangci.yml`](../.golangci.yml): golangci-lint takes the
configuration nearest the directory it runs in, so the storage, postgres, and cmd/docproc modules'
own configurations, not the root's, hold them. Each is the root's but for depguard's lists, which
hold the module to its own requirements.

- The integration tier's files, under the `integration` build tag, are linted with the unit
  tier's: a file of either tier builds under the tag, so one run covers both. cmd/docproc's
  configuration sets no tag.
- The linters are the standard set (errcheck, govet, ineffassign, staticcheck, unused), plus
  depguard, which holds the module to its allowed imports, nolintlint, which requires each
  suppression to name its linter and give a reason, and testpackage, which keeps tests black-box.
  testpackage's skip regexp is narrowed to `export_test.go`: tests live in `<pkg>_test` and drive
  the exported API, and the one white-box file allowed is `export_test.go`. Its `allow-packages`
  is narrowed from its default, `main`, so a command's tests are black-box too, in `main_test`. It
  names only the empty package name, which no package has: an empty list leaves the default,
  `main`, in force.
- depguard's allow entries match by prefix unless they end in `$`, so each module's entries are
  anchored: the module as itself or a package under it (not a sibling like `docproc-extra`). The
  longest match wins, so the deny entries keep the other modules out despite docproc's prefix,
  and a test helper out of the production files despite its module's.
- The base module depends on nothing but the standard library (`unsafe` among it, which `waitid`
  in `tool/exec.go` uses) and `golang.org/x/text`'s packages, which the text stage decodes
  through (`stages/text.go`), so no package imports a model client. The rule covers every Go
  file, tests included: they drive the API with the same imports plus the module itself. The deny
  entries keep the storage, postgres, and cmd/docproc modules, which require the base, out of it.
- The storage module requires the standard library, docproc's base packages, whose
  `dispatch.Content` it implements, and go-storage's `Client` it stores through; its tests add
  go-storage's `Fake`, which the unit tier runs over, and its S3 provider, which the live tier
  runs against.
- The postgres module requires the standard library, docproc's base packages, whose
  `dispatch.Index` it implements, sqlate's packages and its PostgreSQL engine, and pgx v5. Its
  tests add sqlate/postgres's pgtest, which imports testing and so stays out of the module's own
  packages.
- The cmd/docproc module requires the standard library, the module's own packages, docproc's base
  packages, whose stages it wires, go-cli-sdk's `cli`, its command tree, go-core's packages, its
  graph, lifecycle, and process, and `golang.org/x/sys`'s `unix`, the run command's statx; its
  tests add go-core's graphtest, which imports testing and so stays out of the module's own
  packages.

[`postgres/sqlint.toml`](../postgres/sqlint.toml) holds the postgres module's SQL conventions,
run by `check:go` from that directory. The engine names the native forms a standard-tier file may
not use, and the sources name the pattern catalogs the statements' includes resolve against:
sqlate's own. The base module holds no SQL. The index's statements are named for their operation,
the delimiter kept out of comments and literals, and no native form in a standard-tier file; a
native file is exempt by its tier and carries its port note. The migrations role has no
native-forms check; the DDL is PostgreSQL's, the only engine's.

## The compose harness

The compose harness, [`compose.yml`](../compose.yml), is test-only: PostgreSQL on port 5436 and
SeaweedFS's S3 gateway on port 8334, each built from `compose/<service>/Dockerfile`, which pins
its image in its one FROM line, and each keeping its data in tmpfs, so every start is empty. The
default ports let the harness coexist with sqlate's PostgreSQL on 5433, blobfs's on 5434,
go-messaging's on 5435, a service's on 5432, and go-storage's SeaweedFS on 8333.
`DOCPROC_POSTGRES_PORT` and `DOCPROC_SEAWEEDFS_PORT` move the ports, and the variables `mise.toml`
sets for the live tiers follow: `DOCPROC_DSN`, the admin DSN on which each postgres test creates
and drops its own database, and `DOCPROC_S3_ENDPOINT`, `DOCPROC_S3_ACCESS_KEY`, and
`DOCPROC_S3_SECRET_KEY`, the gateway on which each storage test creates its own bucket.

- PostgreSQL's user, password, and database are the ones `DOCPROC_DSN` names; development values,
  not secrets. It is healthy once the server accepts TCP connections to the app database. The
  probe goes over TCP, not the socket, because the first start's initialization runs a temporary
  server on the socket alone, which would report ready before the real server starts.
- SeaweedFS runs as the single-process `weed mini`, its S3 gateway on 8333. Its admin identity,
  which SeaweedFS reads at startup and the live tier signs with through `DOCPROC_S3_ACCESS_KEY`
  and `DOCPROC_S3_SECRET_KEY`, is a development value, not a secret. It is healthy once a request
  signed with that identity lists the buckets, which proves the gateway, its filer, and the
  credential together: the gateway answers unsigned requests before the credential is loaded.
  `-s3.autoCreateBucket=false` turns off SeaweedFS's default of creating a missing bucket on an
  admin's upload, so a write to a missing bucket fails as it does on AWS. Data lives in `/data`,
  the harness's tmpfs.

## The repository's files

- [`go.work`](../go.work) is the workspace over docproc's Go modules, kept in sync with the root
  mise.toml's `GO_MODULES`: the base module, the storage and postgres provider modules, and the
  cmd/docproc command module, which require it, so a change to the base is seen by the providers
  and the command before it is tagged. The check runs every module with `GOWORK=off`, as a
  consumer resolves it.
- [`tools/go.mod`](../tools/go.mod) marks a boundary and holds no Go code. A module zip includes
  every committed file below its go.mod, so without this one the system tools under `tools/`,
  their sources, fixtures, and goldens, would ship in docproc's module zip. The go command stops at
  a directory holding a go.mod, so the subtree belongs to this module, which is never tagged or
  required.
- [`.gitattributes`](../.gitattributes) keeps git from converting the fixtures pinned byte for
  byte: the audio speech fixture and its transcript, and the hand-written RTF fixture, each pinned
  by SHA-256; the committed run directory, which is compared as text with what the writer writes,
  and one record of which ends in a torn line with no newline; and the malformed PDF fixture,
  which holds CR line ends among its text and is pinned by SHA-256.
- [`.claude/marathon.toml`](../.claude/marathon.toml) names the check, the currency command, and
  the commands that publish and merge a finished branch and wait for its CI.
