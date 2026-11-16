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
	// .dotm, .xlsx, .xlsm, .xltx, .xltm, .pptx, .pptm, .potx, .potm, or .ppsx; an OLE2 compound file whose name has a legacy extension, .doc or .dot, .xls or .xlt,
	// .ppt, .pps, or .pot; an encrypted OOXML package, an OLE2 compound file holding an
	// EncryptedPackage stream whose name has one of the OOXML extensions or .xlsb, which office
	// refuses as encrypted; an ODF package; an RTF; a WordPerfect document; or a ZIP whose name
	// has the .xlsb extension, an Excel binary workbook. Extensions are matched in any case, and
	// the caller gives office's Convert the same name so it reads the same extension. The last
	// four go through LibreOffice; office's stage has no processor for the last two yet, which a
	// dispatcher reports.
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
	// or 7z archive, or a cabinet not named .onepkg.
	Archive Route = "archive"
	// Audio is WAV, FLAC, MP3, Ogg, or M4A.
	Audio Route = "audio"
	// Video is MP4, Matroska, WebM, or AVI.
	Video Route = "video"
	// Text is content of no routed signature whose name has a text extension, in any case, or
	// that begins with a UTF-8, UTF-16, or UTF-32 byte order mark; and text whose name has a text
	// extension, whatever a short signature, two or three bytes, its head begins with.
	Text Route = "text"
	// Unrouted is anything else: an executable, by a PE, ELF, or Mach-O signature or a "#!"
	// line; an OLE2 compound file without a legacy or .msg extension, nor an encrypted OOXML
	// package's stream and name; an .ost, an offline store
	// mail refuses; content named .eml or stored as a message whose header isn't a message's
	// shape; HTML, by its .html, .htm, or .xhtml extension, web content no stage takes yet; and
	// other content of no routed signature.
	Unrouted Route = "unrouted"
)

// headerLineMax is the longest line headerShaped reads, mail's LINE_MAX; a longer one ends the
// header block.
const headerLineMax = 64 * 1024

// messageTypes are the stored MIME types, as mime.ParseMediaType lowercases them, that name a
// message.
var messageTypes = map[string]bool{"message/rfc822": true, "message/global": true}

// ooxmlExtensions are the extensions of OOXML packages: the documents, their macro-enabled
// forms, templates, and slide show, which office reads, and .xlsb, an Excel binary workbook,
// which it doesn't. inspect reads the same table through [OOXMLExtension].
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

// legacyExtensions are the legacy extensions office converts from an OLE2 compound file, its
// own rule.
var legacyExtensions = map[string]bool{
	".doc": true, ".dot": true,
	".xls": true, ".xlt": true,
	".ppt": true, ".pps": true, ".pot": true,
}

// encryptedPackageName is the directory entry name of an encrypted OOXML package's stream,
// "EncryptedPackage", in UTF-16LE, with its terminating null, as an OLE2 directory entry begins.
var encryptedPackageName = func() []byte {
	const name = "EncryptedPackage"
	b := make([]byte, 0, 2*len(name)+2)
	for i := range len(name) {
		b = append(b, name[i], 0)
	}
	return append(b, 0, 0)
}()

// ole2EntryLen is the length of an OLE2 directory entry. Every sector, the 512-byte header's
// size or 4096 bytes, is a multiple of it, so every entry begins at a multiple of it.
const ole2EntryLen = 128

// textExtensions are the extensions that name content of no signature text: plain text, tables,
// structured data, markup, subtitles, and logs. HTML, .html, .htm, and .xhtml, is none of them:
// it is web content, which no stage takes yet, so it is unrouted, but for one whose root element
// is an SVG's, which is an SVG.
var textExtensions = map[string]bool{
	".txt": true, ".text": true, ".csv": true, ".tsv": true, ".tab": true,
	".json": true, ".ndjson": true, ".jsonl": true, ".xml": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".md": true, ".markdown": true, ".srt": true, ".vtt": true,
	".log": true,
}

// kindRoutes are the routes of the kinds whose signature decides alone, with their reasons.
var kindRoutes = map[Kind]struct {
	route Route
	why   string
}{
	PDF:  {Document, "PDF signature"},
	PNG:  {Vision, "PNG signature"},
	JPEG: {Vision, "JPEG signature"},
	GIF:  {Vision, "GIF signature"},
	BMP:  {Vision, "BMP signature"},
	TIFF: {VisionConverted, "TIFF signature"},
	EMF:  {VisionConverted, "EMF signature"},
	WMF:  {VisionConverted, "WMF signature"},
	SVG:  {VisionConverted, "SVG root element"},
	HEIC: {VisionConverted, "HEIC brand"},
	WebP: {VisionConverted, "WebP signature"},
	AVIF: {VisionConverted, "AVIF brand"},
	ODF:  {Office, "ODF mimetype entry"},
	RTF:  {Office, "RTF signature"},
	// WordPerfect goes through LibreOffice, which office's stage has no processor for yet.
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
	Gzip:        {Archive, "gzip signature"},
	Zstd:        {Archive, "zstd signature"},
	SevenZip:    {Archive, "7z signature"},
	PE:          {Unrouted, "PE signature, an executable"},
	ELF:         {Unrouted, "ELF signature, an executable"},
	MachO:       {Unrouted, "Mach-O signature, an executable"},
	Shebang:     {Unrouted, `"#!" line, an executable script`},
}

// boms are the byte order marks' names, for Text's reason.
var boms = map[Kind]string{UTF8BOM: "UTF-8", UTF16BOM: "UTF-16", UTF32BOM: "UTF-32"}

// File returns the route of the file at path and why, in a few words, such as "PDF signature":
// it opens the file and routes it by [At], reading its first [HeadLen] bytes for the head. It
// returns an error, as os reports it, when the file can't be opened or read.
func File(path, name, mimeType string) (Route, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }() // read only: a close error loses nothing
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
// beyond head: a ZIP's central directory, a name at a time, for a [Content_Types].xml entry, which
// makes it an OOXML package whatever its name; an OLE2 file, 128 bytes at a time, for an
// EncryptedPackage stream's directory entry, by [EncryptedPackage], when it is named as an OOXML
// package; and content of no signature, a line at a time, each at most 64 KiB, for a message's
// header. Content of no signature and no text extension whose header is a message's, From and
// either Date or Received in a header block whose first line is a field, is a message whatever
// its name. It returns an error only for r's own.
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
		holds, err := zipHolds(r, size, contentTypesName)
		if err != nil {
			return "", "", err
		}
		if holds {
			return Office, "ZIP signature with a " + contentTypesName + " entry, an OOXML package", nil
		}
	}
	if route, why := byKind(kind, ext); route != "" {
		return route, why, nil
	}
	// Content of no routed signature: a byte order mark's, or none. Its header is read only when
	// a message's name or type, or no text extension, calls for it.
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

// shortSignatures are the kinds whose signature is two or three bytes that text may begin:
// JPEG's, BMP's, gzip's, PE's "MZ", a script's "#!", and MP3's "ID3" and frame sync. Content
// whose head is text and whose name has a text extension is text whatever one of them says.
var shortSignatures = map[Kind]bool{JPEG: true, BMP: true, Gzip: true, PE: true, Shebang: true, MP3: true}

// isText reports whether head reads as text: UTF-8, but for a rune the head's end may cut, with
// no control character but tab, line feed, form feed, and carriage return.
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

// byKind routes content of kind, named with ext, as [Ext] returns it, which decides only within
// a ZIP or OLE2 container, for a cabinet, and for an mbox. It returns no route for content of no
// routed signature, a byte order mark's included, and for an mbox named with a text extension,
// which At decides.
func byKind(kind Kind, ext string) (Route, string) {
	if r, ok := kindRoutes[kind]; ok {
		return r.route, r.why
	}
	switch kind {
	case ZIP:
		switch {
		case ext == ".xlsb":
			// An Excel binary workbook, which office doesn't read: it goes through LibreOffice.
			return Office, "ZIP signature, .xlsb extension, an Excel binary workbook"
		case ooxmlExtensions[ext]:
			return Office, "ZIP signature, " + ext + " extension"
		}
		return Archive, "ZIP signature without an OOXML extension, " + contentTypesName + ", or ODF mimetype"
	case OLE2:
		switch {
		case legacyExtensions[ext]:
			return Office, "OLE2 signature, " + ext + " extension"
		case ext == ".msg":
			return Nested, "OLE2 signature, .msg extension"
		}
		return Unrouted, "OLE2 signature without a legacy or .msg extension"
	case Mbox:
		// "From " is a weak signature, which prose begins too: a text extension decides first.
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
	// The header's 512 bytes hold no entry.
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

// ignoreEOF returns nil for an error that ends a read at the end of its input, else err.
func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}

// messageNamed returns what names content a message, its ext, the .eml extension, else its
// stored type, mimeType, message/rfc822 or message/global; or "" when neither does. The type is
// parsed as a media type, in any case and its parameters ignored, malformed ones included.
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

// headerFields are what a header block holds of the fields that make it a message's: whether its
// first line is a field, and whether it has a From, a Date, and a Received field.
type headerFields struct {
	first, from, date, received bool
}

// shaped reports whether the header has the shape mail reads a message by, its header_shape: its
// first line a field, and a From or Date field.
func (h headerFields) shaped() bool { return h.first && (h.from || h.date) }

// message reports whether the header is a message's though nothing names it one: its first line
// a field, a From field, and a Date or Received field.
func (h headerFields) message() bool { return h.first && h.from && (h.date || h.received) }

// dated returns the field that dates a message's header, for its reason: Date, else Received.
func (h headerFields) dated() string {
	if h.date {
		return "Date"
	}
	return "Received"
}

// header reads r's header block, as RFC 5322 shapes one, after an optional UTF-8 byte order mark,
// and returns which of the fields that make it a message's it holds. The block runs to its blank
// line, to the end of the input, or to the first line that is neither a field nor a field's folded
// continuation; a line past headerLineMax ends it once its first headerLineMax bytes are read. An
// mbox's "From " separator is no field, so an input that begins with one has no first field. The
// field's name is checked by fieldName, not net/textproto's, which refuses the space before the
// colon mail allows.
func header(r io.Reader) (headerFields, error) {
	var h headerFields
	// A line is read through its newline or to the buffer's size, whichever comes first.
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
			// The end of the input, or a line past headerLineMax.
			return h, nil
		}
	}
}

// fieldName returns the name of the header field line starts, or nil if it starts none: printable
// ASCII but the colon (RFC 5322 3.6.8's ftext), then the colon, after any spaces or tabs, as RFC
// 5322 4.5.8's obsolete syntax allows.
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
