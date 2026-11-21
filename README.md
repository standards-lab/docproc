# docproc

docproc extracts documents for the models a layer above it runs. `github.com/standards-lab/docproc`
is its Go base module: it inspects and routes each part of an input, reads it through the stage
for its format and the system tools that stage runs, stores each output's content, and records
what it prepared in an index. It calls no model and runs no inference: the text, pages, pictures,
audio windows, and video frames it prepares are for a layer above, such as docinfer, to send on.

The base module requires `golang.org/x/text` alone, builds on every 64-bit platform, and has no root
package. Running a tool needs Linux. Its three system tools, docproc-ooxml, docproc-mail, and
docproc-onenote, are built in this repository under [`tools/`](tools), each released on its own
tags.

## Install

```sh
go get github.com/standards-lab/docproc@v0.4.0
```

The module's releases are tagged `vX.Y.Z`, and [`CHANGELOG.md`](CHANGELOG.md) states what each
changes. Each package's documentation, `go doc github.com/standards-lab/docproc/<package>`, is
the reference for its exported names, and its doc comment lists them.

## Documentation

- [Packages](docs/packages.md): each package and what it does, and the bounds a host sets.
- [The command](docs/command.md): `cmd/docproc`'s commands, exit statuses, and configuration.
- [Limits](docs/limits.md): what docproc doesn't read, and the limits each stage inherits from its
  tool.
- [Development](docs/development.md): the toolchain, the tasks, the compose harness, and releases.

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
build, missing, too old, or failing in that environment, so that route has no processor;
`Set.Rows` reports each route's stage and tools.

### docproc's own tools

| Tool | Minimum | Current release | Stage |
| --- | --- | --- | --- |
| docproc-ooxml | 0.1.0 | `tools/ooxml/v0.1.2` | office |
| docproc-mail | 0.1.0 | `tools/mail/v0.1.3` | email |
| docproc-onenote | 0.1.0 | `tools/onenote/v0.1.1` | onenote |

Each minimum is the tool's first schema `"2"` release, which its reader requires. Fetch a tool by
its own tag, never as the latest release: the repository's releases include the base module's
and every tool's, so "latest" names whichever was tagged last. For linux-x64:

```sh
gh release download tools/mail/v0.1.3 --repo standards-lab/docproc \
  --pattern 'docproc-mail-v0.1.3-linux-x64.tar.gz' --pattern SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf docproc-mail-v0.1.3-linux-x64.tar.gz docproc-mail
```

Put each binary on the host's PATH. Each tool's README states its install, its output, and its
limits: [docproc-ooxml](tools/ooxml/README.md), [docproc-mail](tools/mail/README.md), and
[docproc-onenote](tools/onenote/README.md).

The exec runner starts every tool in a fixed environment, never the host's: PATH as it was when
the runner was built, `LANG=C.UTF-8`, `HOME` set to the run's own working directory, and `TMPDIR`
set to a new empty directory of the run's own, `docproc-run-*`, under the host's `TMPDIR`. The
program PATH finds must therefore be the program itself, not a version manager's shim, such as
mise's or asdf's, which reads the user's configuration again on every run. Under that environment
mise refuses the user's global configuration as untrusted. `stages.Build` checks each tool's
version in the same environment, with `HOME` a new empty directory of the check's own, so such a
shim reads as unavailable: its row in the tools table says why, and the stages that need it are
`missing`.

A tool's temporary files live in its run's own `TMPDIR`, never beside its input. The runner removes
that directory, with whatever the tool left in it, once the tool's process group has been killed
and the tool reaped, whether the run succeeded, failed, was cancelled, or passed its bound, so a
tool ended early leaves nothing under the host's `TMPDIR` either. A run whose directory can't be
made fails before the tool starts.

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

Apache-2.0.
