# docproc standards

The judgement calls the standards-reviewer applies to docproc, beyond what `mise run check`
enforces: each one line, with a pointer to the architecture page that applies where one does. The
Go part applies to the library, its provider modules `storage` and `postgres`, and `cmd/docproc`;
the tools part to the system tools under `tools/`.

## Go

- `architecture/principles/tool-beside-library.md`: `cmd/docproc` is a thin consumer of the
  library's packages.
- `architecture/principles/composition-root.md`: `cmd/docproc`'s `internal/app` is its one
  composition root, and the only place a tool is looked up; the library builds nothing a host
  passes it, `stages.Build` wiring stages from the lookup it is given.
- `architecture/principles/service-tiers.md`: `dispatch.Content` and `dispatch.Index`, each with
  its filesystem and memory implementations in the base module and its provider, `storage` over
  go-storage and `postgres` over sqlate, an adapter behind the one interface.
- `architecture/principles/validation-first.md`: `inspect.Options.Validate` at `dispatch.New` and
  `stages.Build`, a tool's version and schema checked when its adapter is built, and
  `cmd/docproc`'s inputs collected before any tool is probed.
- `architecture/principles/minimal-footprint.md`: the base module requires `golang.org/x/text`
  alone, each provider's dependencies stay in its sub-module, and `cmd/docproc` adds go-cli-sdk,
  go-core, and `golang.org/x/sys` alone.
- `architecture/principles/rolling-currency.md`: every module's requirements and the root's mise
  pins are current, a hold or an exact pin carrying a comment naming why.
- `architecture/principles/downward-dependencies.md`: no docproc package imports docinfer or
  names a dependent, and each store's interface is declared by `dispatch`, which consumes it.
- `architecture/principles/repository-topology.md`: docproc is an adjacent library whose base
  module pins no tier; `storage` sits above go-storage's, and `postgres`, over sqlate, above none.
- `architecture/principles/independent-releases.md`: the base module, `storage`, `postgres`,
  `cmd/docproc`, and each tool release on their own tags, with no umbrella version.
- `architecture/principles/context-architecture.md`: the README, each `doc.go`, and, while in
  flight, `context/layers.md` and `context/design.md` are the homes, each contract stated once, on
  its symbol.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line and no
  provider in a base, across the base, `storage`, `postgres`, and `cmd/docproc` `go.mod` files.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: the library ships no
  policy: `dispatch.Limits`, `stages.Options`' run bounds, and a stage's output caps have no
  library default, and `cmd/docproc` ships the operator's starting values.
- `architecture/standards/go-elemental/principles/construction-and-execution.md`: a constructor
  takes a `Config` struct, panics by name on a nil required field, and returns an error only for
  the environment's fault, a tool too old; `stages.Build` takes one `Options`; a refused request
  matches its package's one `ErrRequest`.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module, the
  `storage` and `postgres` provider sub-modules, and the `cmd/docproc` command module, and their
  tags; no root package; the module builds on every platform, the Linux tag on the exec runner
  alone.
- `architecture/standards/go-elemental/principles/utc-times.md`: every time the library and
  `rundir/schema` return or write is UTC, each package's `TestMain` setting a non-UTC zone.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the unit tier over
  `internal/tooltest`'s stand-in and the tools' committed goldens read in place, the real tools
  behind `-tags integration`, `dispatch/dispatchtest`'s conformance suites run by every store,
  ~/docs read by the acceptance task alone, and the doc.go inventory of every package.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency, the
  root `go.work`, a changelog per module, and `tools/go.mod`, which keeps the tools out of the
  library's module zip.
- `architecture/standards/go-elemental/principles/dsl-driven-services.md`: `postgres`'s authored
  statements and migrations.
- `architecture/standards/go-elemental/principles/cli-applications.md`: `cmd/docproc`'s
  composition root and its fixtures, its exit codes, and its usage errors, which build nothing.
- `architecture/standards/go-elemental/principles/domain-files.md`: `cmd/docproc`'s command
  packages, `run` and `report`, only.
- The exception for a command nested in its library's repository: `cmd/docproc`'s `main.go` sits
  at its module's root and its packages, `app`, `apptest`, `run`, and `report`, under `internal/`,
  where `cli-applications.md` has `cmd/<name>/main.go` and `topology-and-naming.md` domain
  packages at the root level, since the module's one public surface is the command.
- `architecture/standards/go-elemental/principles/system-tools.md`: a mature system tool is run
  through an adapter, never reimplemented in Go.
- An adapter declares `Spec()` and `Minimum`, unprefixed with one tool and named for the tool with
  several, and checks its tools' versions through `Tool.AtLeast` alone, when it is built, so a
  missing, outdated, or other-schema tool fails at startup.
- A tool runs under the caller's context, and its temporary files live in a directory the caller
  owns.
- A tool's options, a bound or an output cap among them, are per-request values, never library
  policy.
- No stage re-encodes a picture lossily; a conversion writes PNG.

## Tools

- `architecture/principles/minimal-footprint.md`: a dependency joins a tool only when it is
  actively maintained and freely licensed, never an unmaintained or paid project.
- `architecture/principles/validation-first.md`: a tool refuses its input, exit 3, before any
  output, and a usage error reads nothing.
- `architecture/principles/rolling-currency.md`: each tool's toolchain and dependencies are pinned
  and current, a hold or an exact pin carrying a comment naming why.
- A tool is used like any system tool: its contract, schema "2"'s lines and the Markdown inside
  them, is versioned with its tags, and a change to it is a CHANGELOG entry.
- Nothing is suppressed to quiet a NativeAOT or trim warning, and no clippy lint is allowed,
  without a comment naming why it is safe.
- A gap in a dependency is filled in our own code, owned and tested; an upstream repository is
  never engaged unless something is legitimately broken, and then only to report it.
