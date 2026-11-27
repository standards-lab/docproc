// Package metadata is the shapes of docproc's metadata records: one record output per format,
// holding what the format stores of a part beyond its content, each a JSON value under a versioned
// media type that dispatch declares. It is shapes alone, importing no docproc package but
// dispatch, so a reader such as cmd/docinfer decodes a record without compiling the stages, the
// format packages, or their tools.
//
// A record holds its format's fields alone. The part it describes, its name, size, SHA-256,
// content type, parent, and depth, is the record output's event's dispatch.Part, and the record's
// content is in the content store under the output's SHA-256, never on the event. Each type's doc
// comment is its part of the contract: a field's JSON name, what it holds, and when it is absent.
// A record's shape grows by addition only, under its media type's version.
//
// The package exports:
//
//   - [Office], an office package's metadata record, under [dispatch.MediaOfficeMetadata]
//   - [PDF], a PDF's metadata record, under [dispatch.MediaPDFMetadata], its information
//     dictionary a [PDFInfo] and its XMP metadata an [XMP]
//   - [Media], an audio or video file's metadata record, under [dispatch.MediaMediaMetadata], each
//     of its streams a [MediaStream]
//   - [Properties], one property part's properties in stored order, each a [Property]
package metadata
