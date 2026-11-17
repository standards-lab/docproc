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
go get github.com/standards-lab/docproc@v0.1.0
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
| `stages` | `Build` and its `Options`: docproc's shipped wiring, each route's stage over its format package and tools, found on the host, under the host's run bounds and requests, with the one failure classification and each stage's identity. |
| `internal/cli` | The envelope reader the three readers share: their lines, stderr warnings, exit codes, and media names. |
| `internal/goldentest`, `internal/mediatest`, `internal/tooltest` | Test helpers: PNG goldens, media fixtures, and the unit tier's stand-in tool. |

A host registers `stages.Build`'s stages with a `dispatch.Dispatcher` and ranges over its
events. No bound, cap, or limit has a library default: the run bounds (`stages.Options`' Timeout,
Idle, and Memory), `dispatch.Limits`, and the caps in each stage's request are none until the host
sets them. `stages.Options.Concurrency`, the pages and windows held at once, is one until the host
raises it.

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
minimum, and leaves out every stage that requires a tool it can't build, missing or too old, so
that route has no processor; `Set.Rows` reports each route's stage and tools.

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
  MaxPixels is rendered at the largest DPI that fits; a page that fits at none fails.
- **audio and media.** A source ffprobe reports no duration for is refused. media reads files
  alone: no live source, stream URL, or capture.

## Development

The toolchain, Go, golangci-lint, actionlint, shellcheck, jq, and the integration tier's 7zz, is
pinned in [`mise.toml`](mise.toml); each tool under `tools/` pins its own. Tasks run from the
repository root, `mise run <task>`:

- `check`: shellchecks the scripts, lints the workflows, and tests the release workflow's tag
  routing; builds, vets, format-checks, fixes, tidies, tests under the race detector, and lints
  every Go module, the base and both providers, each with the workspace off, then sqlints the
  postgres module's SQL; then runs each tool's check. It is the unit tier: each package over its own
  testdata and a stand-in tool, the readers replaying the tools' committed goldens in place. It
  reads no network and runs no real tool but util-linux's `prlimit`, which the memory-bound tests
  need on PATH.
- `test:integration`: the integration tier, under the `integration` build tag: the base module's
  tools tier, which runs the real tools and checks every committed fixture's route and outcome
  through `stages`, then `integration`, below. It builds docproc-ooxml, docproc-mail, and
  docproc-onenote from this checkout through their publish tasks and puts those three ahead of
  everything else on PATH, so no installed release stands in for them; it runs 7zz at the root's
  pin. Run it on your machine before each Go tag; CI doesn't. The other programs come from the host,
  each at its minimum or later: `gs`, `qpdf`, `pdfdetach`, `ffmpeg`, `ffprobe`, `magick`, `soffice`,
  util-linux's `prlimit`, and `docker`, which runs the compose harness and pulls
  `audio.FFmpegImage` and `picture.LibreOfficeImage` for the tools tier.
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
