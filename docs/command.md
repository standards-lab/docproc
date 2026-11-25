# The command

`github.com/standards-lab/docproc/cmd/docproc` is a nested module, released on its own
`cmd/docproc/vX.Y.Z` tags, with its own [`CHANGELOG.md`](../cmd/docproc/CHANGELOG.md). It wires
`stages.Build` over the host's tools and writes what it prepares to a run directory through
`rundir`, then renders a run's report from that directory. Running it needs Linux and the system
tools the [README](../README.md#the-system-tools) lists, each for the stages that run it;
rendering a report needs neither the tools nor the configuration below.

```sh
go install github.com/standards-lab/docproc/cmd/docproc@<version>
```

- `docproc version` prints the module's version, or `(devel)` for a binary that carries none. It
  reads no configuration and looks up no tool. A run's manifest names the same version and the
  docproc library's version as the build info records it: a replaced library's replacement
  version, or `(devel)` when a directory replaces the library or the build names no library.
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

## Configuration

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
| `DOCPROC_TOOL_MEMORY` | Every tool's memory bound, in bytes (`stages.Options.Memory`); set, the exec runner needs `prlimit` on PATH; [Limits](limits.md) gives the tools' needs under it | 2 GiB |
| `DOCPROC_UNIT_TIMEOUT` | A unit run's bound (`Options.Timeout`): gs, qpdf, pdfdetach, pdftotext, magick, soffice, docproc-ooxml, ffprobe; a gs process shared by a PDF's pages takes it as an idle bound between its pages | 10m |
| `DOCPROC_STREAM_IDLE` | A stream run's idle bound (`Options.Idle`): ffmpeg, docproc-mail, docproc-onenote, 7zz | 2m |
| `DOCPROC_MAX_PARTS` | The most parts one dispatch admits, the input among them (`dispatch.Limits.Parts`) | 100000 |
| `DOCPROC_MAX_BYTES` | The most bytes one dispatch's parts may come to (`Limits.Bytes`); it bounds what the archive and document stages write too, and an entry stopped at it is recorded `limited`, with no size or SHA-256 | 32 GiB |
| `DOCPROC_MAX_DEPTH` | The deepest part one dispatch admits, the input at depth 0 (`Limits.Depth`) | 16 |
| `DOCPROC_BOMB_RATIO` | The bomb rule's ratio of expanded to compressed size (`inspect.Options.BombRatio`); zero exactly when `DOCPROC_BOMB_SIZE` is, for no bomb rule, and a rule half off names both variables | 1000 |
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
