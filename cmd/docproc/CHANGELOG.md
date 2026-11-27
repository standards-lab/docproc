# Changelog

All notable changes to `github.com/standards-lab/docproc/cmd/docproc`, the docproc command, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `cmd/docproc/vX.Y.Z` tag carries, and is the release's notes.
Its command line, run directory, and exit codes are its contract: a change to any of them is a
change here. This changelog covers the cmd/docproc module only; the base module, the storage and
postgres modules, and each system tool under `tools/` keep their own.

## [Unreleased]

## [v0.2.8] - 2026-11-27

This release requires the docproc base module at v0.10.0, whose changes reach `docproc run` as
Changed says. Its command line, exit codes, and run directory's schema are unchanged; the run
directory holds each format's metadata record, a message's meta holds its threading keys, and the
document and vision stages each require one more tool.

### Changed

- The module requires the docproc base module at v0.10.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - A record output named `metadata` in `record.jsonl` holds a format's own metadata, under a
    versioned media type of its own: a PDF's information dictionary and XMP packet, read by
    pdfinfo; an office package's core, extended, and custom properties and the format it was
    converted from; an audio or video file's container and streams, read by ffprobe; and a
    picture's EXIF, read by magick. Its event carries the record's SHA-256, and the record is in
    the content store. A PDF or picture whose metadata can't be read gets a `metadata` warning
    and still prepares. A message's mail record and a OneNote page's record serve as their
    metadata records.
  - A message's outputs carry `in_reply_to` and `references` in their meta when the message
    stores them, each as stored, `references` its Message-IDs separated by spaces.
  - The document stage requires pdfinfo, from poppler-utils 22.02 or later, beside gs, qpdf,
    pdfdetach, and pdftotext. poppler-utils ships pdfinfo with pdfdetach and pdftotext, so a host
    that ran the document stage before runs it still. The stage table's document row carries
    pdfinfo's version line, and `DOCPROC_UNIT_TIMEOUT` bounds its runs.
  - The vision stage requires magick, ImageMagick 7.1 or later, which the picture and office
    stages require already. On a host without it, or with an older one, `docproc run` leaves the
    vision stage out, and its stage table says why, so PNG, JPEG, GIF, and BMP parts have no
    processor. The stage table's vision row carries magick's version line, and
    `DOCPROC_UNIT_TIMEOUT`, `DOCPROC_TOOL_MEMORY`, and the `DOCPROC_MAGICK_*` limits bound its
    runs.
  - Each stage that yields a new record or key hashes its own version anew, so a run over an index
    prepares the office, document, audio, video, vision, and vision-converted routes' content and
    the nested route's mail parts again rather than replaying them, and replays every other
    stage's.
  - soffice runs in `gotenberg/gotenberg:9.10.2-libreoffice`, which carries the same LibreOffice,
    26.8.2.2, and the ffmpeg default is `linuxserver/ffmpeg:9.0.2-cli-ls91`, which carries the
    same FFmpeg, 9.0.2, each pinned by its multi-arch index digest.

## [v0.2.7] - 2026-11-25

This release requires the docproc base module at v0.9.0, whose changes reach `docproc run` as
Changed says. Its command line, exit codes, and run directory's schema are unchanged; the office
stage prepares formats that had no processor yet, and over docproc-ooxml 0.2.0 and docproc-mail
0.2.0 the office and email stages prepare more of a document and a message.

### Changed

- The module requires the docproc base module at v0.9.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - The office stage converts, through LibreOffice's soffice, ODF text, spreadsheet, and
    presentation packages and their templates (`.odt`, `.ott`, `.ods`, `.ots`, `.odp`, `.otp`), an
    ODF drawing or drawing template (`.odg`, `.otg`), a WordPerfect document, and an Excel binary
    workbook (`.xlsb`, by its name), then reads the result through docproc-ooxml, as it does a
    legacy document. Each is prepared under the `office` route in `record.jsonl`, where it had no
    processor yet, and the document Markdown's `converted_from` names its source format: the ODF
    type's extension, `wpd`, or `xlsb`.
  - An unencrypted ODF package of any other type, a formula, chart, database, or image among
    them, still has no processor yet, its reason now the office stage's, naming the package's
    type. An ODF package of any type whose manifest holds encryption data is encrypted, and one
    whose ZIP or manifest can't be read is refused, before soffice runs.
  - With docproc-ooxml 0.2.0 on the host, the office stage's output holds a document's text
    boxes, a pptx's comments, and an xlsx's notes and threaded comments, and its warnings no
    longer list them as not read. With docproc-mail 0.2.0, the email stage's output holds a TNEF
    part's attachments, as the message's own, and its bodies, where it listed one `winmail.dat`
    attachment with a warning. An older release of either tool still runs, reading as it did, and
    the stage table names the version line the host's tool prints.
  - The office stage's identity hashes the stage's own version, so a run over an index prepares
    its office content again rather than replaying it, and replays every other stage's; a host
    that moves to docproc-ooxml 0.2.0 or docproc-mail 0.2.0 prepares its office or email content
    again, since each stage's identity holds its tool's version line.

## [v0.2.6] - 2026-11-25

This release requires the docproc base module at v0.8.0, whose changes reach `docproc run` as
Changed says. Its command line, exit codes, and run directory's schema are unchanged; a PDF page's
text output changes shape, and the document stage requires pdftotext.

### Changed

- The module requires the docproc base module at v0.8.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - A PDF page's `page N` text output is read by poppler's pdftotext in its reading order, where
    gs's txtwrite laid it out: a multi-column page reads column by column with no run of spaces
    padding its columns, a word hyphenated at a line's end is joined, and the paragraphs of
    columns that line up may alternate between the columns. The output's name, media type, and
    place in `record.jsonl` are unchanged, and the run directory's schema stays `"2"`.
  - The document stage requires pdftotext, from poppler-utils 22.02 or later, beside gs, qpdf,
    and pdfdetach. On a host without it, or with an older one, `docproc run` leaves the document
    stage out, and its stage table says why. pdfdetach already required poppler-utils at 22.02,
    which ships pdftotext with it, so a host that ran the document stage before runs it still.
    The stage table's document row carries pdftotext's version line, and
    `DOCPROC_UNIT_TIMEOUT` bounds its runs.
  - The stage adapters' version is 7, so a run over an index prepares its archive, audio,
    document, email, office, OneNote, picture, and video content again rather than replaying it;
    text content is replayed.

## [v0.2.5] - 2026-11-25

This release requires the docproc base module at v0.7.0, whose changes reach `docproc run` as
Changed says. Its command line and exit codes are unchanged; its run directory's routes change for
content a name's extension made text and for an mbox without a message header, both breaking
changes.

### Changed

- The module requires the docproc base module at v0.7.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - `docproc run` prepares HTML, CSS, JavaScript, an EPUB's entries, `#!` scripts, and other text
    of no signature as raw text, with no structure, recording them under the `text` route in
    `record.jsonl`, where each was `unrouted` and had no processor. Content no processor claims,
    or that fails a claim's own check, routes `text` unless its first 4096 bytes hold a NUL byte.
    Content named `.eml` or stored as a message whose header isn't a message's shape, and text
    stored as audio or video, route `text` too. A `#!` script keeps its kind, `shebang`, and its
    executable flag.
  - **Breaking:** a name's extension never makes content text. A `.txt`, or content of another
    extension the text list held, whose head holds a NUL byte is `unrouted` in `record.jsonl`,
    from `text`, and has no processor; one whose content carries a signature or a message's header
    routes by it. The run directory's schema stays `"2"`.
  - **Breaking:** an mbox is claimed only when its `From ` line is followed by a header block
    whose first line is a field and which holds From and either Date or Received. Content that
    begins `From ` without one routes `text`, from `nested`, and is read as raw text.
  - `route_reason` in `record.jsonl` changes for content that falls back: it names the claim the
    content failed, or `no routed signature`, then `, text by its head` or `, not text`. A `#!`
    script's is `"#!" line, a script`, and `text by its <ext> extension` is gone.
  - A `#!` script, content of no signature, or text after a byte order mark carries the
    `polyglot` flag when a ZIP is appended to it.
  - A run over an index prepares content that was unrouted at its next dispatch, since the index
    recorded nothing for it.

## [v0.2.4] - 2026-11-25

This release requires the docproc base module at v0.6.0, whose changes reach `docproc run` as
Changed says. Its command line and exit codes are unchanged; its run directory's kinds change for
an Ogg holding video, and its archive stage needs 7zz 26.06, both breaking changes.

### Changed

- The module requires the docproc base module at v0.6.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - **Breaking:** the archive stage takes 7zz 26.06 or later, from 24.07. On a host with an older
    7zz, `docproc run` leaves the archive stage out, and its stage table says why.
  - **Breaking:** an Ogg whose first pages begin a video stream, such as an `.ogv`, has the kind
    `ogg-video` in `record.jsonl`, from `ogg`, and routes to video, from audio: it is prepared
    with frames and windows under the video plan in place of audio windows. Any other Ogg stays
    `ogg` and routes to audio. The run directory's schema stays `"2"`.
  - `docproc run` prepares RAR4 and RAR5 archives, xz and bzip2 streams, and the tars inside
    them, recording the kinds `rar4`, `rar5`, `xz`, and `bzip2` under the archive route, and MPEG
    program streams, `mpeg-ps`, under the video route. Each routed to no stage before. A RAR
    whose headers are encrypted fails as encrypted, and a volume of a split RAR has no processor;
    neither has its entries read.
  - The archive stage's identity carries 7zz's version line, so a run over an index prepares its
    earlier archive entries again once 7zz is upgraded.

## [v0.2.3] - 2026-11-25

This release requires the docproc base module at v0.5.0, whose changes reach `docproc run` as
Changed says. Its command line and exit codes are unchanged; its run directory's text outputs
change, a breaking change for a reader of the combined encoding value.

### Changed

- The module requires the docproc base module at v0.5.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - **Breaking:** a text output in `record.jsonl` carries `encoding`, the lowercase charset alone,
    and `encoding_by`, `bom` or `fallback`, in place of the combined value, such as
    `windows-1252 (fallback)`; valid UTF-8 with no byte order mark has no `encoding_by`. The run
    directory's schema stays `"2"`, an exception to its growth by addition only.
  - The text stage's identity version is 3, from 2, so a run over an index prepares its earlier
    text entries again rather than replaying them.
  - soffice runs in `gotenberg/gotenberg:9.10.1-libreoffice`, which carries the same LibreOffice,
    26.8.2.2, pinned by its multi-arch index digest.

## [v0.2.2] - 2026-11-23

This release requires the docproc base module at v0.4.2, whose default container images reach
`docproc run` as Changed says. Its command line, run directory, and exit codes are unchanged.

### Changed

- The module requires the docproc base module at v0.4.2, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full: soffice runs in
  `gotenberg/gotenberg:9.9.3-libreoffice`, which carries the same LibreOffice, 26.8.2.2, and the
  ffmpeg default is `linuxserver/ffmpeg:9.0.2-cli-ls90`, each pinned by its multi-arch index
  digest.

## [v0.2.1] - 2026-11-20

This release requires the docproc base module at v0.4.1, whose fixes reach `docproc run` as
Changed says. Its command line, run directory, and exit codes are unchanged.

### Changed

- The module requires the docproc base module at v0.4.1, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - Each tool run keeps its temporary files in a directory of its own under the host's `TMPDIR`,
    removed once the run's program is reaped, so a gs ended early leaves nothing beside its input.
    A host with a small tmpfs `/tmp` points `TMPDIR` at room enough.
  - A PDF portfolio's page is the portfolio's own page, one PNG and that page's text, where before
    it held each embedded document's pages. The stage adapters' version is 6, so a run over an
    index prepares its content again.
  - A ZIP holding an XPS package's `FixedDocumentSequence.fdseq`, whatever its name, is recorded
    as no processor yet, rather than failing in docproc-ooxml.
  - soffice's exit 77, a fatal error at its start such as a full disk, fails the part unrecorded,
    so a later run tries it again, rather than recording it failed.
  - docproc builds on 64-bit platforms only.

## [v0.2.0] - 2026-11-19

This release adds the audio window's variable and requires the docproc base module at v0.4.0,
whose changes reach `docproc run` as Changed says.

### Added

- `DOCPROC_AUDIO_LIMIT`, the longest audio window, its overlaps included, which an audio part's
  windows are cut under: `stages.Options.Audio`'s `Limit`. It starts at 30s, and a host sets it
  to its model's request size, the longest audio one request takes; a source no longer than it is
  one window. A value of 4s or less, which is not over twice the 2 s overlap between windows, zero
  among them, fails the run before any tool is looked up, as a value that doesn't parse does, the
  error naming the variable. The base module's
  [command page](../../docs/command.md#configuration) lists it.

### Changed

- The module requires the docproc base module at v0.4.0, and `docproc run` takes its changes,
  which its [CHANGELOG](../../CHANGELOG.md) states in full:
  - `DOCPROC_MAX_BYTES` bounds what the archive and document stages write, not only what a
    dispatch admits: an archive's entry, or a PDF's attachment, is stopped at the write that would
    pass it, its partial file removed, and is recorded as an unread child, `limited`, `past the
    bytes cap, <cap>`, with no size or SHA-256, where before it was written whole and then
    limited.
  - A PDF's pages are rendered, and their text layers read, in gs processes shared up to 8 pages
    a process, at `DOCPROC_CONCURRENCY` lanes; a shared process reads `DOCPROC_UNIT_TIMEOUT` as an
    idle bound between its pages. Every page's PNG and text, and the run directory's records of
    them, are as before.
  - A page gs renders with a message of its own, such as one whose `/Annots` is null, is
    rendered, where before it failed with "no PNG on stdout".
  - Each tool's version is checked in the environment its runs get, so a version manager's shim
    that answers only under the host's `HOME` is listed as unavailable in the tools table, and
    the stages that need it are `missing`, where before its runs each failed.

## [v0.1.0] - 2026-11-18

The first release. The command is built from spike-ai-pipeline at 6a631f1, its `cmd/docproc` and
its `internal/docproc/app` and `internal/docproc/apptest` packages, with `internal/app` and
`internal/apptest` started again from go-cli-sdk-template's template/v0.3.0 on go-cli-sdk v0.4.0
and go-core v0.12.0, in place of the spike's port of go-cli-sdk v0.2.0's.

### Added

- The module. `main.go` sits at its root, so
  `go install github.com/standards-lab/docproc/cmd/docproc@<version>` installs `docproc`. Its
  composition root, `internal/app`, its fixtures, `internal/apptest`, and its command packages,
  `internal/run` and `internal/report`, sit under `internal/`, and `integration` holds the
  built-binary tier's harness. It requires the docproc base module at v0.3.0, go-cli-sdk,
  go-core, and golang.org/x/sys.
- `docproc version`, which prints the module's version, or `(devel)` for a binary that carries
  none, and builds nothing: it reads no configuration and looks up no tool.
- `docproc run <path>... --out <dir> [--workers N] [--exclude <route>]... [--report]`. It collects
  its inputs first: a file argument is one input, and a directory yields its regular files in
  lexical order, each once. It follows a named symbolic link and no link under a directory, and
  fails before any tool is looked up on an argument that is neither a file nor a directory, or on
  arguments that yield no input. It then builds the stages over the host's tools under the run's
  bounds, leaving out each route `--exclude` names, prints the tools and the stage table, and
  dispatches the inputs, `--workers` at a time (at least 1, the host's CPUs by default), into a
  schema "2" run directory through `rundir`. It writes the run's manifest; one record line per
  event, each input's own line carrying its file's metadata, sniffed type, and time; each output's
  content, to the run directory's content store; and each quarantined part's file, kept and named
  on its line. The terminal shows one line per input and a summary.
- `docproc report <run-dir> [--run <id>]`, which renders a run's report and prints its index
  page's path: static HTML pages in the run's report directory, `runs/<run>/report/`, replacing
  earlier ones whole. It renders the latest run that ended, or the run `--run` names, one that
  never ended among them, its pages marked not ended. It reads the run directory alone: it reads
  no configuration and looks up no tool. An index page, `index.html`, has the run's header, from
  its manifest, a summary counting the inputs and their parts by route and outcome, and every
  input, sorted by path, with its route, outcome, and time, linking its content's page. A page
  per distinct content, `<sha256>.html`, lists the inputs holding it, then its part tree, each
  part's record metadata, and its outputs: a text, Markdown, or record output inline, a PNG or
  JPEG image and a WAV window shown at its content's file, any other output linked, and a
  quarantined part's kept file named. Every link into the run directory is relative, so the run
  directory moves or archives whole, and the pages load nothing from elsewhere. Every time is the
  record's, in UTC, so the same run directory renders the same bytes in any zone.
- `docproc run --report`, which, once the summary is printed, renders the run's report and prints
  its index page's path, as `docproc report` does: after a run that ended, and after a failure of
  the index or the content store, which still exits 1; never after a cancellation.
- The run's bounds, at docproc's starting values, each overridden by its `DOCPROC_` variable: the
  tools' memory bound, unit timeout, and stream idle bound; a dispatch's most parts, bytes, and
  depth; the bomb rule's ratio and size and an archive's most entries; a claim's release bound;
  the pages and windows held at once; a PDF's DPI, text layer, and most pixels; a video frame's
  largest side; and ImageMagick's area, memory, map, and disk limits. A value the command can't
  take fails the run before any tool is looked up, and the error names every such variable.
  `DOCPROC_SHUTDOWN_TIMEOUT` bounds the shutdown. The base module's
  [command page](../../docs/command.md#configuration) lists each variable with its bound and
  starting value.
- `docproc run`'s exit statuses: 0 when the run ends, failed parts and inputs stopped with an
  error included, the summary's last line counting both, a failure replayed from the index among
  the failed parts; 1 when it stops or never begins, a failure of the index or the content store
  or a cancellation ending the run's manifest with the error, or, with `--report`, a run that
  ended whose pages couldn't be rendered, which `docproc report` renders again; and 2 for help and
  every usage error, which build nothing. `docproc report`'s: 0 when the report is rendered; 1
  when nothing is: a run directory that doesn't exist or isn't one, a `--run` that isn't one of
  its runs, no `--run` and no run that ended, or a page that couldn't be read or written; and 2
  for help and every usage error.
- The built-binary integration tier, under the `integration` build tag, run by
  `mise run test:integration`: it builds `docproc` and runs it over the repository's committed
  fixtures, asserting its arguments, output, exit status, signals, and the run directory it
  writes, read back through `rundir`, and the report it renders of a fixture run and of a killed
  run.

[Unreleased]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.8...HEAD
[v0.2.8]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.7...cmd/docproc/v0.2.8
[v0.2.7]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.6...cmd/docproc/v0.2.7
[v0.2.6]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.5...cmd/docproc/v0.2.6
[v0.2.5]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.4...cmd/docproc/v0.2.5
[v0.2.4]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.3...cmd/docproc/v0.2.4
[v0.2.3]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.2...cmd/docproc/v0.2.3
[v0.2.2]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.1...cmd/docproc/v0.2.2
[v0.2.1]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.2.0...cmd/docproc/v0.2.1
[v0.2.0]: https://github.com/standards-lab/docproc/compare/cmd/docproc/v0.1.0...cmd/docproc/v0.2.0
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/cmd/docproc/v0.1.0
