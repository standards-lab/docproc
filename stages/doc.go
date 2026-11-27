// Package stages builds docproc's stages, the shipped wiring a host such as cmd/docproc or Bunrei
// registers with a dispatch.Dispatcher. [Build] finds each tool a route's stage requires through
// a [Lookup] the host gives, checks it against its package's minimum, and builds the route's stage
// over it under [Options], which carry every run bound and each stage's request. A tool it can't
// build, missing or too old, leaves out every stage that requires it, and a host may exclude a
// route; either way the route has no processor yet. Its [Set] holds the stages, and the stage
// table a host reports, dispatch's Tool and Row, one row per route.
//
// The stage adapters are this package's, unexported, each a dispatch.Stage over its format
// package and the tools it requires, with no partial mode: a stage has its tools, or isn't wired.
//
//   - document, route.Document: a PDF through gs, qpdf, pdfdetach, and pdftotext: qpdf's finding
//     that it needs a password raised as the part's flag, then dispatch.ErrEncrypted; its
//     AcroForm's "forms" record; each embedded file found "attachment <name>", under the bomb
//     rule; each page's text and PNG.
//   - office, route.Office: docproc-ooxml's document Markdown, "document", then each sheet's
//     Markdown and rows, both "sheet/<n>"; each medium found "document, media <n>". A legacy
//     document, an RTF, a WordPerfect document, an .xlsb, and an ODF text, spreadsheet,
//     presentation, or drawing package is converted through LibreOffice first, its Markdown's
//     converted_from naming the format; any other ODF package has no processor yet, and an
//     encrypted one is encrypted, as office's errors say.
//   - picture, route.VisionConverted: a TIFF's pages, or an EMF, WMF, SVG, HEIC, WebP, or AVIF,
//     to PNGs.
//   - vision, route.Vision: a PNG, JPEG, GIF, or BMP passed through.
//   - email, route.Nested: docproc-mail's lines as records, a message's Markdown, and its
//     attachments and media found where its message says; Item.After on a message's last output.
//   - onenote, route.OneNote: docproc-onenote's lines under --file-only, a page's Markdown, and
//     its media found "page <id>, media <n>".
//   - archive, route.Archive: a ZIP, a tar, a gzip, bzip2, zstd, or xz stream, a 7z, a RAR4 or
//     RAR5, and a cabinet, each entry found, or an unread child, its 7-Zip listing's flags raised
//     as the part's own; a split RAR volume has no processor yet, and 7zz never runs for it.
//   - audio, route.Audio: windows from one decode, then the plan record.
//   - video, route.Video: the plan record, then frames and windows from one ffmpeg run.
//   - text, route.Text: text decoded to UTF-8.
//
// A stage's outcome is classified once, here, from the facts package tool reports, for a unit of
// a part and for a part alike:
//
//	the content's, recorded  the tool exited nonzero by its own code: a docproc tool's 3 and 4,
//	                         gs's 1, qpdf's 2, 7zz's 8 (out of memory), and every other tool's
//	                         every nonzero exit; it died by its own fault (SIGSEGV, SIGABRT,
//	                         SIGBUS, SIGFPE, SIGILL), an allocation refused under the memory
//	                         bound included; it passed its own time bound (TimedOut); its output
//	                         didn't parse; a format package's content error (no audio or media
//	                         stream, an archive 7zz can't open or list, or listing past
//	                         inspect's Entries, an item unlike its listing); and, the
//	                         dispatcher's, a put over the store's size (dispatch.ErrTooLarge)
//	the run's, retried       the context ended; SIGKILL, SIGTERM, or another signal docproc
//	                         didn't send; the program didn't start; a docproc tool's exit 1 (not
//	                         written) or 2 (usage); 7zz's exit 7 (command line) or 255 (stopped);
//	                         soffice's exit 77 (a fatal error at its start); any error on
//	                         docproc's side of the pipe
//	encrypted                a stage's dispatch.ErrEncrypted alone, mapped from a refusal's
//	                         reason by internal/cli's one mapping
//	read elsewhere           a stage's dispatch.ErrReadElsewhere alone, onenote's
//	                         table-of-contents refusal, by the same mapping
//
// A unit's failure of the content's class is a dispatch.Warning naming the unit, and the part is
// prepared with the outputs it has; a part's fails it with dispatch.ErrRefused, recorded. A
// failure of the run's class fails the part unrecorded, so a transient fault is never replayed as
// a permanent gap. A death by a signal under a memory bound names the bound, as in "killed by
// SIGABRT under a 256 MiB memory bound".
//
// Three rules are each this package's once:
//
//   - classify reads a unit's or a part's failure by the table above.
//   - Each stage's identity hashes its format package's request resolved, every default filled
//     and each field that changes no output tagged out of its JSON, with the adapters' version,
//     and the office stage's own version too, raised when it reads content it ended unread, so
//     an index prepares that stage's parts again and no other's; its Version is its tools' whole
//     version lines.
//   - One rule, a stash's item, turns a tool's line's media and attachments into found parts, each
//     claiming the SHA-256 internal/cli's media name rule resolved, or unread children.
//
// The Meta keys and the record media types the stages set are dispatch's constants.
//
// What each stage yields, how the archive stage reads each kind, and how the stages are built,
// with the reasons, are on docs/api.md, "Dispatch".
//
// Its exported identifiers:
//
//   - [Build] builds a [Set] under [Options], through a [Lookup].
//   - [Routes] returns the ten routes in table order, and [ParseRoutes] parses route names,
//     refusing an unknown one with [ErrRequest], a refused request's cause, Build's refused
//     options among them.
package stages
