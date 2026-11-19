# docproc

docproc extracts documents for the models a layer above it runs. `github.com/standards-lab/docproc`
is its Go base module: it inspects and routes each part of an input, reads it through the stage
for its format and the system tools that stage runs, stores each output's content, and records
what it prepared in an index. It calls no model and runs no inference: the text, pages, pictures,
audio windows, and video frames it prepares are for a layer above, such as docinfer, to send on.

The base module requires `golang.org/x/text` alone, builds on every platform, and has no root
package. Running a tool needs Linux. Its three system tools, docproc-ooxml, docproc-mail, and
docproc-onenote, are built in this repository under [`tools/`](tools), each released on its own
tags.

## Install

```sh
go get github.com/standards-lab/docproc@v0.2.0
```

The module's releases are tagged `vX.Y.Z`, and [`CHANGELOG.md`](CHANGELOG.md) states what each
changes. Each package's documentation, `go doc github.com/standards-lab/docproc/<package>`, is
the reference for its exported names, and its doc comment lists them.

## Packages

| Package | What it does |
| --- | --- |
| `route` | Identifies content by its signature, the extension table, and the encrypted-package detector, and names the route of the stage it goes to. |
| `inspect` | Inspects a part before its stage and says whether it may go on: an unsafe part, such as a bomb or an unreadable directory, is quarantined; an active one, such as a link, a VBA project, or an external relationship, is flagged. |
| `tool` | Runs a system tool behind one contract, through the exec runner (Linux) or the container runner (the docker CLI), each reporting a run's exit code, signal, and time bound, and applying a memory bound when one is set. |
| `office`, `email`, `onenote` | The readers of docproc-ooxml, docproc-mail, and docproc-onenote, each decoding its tool's schema `"2"` JSON Lines; `email` also threads a store's messages. |
| `document` | A PDF's pages rendered to PNG files through Ghostscript, with their text layer, and its password requirement, form, and embedded files through qpdf and pdfdetach. |
| `picture` | A TIFF's pages, and EMF, WMF, SVG, HEIC, WebP, and AVIF pictures, converted to PNG through ImageMagick and LibreOffice. |
| `audio` | Speech cut into windows that end in a silence, from one ffmpeg decode, and the audio plan record beside them. |
| `media` | A file's audio windows and its video frames, at its scene changes and at least every so often, from one ffmpeg run, and the video plan record beside them. |
| `sevenzip` | 7z, zstd, cabinet, and a ZIP's entries of methods `archive/zip` doesn't read, listed and read through 7-Zip's 7zz. |
| `dispatch` | The walk over an input's parts to their stages; the Stage contract and its error classes; the content store and its layout, and the index, each with a file and a memory implementation; the shapes of its events, parts, outputs, and warnings; and the Meta keys every stage sets and the record media types. It imports `route` and `inspect` alone. |
| `dispatch/dispatchtest` | The Index and Content conformance suites, for every store, the module's own and a provider's, and the fixture stage a host's tests dispatch over. |
| `stages` | `Build` and its `Options`: docproc's shipped wiring, each route's stage over its format package and tools, found on the host, under the host's run bounds and requests, with the one failure classification and each stage's identity. `cmd/docproc` is its first caller. |
| `rundir` | `cmd/docproc`'s run directory, read and written: `Open` to read it and `Create` to write it, its runs, the latest that ended, each run's manifest and record, its content store and the path of a content's file, its index, a quarantined part's kept file, and a run's report directory, `runs/<run>/report/`. One process writes a run directory. |
| `rundir/schema` | The run directory's schema `"2"`: the shapes `cmd/docproc` writes and a reader decodes, a record line, a manifest, an input's file, and a time, each in UTC. It imports no docproc package but `dispatch` and `inspect`. |
| `internal/cli` | The envelope reader the three readers share: their lines, stderr warnings, exit codes, and media names. |
| `internal/goldentest`, `internal/mediatest`, `internal/tooltest` | Test helpers: PNG goldens, media fixtures, and the unit tier's stand-in tool. |

A host registers `stages.Build`'s stages with a `dispatch.Dispatcher` and ranges over its
events. No bound, cap, or limit has a library default: the run bounds (`stages.Options`' Timeout,
Idle, and Memory), `dispatch.Limits`, and the caps in each stage's request are none until the host
sets them. `stages.Options.Concurrency`, the pages and windows held at once, is one until the host
raises it.

## The command

`github.com/standards-lab/docproc/cmd/docproc` is a nested module, released on its own
`cmd/docproc/vX.Y.Z` tags, with its own [`CHANGELOG.md`](cmd/docproc/CHANGELOG.md). It wires
`stages.Build` over the host's tools and writes what it prepares to a run directory through
`rundir`, then renders a run's report from that directory. Running it needs Linux and the system
tools below, each for the stages that run it; rendering a report needs neither the tools nor the
configuration below.

```sh
go install github.com/standards-lab/docproc/cmd/docproc@<version>
```

- `docproc version` prints the module's version, or `(devel)` for a binary that carries none. It
  reads no configuration and looks up no tool.
- `docproc run <path>... --out <dir>` runs the stages over each file argument and each regular
  file under each directory argument, in lexical order, each file once. A named symbolic link is
  followed; a link under a directory is not. It collects its inputs before it looks up any tool,
  prints the tools and the stage table, then writes one run to the run directory `--out` names,
  made when it doesn't exist: the run's manifest, one record line per event, each output's
  content, and each quarantined part's file. `--workers N` dispatches N inputs at once, at least
  1, the host's CPUs by default; `--exclude <route>`, repeatable, leaves a route's stage out, so
  its parts have no processor; `--report`, once the summary is printed, renders the run's report
  and prints its index page's path, as `docproc report` does: after a run that ended, and after a
  failure of the index or the content store, and never after a cancellation.
- `docproc report <run-dir>` renders a run's report into its report directory,
  `runs/<run>/report/` under the run directory, replacing earlier pages, and prints its index
  page's path. It renders the latest run that ended, or the run `--run <id>` names, one that never
  ended among them, its pages marked not ended. It reads the run directory alone: no
  configuration, no tool. The report is static HTML: `index.html` has the run's header, from its
  manifest, a summary of the inputs and their parts by route and outcome, and every input, sorted
  by path, linking a page per distinct content, `<sha256>.html`, that lists the inputs holding it,
  its part tree with each part's record metadata, and its outputs, a text inline, an image or an
  audio window shown, and any other output linked. Its links into the run directory are relative,
  so the run directory moves or archives whole; the pages load nothing from elsewhere, and every
  time is the record's, in UTC.

`docproc run`'s exit statuses:

| Status | When |
| --- | --- |
| 0 | The run ended. Failed parts and inputs stopped with an error leave it 0; the summary's last line counts both, a failure replayed from the index among the failed parts. With `--report`, its pages were rendered too. |
| 1 | The run stopped or never began: an argument that is neither a file nor a directory, arguments that yield no input, a run directory that can't be written, a failure of the index or the content store, or a cancellation. A run that began ends its manifest with the error. With `--report`, a run stopped by a failure of the index or the content store still renders its pages, and a canceled run renders none; a run that ended whose pages couldn't be rendered exits 1 too, and `docproc report` renders them again. |
| 2 | Help, or a usage error, such as an unknown route to `--exclude` or `--workers` under 1. Neither builds anything. |

`docproc report`'s exit statuses:

| Status | When |
| --- | --- |
| 0 | The report was rendered, a run that never ended among them, its pages marked not ended. |
| 1 | Nothing was rendered: a run directory that doesn't exist or isn't one, a `--run` that isn't one of its runs, no `--run` and no run that ended, or a page that couldn't be read or written. |
| 2 | Help, or a usage error, such as no run directory or more than one. |

### Configuration

Each `DOCPROC_` variable below overrides one of `docproc run`'s starting bounds; the library sets
no bound of its own. A duration is a Go duration, such as `90s` or `10m`; a size is a count of
bytes, so 2 GiB is `2147483648`; a count, a ratio, or a pixel count is a decimal number; and a
switch is read by Go's `strconv.ParseBool`. An unset or empty variable keeps its starting value.
A zero means no bound, as it does to the library, unless the table says otherwise, and no value
may be negative. A value the command can't take fails the run before any tool is looked up, and
the error names every such variable. `DOCPROC_SHUTDOWN_TIMEOUT`, 10s by default, bounds the
shutdown.

| Variable | Bound | Starting value |
| --- | --- | --- |
| `DOCPROC_TOOL_MEMORY` | Every tool's memory bound, in bytes (`stages.Options.Memory`); set, the exec runner needs `prlimit` on PATH | 2 GiB |
| `DOCPROC_UNIT_TIMEOUT` | A unit run's bound (`Options.Timeout`): gs, qpdf, pdfdetach, magick, soffice, docproc-ooxml, ffprobe; a gs process shared by a PDF's pages takes it as an idle bound between its pages | 10m |
| `DOCPROC_STREAM_IDLE` | A stream run's idle bound (`Options.Idle`): ffmpeg, docproc-mail, docproc-onenote, 7zz | 2m |
| `DOCPROC_MAX_PARTS` | The most parts one dispatch admits, the input among them (`dispatch.Limits.Parts`) | 100000 |
| `DOCPROC_MAX_BYTES` | The most bytes one dispatch's parts may come to (`Limits.Bytes`) | 32 GiB |
| `DOCPROC_MAX_DEPTH` | The deepest part one dispatch admits, the input at depth 0 (`Limits.Depth`) | 16 |
| `DOCPROC_BOMB_RATIO` | The bomb rule's ratio of expanded to compressed size (`inspect.Options.BombRatio`); zero exactly when `DOCPROC_BOMB_SIZE` is, for no bomb rule | 1000 |
| `DOCPROC_BOMB_SIZE` | The bomb rule's expanded size, in bytes (`inspect.Options.BombSize`) | 1 GiB |
| `DOCPROC_ARCHIVE_ENTRIES` | The most entries a ZIP or a tar may hold (`inspect.Options.Entries`) | 100000 |
| `DOCPROC_CLAIM_RELEASE` | The bound on a claim's release once a dispatch's context has ended (`dispatch.Config.Release`) | 10s |
| `DOCPROC_CONCURRENCY` | A PDF's pages and an audio part's windows held at once (`Options.Concurrency`); zero is 1 | 1 |
| `DOCPROC_PDF_DPI` | The DPI a PDF's pages are rendered at; zero is `document.DefaultDPI` | 150 |
| `DOCPROC_PDF_TEXT` | Whether a PDF page's text layer is read | true |
| `DOCPROC_PDF_MAX_PIXELS` | The most pixels a page's PNG may hold; a larger page is rendered at a lower DPI | 64000000 |
| `DOCPROC_FRAME_MAX_SIDE` | The largest side a video frame is written at, in pixels | 2048 |
| `DOCPROC_MAGICK_AREA` | The most pixels one image may hold in ImageMagick's pixel cache (`-limit area`) | 256000000 |
| `DOCPROC_MAGICK_MEMORY` | The most bytes of heap the pixel cache may take (`-limit memory`) | 1 GiB |
| `DOCPROC_MAGICK_MAP` | The most bytes of the pixel cache that may be memory-mapped (`-limit map`) | 2 GiB |
| `DOCPROC_MAGICK_DISK` | The most bytes of the pixel cache that may be on disk (`-limit disk`) | 8 GiB |
| `DOCPROC_AUDIO_LIMIT` | The longest audio window, its overlaps included (`Options.Audio.Limit`); a host sets it to its model's request size, the longest audio one request takes. A source no longer than it is one window. It must be over 4s, twice the 2 s overlap between windows, so zero is refused | 30s |

## Providers

Two nested modules implement `dispatch`'s stores over a stack library, each released on its own
tags and with its own README, which states its configuration, contracts, tiers, and limits:

| Module | What it is | Tags |
| --- | --- | --- |
| [`github.com/standards-lab/docproc/storage`](storage/README.md) | `storage.New(Config{Client, Prefix, Renew})`: a `dispatch.Content` in object storage over a go-storage `Client`, at a stable key layout, swept by the host's lifecycle rule. | `storage/vX.Y.Z` |
| [`github.com/standards-lab/docproc/postgres`](postgres/README.md) | `postgres.New(ctx, Config{DB, Lease, Flush})`: a `dispatch.Index` in PostgreSQL over sqlate, its `Migrations` the host applies, `Verify`, and `NewRetention`, which the host's sweeper drives. | `postgres/vX.Y.Z` |

Each requires the base module, and the base module requires neither. Both pass `dispatchtest`'s
suite for their store.

## The system tools

`stages.Build` looks up each tool a stage requires on the host, checks it against its package's
minimum in the environment its runs get, and leaves out every stage that requires a tool it can't
build, missing, too old, or failing in that environment, so that route has no processor; `Set.Rows` reports each route's stage and tools.

### docproc's own tools

| Tool | Minimum | Current release | Stage |
| --- | --- | --- | --- |
| docproc-ooxml | 0.1.0 | `tools/ooxml/v0.1.1` | office |
| docproc-mail | 0.1.0 | `tools/mail/v0.1.1` | email |
| docproc-onenote | 0.1.0 | `tools/onenote/v0.1.0` | onenote |

Each minimum is the tool's first schema `"2"` release, which its reader requires. Fetch a tool by
its own tag, never as the latest release: the repository's releases include the base module's
and every tool's, so "latest" names whichever was tagged last. For linux-x64:

```sh
gh release download tools/mail/v0.1.1 --repo standards-lab/docproc \
  --pattern 'docproc-mail-v0.1.1-linux-x64.tar.gz' --pattern SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf docproc-mail-v0.1.1-linux-x64.tar.gz docproc-mail
```

Put each binary on the host's PATH. Each tool's README states its install, its output, and its
limits: [docproc-ooxml](tools/ooxml/README.md), [docproc-mail](tools/mail/README.md), and
[docproc-onenote](tools/onenote/README.md).

The exec runner starts every tool in a fixed environment, never the host's: PATH as it was when
the runner was built, `LANG=C.UTF-8`, and `HOME` and `TMPDIR` set to the run's own working
directory. The program PATH finds must therefore be the program itself, not a version manager's
shim, such as mise's or asdf's, which reads the user's configuration again on every run. Under
that environment mise refuses the user's global configuration as untrusted. `stages.Build` checks
each tool's version in the same environment, with `HOME` and `TMPDIR` a new empty directory of
the check's own, so such a shim reads as unavailable: its row in the tools table says why, and
the stages that need it are `missing`.

### Other system tools

| Program | From | Minimum | Stages |
| --- | --- | --- | --- |
| `gs` | Ghostscript | 10.0 | document |
| `qpdf` | qpdf | 11.0 | document |
| `pdfdetach` | poppler-utils | 22.02 | document |
| `magick` | ImageMagick | 7.1 | office, picture |
| `soffice` | LibreOffice | 24.2 | office, picture |
| `7zz` | 7-Zip | 24.07 | archive |
| `ffmpeg`, `ffprobe` | FFmpeg | 9.0 | audio, video |

7zz 24.07 is the oldest release whose zstd decoder is fixed of CVE-2024-11477. The vision and
text stages run no tool.

The container runner runs a tool inside a digest-pinned image through the docker CLI, 20.10 or
later: `audio.FFmpegImage` for ffmpeg and ffprobe, `picture.LibreOfficeImage` for soffice.

### The memory bound

`stages.Options.Memory`, each tool's `tool.Spec.MemoryLimit`, has no default. Set, the container
runner passes it as `--memory`, and the exec runner starts every run of the tool through
util-linux's `prlimit --as=<bytes> --`, so the address-space bound is in place before the tool
runs: `prlimit` must then be on PATH, and `tool.NewExec` refuses a bounded tool without it. A
tool that dies by a signal under the bound is recorded with both, as in "killed by SIGABRT under a
256 MiB memory bound".

## Limits

docproc reads no ODF, WordPerfect, `.xlsb`, ODG, RAR, ISO, xz, bzip2, HTML, or bare RFC 822 file:
each routes to no stage. It runs no virus scan and flags no macro in an OLE2 file. It detects
no text's encoding: text is read by its byte order mark, as UTF-8, or else as windows-1252.
Beyond that, each stage inherits its tool's
limits, as the tools' READMEs state them in full; these are the ones a host sizes or routes by.

- **office.** Pictures are listed with no layout, so a picture another covers, that sits off the
  slide or page, or that is cropped away is still listed and handed onward. Text boxes,
  equations, SmartArt, a pptx's comments, and an xlsx's cell notes are not read; no formula is
  evaluated, a cell's value being the one the file caches; tracked changes are read accepted
  only. An EMF or WMF is rendered on an A4 page and trimmed to what differs from the page's
  white, so a picture framed in white loses its frame, and an all-white picture is one pixel.
  docproc-ooxml links glibc, 2.34 or newer. Under a memory bound on x64 it aborts by SIGABRT in
  about 1 run in 20 at 256 MiB, its last stderr line naming libssl, not memory, and converts in
  20 of 20 at 512 MiB and above; arm64 is unmeasured.
- **email.** In a `.pst`, an embedded message is listed without its content; an `.ost` is
  refused; TNEF and opaque S/MIME parts are not read; an attachment by reference has no file;
  every mbox is read as an mboxrd. docproc-mail's memory is a small multiple of the largest
  single attachment, and of the file for an `.eml`, which is read whole. The Threader keeps each
  message's threading keys alone: the spike measured it over 200,000 messages at 362 bytes a
  message held, 435 while Threads runs, and the keys are unchanged since. It holds them in
  memory; it doesn't spill.
- **onenote.** A password-protected section is refused as an input and skipped inside a
  notebook; a page is read at its latest revision only; merged table cells are read as their
  own; a section that fails inside a section group is lost with no warning. docproc-onenote holds
  a notebook whole in memory before its first line, so size a host by the largest notebook or
  section it is given. Its read thread's stack is an address-space reservation of a quarter of
  the memory bound, at least 8 MiB and at most 256 MiB, which counts against the bound.
- **archive.** A listing past `inspect.Options.Entries` fails its part; an entry flagged by name
  or type (a traversal, an absolute name, a link, a device), an encrypted one, and a bomb as
  written are unread children, not read.
- **document.** A PDF that needs a password is flagged and not read. A page past the request's
  MaxPixels is rendered at the largest DPI that fits; a page that fits at none fails. A PDF's
  pages share gs processes: at concurrency C, lane k renders pages k, k+C, k+2C, and so on, up to
  `ImagesRequest.PagesPerProcess` pages a process (zero is 32), with one process before it that
  writes their text layers to a temporary directory of its own, never the input's; a page
  MaxPixels lowers gets a process of its own. Every page's PNG and text are byte for byte what a
  process of its own gives. A shared process's bound is `stages.Options.Timeout` read as an idle
  bound, between its pages, the time it waits on the consumer not counted. When one fails on the
  content, the pages it handed over stand and each other page gets processes of its own, each
  bounded by the whole timeout, so only a page that fails there fails.
- **audio and media.** A source ffprobe reports no duration for is refused. media reads files
  alone: no live source, stream URL, or capture.

## Development

The toolchain, Go, golangci-lint, actionlint, shellcheck, jq, and the integration tier's 7zz, is
pinned in [`mise.toml`](mise.toml); each tool under `tools/` pins its own. Tasks run from the
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

The compose harness, [`compose.yml`](compose.yml), is test-only: PostgreSQL on port 5436 and
SeaweedFS's S3 gateway on port 8334, each built from `compose/<service>/Dockerfile`, which pins
its image, and each keeping its data in tmpfs, so every start is empty. `DOCPROC_POSTGRES_PORT`
and `DOCPROC_SEAWEEDFS_PORT` move the ports, and the variables `mise.toml` sets for the live
tiers follow: `DOCPROC_DSN`, the admin DSN on which each postgres test creates and drops its own
database, and `DOCPROC_S3_ENDPOINT`, `DOCPROC_S3_ACCESS_KEY`, and `DOCPROC_S3_SECRET_KEY`, the
gateway on which each storage test creates its own bucket.

A Go module's release is a `vX.Y.Z` tag (`<module>/vX.Y.Z` for a nested module), a tool's
`tools/<format>/vX.Y.Z`; the release workflow publishes each from its own changelog.

Apache-2.0.
