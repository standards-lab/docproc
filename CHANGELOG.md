# Changelog

All notable changes to `github.com/standards-lab/docproc`, docproc's Go base module, are documented
here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is
headed by the version its `vX.Y.Z` tag carries, and is the release's notes. Its exported API is its
contract: a change to it is a change here. This changelog covers the base module only; each system
tool under `tools/` keeps its own.

## [Unreleased]

## [v0.10.0] - 2026-11-27

This release captures each format's own metadata as a record output named `metadata`: an office
package's properties, a PDF's information dictionary and XMP packet, an audio or video file's
container and streams, and a picture's EXIF. Each format's record is a type of the new package
`metadata`, under a versioned media type of its own. A mail message's record and a OneNote page's
record serve as their metadata records, and a message's meta gains its threading keys. The
document stage now requires pdfinfo, and the vision stage requires magick. Each stage that yields a
new record or key hashes its own version anew, so an index prepares the parts of the office,
document, audio, video, vision, and vision-converted routes again, and the nested route's mail
parts, and no other stage's. `dispatch.Version` stays "1", grown only by the new media types and
keys, and the run directory's schema stays "2".

### Added

- Package `metadata`, the metadata records' shapes, one per format, importing no docproc package
  but `dispatch`, so a reader decodes a record without compiling the stages or the format
  packages. Each record is a record output named `metadata` whose event carries its SHA-256,
  never its content, which is in the content store.
- `metadata.Office`, under `dispatch.MediaOfficeMetadata`,
  `application/vnd.docproc.office-metadata+json; version=1`: an office package's core, extended,
  and custom properties, each in its stored order and stored types, and the format it was
  converted from, such as `doc` or `odt`. The office stage yields one after the document's
  Markdown, for every package with a property part or converted from another format.
- `office.Document.Metadata` is a document's `metadata.Office`, or nil for a package with no
  property part, converted from no other format.
- `metadata.PDF`, under `dispatch.MediaPDFMetadata`,
  `application/vnd.docproc.pdf-metadata+json; version=1`: a PDF's information dictionary as a
  `metadata.PDFInfo`, its standard keys typed, CreationDate and ModDate in UTC, and its custom keys
  kept as stored; its XMP packet as a `metadata.XMP`, its common Dublin Core and XMP properties
  typed, dates in UTC, and the packet kept whole; and its PDF version. The document stage yields
  one before the forms record, for every PDF with an information dictionary or an XMP packet. A
  PDF whose metadata pdfinfo can't read, or whose packet doesn't parse, gets a `metadata` warning
  and its pages still prepare; a PDF that needs a password gets the warning and ends with
  `ErrEncrypted`, as before.
- `document.InfoReader` reads a PDF's metadata through poppler's pdfinfo: `InfoReader.Metadata`
  returns a `metadata.PDF`, and `document.ErrXMP` is the cause of a packet that doesn't parse.
  `document.NewInfoReader` builds one from `document.Config.PDFInfo`, `document.PDFInfoSpec`
  declares pdfinfo, and `document.PDFInfoMinimum`, 22.02, is its minimum.
- `metadata.Media`, under `dispatch.MediaMediaMetadata`,
  `application/vnd.docproc.media-metadata+json; version=1`: an audio or video file's container
  format, duration in seconds, bit rate, and tags, kept as stored, in order; and each of its
  streams as a `metadata.MediaStream`: its index, type, codec, and tags, an audio stream's sample
  rate and channels, and a video stream's width, height, frame rate, and whether it is an attached
  picture. The audio and video stages yield one for every part whose probe finds a stream they
  decode: the audio stage before its first window, the video stage first, before its plan. The
  audio and video plan records keep their shapes.
- `audio.Source.Metadata` is the source's metadata record, read by the same ffprobe run that finds
  its audio stream, which `audio.Splitter.Probe` returns and `audio.WindowFile.Metadata` carries on
  the first window; `media.Event.Metadata` is a file's, which `media.Streamer.Events` yields first,
  before ffmpeg runs.
- `metadata.Picture`, under `dispatch.MediaPictureMetadata`,
  `application/vnd.docproc.picture-metadata+json; version=1`: a picture's EXIF capture time,
  DateTimeOriginal at OffsetTimeOriginal, in UTC, a time with no offset read as UTC; its camera's
  make and model; its orientation; its size in pixels; its GPS position as a `metadata.GPS`, in
  decimal degrees; and every EXIF property as magick prints it. The picture and vision stages
  yield one before the picture's PNGs or the picture itself, for every picture whose first image
  holds EXIF: a JPEG, PNG, TIFF, HEIC, WebP, or AVIF. A picture whose EXIF magick can't read gets
  a `metadata` warning and still prepares, and reading EXIF never changes the picture.
- `picture.EXIFReader` reads a picture's EXIF through ImageMagick's magick:
  `EXIFReader.Metadata` returns a `metadata.Picture`, and `picture.ErrMetadata` is the cause of
  magick output that can't be read: past 1 MiB, or with no size line. `picture.NewEXIFReader`
  builds one from `picture.Config`'s Magick and Limits, and `picture.MagickMinimum`, 7.1, is its
  minimum.
- The email stage's messages carry `dispatch.MetaInReplyTo`, `in_reply_to`, and
  `dispatch.MetaReferences`, `references`, in their outputs' meta when the message stores them,
  each as stored, `references` its Message-IDs separated by spaces. A message's `MediaMail` record
  is its metadata record, as a OneNote page's `MediaOneNote` record is the page's; neither format
  yields another.

### Changed

- The document stage requires pdfinfo, from poppler-utils 22.02 or later, beside gs, qpdf,
  pdfdetach, and pdftotext; `stages.Build` looks it up as it does the other poppler tools, a host
  without it, or with an older one, leaving the document stage out. poppler-utils ships pdfinfo
  with pdfdetach and pdftotext, so a host that ran the document stage before runs it still. The
  stage table's document row carries pdfinfo's version line, and `Options.Timeout` bounds its
  runs.
- The vision stage requires magick, ImageMagick 7.1 or later, which the picture and office stages
  require already; `stages.Build` looks it up for the vision route too, a host without it, or with
  an older one, leaving the vision stage out, so PNG, JPEG, GIF, and BMP parts have no processor
  there. The stage table's vision row carries magick's version line, its identity's version is
  magick's, and `Options.Timeout`, `Options.Memory`, and `Options.Magick` bound its runs.
- Each stage that yields a new record or key hashes its own version in its identity, so an index
  prepares its route's parts again: the office stage's, 3, the office route's parts; the email
  stage's, 2, the nested route's mail parts; the document stage's, 2, the document route's PDFs;
  the audio and video stages', 2 each, the audio and video routes' parts; and the picture and
  vision stages', 2 each, the vision route's PNGs, JPEGs, GIFs, and BMPs and the vision-converted
  route's TIFFs, EMFs, WMFs, SVGs, HEICs, WebPs, and AVIFs.
- The `audio` and `media` packages' ffprobe runs ask for the container's format name, bit rate,
  and tags and each stream's codec, sample rate, channels, frame size, frame rates, tags, and
  attached-picture disposition beside what they asked for before.
- `picture.LibreOfficeImage` is `gotenberg/gotenberg:9.10.2-libreoffice`, from 9.10.1-libreoffice,
  and `audio.FFmpegImage` is `linuxserver/ffmpeg:9.0.2-cli-ls91`, from 9.0.2-cli-ls90, each pinned
  by its multi-arch index digest. The new Gotenberg image carries the same LibreOffice, 26.8.2.2,
  and the new ffmpeg image the same FFmpeg, 9.0.2.

## [v0.9.0] - 2026-11-25

This release converts ODF text, spreadsheet, presentation, and drawing packages, WordPerfect
documents, and Excel binary workbooks through LibreOffice and reads them as the office stage reads a
legacy document, where they had no processor yet. Run over docproc-ooxml 0.2.0 and docproc-mail
0.2.0, the office and email stages also prepare what those releases read anew: text boxes, a pptx's
comments, an xlsx's notes and threaded comments, and an `.eml`'s or an mbox message's TNEF parts.
The office stage's identity now hashes the stage's own version, so an index prepares the office
route's parts again and no other stage's. v0.8.0's notes were wrong to say an index prepares every
PDF again: the stage adapters' version 7 re-prepared the parts of every stage but text and vision,
whose identities don't hash it. The module still requires golang.org/x/text alone.

### Added

- The office stage converts, through LibreOffice, ODF text, spreadsheet, and presentation
  packages and their templates (`.odt`, `.ott`, `.ods`, `.ots`, `.odp`, `.otp`) to docx, xlsx,
  and pptx, an ODF drawing or drawing template (`.odg`, `.otg`) to pptx through Impress's import
  filter, a WordPerfect document to docx through LibreOffice's WordPerfect import filter, and an
  Excel binary workbook (`.xlsb`, by its name) to xlsx, then reads the result through
  docproc-ooxml, as it does a legacy document.
  `office.Document.ConvertedFrom`, and the document Markdown's `converted_from`, name the source
  format: the ODF type's extension, `wpd`, or `xlsb`.
- `picture.LibreOfficeRun.InFilter`, soffice's `--infilter`, the import filter a run reads every
  input through.

### Changed

- An unencrypted ODF package of any other type, a formula, chart, database, or image among them,
  still ends with no processor yet, the error now office's, matching `dispatch.ErrNoProcessor` and
  naming the package's type. An ODF package of any type whose manifest holds encryption data ends
  with an error matching `dispatch.ErrEncrypted`, and one whose ZIP or manifest can't be read with
  one matching `dispatch.ErrRefused`, before LibreOffice runs.
- The office stage's identity hashes its own version, 2, so an index that holds ODF, WordPerfect,
  or `.xlsb` parts as no processor yet prepares the office route's parts again, and no other
  stage's; the stage adapters' version stays 7.
- With docproc-ooxml 0.2.0 on the host, the office stage's output holds a document's text boxes,
  a pptx's comments, and an xlsx's notes and threaded comments, and its warnings no longer list
  them as not read; with docproc-mail 0.2.0, the email stage's output holds a TNEF part's
  attachments, as the message's own, and its bodies, where it listed one `winmail.dat` attachment
  with a warning. Each stage's identity holds its tool's version line, so an index prepares that
  route's parts again under the new release. `office.OfficeMinimum` and `email.MailMinimum` stay
  0.1.0, and an older release still runs, reading as it did.

## [v0.8.0] - 2026-11-25

This release reads a PDF page's text with poppler's pdftotext in its reading order, where gs's
txtwrite laid it out: a multi-column page reads column by column, without the runs of spaces gs
padded its columns with ([`docs/acceptance.md`](docs/acceptance.md), "PDF page text").
`document.Ghostscript.Text` is removed, a breaking change in a minor release before 1.0, and the
document stage requires pdftotext beside gs, qpdf, and pdfdetach. The stage adapters' version is
7, so an index prepares every PDF again. No wire value and no run directory's schema changes.
The module still requires golang.org/x/text alone.

### Added

- `document.TextReader`, which reads a PDF page's text layer through poppler's pdftotext in its
  reading-order mode, neither `-layout` nor `-raw`, one run a page. `document.NewTextReader`
  builds one over `document.Config.PDFToText`, `document.PDFToTextSpec` declares pdftotext, and
  `document.PDFToTextMinimum` is the oldest it accepts, 22.02. `TextReader.Text` drops the form
  feed pdftotext ends a page with. It refuses a page below 1 before pdftotext runs, and a page
  past the last, which pdftotext refuses with exit 99, each with an error wrapping
  `document.ErrRequest`. It gives any other failure as a `*tool.ExitError` carrying the first
  4 KiB of pdftotext's stderr.
- `document.Config.PDFToText`, the pdftotext Tool `NewTextReader` requires, and
  `document.Config.Text`, the `*document.TextReader` `NewPreparer` requires.
- pdftotext is a required tool of the document stage, beside gs, qpdf, and pdfdetach:
  `stages.Build` looks it up and checks its version, and a missing or too old pdftotext leaves the
  document route Missing with its reason, even under a request that reads no text. Its version
  line is part of the stage's identity, and `stages.Options.Timeout` bounds its runs.

### Changed

- A PDF page's text, `document.PageImage.Text` and the document stage's `page N` text output,
  is pdftotext's reading order, where it was gs's txtwrite layout: a multi-column page reads
  column by column with no run of spaces padding its columns, a word hyphenated at a line's end
  is joined, and the paragraphs of columns that line up may alternate between the columns.
- `document.Preparer.Images` reads each page's text through the Preparer's `TextReader`, one
  pdftotext run a page, before the page renders, under either runner; gs's processes, shared or a
  page's own, render only, and no text scratch directory is made. A page whose text run fails
  fails alone, at its `text` step, unrendered, and the stream carries on. Page order, the
  concurrency bound, and every page's PNG are as before.
- `document.NewPreparer` requires `Config.Text` beside `Config.Renderer`, and panics naming
  `Config.Text` when it is nil.
- The stage adapters' version, part of every adapter's configuration hash, is 7, from 6, since
  every PDF's page text changes shape: an index prepares every PDF again.

### Removed

- `document.Ghostscript.Text` and gs's shared txtwrite run: gs counts and renders a PDF's pages
  and reads no text.
- The text half of `document.ImagesRequest.PagesPerProcess`, which bounds a shared gs process's
  renders alone.

## [v0.7.0] - 2026-11-25

This release makes text the last resort: content no processor claims, or that fails a claim's own
check, is read as raw text unless its head holds a NUL byte, so HTML, EPUB entries, `#!` scripts,
and unlisted text are prepared where they were unrouted. Routes and reasons change for callers;
no wire value is new. The module still requires golang.org/x/text alone.

### Changed

- Content no processor claims, or that fails a claim's own check, falls back on its head: it
  routes `text` when its first `route.HeadLen` bytes hold no NUL byte, and `unrouted` otherwise.
  HTML, CSS, JavaScript, an EPUB's entries, a LICENSE, and other text of no signature, which were
  unrouted, are read as raw text, with no structure. UTF-16 without a byte order mark holds NULs,
  so it is unrouted. An OLE2 file with neither a legacy nor a `.msg` extension falls back, and is
  unrouted as not text.
- The text-extension list is gone, with the rule by which it outranked a short signature: a name's
  extension never makes content text. A `.txt` whose head holds a NUL byte is unrouted.
- A `#!` script routes `text`, its kind still `shebang` and inspect still flagging it executable;
  before, it was unrouted.
- An mbox is claimed only by its `From ` line followed by a header block whose first line is a
  field and which holds From and either Date or Received. Without one, the content falls back.
- Content named `.eml` or stored as `message/rfc822` or `message/global` whose header isn't
  message-shaped falls back, where before it was unrouted. A stored audio or video type claims
  content of no signature only when its head holds a NUL byte; text stored as audio routes
  `text`.
- Route reasons: the fallback's reason is the claim it failed, or `no routed signature`, then
  `, text by its head` or `, not text`, such as `OLE2 signature without a legacy or .msg
  extension, not text` and `mbox "From " line without a message header, text by its head`; a
  message that isn't header-shaped is `a message by its <name or type>, but not header-shaped`,
  then `; text by its head` when it is text. A script's reason is `"#!" line, a script`, where it
  was `"#!" line, an executable script`. `text by its <ext> extension`, its short-signature form,
  and `no routed signature` alone are gone.
- inspect's ZIP end-of-central-directory scan reaches `#!` scripts, content of no signature, and
  text after a byte order mark, so one with a ZIP appended is flagged polyglot. The PDF-header
  pattern still skips those kinds, so text quoting `%PDF-` isn't flagged. Evidence for content of
  no signature reads `in content of no signature`.
- No wire value is new, and the text stage's output is unchanged. An index recorded nothing for
  content that was unrouted; the next dispatch prepares what now routes `text`.

## [v0.6.0] - 2026-11-25

This release reads RAR, xz, and bzip2 archives, which routed to no stage, and routes MPEG program
streams and Ogg video to the video stage. Two changes break callers: 7-Zip's minimum rises to
26.06, and an Ogg holding video gets a kind of its own. The module still requires
golang.org/x/text alone.

### Added

- `route.RAR4` (`rar4`), `route.RAR5` (`rar5`), `route.XZ` (`xz`), and `route.Bzip2` (`bzip2`),
  each identified by its signature and routed `archive`. A bzip2 stream is checked past `BZh` by
  its block size digit and by its first block's or end-of-stream magic.
- `route.MPEGPS` (`mpeg-ps`), an MPEG-1 or MPEG-2 program stream identified by its pack start
  code, and `route.OggVideo` (`ogg-video`), an Ogg whose beginning-of-stream pages in the head
  start a Theora, VP8, Dirac, or OGM video stream, each routed `video`.
- `route.RARVolume` reports whether a RAR's main archive header marks it as one volume of a split
  set: RAR4's volume flag, or the volume bit of RAR5's archive flags. It reports no volume for a
  RAR5 whose first header is its encryption header.
- The archive stage reads a RAR4 through 7zz's rar handler and a RAR5 through its rar5 handler,
  and each handler refuses the other version. A RAR's entries are checked as a 7z's are, each
  found `RAR entry <name>`. A RAR whose headers are encrypted ends with `ErrEncrypted`; an
  encrypted entry under clear headers is an unread child, and its siblings are read. A RAR whose
  first header marks it as a volume of a split set ends with `ErrNoProcessor`, "a split RAR
  volume", and 7zz never runs for it, so no sibling volume is opened.
- The archive stage reads an xz stream through 7zz, as it reads a zstd stream: its content is
  found `xz content <name>` and held to the size the stream's index declares. It reads a bzip2
  stream through compress/bzip2, as it reads a gzip stream: concatenated streams are read whole,
  and the content is found `bzip2 content <name>`. The part takes the stream's name without its
  `.xz` or `.bz2`. A tar inside either, named `.txz`, `.tbz2`, `.tbz`, `.tar.xz`, or `.tar.bz2`,
  is a `.tar` part read as a tar.
- `sevenzip` lists and reads `route.RAR4`, `route.RAR5`, and `route.XZ`. An `Extract` of an xz
  stream names no item, as an `Extract` of a zstd stream does.
- The video stage prepares MPEG-1 and MPEG-2 program streams, and Ogg files holding Theora, VP8,
  Dirac, or OGM video, taking frames and windows from one ffmpeg run.
- inspect's extension table names `.rar` as RAR4 or RAR5; `.xz` and `.txz` as xz; `.bz2`,
  `.tbz2`, and `.tbz` as bzip2; `.mpg`, `.mpeg`, and `.vob` as an MPEG program stream; and `.ogv`
  as Ogg video. Content under one of those names that isn't its kind is flagged
  `signature-mismatch`. `.ogg` accepts Ogg video as well as Ogg.

### Changed

- **Breaking:** `sevenzip.SevenZipMinimum` is 26.06, from 24.07, matching the root `mise.toml`'s
  pin. 25.00 fixes CVE-2025-53816 in RAR's decoder, 26.02 fixes CVE-2026-14266 in xz's, and 26.03
  through 26.06 fix vulnerabilities they don't name. An older 7zz fails `sevenzip.NewReader` with
  a `*tool.VersionError` wrapping `tool.ErrTooOld`, and `stages.Build` leaves out the archive
  stage. The archive stage's identity carries 7zz's version line, so an index prepares its archive
  content again once 7zz is upgraded.
- **Breaking:** an Ogg whose first pages begin a video stream is `route.OggVideo` (`ogg-video`)
  and routes to video; before, it was `route.Ogg` (`ogg`) and routed to audio. A record's kind for
  an `.ogv`, or for an `.ogg` holding video, changes from `ogg`. The part is prepared as video,
  with frames and windows under the video plan, in place of audio windows. Any other Ogg stays
  `ogg` and routes to audio.
- `inspect.CheckListing` accepts `route.RAR4` and `route.RAR5`, naming and checking their entries
  as a 7z's, and `route.XZ`, checking its one entry by its declared size as a zstd stream's.
  Before, it refused them as a kind it doesn't read.
- An index recorded nothing for content that was Unrouted: a RAR, an xz or bzip2 stream, or an
  MPEG program stream. The next dispatch prepares it.

## [v0.5.0] - 2026-11-25

This release splits the text stage's encoding into the charset and how it was chosen, a breaking
change to the text output's Meta, and moves the default LibreOffice image. The module still
requires golang.org/x/text alone.

### Added

- `dispatch.MetaEncodingBy`, the text output's `encoding_by`: `bom` when a byte order mark chose
  its encoding, `fallback` for windows-1252 taken because the text isn't UTF-8, and absent for
  valid UTF-8 with no mark, an empty part included.

### Changed

- **Breaking:** the text output's `encoding` (`dispatch.MetaEncoding`) is the lowercase charset
  alone, one of `utf-8`, `utf-16le`, `utf-16be`, `utf-32le`, `utf-32be`, or `windows-1252`, and
  how it was chosen moves to `encoding_by`:
  - `utf-8` → `encoding: utf-8`, no `encoding_by`;
  - `windows-1252 (fallback)` → `encoding: windows-1252, encoding_by: fallback`;
  - `<charset> (byte order mark)` → `encoding: <charset>, encoding_by: bom`.

  dispatch's wire stays `"1"` and the run directory's schema `"2"`. The split is an exception to
  their growth by addition only, which `dispatch.Version` and the [API page](docs/api.md) record.
  The combined value had to be parsed to recover the charset, and no reader could look it up as an
  encoding name. The split comes before more text sources are added. Its one known reader,
  docs-corpus's `tools/coverage`, decodes text outputs by it and changes in docs-corpus; a text
  output is always UTF-8, so a reader needs neither key to decode one.
- The text stage's identity version is 3, from 2, so an index prepares its earlier text entries
  again rather than replaying them.
- `picture.LibreOfficeImage` is `gotenberg/gotenberg:9.10.1-libreoffice`, from 9.9.3-libreoffice,
  pinned by its multi-arch index digest. The new image carries the same LibreOffice, 26.8.2.2, and
  the pictures' golden sums are unchanged.

## [v0.4.2] - 2026-11-23

This release moves the default container images that soffice and ffmpeg run in, and currency now
keeps them current. Its exported API is unchanged, and the module still requires golang.org/x/text
alone.

### Changed

- `picture.LibreOfficeImage` is `gotenberg/gotenberg:9.9.3-libreoffice`, from 9.1.0-libreoffice,
  and `audio.FFmpegImage` is `linuxserver/ffmpeg:9.0.2-cli-ls90`, from 9.0.2-cli-ls89, each pinned
  by its multi-arch index digest. The new Gotenberg image carries the same LibreOffice, 26.8.2.2,
  and the pictures' golden sums are unchanged.
- Currency reports, and upgrade moves, the container images pinned in the Go source, tag and
  digest together, as it does the compose harness's.

## [v0.4.1] - 2026-11-20

This release fixes how the tools' runs keep their temporary files, how a PDF portfolio's pages are
rendered, and how three failures are classified or reported. Its exported API is unchanged, and
the module still requires golang.org/x/text alone.

### Changed

- docproc supports 64-bit platforms only. The README and the package docs, which said the module
  builds on every platform, say every 64-bit platform; nothing is checked or built for 32-bit.
- `document.Ghostscript.Render` and `Text` give gs the PDF's path through
  `--permit-file-read`, as `Count` does, and gs splits a permitted path at `:`, so a path holding
  a colon is refused by Render and Text, as Count, and so `document.Preparer.Images`, already
  refused it. A file that isn't a PDF is opened as a PDF rather than run as PostScript: gs reports
  it can't open it and exits zero, so Render fails with "no PNG on stdout", quoting gs's message,
  and Text's error wraps `document.ErrRequest`, where before gs exited 1.
- The stage adapters' version, part of every adapter's configuration hash, is 6, from 5, since a
  portfolio's page outputs change: an index prepares its content again.
- The development harness's PostgreSQL, compose's one pin, is 19.0-alpine, from 18.7-alpine; the
  index's live tier passes unchanged against it.
- The stages package's release test also dispatches an mbox it generates, 100 messages each with
  an 8 KiB body and a distinct 64 KiB attachment, and checks the working directory's peak stays
  below the attachments' sum, so a mail stage that released nothing fails it.

### Fixed

- Each exec-runner run's `TMPDIR` is a new, empty directory of the run's own, `docproc-run-*`,
  under the host's `TMPDIR`, removed once the program's group has been killed and reaped, whether
  the run succeeded, failed, was cancelled, or passed its bound. A run with no working directory
  gets one too, and a run whose directory can't be made fails before the program starts. Before,
  `TMPDIR` was the run's working directory, so a gs ended early left its temporary files, an
  empty `gs_` file or a portfolio's `gpdf-collection-` files, beside its input, and a read-only
  input directory left gs nowhere to write. `HOME` is still the run's working directory, and the
  version check's `HOME` is still a new empty directory of its own; its `TMPDIR` is now the run's,
  as every run's is. The container runner is unchanged.
- soffice's exit 77, LibreOffice's fatal error at its start, such as no disk space, no write
  access, or a broken profile or installation, is the run's failure, as 7zz's 7 and 255 are: a
  part's 77 fails it unrecorded, so a later dispatch tries it again, and a unit's ends the stage.
  Before, it was the content's, and the part was recorded as failed. Every other nonzero soffice
  exit is still the content's.
- `sevenzip.Reader.Extract` returns its writer's error, wrapped with the item and the archive,
  when a write to it fails. Before, 7zz's death at the broken pipe came back in its place, a
  `*tool.ExitError`, and the writer's error was lost. Every other outcome is as before.
- A ZIP holding `FixedDocumentSequence.fdseq`, whole or as its first piece, routes `Unrouted` as
  "an XPS package", whatever its name, ahead of the OOXML rules; with no stage for it, its outcome
  is "no processor yet". Before, an XPS package, an OPC package with `[Content_Types].xml` like
  OOXML's, routed by its name, so one named `.docx` went to office and failed in docproc-ooxml.
- A PDF portfolio's page, rendered or read by a process of its own, as `document.Preparer.Images`
  prepares every portfolio page, is the portfolio's own page: one PNG and that page's text. Before,
  gs rendered the portfolio's embedded documents in its place, the page range applied to each, so
  Render wrote a PNG for each document and Text gave every document's text. Every other PDF's
  pages, PNG, text, and messages, are byte for byte as before.

## [v0.4.0] - 2026-11-19

This release runs a PDF's pages through shared gs processes, holds what the stages write to the
bytes cap, and checks each tool's version in the environment its runs get. The module still
requires golang.org/x/text alone.

### Added

- `document.ImagesRequest.PagesPerProcess`, the most pages one gs process renders, and reads the
  text layers of, for a PDF's pages: zero is a default of 8, and a negative value is refused by
  `Images` and by `stages.Build`. Like `Concurrency`, it is outside the request's JSON and so the
  document stage's identity.
- `tool.Tool.Timeout`, the Spec's Timeout, for an adapter that runs many units in one process
  and bounds its idleness by a unit's bound.
- `dispatch.Remaining`, the bytes the dispatch that called a stage's `Prepare` may still admit:
  `Limits.Bytes` less the sizes it had admitted as `Prepare` started, the part being prepared
  among them, read from the context the dispatcher passes to `Prepare`. It says nothing when no
  bytes cap is set. A stage that writes the parts it finds holds them to it, and finds a part it
  stopped writing there as an unread child, `dispatch.Limited`, which `Found` now takes beside
  quarantined, encrypted, no processor yet, and failed; the dispatcher gives it the bytes cap's
  reason, `past the bytes cap, <cap>`, and doesn't record its parent, as for a found part past a
  cap.

### Changed

- `document.Preparer.Images` runs a PDF's pages through shared gs processes, for every PDF: at
  concurrency C, lane k takes pages k, k+C, k+2C, and so on, up to `PagesPerProcess` a process
  through gs's page list, its PNGs split from one stdout, and, for a request with `Text`, one
  txtwrite process before it whose files, a page each, go to a temporary directory of the
  Preparer's own, never the input's. The run that counts the pages and the shared processes work in
  that directory, which is their `TMPDIR`, so the temporary files a gs ended early leaves are
  removed with it and never left beside the input. A process's pages go to gs as a page list, each
  run of consecutive pages a range, and a batch ends early where its list would pass the 2047 bytes
  gs takes in an argument. A page `MaxPixels` lowers gets a process of its own, as does a batch of
  one page, and every page of a PDF portfolio, whose processes gs runs over its embedded documents'
  pages. Pages are yielded in order, at most C held, and every page's PNG and text, the document
  stage's identity, and its outputs and warnings are as before. A shared process takes the gs
  Tool's Timeout as an idle bound between its writes, time waiting on the consumer not counted; a
  process of a page's own takes it whole, as before. A shared process that fails as the content's
  keeps the pages it handed over, and each other page is prepared in processes of its own, as
  before, so only a page that fails there fails; one that fails as the run's fails the pages it
  didn't hand over with its error, which ends the stage as before. Under the container runner,
  whose runs see only the PDF's directory, each page's text is still read by a process of its own.

### Fixed

- `Limits.Bytes` bounds what the stages package writes, not only what the dispatcher admits. The
  archive stage's ZIP, tar, 7z, and cabinet entries and gzip and zstd content, and the document
  stage's attachments, stop at the write that would take them past `dispatch.Remaining`, less what
  the same `Prepare` has written for its earlier parts, so one archive's entries together stay
  within it; the partial file is removed, and the entry is an unread child, limited, `past the
  bytes cap, <cap>`, with no size or SHA-256. Before, such an entry was written whole, then hashed
  and refused: a 107 MB ZIP whose one entry expands 999:1 to 100 GiB wrote all 100 GiB, about
  163 s, before it was limited. An entry within the cap is written and admitted as before. The
  Rust tools' children, which their processes write, are still admitted once written.
- `tool.NewExec`, and so `stages.Build`, checks a tool's version in the environment its runs get:
  the fixed environment and the Spec's `Env`, with `HOME` and `TMPDIR` a new empty directory of
  the check's own, standing in for each run's working directory, where the check gave them none.
  A program that answers only under the host's `HOME`, such as a mise shim, which then refuses
  the user's configuration as untrusted, is refused at construction: its row in the tools table
  says why, the stages that need it are `missing`, and no part runs through it, where before its
  route read `wired` and every run of it failed. A tool that answers under both builds as before.
- `document.Ghostscript.Render`, and so `document.Preparer.Images`, renders a page gs renders with
  a message of its own, such as `**** Error: Page drawing error occurred.` for a page whose
  `/Annots` is null. gs's render runs, a page's own and a shared process's, take
  `-sstdout=%stderr`, as its text runs did, so its messages go to stderr, dropped for a run that
  succeeds, and stdout holds the PNGs alone; before, the message came ahead of the PNG on stdout,
  the page failed with "no PNG on stdout", and its error quoted up to 4 KB of the PNG. A page past
  the last is still an error carrying gs's message, now read from stderr. Every other page's PNG
  is as before.

## [v0.3.0] - 2026-11-18

This release names a run's report directory, which `cmd/docproc`'s report renders into. The
module still requires golang.org/x/text alone.

### Added

- `rundir.Dir.ReportDir`, a run's report directory, `runs/<run>/report`, relative to the run
  directory, which schema `"2"`'s layout now names. A reader from `Open` answers it, as does a
  writer for the run it is writing; it refuses an id that isn't a run of the directory, and it
  makes nothing: a report's writer makes the directory.

## [v0.2.0] - 2026-11-18

This release adds the run directory's packages, moved from spike-ai-pipeline's
`internal/docproc/rundir` and `internal/docproc/schema` at 6a631f1 and made public, so
`cmd/docproc` writes a run directory and a reader in another module, such as `cmd/docinfer`,
reads it. The module still requires golang.org/x/text alone.

### Added

- `rundir`, the run directory's one handle. `Open` reads a run directory and creates nothing;
  `Create` makes the directory, its `rundir.json`, the index, and the content store. Each refuses
  a run directory of another schema, or one holding `runs/` with no `rundir.json`, which schema
  `"1"` wrote. A reader has `Dir.Runs`; `Latest`, the latest run that ended; `Manifest`; `Record`,
  a run's lines as an `iter.Seq2` that yields `ErrNotEnded` last for a run that hasn't ended;
  `Content`; and `Kept`, a quarantined part's kept file. `ContentPath` returns a content's file
  relative to the run directory. A writer has `Dir.Index`; `Begin`, which begins a `Run` and
  removes the working directories a killed run left; and `Quarantine`, which keeps a quarantined
  part's file once, at `quarantine/<sha256>`. A `Run` appends one record line per `Write`, each
  written through, and `End` records its end and its error. Every method that takes a run's id
  refuses one that isn't a run of the directory. One process writes a run directory.
- `rundir/schema`, the run directory's schema `"2"`, which holds shapes alone. `Event` is a record
  line of kind part, output, warning, or error, with its `Span`. `Manifest` holds a run's docproc
  `Build`, its start and end, its workers, its path arguments and how many inputs they named, the
  tools and the stage table as `dispatch.Tool` and `dispatch.Row`, and the error it ended with.
  `File` holds an input's file metadata and its sniffed type. `Time`, which `TimeOf` makes, is a
  time in UTC. The schema includes `dispatch`'s shapes at `dispatch.Version` `"1"`, aliased, and
  the package imports no docproc package but `dispatch` and `inspect`.

## [v0.1.0] - 2026-11-16

The first release. The base module was copied, in one commit, from spike-ai-pipeline-processing
v0.9.0 (4402f7c), its packages, and from spike-ai-pipeline at 6a631f1, its
`internal/docproc/stages` package, which becomes `stages`; the module path and the import paths
changed and nothing else. Changed says how it differs from the spike. The spike's findings and
bake-off tiers, its corpus, vision, and audio test helpers, its document experiment surface,
media's live sources, silencedetect, every test that imports spike-ai-pipeline-client, and its
fixtures scripts stay in the spike.

### Added

- The packages `route`, `inspect`, `tool`, `office`, `email`, `onenote`, `document`, `picture`,
  `audio`, `media`, `sevenzip`, `dispatch`, `dispatch/dispatchtest`, and `stages`, with
  `internal/cli`, `internal/goldentest`, `internal/mediatest`, and `internal/tooltest`. The
  module requires golang.org/x/text alone, at v0.44.0, and no longer
  spike-ai-pipeline-client; its go directive is 1.27.4. It builds on every platform, and only
  the exec runner needs Linux. No package imports a model client.
- `stages.Build(ctx, lookup, Options)`, the shipped wiring: each route's stage over its format
  package and the tools it requires, found through the host's `Lookup` and checked against each
  package's minimum, a stage whose tool is missing or too old left out; its `Set` holds the
  stages, dispatch's tool table and stage rows, and the inspection options. `Options` carries
  every run bound (Timeout, Idle, Memory, Concurrency, Env), ImageMagick's limits, the
  inspection options, and each stage's request, none with a default but Concurrency, which is one
  when zero.
- One failure classification: a tool's own exit, its own fatal signal, its own time bound,
  output that doesn't parse, and a format package's content errors are the content's, a part's
  matching `dispatch.ErrRefused` and recorded, a unit's a `dispatch.Warning`; anything else is
  the run's, never recorded. A death by signal under a memory bound names both: "killed by
  SIGABRT under a 256 MiB memory bound".
- `dispatch`'s content store, `Content`, with `FileContent` and `MemoryContent`, and its index
  log, `Index.Items` and `Append` over a part's interleaved outputs, warnings, and children,
  with leases, renewals, and takeovers; `dispatch/dispatchtest`'s Index and Content conformance
  suites, which every store passes, and its fixture stage.
- dispatch's shapes as JSON, versioned by `dispatch.Version` "1": events, parts and their
  `ContentType`, outputs with their SHA-256, warnings, the tool table and stage rows, the Meta
  keys, and the record media types.
- `tool`: a run's facts, `ExitError.Signal` and `TimedOut`; `Spec.MemoryLimit`, applied by the
  exec runner through util-linux's `prlimit --as=<bytes> --` before the program runs, and by the
  container runner as `--memory`; `Spec.Timeout`, `Spec.Idle`, and `Spec.Env`; `Tool.AtLeast`
  and `Tool.VersionLine`.
- `internal/cli`, the envelope reader the three readers share: a tool's lines, schema `"2"`
  checked on the first, its stderr warnings parsed as they arrive, its exit codes and refusal
  classes, and its media names resolved to a path and SHA-256.
- `audio.Cutter`, the stream cutter `audio` and `media` share, and the audio and video plan
  records, `audio.PlanRecord` and `media.VideoPlan`.
- The unit tier on `internal/tooltest`'s stand-in tool, which reads no `~/docs` and runs no real
  program but util-linux's `prlimit`, for the memory bound; the readers replay each tool's committed
  goldens in place, skipping, by name, when the tools tree is absent. The integration tier, under
  the `integration` build tag, runs the real tools, docproc's three built from the same checkout,
  through `mise run test:integration`; its whole-fixture test checks every committed fixture's route
  and outcome through `stages`.

### Changed

- Constructors take a Config and panic by name on a nil required field; each package's refused
  request matches its one `ErrRequest`; every time returned or written is UTC.
- No bound, cap, or limit has a library default: inspect's bomb rule, the memory index's lease,
  and tool's kill delay are the host's, and the page and window concurrency is one at a time
  until the host raises it.
- `route` owns the extension table and the encrypted-package detector; a ZIP holding
  `[Content_Types].xml` routes as office whatever its name, and content of no known signature
  routes by a stored audio or video type.
- `inspect`'s per-format archive checks become seven rule names; a link, a device, and a
  traversing or absolute name are active, a bomb and an unreadable directory unsafe. The bomb
  rule weighs each entry and the archive's summed declared sizes, a ZIP whose declared
  compressed sizes pass its own size is a bomb, and a ZIP listing past `Options.Entries` is
  unreadable before it is opened. The XML scans decode a declared charset or a UTF-16 mark.
- `dispatch` walks an input from a path or a spooled stream, its SHA-256 checked, puts each
  output's content in order while the stage works, replays a seen part from its log, resumes an
  input after its cursor, and guards every string it writes. It imports `route` and `inspect`
  alone: the stage adapters move to `stages`, unexported.
- The `office`, `email`, and `onenote` readers decode docproc-ooxml's, docproc-mail's, and
  docproc-onenote's schema `"2"`, each at minimum 0.1.0, through `internal/cli`.
- `document`'s form never fails on a field's value, and a signature field reports Signed;
  `Images` writes each page to a file, a page past MaxPixels rendered at the largest DPI that
  fits.
- `picture` takes the caller's kind and carries ImageMagick's limits on every magick run;
  `Converter.RunLibreOffice` is the one LibreOffice runner, office's legacy and RTF conversion
  among its callers.
- `audio` decodes a source once, its windows cut as the stream arrives, v0.9.0's windows byte
  for byte under its tracker detector; `media` cuts with the same cutter.
- `sevenzip` reads a whole archive in one 7zz run, each item held to its listed size and
  CRC-32, and requires 7zz 24.07 or later.

### Removed

- `internal/textscore`, its speech harness included; the text scoring the document tests need
  is theirs, unexported.
- `NewScript`, the silencedetect and volumedetect detectors, `audio.Join` and the transcript
  join (docinfer's), `picture`'s Corrector, `document`'s region rendering and corrections, the
  stages' own tool table, the schema `"1"` decoders, and the per-stage refusal mappings.

### Known limits

The [limits page](docs/limits.md) states them: each stage's tool's limits, docproc-ooxml's
behavior under a memory bound, the Threader's memory, and the formats no stage reads.

[Unreleased]: https://github.com/standards-lab/docproc/compare/v0.10.0...HEAD
[v0.10.0]: https://github.com/standards-lab/docproc/compare/v0.9.0...v0.10.0
[v0.9.0]: https://github.com/standards-lab/docproc/compare/v0.8.0...v0.9.0
[v0.8.0]: https://github.com/standards-lab/docproc/compare/v0.7.0...v0.8.0
[v0.7.0]: https://github.com/standards-lab/docproc/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/standards-lab/docproc/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/standards-lab/docproc/compare/v0.4.2...v0.5.0
[v0.4.2]: https://github.com/standards-lab/docproc/compare/v0.4.1...v0.4.2
[v0.4.1]: https://github.com/standards-lab/docproc/compare/v0.4.0...v0.4.1
[v0.4.0]: https://github.com/standards-lab/docproc/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/standards-lab/docproc/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/standards-lab/docproc/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/v0.1.0
