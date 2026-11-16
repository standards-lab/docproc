// Package office is docproc-ooxml's reader: it prepares office documents, docx, xlsx, and pptx,
// the legacy doc, xls, and ppt, and RTF, for a model through docproc-ooxml, a document's path in,
// its records out, streamed, with the media the document embeds. It provides pre-inference
// preparation only; a layer above sends what it yields to a model. The package builds on every
// platform, on the standard library and this module's tool, route, picture, and internal/cli
// packages; every program it runs is a tool.Tool the caller built.
//
// A Converter is built at the composition root over a docproc-ooxml Tool the exec runner built,
// OfficeSpec declaring it; a container runner's is refused, since docproc-ooxml reads the part's
// path in place. NewConverter refuses one older than 0.1.0, the first schema "2" release, and the
// Tool's own construction a missing one or one of another schema, so a missing or outdated
// docproc-ooxml fails at startup. docproc-ooxml's contract is context/layers.md's "The tool
// contract" and its README: schema "2" JSON Lines, the common envelope's stderr, media names, and
// exit codes, which internal/cli reads.
//
// Convert reads the document at Request.Path in place and returns the sequence of its records:
// docproc-ooxml runs once, in the working directory the Request names, which the caller owns,
// with a media directory of its own made there, as the sequence is ranged over. Each stdout line
// is decoded as it arrives into its record: the Document line first, then for an xlsx, per sheet
// in workbook order, a Sheet and its Rows, so no record holds a whole sheet; a line of a kind the
// package doesn't know is an UnknownRecord, never an error, since the contract grows by addition.
// Each warning docproc-ooxml writes to stderr is a Warning record among them. The Markdown
// fragments of the records, concatenated in order, are the document's whole rendering. A
// Document's Properties keep each part's stored order, as ordered pairs.
//
// A legacy document, .doc, .xls, or .ppt and their templates and shows, is one with the OLE2
// compound-file signature whose Request.Name has a legacy extension, route.LegacyTarget's table.
// Given a LibreOffice Tool in the Config, Convert copies it into the working directory, the only
// document it copies, and converts the copy to the OOXML format its extension names, and runs
// docproc-ooxml on that; the Document's ConvertedFrom names the legacy format. Without one, a
// legacy document is refused with ErrNoLibreOffice before anything runs. Any other OLE2 file,
// such as an encrypted OOXML package, goes to docproc-ooxml, which refuses it. An RTF, found by
// its signature whatever its name, is converted the same way to docx, through Writer's docx export
// filter, as a .doc is, and ConvertedFrom is "rtf"; without LibreOffice it is refused with
// ErrNoLibreOffice too. The picture package's LibreOfficeSpec declares soffice, minimum 24.2, and
// its LibreOfficeImage is the default image the container runner runs it in, under its full
// lockdown: no network, the caller's uid:gid, every capability dropped. Each conversion gives
// soffice a user profile of its own inside the working directory, which the runner mounts, and
// removes it with the copied and converted documents when the run ends.
//
// The Document's Media are every picture, embedded package, and OLE object docproc-ooxml wrote,
// each resolved in the media directory by the envelope's media name rule, internal/cli's: its
// Path, the file handed onward, its SHA-256 as its name claims it, and whether it was converted.
// A linked picture is no file, never fetched, and has no Path. Pictures are listed as the document
// holds them, with no layout: one that another covers, that sits off the slide or page, or that is
// cropped away is listed and handed onward like any other.
//
// Pictures follow one standard, and no conversion encodes a picture lossily: each writes PNG. Once
// the Document line is read, and before it is yielded, its pictures are converted in one batch. A
// picture vision decodes, PNG, JPEG, GIF, or BMP, is handed on exactly as stored, as is any kind
// the standard names no converter for, an embedded package, and an OLE object. By its stored
// file's signature, an EMF or WMF is rendered to PNG through the LibreOffice Tool, all of a
// document's in one soffice run with LibreOffice's Draw PNG export, and each trimmed to its
// picture through a magick Tool; a TIFF is converted through magick, its first image alone. The
// picture package converts them, each picture copied into a directory of its own, and its
// MagickSpec declares magick, minimum 7.1. A converted picture's PNG is named by the stored name's
// stem, its SHA-256, and .png, and written into the media directory beside the stored file, which
// stays; magick writes it without the time of the run, so a picture converted twice is the same
// bytes both times. A picture whose converter the Config didn't give, for EMF and WMF either of
// LibreOffice and magick, is handed on as stored, and the conversion still succeeds; a picture
// conversion that fails fails the conversion.
//
// Draw places a metafile at its own size, at 96 dpi, in the middle of a page, so a metafile is
// rendered as a page at Request.MetafileScale times 96 dpi, picture.DefaultMetafileScale, 3, when
// it is zero and at most picture.MaxMetafileScale, 8, and magick's -trim then crops the page to the
// picture: what differs from the color of the page's corners. The page is A4 whatever the paper of
// the system soffice runs on, since the run's profile gives LibreOffice a British English locale,
// so at the default scale it is 2382x3369 pixels. The trim has two known limits: it removes a
// border the picture itself has in the page's white, so a picture framed in white comes out without
// its frame, and a picture that is all white comes out as a single pixel.
//
// In every record's Markdown each converted picture's stored name is swapped for its PNG's
// wherever it appears, without parsing the Markdown, so its links name the PNG; a link to any
// other medium stays as docproc-ooxml wrote it.
//
// A run that exits 1 to 4, or dies by a signal, ends the sequence with internal/cli's
// *cli.ExitError: it matches its code's error through errors.Is, ErrMediaNotWritten, ErrUsage,
// ErrRefused, or ErrFailed, each the exit error every reader this module runs shares, and a
// refusal's class, an encrypted package's among them, is its Refusal method's. A conversion whose
// context ends returns an error matching the context's, as every Tool run does.
//
// One picture outside a document, such as a mail attachment the email package routes to
// route.VisionConverted, is the picture package's to convert, through the same code a document's
// picture takes: its Converter's Convert.
//
// Its exported identifiers:
//
//   - [OfficeSpec] declares docproc-ooxml for the exec runner's constructor.
//   - [OfficeMinimum] is the oldest docproc-ooxml the Converter accepts, 0.1.0.
//   - [ErrNoLibreOffice] is the cause of a legacy document's or an RTF's refusal without a
//     LibreOffice Tool.
//   - [Config] is the Tools a Converter is built over: docproc-ooxml, LibreOffice for legacy
//     documents, RTFs, and, with magick, EMF and WMF pictures, and magick for TIFF pictures.
//   - [Converter] converts office documents through docproc-ooxml.
//   - [NewConverter] builds a Converter over a Config.
//   - [Converter.Convert] converts one document as a Request says, yielding its Records.
//   - [Request] is one document's conversion: its path, its working directory, its file name,
//     and the scale its EMF and WMF pictures are rendered at.
//   - [Record] is one item of a conversion: [Document], [Sheet], [Rows], [Warning], or
//     [UnknownRecord].
//   - [Properties] are a Document's property parts, each a [PropertyList] of [Property] pairs in
//     stored order.
//   - [Medium] is one of a Document's media, of a [MediumKind]: [Picture], [Embedded], or [OLE].
//   - [Row] and [Cell] are a Rows record's sheet rows and cells; [ValueType] is a cell's value
//     type: [ValueNumber], [ValueText], [ValueBoolean], [ValueError], or [ValueDate].
//   - [ErrMediaNotWritten], [ErrUsage], [ErrRefused], and [ErrFailed] are docproc-ooxml's exit
//     codes 1 to 4, internal/cli's.
//   - [ErrSchema] is the cause of a first line that isn't a schema "2" document, internal/cli's.
//   - [ErrRequest] is the cause of a refused request, picture's own.
package office
