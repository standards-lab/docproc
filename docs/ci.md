# CI and releases

docproc's two workflows, [`ci.yml`](../.github/workflows/ci.yml) and
[`release.yml`](../.github/workflows/release.yml), run the tasks [development](development.md)
describes. Actions are pinned by their exact release tag, which `mise run currency` compares with
each action's latest release. The root mise.toml's 7zz is the integration tier's, which runs on the
developer's machine alone, so both workflows set `MISE_DISABLE_TOOLS` to it, and no job installs it,
through mise-action or a task's auto-install.

## CI

CI runs on every pull request into main, every push to main, and on demand. A newer run on the
same ref cancels the one it supersedes.

- **changes** decides which of the tool, go, and integration jobs run, through one paths filter
  each.
- **tool**: each system tool's check runs once per runner, natively on x64 and arm64, followed by
  its third-party notices, but only when the tool's directory or the wiring every tool shares
  changed.
- **go**: the root's `check:go` on both runners, only when what the Go modules build or read
  changed.
- **lint**: the root's `lint`, shellcheck and actionlint, the release workflow's tag-routing test,
  and lint-context and lint-references, on every run, since a lint reads the whole tree.
- **integration**: the provider modules' live tiers against the compose harness, on a push to main
  when what they build or run against changed and on demand, never on a pull request.
- **ci** always runs last and is the one check docproc's `main` ruleset requires: it fails when
  any job it needs failed or was cancelled, and passes when they succeeded or were skipped, by
  their paths or, the integration job, by a pull request, so a skipped job never blocks a merge.

### The paths filters

The tools filter has one filter per tool, named as its directory under `tools/`, so the filters
that match are the matrix's tools. Each begins with the wiring every tool shares, anchored on the
first: the CI workflow, the root mise.toml its check loads, and `tools/`'s scripts and envelope
table. A Rust tool's adds the Rust tools' shared files, anchored on the first Rust tool: the shared
envelope crate's directory, which each Rust tool's check tests against its own lock, and the
standard library's and musl's notices every Rust tool's notices end with. So a change to either
runs every Rust tool, and no .NET one.

The go filter is what the Go modules build or read: their sources, go.mod and go.sum files (the
storage, postgres, and cmd/docproc modules', and `tools/go.mod`, the boundary that keeps the tools
out of the base module, among them), any testdata, each module's lint config, the postgres module's
SQL and the `sqlint.toml` check:go lints it under, the CI workflow, and the root mise.toml check:go
runs from; and, in the tools' trees, only what the Go tests read in place: the shared envelope cases
and each tool's committed goldens and expected outputs. A change to a tool's code alone doesn't run
it.

The integration filter is what the provider modules' live tiers build or run against: the storage
and postgres modules' sources, go.mod and go.sum, testdata, and the postgres module's SQL, never
their docs; each requires the base module at its released version, not this tree's, so a change to
the base alone doesn't run them. Then the compose harness, the CI workflow, and the root mise.toml
that defines the integration tasks and the harness's environment.

### The jobs' toolchains and caches

- The lint job needs only the root's shellcheck, actionlint, and marathon pin, which provides
  lint-context and lint-references, not its Go toolchain; the routing test needs bash alone.
- `check:go` needs only the root's Go toolchain and golangci-lint. Its cache holds the modules
  go.sum pins, as downloaded, and the build cache; both are keyed on go.sum, the build cache being
  only a speedup the go command checks entry by entry.
- A tool's job: NativeAOT links with clang against zlib, so a .NET tool (a `.slnx`) alone needs
  them; a Rust tool's musl targets come with its pinned toolchain. mise installs from the tool's
  directory, so the toolchain the tool's mise.toml pins is the one installed; the tool's tasks put
  it ahead of the runner's own dotnet on PATH. A .NET tool's cache holds the packages its lock files
  pin, as downloaded; `bin/` and `obj/` are built fresh. A Rust tool's holds the crates its
  Cargo.lock and the envelope's pin, as downloaded, and any git sources; `target/` is built fresh.
  The notices a release would pack for the runner are written after the check, so a package the
  script can't give a notice for fails there, not at the tag.
- The integration job runs the provider modules' live tiers, each against its compose service,
  the same tasks a developer runs; never the base module's tools tier, nor cmd/docproc's
  built-binary tier, which runs the same tools: both are `test:integration`'s, on the developer's
  machine. It runs on a push to main whose paths need it, and always on demand, where the paths
  filter has no push to compare; it is skipped on a pull request, below the per-PR rate by design.
  Each task starts its service, prints its log on a failure, and stops it. The tasks need only the
  root's Go toolchain; docker is the runner's.

## Releases

Release runs on a pushed tag, which the release procedure pushes only once main's CI has passed;
nothing in the workflow tags. Runs for one tag are serialized and never cancelled. The first job
routes the tag through `scripts/release-tag.sh`, which `release-tag-test.sh` holds to its routing,
and refuses any tag that is neither a system tool's nor a Go module's, so no job after it runs for
one; each later job runs for its kind alone. The routing reads the tree: a module tag needs its
go.mod and CHANGELOG.md, so a release is never made from another module's changelog. The first
job appends the script's key=value lines to `$GITHUB_OUTPUT`.

Each version is X.Y.Z, digits alone. A tool's `<format>` is lowercase letters and digits;
`tools/pack.sh` refuses a format that isn't a tool. A module directory is one or more path
segments, each beginning with a letter or digit, so none is empty, `.` or `..`, and the directory
is never absolute; it lies outside `tools/`, whose go.mod only keeps the tools' trees out of the
base module. A module's output names its directory (`module`, `.` for the base), its changelog,
the tag's prefix before its vX.Y.Z (`prefix`, empty for the base), and its version.

A system tool's tag, `tools/<format>/vX.Y.Z`, names the tool's directory, `tools/<format>`, its
changelog, `tools/<format>/CHANGELOG.md`, and its version. Each runner builds the tool's release
binary natively, linux-x64 and linux-arm64, and `tools/pack.sh` refuses to pack it unless its
`--version` is the tag's version and, for a binary that links glibc, as docproc-ooxml's NativeAOT
binary does, unless the newest glibc symbol version it requires is within the floor the tool's
mise.toml declares (`GLIBC_FLOOR`, 2.34 for docproc-ooxml); a binary that links glibc with no
floor declared is refused too. NativeAOT links with clang against zlib, and mise installs from
the tool's directory, so the toolchain the tool's mise.toml pins is the one installed. Each
archive, `docproc-<format>-vX.Y.Z-<rid>.tar.gz`, holds the binary, LICENSE, and the tool's
`THIRD-PARTY-NOTICES.txt`; its entries are sorted, owned by root, and dated by the commit, so the
same binary packs to the same bytes. The tool's release job creates the GitHub release as a draft
from the tag's section of the tool's changelog, attaches both archives and their SHA256SUMS, and
only then publishes it, so a release appears only whole: a failed upload leaves a draft, never a
published release without its assets.

A Go module's tag is the base module's, `vX.Y.Z`, or a nested module's, `<module>/vX.Y.Z` for a
go.mod at `<module>/` outside `tools/`. The module job publishes its GitHub release from the tag's
section of the module's own changelog, CHANGELOG.md at the root or `<module>/CHANGELOG.md`, with
no assets: the module proxy serves the module from the tag itself.

In both release jobs, the release action's prefix is the tag's part before its `vX.Y.Z`, empty for
the base module, so the action finds the version's section in the tool's or module's own changelog.
