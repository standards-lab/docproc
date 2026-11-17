// Package stages builds docproc's stages, the shipped wiring a host such as cmd/docproc or Cerebro
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
//   - document, route.Document: a PDF through gs, qpdf, and pdfdetach: qpdf's finding that it
//     needs a password raised as the part's flag, then dispatch.ErrEncrypted; its AcroForm's
//     "forms" record; each embedded file found "attachment <name>", under the bomb rule; each
//     page's text and PNG.
//   - office, route.Office: docproc-ooxml's document Markdown, "document", then each sheet's
//     Markdown and rows, both "sheet/<n>"; each medium found "document, media <n>".
//   - picture, route.VisionConverted: a TIFF's pages, or an EMF, WMF, SVG, HEIC, WebP, or AVIF,
//     to PNGs.
//   - vision, route.Vision: a PNG, JPEG, GIF, or BMP passed through.
//   - email, route.Nested: docproc-mail's lines as records, a message's Markdown, and its
//     attachments and media found where its message says; Item.After on a message's last output.
//   - onenote, route.OneNote: docproc-onenote's lines under --file-only, a page's Markdown, and
//     its media found "page <id>, media <n>".
//   - archive, route.Archive: a ZIP, a tar, a gzip stream, a zstd stream, a 7z, and a cabinet,
//     each entry found, or an unread child, its 7-Zip listing's flags raised as the part's own.
//   - audio, route.Audio: windows from one decode, then the plan record.
//   - video, route.Video: the plan record, then frames and windows from one ffmpeg run.
//   - text, route.Text: text decoded to UTF-8.
//
// Three rules are each this package's once:
//
//   - classify reads a unit's or a part's failure as layers.md's table states, from the facts
//     package tool reports: a tool's own exit, its own fatal signal, its own time bound, and
//     output that doesn't parse are the content's, recorded, a part's matching
//     dispatch.ErrRefused and a unit's a dispatch.Warning naming the unit; anything else is the
//     run's, never recorded. A death by a signal under a memory bound names the bound, as in
//     "killed by SIGABRT under a 256 MiB memory bound".
//   - Each stage's identity hashes its format package's request resolved, every default filled
//     and each field that changes no output tagged out of its JSON, with the adapters' version;
//     its Version is its tools' whole version lines.
//   - One rule, a stash's item, turns a tool's line's media and attachments into found parts, each
//     claiming the SHA-256 internal/cli's media name rule resolved, or unread children.
//
// The Meta keys and the record media types the stages set are dispatch's constants.
//
// Its exported identifiers:
//
//   - [Build] builds a [Set] under [Options], through a [Lookup].
//   - [Routes] returns the ten routes in table order, and [ParseRoutes] parses route names,
//     refusing an unknown one with [ErrRequest], a refused request's cause, Build's refused
//     options among them.
package stages
