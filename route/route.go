package route

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"mime"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

// Route names the stage content goes to next, for the caller to run, as the package doc lists
// them.
type Route string

const (
	// Document is a PDF, by its %PDF- signature: the document package's stage.
	Document Route = "document"
	// Office is an office document: a ZIP holding a [Content_Types].xml entry, an OOXML package,
	// whatever its name, or whose name has an OOXML extension office reads, .docx, .docm, .dotx,
	// .dotm, .xlsx, .xlsm, .xltx, .xltm, .pptx, .pptm, .potx, .potm, or .ppsx, but for an XPS
	// package, a ZIP holding a FixedDocumentSequence.fdseq entry, whole or as its first piece,
	// which is [Unrouted] whatever its name; an OLE2 compound file whose name has a legacy
	// extension, .doc or .dot, .xls or .xlt, .ppt, .pps, or .pot; an encrypted OOXML package, an
	// OLE2 compound file holding an EncryptedPackage stream whose name has one of the OOXML
	// extensions or .xlsb, which office refuses as encrypted; an ODF package; an RTF; a
	// WordPerfect document; or a ZIP whose name has the .xlsb extension, an Excel binary workbook.
	// Extensions are matched in any case, and the caller gives office's Convert the same name so
	// it reads the same extension. The last four go through LibreOffice; office's stage has no
	// processor for the last two yet, which a dispatcher reports.
	Office Route = "office"
	// Vision is a picture a vision model decodes as stored: PNG, JPEG, GIF, or BMP.
	Vision Route = "vision"
	// VisionConverted is a picture a vision model decodes only once converted to PNG: TIFF,
	// EMF, WMF, SVG, HEIC, WebP, or AVIF.
	VisionConverted Route = "vision-converted"
	// Nested is a message: an OLE2 compound file whose name has the .msg extension, or content of
	// no routed signature whose name has the .eml extension, in any case, or whose stored MIME
	// type is message/rfc822 or message/global, its parameters ignored, and whose header has the
	// shape mail reads a message by, after an optional UTF-8 byte order mark: its first line a
	// header field, and a From or Date field before its header block ends. Content of no routed
	// signature and no text extension is one by its header alone when the header holds From and
	// either Date or Received. It is a mail store too: a .pst, by its signature, and an
	// mbox, by its first line's "From ", unless its name has a text extension, since prose may
	// begin so too. The caller reads it with the email package's Reader.
	Nested Route = "nested"
	// OneNote is a OneNote section or table of contents, by its OneStore signature, or a cabinet
	// whose name has the .onepkg extension, a OneNote package.
	OneNote Route = "onenote"
	// Archive is a ZIP that is neither an office document nor an ODF package, a tar, gzip, zstd,
	// 7z, RAR4, RAR5, xz, or bzip2 archive, or a cabinet not named .onepkg.
	Archive Route = "archive"
	// Audio is WAV, FLAC, MP3, an Ogg that begins no video stream, or M4A.
	Audio Route = "audio"
	// Video is MP4, Matroska, WebM, AVI, an MPEG program stream, or an Ogg that begins a Theora,
	// VP8, Dirac, or OGM video stream.
	Video Route = "video"
	// Text is content of no routed signature whose name has a text extension, in any case, or
	// that begins with a UTF-8, UTF-16, or UTF-32 byte order mark; and text whose name has a text
	// extension, whatever a short signature, two or three bytes, its head begins with.
	Text Route = "text"
	// Unrouted is anything else: an executable, by a PE, ELF, or Mach-O signature or a "#!"
	// line; an OLE2 compound file without a legacy or .msg extension, nor an encrypted OOXML
	// package's stream and name; an XPS package, a ZIP holding a FixedDocumentSequence.fdseq
	// entry, whole or as its first piece, whatever its name, ahead of the OOXML rules, since one
	// is an OPC package too and may be named .docx; an .ost, an offline store
	// mail refuses; content named .eml or stored as a message whose header isn't a message's
	// shape; HTML, by its .html, .htm, or .xhtml extension, web content no stage takes yet; and
	// other content of no routed signature.
	Unrouted Route = "unrouted"
)

const headerLineMax = 64 * 1024

var messageTypes = map[string]bool{"message/rfc822": true, "message/global": true}

var ooxmlExtensions = map[string]bool{
	".docx": true, ".docm": true, ".dotx": true, ".dotm": true,
	".xlsx": true, ".xlsm": true, ".xltx": true, ".xltm": true, ".xlsb": true,
	".pptx": true, ".pptm": true, ".potx": true, ".potm": true, ".ppsx": true,
}

// OOXMLExtension reports whether ext, as [Ext] returns it, is an OOXML package's: one an
// encrypted package keeps on its OLE2 compound file.
func OOXMLExtension(ext string) bool { return ooxmlExtensions[ext] }

// Ext returns name's extension, its dot included, lowercased, after trailing dots and spaces are
// trimmed, as Windows trims them: "Report.PDF. " gives ".pdf". name may be a path, by either
// separator.
func Ext(name string) string {
	return strings.ToLower(path.Ext(strings.TrimRight(strings.ReplaceAll(name, `\`, "/"), ". ")))
}

var legacyExtensions = map[string]string{
	".doc": "docx", ".dot": "docx",
	".xls": "xlsx", ".xlt": "xlsx",
	".ppt": "pptx", ".pps": "pptx", ".pot": "pptx",
}

// LegacyTarget returns the OOXML format an OLE2 compound file named with ext, as [Ext] returns
// it, converts to before office reads it: "docx" for .doc and .dot, "xlsx" for .xls and .xlt, and
// "pptx" for .ppt, .pps, and .pot; "" when ext isn't a legacy extension. It is the one table of
// them, the one byKind routes by.
func LegacyTarget(ext string) string { return legacyExtensions[ext] }

var encryptedPackageName = func() []byte {
	const name = "EncryptedPackage"
	b := make([]byte, 0, 2*len(name)+2)
	for i := range len(name) {
		b = append(b, name[i], 0)
	}
	return append(b, 0, 0)
}()

const ole2EntryLen = 128

var textExtensions = map[string]bool{
	".txt": true, ".text": true, ".csv": true, ".tsv": true, ".tab": true,
	".json": true, ".ndjson": true, ".jsonl": true, ".xml": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".md": true, ".markdown": true, ".srt": true, ".vtt": true,
	".log": true,
}

var kindRoutes = map[Kind]struct {
	route Route
	why   string
}{
	PDF:         {Document, "PDF signature"},
	PNG:         {Vision, "PNG signature"},
	JPEG:        {Vision, "JPEG signature"},
	GIF:         {Vision, "GIF signature"},
	BMP:         {Vision, "BMP signature"},
	TIFF:        {VisionConverted, "TIFF signature"},
	EMF:         {VisionConverted, "EMF signature"},
	WMF:         {VisionConverted, "WMF signature"},
	SVG:         {VisionConverted, "SVG root element"},
	HEIC:        {VisionConverted, "HEIC brand"},
	WebP:        {VisionConverted, "WebP signature"},
	AVIF:        {VisionConverted, "AVIF brand"},
	ODF:         {Office, "ODF mimetype entry"},
	RTF:         {Office, "RTF signature"},
	WordPerfect: {Office, "WordPerfect signature, a WordPerfect document"},
	PST:         {Nested, "PST signature, a .pst"},
	OST:         {Unrouted, "PST signature with an .ost's client magic, an offline store mail refuses"},
	Tar:         {Archive, "tar signature"},
	OneStore:    {OneNote, "OneStore signature"},
	WAV:         {Audio, "WAV signature"},
	FLAC:        {Audio, "FLAC signature"},
	MP3:         {Audio, "MP3 signature"},
	Ogg:         {Audio, "Ogg signature"},
	M4A:         {Audio, "M4A brand"},
	MP4:         {Video, "MP4 brand"},
	MKV:         {Video, "Matroska signature"},
	WebM:        {Video, "WebM signature"},
	AVI:         {Video, "AVI signature"},
	MPEGPS:      {Video, "MPEG pack start code, a program stream"},
	OggVideo:    {Video, "Ogg signature with a video stream"},
	Gzip:        {Archive, "gzip signature"},
	Zstd:        {Archive, "zstd signature"},
	SevenZip:    {Archive, "7z signature"},
	RAR4:        {Archive, "RAR4 signature"},
	RAR5:        {Archive, "RAR5 signature"},
	XZ:          {Archive, "xz signature"},
	Bzip2:       {Archive, "bzip2 signature"},
	PE:          {Unrouted, "PE signature, an executable"},
	ELF:         {Unrouted, "ELF signature, an executable"},
	MachO:       {Unrouted, "Mach-O signature, an executable"},
	Shebang:     {Unrouted, `"#!" line, an executable script`},
}

var boms = map[Kind]string{UTF8BOM: "UTF-8", UTF16BOM: "UTF-16", UTF32BOM: "UTF-32"}

// File returns the route of the file at path and why, in a few words, such as "PDF signature":
// it opens the file and routes it by [At], reading its first [HeadLen] bytes for the head. It
// returns an error, as os reports it, when the file can't be opened or read.
func File(path, name, mimeType string) (Route, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	head := make([]byte, HeadLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", "", err
	}
	return At(f, info.Size(), head[:n], name, mimeType)
}

// At returns the route of content r holds, size bytes, whose first bytes are head, at most
// [HeadLen] of them, and why: [File]'s form over a file already open, which a dispatcher calls
// with the file it hashed and the head it kept, so no file is opened again. head's signature
// decides whatever mimeType, the content's stored MIME type, says, but for a short one, two or
// three bytes, that text may begin: content whose head is text, named with a text extension, is
// text, whatever such a signature. The extension is name's, by [Ext], which may differ from the
// file's own name, as a file named by its SHA-256 does; it decides within an OLE2 container, for
// a ZIP holding no [Content_Types].xml, for a cabinet, whether an mbox's first line is text's
// instead, and, with mimeType, whether content of no signature is a message. Only then is r read
// beyond head: a ZIP's central directory, a name at a time, for a FixedDocumentSequence.fdseq
// entry, whole or as its first piece, FixedDocumentSequence.fdseq/[0].piece, which makes it an
// XPS package, [Unrouted], whatever its name, and otherwise for a [Content_Types].xml entry,
// which makes it an OOXML package whatever its name; an OLE2 file, 128 bytes at a time, for an
// EncryptedPackage stream's directory entry, by [EncryptedPackage], when it is named as an OOXML
// package; and content of no signature, a line at a time, each at most 64 KiB, for a message's
// header. Content of no signature and no text extension whose header is a message's, From and
// either Date or Received in a header block whose first line is a field, is a message whatever
// its name. Content of no signature Identify knows whose mimeType's top-level type is audio or
// video, such as a OneNote recording's stored "audio", routes Audio or Video. It returns an error
// only for r's own.
func At(r io.ReaderAt, size int64, head []byte, name, mimeType string) (Route, string, error) {
	head = head[:min(len(head), HeadLen)]
	ext := Ext(name)
	kind := Identify(head)
	if shortSignatures[kind] && textExtensions[ext] && isText(head) {
		return Text, "text by its " + ext + " extension, before a short " + string(kind) + " signature", nil
	}
	switch kind {
	case OLE2:
		if ooxmlExtensions[ext] {
			encrypted, err := EncryptedPackage(r, size)
			if err != nil {
				return "", "", err
			}
			if encrypted {
				return Office, "OLE2 signature with an EncryptedPackage stream, " + ext + " extension, an encrypted OOXML package", nil
			}
		}
	case ZIP:
		names := []string{xpsSequenceName, xpsSequencePiece, contentTypesName}
		held, err := zipHolds(r, size, names...)
		if err != nil {
			return "", "", err
		}
		for i, name := range names[:2] {
			if held[i] {
				return Unrouted, "ZIP signature with a " + name + " entry, an XPS package", nil
			}
		}
		if held[2] {
			return Office, "ZIP signature with a " + contentTypesName + " entry, an OOXML package", nil
		}
	}
	if route, why := byKind(kind, ext); route != "" {
		return route, why, nil
	}
	readHeader := func() (headerFields, error) { return header(io.NewSectionReader(r, 0, size)) }
	if as := messageNamed(ext, mimeType); as != "" {
		fields, err := readHeader()
		switch {
		case err != nil:
			return "", "", err
		case !fields.shaped():
			return Unrouted, "a message by its " + as + ", but not header-shaped", nil
		}
		return Nested, "a message by its " + as + ", header-shaped", nil
	}
	if textExtensions[ext] {
		return Text, "text by its " + ext + " extension", nil
	}
	if r := storedMedia(mimeType); r != "" && kind == Unknown {
		return r, "no known signature, stored as " + mimeType, nil
	}
	if kind == Unknown || kind == UTF8BOM {
		fields, err := readHeader()
		if err != nil {
			return "", "", err
		}
		if fields.message() {
			return Nested, "a message by its header, From and " + fields.dated(), nil
		}
	}
	if bom, ok := boms[kind]; ok {
		return Text, "text by its " + bom + " byte order mark", nil
	}
	return Unrouted, "no routed signature", nil
}

func storedMedia(mimeType string) Route {
	top, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mimeType)), "/")
	switch top {
	case "audio":
		return Audio
	case "video":
		return Video
	}
	return ""
}

var shortSignatures = map[Kind]bool{JPEG: true, BMP: true, Gzip: true, PE: true, Shebang: true, MP3: true}

func isText(head []byte) bool {
	for i := 0; i < len(head); {
		r, n := utf8.DecodeRune(head[i:])
		switch {
		case r == utf8.RuneError && n <= 1:
			return !utf8.FullRune(head[i:])
		case r < 0x20 && r != '\t' && r != '\n' && r != '\f' && r != '\r', r == 0x7f:
			return false
		}
		i += n
	}
	return true
}

func byKind(kind Kind, ext string) (Route, string) {
	if r, ok := kindRoutes[kind]; ok {
		return r.route, r.why
	}
	switch kind {
	case ZIP:
		switch {
		case ext == ".xlsb":
			return Office, "ZIP signature, .xlsb extension, an Excel binary workbook"
		case ooxmlExtensions[ext]:
			return Office, "ZIP signature, " + ext + " extension"
		}
		return Archive, "ZIP signature without an OOXML extension, " + contentTypesName + ", or ODF mimetype"
	case OLE2:
		switch {
		case legacyExtensions[ext] != "":
			return Office, "OLE2 signature, " + ext + " extension"
		case ext == ".msg":
			return Nested, "OLE2 signature, .msg extension"
		}
		return Unrouted, "OLE2 signature without a legacy or .msg extension"
	case Mbox:
		if textExtensions[ext] {
			return "", ""
		}
		return Nested, `mbox "From " line`
	case MSCF:
		if ext == ".onepkg" {
			return OneNote, "cabinet signature, .onepkg extension"
		}
		return Archive, "cabinet signature without a .onepkg extension"
	}
	return "", ""
}

// EncryptedPackage reports whether the OLE2 compound file r holds, size bytes, holds an
// EncryptedPackage stream, as an encrypted OOXML package does: whether a directory entry past the
// header, at a multiple of 128 bytes, begins with the stream's name, in UTF-16LE, and its null.
// It reads r once, 128 bytes at a time, through its end or the entry, without following the
// directory's sector chain, so an entry's name copied into a stream's data at such an offset
// counts too. It returns an error only for r's own.
func EncryptedPackage(r io.ReaderAt, size int64) (bool, error) {
	br := bufio.NewReader(io.NewSectionReader(r, 512, max(size-512, 0)))
	entry := make([]byte, ole2EntryLen)
	for {
		if _, err := io.ReadFull(br, entry); err != nil {
			return false, ignoreEOF(err)
		}
		if bytes.HasPrefix(entry, encryptedPackageName) {
			return true, nil
		}
	}
}

func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}

func messageNamed(ext, mimeType string) string {
	if ext == ".eml" {
		return ".eml extension"
	}
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil && !errors.Is(err, mime.ErrInvalidMediaParameter) {
		return ""
	}
	if messageTypes[mediaType] {
		return mediaType + " type"
	}
	return ""
}

type headerFields struct {
	first, from, date, received bool
}

func (h headerFields) shaped() bool { return h.first && (h.from || h.date) }

func (h headerFields) message() bool { return h.first && h.from && (h.date || h.received) }

func (h headerFields) dated() string {
	if h.date {
		return "Date"
	}
	return "Received"
}

func header(r io.Reader) (headerFields, error) {
	var h headerFields
	br := bufio.NewReaderSize(r, headerLineMax)
	if bom, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(bom, utf8BOM) {
		_, _ = br.Discard(len(utf8BOM))
	}
	for first := true; ; first = false {
		line, err := br.ReadSlice('\n')
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
			return h, err
		}
		content := bytes.TrimSuffix(line, []byte("\n"))
		content = bytes.TrimSuffix(content, []byte("\r"))
		if len(content) == 0 {
			return h, nil
		}
		folded := content[0] == ' ' || content[0] == '\t'
		name := fieldName(content)
		switch {
		case name != nil:
			h.first = h.first || first
			h.from = h.from || bytes.EqualFold(name, []byte("From"))
			h.date = h.date || bytes.EqualFold(name, []byte("Date"))
			h.received = h.received || bytes.EqualFold(name, []byte("Received"))
		case folded && !first:
		default:
			return h, nil
		}
		if !bytes.HasSuffix(line, []byte("\n")) {
			return h, nil
		}
	}
}

func fieldName(line []byte) []byte {
	end := 0
	for end < len(line) && (line[end] >= 33 && line[end] <= 126 && line[end] != ':') {
		end++
	}
	rest := bytes.TrimLeft(line[end:], " \t")
	if end == 0 || len(rest) == 0 || rest[0] != ':' {
		return nil
	}
	return line[:end]
}
