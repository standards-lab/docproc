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
// MP4, Matroska, WebM, or AVI; an MPEG program stream, by its pack start code; an Ogg whose first
// pages begin a Theora, VP8, Dirac, or OGM video stream; a tar, gzip, zstd, 7z, RAR4, RAR5, or xz
// archive; a bzip2 stream, by "BZh", its block size digit, and its first block's or end's magic;
// a PE executable, by "MZ" and the PE signature its DOS header points to, an ELF or Mach-O
// executable, or a script's "#!" line naming an absolute path; or text marked by a byte order
// mark. A short signature is checked past its bytes where its format allows: an MP3's ID3 tag by
// its version and size bytes.
//
// [File] routes the file at a path, and [At] the same content already open, by its kind, never by
// its stored MIME type, and by its name's extension, as [Ext] reads it, only where the content
// can't tell formats apart: within an OLE2 container, for a ZIP holding no [Content_Types].xml,
// for a cabinet, and for content of no signature, which is a message by the .eml rule. A name's
// extension never makes content text, nor does a stored audio or video type make text media.
// Content no route claims, or that fails a claim's own check, falls back on its head: text when
// its first [HeadLen] bytes hold no NUL byte, and unrouted when they do, its reason the claim it
// failed, or "no routed signature", then ", text by its head" or ", not text"; content named .eml
// or stored as a message whose header isn't a message's shape is the exception, its reason "a
// message by its <name or type>, but not header-shaped", then "; text by its head" when it is
// text and nothing more when it isn't. A ZIP is read by its central directory, decompressing
// nothing, for a FixedDocumentSequence.fdseq entry, whole or as its first piece, which makes it
// an XPS package whatever its name, and for a [Content_Types].xml entry, which makes any other
// ZIP an OOXML package whatever its name; an OLE2 file named with an OOXML extension is read
// whole for an EncryptedPackage stream's directory entry, by [EncryptedPackage], which inspect
// calls too; an mbox is read past its "From " line for a message's header; and content of no
// signature is read for a message's header, a header after a UTF-8 byte order mark among them.
// The routes:
//
//   - [Document]: a PDF, for the document package's stage.
//   - [Office]: an office document: a ZIP holding [Content_Types].xml, or named with an OOXML
//     extension office reads, but for an XPS package, an OLE2 compound file named with a legacy
//     one, an encrypted OOXML package (an OLE2 compound file holding an EncryptedPackage stream,
//     named with an OOXML extension or .xlsb), which office refuses as encrypted, an ODF package,
//     an RTF, a WordPerfect document, or a ZIP named .xlsb, an Excel binary workbook. The last
//     four go through LibreOffice.
//   - [Vision]: a picture a vision model decodes as stored: PNG, JPEG, GIF, or BMP.
//   - [VisionConverted]: a picture a vision model decodes only once converted to PNG: TIFF, EMF,
//     WMF, SVG, HEIC, WebP, or AVIF.
//   - [Nested]: a message or a mail store, for the email package's Reader: an OLE2 compound file
//     named .msg; content of no signature named .eml or stored as message/rfc822 or
//     message/global whose header has a message's shape; content of no signature, or of a UTF-8
//     byte order mark, whose header holds From and either Date or Received, whatever its name; a
//     .pst; or an mbox, a "From " line followed by a header block whose first line is a field and
//     which holds From and either Date or Received.
//   - [OneNote]: a OneNote section or table of contents, by its OneStore signature, or a cabinet
//     named .onepkg, a OneNote package.
//   - [Archive]: a ZIP that is neither office's nor ODF, a tar, gzip, zstd, 7z, RAR4, RAR5, xz,
//     or bzip2 archive, or a cabinet not named .onepkg.
//   - [Audio]: WAV, FLAC, MP3, an Ogg that begins no video stream, or M4A; or content of no
//     signature stored as an audio type whose head holds a NUL byte.
//   - [Video]: MP4, Matroska, WebM, AVI, an MPEG program stream, or an Ogg video; or content of
//     no signature stored as a video type whose head holds a NUL byte.
//   - [Text]: content marked by a byte order mark; a script, by its "#!" line; and the
//     fallback's content whose head holds no NUL byte, whatever its name or stored audio or video
//     type: HTML, CSS, JavaScript, an EPUB's entries, a .txt, .csv, or .json, a LICENSE, prose
//     that begins "From " without a message header, or content named .eml or stored as a message
//     whose header isn't a message's shape, but for an SVG, which routes by its root element.
//   - [Unrouted]: a deliberate refusal: a PE, ELF, or Mach-O executable, whose reason names it an
//     executable; an .ost, whose reason names it, since mail refuses one; an XPS package, a ZIP
//     holding FixedDocumentSequence.fdseq, whole or as its first piece, whatever its name, ahead
//     of the OOXML rules, since one is an OPC package too and may be named .docx; and the
//     fallback's content whose head holds a NUL byte, an OLE2 compound file without a legacy or
//     .msg extension, an mbox's "From " line without a message header, and content named .eml or
//     stored as a message whose header isn't a message's shape among it.
//
// Its exported identifiers:
//
//   - [Kind] names the format a signature identifies, [Unknown] for none: [PDF]; [PNG], [JPEG],
//     [GIF], and [BMP]; [TIFF], [EMF], [WMF], [SVG], [HEIC], [WebP], and [AVIF]; [ZIP], [OLE2],
//     [ODF], [RTF], and [WordPerfect]; [PST], [OST], and [Mbox]; [OneStore] and [MSCF]; [WAV],
//     [FLAC], [MP3], [Ogg], and [M4A]; [MP4], [MKV], [WebM], [AVI], [MPEGPS], and [OggVideo];
//     [Gzip], [Zstd], [SevenZip], [Tar], [RAR4], [RAR5], [XZ], and [Bzip2]; [PE], [ELF],
//     [MachO], and [Shebang]; and [UTF8BOM], [UTF16BOM], and [UTF32BOM].
//   - [Identify] names the Kind a file's first bytes begin.
//   - [RARVolume] reports whether a RAR's head marks it one volume of a split set.
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
