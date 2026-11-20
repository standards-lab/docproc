// Package route identifies content by its signature and names the stage it goes to next, on the
// standard library alone. It names a stage and never runs one: the caller, or a dispatcher, runs
// it, and reports "no processor yet" for a route whose stage doesn't exist.
//
// [Identify] names the [Kind] a file's first bytes begin, by its signature, as the head of
// [HeadLen] bytes shows it: a PDF, its header after nothing but white space or a byte order mark
// within its first 1,024 bytes; a PNG, JPEG, GIF, or BMP; a TIFF, EMF, or WMF; an SVG, in UTF-8 or
// UTF-16; a HEIC, WebP, or AVIF; a ZIP, an OLE2 compound file, or an ODF package, a ZIP whose
// first entry, mimetype, names an OpenDocument type; an RTF or a WordPerfect document; a .pst or
// an .ost, by the PST header's client magic, and an mbox, by its first line's "From ", as mail
// sniffs them; a OneNote OneStore file or a cabinet (MSCF); a WAV, FLAC, MP3, Ogg, or M4A; an
// MP4, Matroska, WebM, or AVI; a tar, gzip, zstd, or 7z archive; a PE executable, by "MZ" and the
// PE signature its DOS header points to, an ELF or Mach-O executable, or a script's "#!" line
// naming an absolute path; or text marked by a byte order mark. A short signature is checked
// past its bytes where its format allows: an MP3's ID3 tag by its version and size bytes.
//
// [File] routes the file at a path, and [At] the same content already open, by its kind, never by
// its stored MIME type, and by its name's extension, as [Ext] reads it, only where the content
// can't tell formats apart: within an OLE2 container, for a ZIP holding no [Content_Types].xml,
// for a cabinet, for an mbox's "From " line, which prose named with a text extension begins too,
// for a short signature, two or three bytes, that text named with a text extension begins, and
// for content of no signature, which is a message by the .eml rule or text by its extension. A
// ZIP is read by its central directory, decompressing nothing, for a FixedDocumentSequence.fdseq
// entry, whole or as its first piece, which makes it an XPS package whatever its name, and for a
// [Content_Types].xml entry, which makes any other ZIP an OOXML package whatever its name; an
// OLE2 file named with an OOXML extension is read whole for an EncryptedPackage stream's
// directory entry, by [EncryptedPackage], which inspect calls too; and content of no signature is
// read for a message's header, a header after a UTF-8 byte order mark among them. The routes:
//
//   - [Document]: a PDF, for the document package's stage.
//   - [Office]: an office document: a ZIP holding [Content_Types].xml, or named with an OOXML
//     extension office reads, but for an XPS package, an OLE2 compound file named with a legacy
//     one, an encrypted OOXML package (an OLE2 compound file holding an EncryptedPackage stream,
//     named with an OOXML extension or .xlsb), which office refuses as encrypted, an ODF package,
//     an RTF, a WordPerfect document, or a ZIP named .xlsb, an Excel binary workbook. The last
//     four go through LibreOffice, and office's stage has no processor yet for the last two, which
//     a dispatcher reports.
//   - [Vision]: a picture a vision model decodes as stored: PNG, JPEG, GIF, or BMP.
//   - [VisionConverted]: a picture a vision model decodes only once converted to PNG: TIFF, EMF,
//     WMF, SVG, HEIC, WebP, or AVIF.
//   - [Nested]: a message or a mail store, for the email package's Reader: an OLE2 compound file
//     named .msg; content of no signature named .eml or stored as message/rfc822 or
//     message/global whose header has a message's shape; content of no signature and no text
//     extension whose header holds From and either Date or Received; a .pst; or an mbox not named
//     with a text extension.
//   - [OneNote]: a OneNote section or table of contents, by its OneStore signature, or a cabinet
//     named .onepkg, a OneNote package.
//   - [Archive]: a ZIP that is neither office's nor ODF, a tar, gzip, zstd, or 7z archive, or a
//     cabinet not named .onepkg.
//   - [Audio]: WAV, FLAC, MP3, Ogg, or M4A.
//   - [Video]: MP4, Matroska, WebM, or AVI.
//   - [Text]: content of no signature named with a text extension, such as .txt, .csv, or
//     .json, or marked by a byte order mark; and text named so whose head a short signature
//     begins, such as a note that begins "#!/bin/sh".
//   - [Unrouted]: anything else, an executable among it, whose reason names it an executable; an
//     .ost, whose reason names it, since mail refuses one; an XPS package, a ZIP holding
//     FixedDocumentSequence.fdseq, whole or as its first piece, whatever its name, ahead of the
//     OOXML rules, since one is an OPC package too and may be named .docx; and HTML, which is web
//     content, not text, unless its root element is an SVG's.
//
// Its exported identifiers:
//
//   - [Kind] names the format a signature identifies, [Unknown] for none: [PDF]; [PNG], [JPEG],
//     [GIF], and [BMP]; [TIFF], [EMF], [WMF], [SVG], [HEIC], [WebP], and [AVIF]; [ZIP], [OLE2],
//     [ODF], [RTF], and [WordPerfect]; [PST], [OST], and [Mbox]; [OneStore] and [MSCF]; [WAV],
//     [FLAC], [MP3], [Ogg], and [M4A]; [MP4], [MKV], [WebM], and [AVI]; [Gzip], [Zstd],
//     [SevenZip], and [Tar]; [PE], [ELF], [MachO], and [Shebang]; and [UTF8BOM], [UTF16BOM],
//     and [UTF32BOM].
//   - [Identify] names the Kind a file's first bytes begin.
//   - [HeadLen] is the most of a file's first bytes Identify reads.
//   - [PNGSignature] is the eight bytes every PNG begins with.
//   - [Route] names the stage content goes to next: [Document], [Office], [Vision],
//     [VisionConverted], [Nested], [OneNote], [Archive], [Audio], [Video], [Text], or
//     [Unrouted].
//   - [File] routes the file at a path, and says why; [At] routes content already open.
//   - [Ext] reads a name's extension; [OOXMLExtension] reports whether it is an OOXML
//     package's, the one table of them; [LegacyTarget] gives the OOXML format a legacy
//     extension converts to, the one table of those; [EncryptedPackage] reports whether an OLE2
//     compound file is an encrypted OOXML package.
package route
