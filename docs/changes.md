# Changes per tool and per package

What each promotion changes from its spike, derived from the pages [`layers.md`](layers.md) lists;
the task that builds it is in brackets. Every tool moves to schema "2" and the common envelope, and
each re-records its goldens under "2" as one `.jsonl` and one `.stderr` per fixture, compared byte
for byte, which tests its determinism.

## docproc-ooxml [ooxml]

Built at `tools/ooxml/`: its README states the tool, and its CHANGELOG how it differs from
spike-ai-pipeline-office 0.1.0. What it leaves to later tasks:

- **A tool's wiring.** Each tool's `mise.toml` pins its toolchain alone and defines `check`,
  `publish`, `golden`, `currency`, and `upgrade`, and `GLIBC_FLOOR` when its binary links glibc;
  the root's `TOOLS` lists it. CI's tool matrix runs the tools whose path filter in the `changes`
  job matched, each tool with its own filter. `tools/pack.sh` and the release workflow take any
  tool by its name.
- **Memory.** Its peak resident memory over ~/docs is 42.8 MiB (`acceptance.md`, "Peak memory");
  its arm64 address-space floor is unmeasured.
- **Unread kinds.** Reading text boxes, equations, SmartArt, pptx comments, and xlsx notes is
  backlog; each kind a document holds is one warning.

## docproc-mail [mail]

Built at `tools/mail/`: its README states the tool, and its CHANGELOG how it differs from
spike-ai-pipeline-mail 0.3.1. What it leaves to later tasks:

- **Shared crate.** The docproc-envelope crate at `tools/envelope` holds the Rust tools' common
  envelope and the content rule, and docproc-mail and docproc-onenote both use it; its `src/lib.rs`
  lists the parts. [`tool-builds.md`](tool-builds.md), "The Rust tools", gives how each Rust tool
  checks, lints, and upgrades it, and [`ci.md`](ci.md), "The paths filters", how a change to it
  runs every Rust tool.
- **Holds.** A tool's hold list is `HOLDS` in its `mise.toml`'s `[env]`, one
  `<item> <pin>: <reason>` per line, which `upgrade` skips and `currency` reports as held; a hold
  names its pin, so moving the pin by hand ends it. mail's list holds its fixture generator's
  MimeKit at 4.18.2, and onenote's holds typed-path at 0.12.4.
- **Notices.** `tools/notices.sh`'s cargo branch runs cargo-about, pinned in each Rust tool's
  `mise.toml`, against the accepted licenses in the tool's `about.toml`; mail's accepts MIT,
  Apache-2.0, BSD-3-Clause, and Unicode-3.0. cargo-about walks proc macros' build-time
  subtrees too, so the notices name those crates.
- **Code pages.** A DOS code page the review of ~/docs finds, which the `codepage` crate doesn't
  know, becomes a table in the tool, as US-ASCII (20127) is in `src/codepage.rs`.
- **Memory.** Its peak resident memory over ~/docs is 3.7 MiB (`acceptance.md`, "Peak memory").

## docproc-onenote [onenote]

Built at `tools/onenote/`: its README states the tool, and its CHANGELOG how it differs from
spike-ai-pipeline-onenote 0.1.0. Nothing is asked of onenote_parser upstream: each workaround of
its gaps is the tool's own tested code. What it leaves to later tasks:

- **Parser pin.** onenote_parser is pinned `=2.1.0`, since the workarounds rest on that release's
  exact text and logic, but it isn't held: an `upgrade` that moves it is a reviewed diff, which
  the workaround tests and the goldens check. typed-path is held at 0.12.4 and moves to 0.13.0
  only with onenote_parser.
- **Memory.** The read thread's stack is an address-space reservation of at most a quarter of
  RLIMIT_AS (README, "Limits"), so `cmd/docproc`'s memory-bound starting value leaves room for
  it. Its peak resident memory over ~/docs is 10.6 MiB (`acceptance.md`, "Peak memory"); a
  per-tool timing, if one is wanted, is one harness for all three tools.
- **Unicode.** docproc-envelope's general-category predicates (Cf, Zs, N*, Sm), which the math
  port reads, make the crate the Rust tools' one Unicode home; the Go and .NET sides are later
  tasks'.

## docproc's packages [library]

Built as `github.com/standards-lab/docproc` from spike-ai-pipeline-processing v0.9.0 and the
spike's `stages`: the README states the packages, the tools they run, and the limits, each
package's doc comment its exported names, and the CHANGELOG how v0.1.0 differs from the spike.
The integration tier, `mise run test:integration`, builds the three docproc tools from the same
checkout and runs on the developer's machine before each Go tag. What it leaves to later tasks:

```text
rundir,             built [docproc]: public packages of this module, since its v0.2.0; one Dir from
  rundir/schema     Open, which reads and creates nothing, or Create, which makes the directory,
                    rundir.json, the index, and the content store; Runs, Latest, Manifest, Record
                    an iter.Seq2 ending on ErrNotEnded for an unended run, Content, Kept,
                    ContentPath, and ReportDir, a run's report directory, added by reports in
                    v0.3.0; the writer's Index, Begin, and Quarantine, and Run's Write and End;
                    rundir/schema shapes alone, the Event line, the Manifest with its Build,
                    File with detected, Span, and Time, made by TimeOf in UTC, importing no docproc
                    package but dispatch and inspect; rundir/testdata one committed schema "2" run
                    directory, read and written again, compared as text
storage (module)    built [providers]: storage.New(Config{Client, Prefix, Renew}), dispatch.Content
                    in object storage over go-storage's Client at <prefix>ab/<sha256>, go-storage's
                    ErrTooLarge mapped to dispatch.ErrTooLarge; no sweeper, the host's lifecycle
                    rule on age
postgres (module)   built [providers]: postgres.New(ctx, Config{DB *sqlate.DB, Lease, Flush}),
                    dispatch.Index in PostgreSQL over sqlate, an entry's row with its flags and its
                    log items' rows, inserted in multi-row statements, its claims, leases, and
                    fencing as conditional statements, Items read under the entry's token, Wait by
                    LISTEN/NOTIFY on one listener connection per Index until Index.Close; its
                    migrations, Migrations, with Verify and Statements; NewRetention(db, age),
                    whose budgeted Pass drops aged entries oldest first
```

The memory bound has no default: `tool.Spec.MemoryLimit` and `stages.Options.Memory` are none
until the host sets them. `cmd/docproc`'s starting value, 2 GiB, stands: no part of a ~/docs run
fails by it, every tool peaks under it, and neither of PI-1's bombs moves a starting bound
(`acceptance.md`, "Peak memory", "Bombs").

Built [acceptance]: a PDF's pages share gs processes. At concurrency C, pages k, k+C, k+2C, and so
on go to the k-th of C concurrent processes through gs's page list, at most
`document.ImagesRequest.PagesPerProcess` pages to a process, a count outside the stage identity as
`Concurrency` is; zero is 8, the smallest count within 5% of the fastest on the 915-page CFR volume
(cfr-2024-title32-vol1.pdf), and a negative count is refused. A page list writes consecutive pages
as ranges and ends a batch where it would pass the 2047 bytes gs takes in an argument. A portfolio,
a one-page batch, and a page `MaxPixels` lowers take the per-page path, and a shared process that
fails on the content leaves each undelivered page a process of its own. `DOCPROC_UNIT_TIMEOUT`
bounds a shared process as an idle bound between its pages. Under the container runner, which
mounts only the PDF's directory, text is read a page a process while renders share. Every page's
PNG and text is identical to the per-page path's (`acceptance.md`, "PDF pages in shared gs
processes").

Built [hardening]: each exec-runner run's `TMPDIR` is a directory of the run's own under the host's
`TMPDIR`, removed once the program is reaped, so a gs ended early leaves nothing beside its input
([`api.md`](api.md), "Running the tools"). A portfolio page's own gs runs read the PDF through the
non-portfolio half of gs's `runpdf`, so the page is its cover's, one PNG and that page's text; the
stage adapters' version is 6, so an index prepares its content again. A ZIP holding
`FixedDocumentSequence.fdseq`, whole or as its first piece, routes `Unrouted` as an XPS package,
whatever its name. The three gaps acceptance left are built: soffice's exit 77, LibreOffice's fatal
error at its start, a full disk among its causes, is the run's failure, retried ([`api.md`](api.md),
"Dispatch"); `sevenzip`'s `Extract` returns its writer's error rather than 7zz's broken pipe; and
the email stage's release test dispatches a generated mbox of 100 messages, each with a distinct 64
KiB attachment, and asserts the working directory's peak stays below the attachments' sum.

## The nested modules

From the spike root's `cmd/` and `internal/`; the third column names the spike packages each
module is built from. `cmd/docproc` is a nested module of docproc's repository, so the library
requires neither go-cli-sdk nor go-core.

```text
docproc/cmd/docproc    [docproc]   built from cmd/docproc and internal/docproc/{app, apptest,
                                   run}: main.go at the module's root; internal/app,
                                   internal/apptest, and internal/run; and integration, the
                                   built-binary tier's harness (below); requires docproc,
                                   go-cli-sdk, go-core, and golang.org/x/sys; app and apptest
                                   started from go-cli-sdk-template's template/v0.3.0, which pins
                                   go-cli-sdk v0.4.0, not ported; the graph's node the stage
                                   builder, stages.Build over exec.LookPath, which apptest.Tools
                                   and apptest.Stages replace, export_test.go gone; wires
                                   docproc's stages, keeps one table writer; run collects the inputs
                                   before it builds the stages, follows a named argument that is a
                                   symbolic link, and fails on one that is neither file nor
                                   directory, or on arguments that yield no input; run is a runner
                                   struct with one method, its recorder writing one line per event,
                                   replay.go, its subtrees, and the warnings counters and row gone;
                                   run holds one fileOf, over Statx on golang.org/x/sys, and detect,
                                   from schema's Statx, FileOf, Stater, and Detect, filling File's
                                   detected beside Statx, the birth-time probe its one
                                   export_test.go hook; an input's time from its first event to its
                                   last, on the loop's goroutine; copies a quarantined part's file
                                   in its loop body, Keep gone, and stores outputs in the run
                                   directory's FileContent, its output handler writing the line
                                   alone; stops the run, exit 1, on a store's failure, failed parts
                                   and error inputs exit 0, stated in the help, the summary's last
                                   line counting both; --workers at least 1; its configuration ships
                                   the operator's starting bounds, which the library leaves none:
                                   dispatch.Limits (parts, bytes, depth), Config.Release,
                                   stages.Options' Timeout, Idle, Memory, and Concurrency, and the
                                   outputs' caps in each stage's request (pages, frames, MaxPixels,
                                   a frame's MaxSide); docproc's README names stages as the shipped
                                   wiring and cmd/docproc its first caller; a committed go.work
                                   over the base module and cmd/docproc, carrying changes between
                                   the base module's tags
                       [reports]   built: internal/report, `docproc report <run-dir>
                                   [--run <id>]` and `docproc run --report`, on schema "2" in
                                   place of internal/docproc/report's "1" pages; index.html and
                                   one <sha256>.html per distinct input content, written into
                                   the run's report directory, runs/<run>/report/, which
                                   rundir's package doc places in the run directory's layout and
                                   Dir.ReportDir names; Render over rundir's handle, its golden
                                   pages over a committed run directory; the base module tagged
                                   v0.3.0 for ReportDir, then cmd/docproc/v0.1.0;
                                   internal/report's package doc states the pages
                       [acceptance] built: DOCPROC_AUDIO_LIMIT, stages.Options.Audio's Limit,
                                   starting 30s; a value of 4 s or less, not over twice the 2 s
                                   overlap, or one that doesn't parse, fails the run before any
                                   tool is looked up; whether audio that fits one model request
                                   goes unsplit is the host's choice, made by setting it to the
                                   model's request size, docproc's own default staying 30 s
```

`cmd/docproc`'s `main.go` sits at its module's root, so `go install
github.com/standards-lab/docproc/cmd/docproc@<version>` installs it, and its packages sit under
`internal/`: `app` and `apptest`, the composition root and its fixtures, and `run` and `report`, its
command packages. Its one package outside `internal/` is `integration`, the built-binary tier's
harness, as go-cli-sdk-template's template/v0.3.0 places it. The module's one public surface is the
command, and the logic an integrator calls is the library's, so it is an exception to Go Elemental's
command packages at a module's root level, which `STANDARDS.md` states; go-web-service's
`tools/slab` layout would make the install path `cmd/docproc/cmd/docproc` and `run.Commands` API
under its tags.

`rundir` and `rundir/schema` are public packages of docproc's base module, with no module or tag
of their own. `cmd/docinfer`, in docinfer's repository, reads `cmd/docproc`'s run directory
through them, and Go's internal rule is by import path, so docproc's `cmd/internal` would bar
it. They version with the library, which both commands require, on the base module's tags
(`design-review.md`, "Releases").

```go
package rundir
// Open opens the run directory at root to read it, creating nothing: it fails with
// fs.ErrNotExist when root is no run directory, and refuses another schema, naming both.
func Open(root string) (*Dir, error)
// Create opens root to write it, making the directory, its rundir.json, the index, and the
// content store when they are absent; over an existing directory it refuses as Open does.
func Create(root string) (*Dir, error)
func (d *Dir) Runs() ([]string, error)                         // its runs' ids, in order
func (d *Dir) Latest() (string, error)                         // the latest run that ended
func (d *Dir) Manifest(id string) (schema.Manifest, error)
func (d *Dir) Record(id string) iter.Seq2[schema.Event, error] // its lines, in order
func (d *Dir) Content() dispatch.Content                       // the run directory's FileContent
func (d *Dir) Kept(sha256 string) (string, bool, error)        // quarantine/<sha256>, if kept
func (d *Dir) ReportDir(id string) (string, error)             // runs/<run>/report, not made
func ContentPath(sha256 string) string // a content's file from the root: the content directory
                                       // joined to dispatch.ContentFile(sha256)
// The writer's, on a Dir from Create:
func (d *Dir) Index() dispatch.Index
func (d *Dir) Begin(m schema.Manifest) (*Run, error)
func (d *Dir) Quarantine(sha256, path string) (string, error) // quarantine/<sha256>, kept once
func (r *Run) ID() string                                     // a version 7 UUID
func (r *Run) Write(e schema.Event) error                     // one line, written through
func (r *Run) End(err error) error
```

One process writes a run directory. Its root's `rundir.json` holds its schema, and every method
that takes an id refuses one that isn't a run of the directory, so no caller checks an id first.
`Begin` removes the `.work-*` directories a killed run left. A record is appended, never renamed:
each line is written through as it is made, so a killed run's record loses at most its last line,
and a reader of a run whose manifest has no `end` drops a last line with no newline; `Latest`
names only a run that ended, and a reader given an unended run by id says so. `Record` yields the
lines in order and ends on the first that doesn't decode.

The run directory is schema "2", which includes `dispatch`'s shapes at `dispatch.Version` "1", so
a break there is a break here. `schema` aliases those shapes (`dispatch.Part`, `ContentType`,
`Identity` as its `Stage`, `Output`, and `Warning`, and `inspect.Flag`), so no shape is defined
twice, and a part's `parent` is its parent's ID, a change of meaning "1" can't take. The
manifest's tools and stage table are `dispatch.Tool` and `dispatch.Row`, which `stages.Set` holds
and `cmd/docproc`'s `run` copies in, so `schema` imports no docproc package but `dispatch` and
`inspect`, and a reader of the run directory, `cmd/docinfer` among them, never compiles `stages` and
the format packages beneath it. An output's content is in the run directory's `FileContent`, found
by its `sha256` in place of "1"'s `ref` path under `outputs/`; the content store's directory and a
content's path in it (`ContentPath`, the content directory joined to `dispatch.ContentFile`'s
layout) are part of "2", since the report links a content's file and a reader that isn't Go follows
it.

The record holds one line per event, of four kinds: `part`, `output`, `warning`, and `error`, an
input whose dispatch stopped before its own event. A `warning` line is a warning event dispatch
yields; "1"'s `warning`, a string the command wrote on a part line when no record held a seen part's
children, is gone with the copy that wrote it, since the dispatcher replays children. A reader may
tell the first three kinds by which of `output` and `warning` is set.

```go
package schema
type Event struct {
    Kind  string `json:"event"` // part, output, warning, error
    Input string `json:"input"` // the input's own absolute path
    dispatch.Event              // its fields inline, its Part left out on an error line
    File       *File  `json:"file,omitempty"`      // an input's own part line
    Quarantine string `json:"quarantine,omitzero"` // quarantine/<sha256>, while it is kept
    Time       *Span  `json:"time,omitempty"`      // an input's: its first event to its last
    Error      string `json:"error,omitzero"`      // an error line's text: Err never marshals
}
```

`quarantine` names the kept file whenever it exists, whether this event copied it or an earlier run
did, and the flags are the line's, with no `flags.json`. The record's other shapes, `File`, `Span`,
and the manifest, are its own; `File` holds what `cmd/docproc` knows of an input's file beside the
part, its `detected` the sniffed MIME type, filled by `run` beside `Statx`. The manifest holds
`schema` and `run` for a manifest read alone, no `record`, whose path is the layout's
(`runs/<id>/record.jsonl`), `docproc`, a `Build` of the command's module version and the
library's from the build info, and `collected`, the number of inputs its arguments named. Every
`Time` is UTC, made by `TimeOf`.
The spike's `schema` `Statx`, `FileOf`, `Stater`, and `Detect`, which
imports `net/http`, are now `cmd/docproc`'s `run`'s, their one caller (`stat_linux.go`, on
`golang.org/x/sys/unix`), so `rundir/schema` is shapes alone and the base module requires
`golang.org/x/text` alone.

