//go:build ignore

// generate writes the inspect package's synthesized hazard fixtures, which README.md describes,
// into the directory it runs in. Run it from inspect/testdata with `go run generate.go`; the check
// never runs it, and reads the committed files.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

var fixedModTime = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

type zipEntry struct {
	name         string
	body         string
	mode         fs.FileMode
	flags        uint16
	declaredSize uint64
}

func main() {
	files := map[string][]byte{
		"traversal.zip": zipOf(
			zipEntry{name: "notes.txt", body: "an ordinary entry\n"},
			zipEntry{name: "docs/../../evil.txt", body: "written outside the extraction directory\n"},
		),
		"absolute.zip": zipOf(
			zipEntry{name: "/tmp/evil.txt", body: "an absolute path\n"},
			zipEntry{name: `C:\evil.txt`, body: "a drive letter\n"},
		),
		"symlink.zip": zipOf(
			zipEntry{name: "notes.txt", body: "an ordinary entry\n"},
			zipEntry{name: "passwd", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777},
		),
		"bomb.zip":          zipOf(zipEntry{name: "zeros.bin", body: string(make([]byte, 8<<20))}),
		"declared-bomb.zip": zipOf(zipEntry{name: "zeros.bin", body: string(make([]byte, 1024)), declaredSize: 4 << 30}),
		"encrypted.zip": zipOf(
			zipEntry{name: "notes.txt", body: "an ordinary entry\n"},
			zipEntry{name: "secret.txt", body: "ciphertext stands in here\n", flags: 0x1},
		),
		"macro.docm": zipOf(macroEntries([]byte(settingsRels))...),
		"hyperlink.docx": zipOf(
			zipEntry{name: "[Content_Types].xml", body: contentTypes},
			zipEntry{name: "_rels/.rels", body: packageRels},
			zipEntry{name: "word/document.xml", body: document},
			zipEntry{name: "word/_rels/document.xml.rels", body: documentRels},
		),
		"macro-latin1.docm": zipOf(macroEntries(latin1(settingsRelsLatin1))...),
		"macro-utf16.docm":  zipOf(macroEntries(utf16LE(strings.Replace(settingsRels, `encoding="UTF-8"`, `encoding="UTF-16"`, 1)))...),
		"javascript.pdf":    []byte(javascriptPDF),
		"script.svg":        []byte(scriptSVG),
		"script-latin1.svg": latin1(strings.Replace(scriptSVG, `encoding="UTF-8"?>`, "encoding=\"ISO-8859-1\"?>\n<!-- Caf\u00e9 cr\u00e8me -->", 1)),
		"script-utf16.svg":  utf16LE(strings.Replace(scriptSVG, `encoding="UTF-8"`, `encoding="UTF-16"`, 1)),
		"invoice.pdf.exe":   pe(),
		"mismatch.pdf":      png(),
		"polyglot.jpg":      append(jpeg(), zipOf(zipEntry{name: "hidden.txt", body: "appended\n"})...),
		"polyglot.png":      append(png(), []byte("%PDF-1.7\n%%EOF\n")...),
		"encrypted.xlsx":    encryptedOOXML(),

		"clean.tar":    tarOf(cleanTar...),
		"clean.tar.gz": gzipOf("clean.tar", tarOf(cleanTar...)),
		"unnamed.tgz":  gzipOf("", tarOf(cleanTar...)),
		"traversal.tar": tarOf(
			tarEntry{name: "notes.txt", body: "an ordinary entry\n"},
			tarEntry{name: "docs/../../evil.txt", body: "written outside the extraction directory\n"},
		),
		"traversal.tar.gz": gzipOf("traversal.tar", tarOf(
			tarEntry{name: "notes.txt", body: "an ordinary entry\n"},
			tarEntry{name: "docs/../../evil.txt", body: "written outside the extraction directory\n"},
		)),
		"absolute.tar": tarOf(
			tarEntry{name: "/tmp/evil.txt", body: "an absolute path\n"},
			tarEntry{name: `C:\evil.txt`, body: "a drive letter\n"},
		),
		"symlink.tar": tarOf(
			tarEntry{name: "notes.txt", body: "an ordinary entry\n"},
			tarEntry{name: "passwd", typ: tar.TypeSymlink, link: "/etc/passwd"},
		),
		"hardlink.tar": tarOf(
			tarEntry{name: "notes.txt", body: "an ordinary entry\n"},
			tarEntry{name: "shadow", typ: tar.TypeLink, link: "/etc/shadow"},
		),
		"device.tar": tarOf(
			tarEntry{name: "notes.txt", body: "an ordinary entry\n"},
			tarEntry{name: "null", typ: tar.TypeChar, major: 1, minor: 3},
			tarEntry{name: "sda", typ: tar.TypeBlock, major: 8},
			tarEntry{name: "pipe", typ: tar.TypeFifo},
		),
		"sparse-bomb.tar": sparseBomb(),
		"bomb.tar.gz":     gzipOf("bomb.tar", tarOf(tarEntry{name: "zeros.bin", body: string(make([]byte, 8<<20))})),
	}
	for name, b := range files {
		if err := os.WriteFile(name, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func macroEntries(rels []byte) []zipEntry {
	return []zipEntry{
		{name: "[Content_Types].xml", body: contentTypes},
		{name: "_rels/.rels", body: packageRels},
		{name: "word/document.xml", body: document},
		{name: "word/_rels/document.xml.rels", body: documentRels},
		{name: "word/_rels/settings.xml.rels", body: string(rels)},
		{name: "word/settings.xml", body: settings},
		{name: "word/vbaProject.bin", body: "stands in for a VBA project"},
		{name: "word/activeX/activeX1.xml", body: `<ax:ocx xmlns:ax="http://schemas.microsoft.com/office/2006/activeX"/>`},
		{name: "word/embeddings/oleObject1.bin", body: "stands in for an OLE object"},
	}
}

func latin1(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			panic(fmt.Sprintf("%q isn't Latin-1", r))
		}
		b = append(b, byte(r))
	}
	return b
}

func utf16LE(s string) []byte {
	b := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

func zipOf(entries ...zipEntry) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: fixedModTime}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		h.Flags |= e.flags
		if err := add(w, h, e); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func add(w *zip.Writer, h *zip.FileHeader, e zipEntry) error {
	if e.flags != 0 || e.declaredSize != 0 {
		return addRaw(w, h, e)
	}
	f, err := w.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(e.body))
	return err
}

func addRaw(w *zip.Writer, h *zip.FileHeader, e zipEntry) error {
	var packed bytes.Buffer
	fw, err := flate.NewWriter(&packed, flate.BestCompression)
	if err != nil {
		return err
	}
	if _, err := fw.Write([]byte(e.body)); err != nil {
		return err
	}
	if err := fw.Close(); err != nil {
		return err
	}
	h.CRC32 = crc32.ChecksumIEEE([]byte(e.body))
	h.CompressedSize64 = uint64(packed.Len())
	h.UncompressedSize64 = uint64(len(e.body))
	if e.declaredSize != 0 {
		h.UncompressedSize64 = e.declaredSize
	}
	f, err := w.CreateRaw(h)
	if err != nil {
		return err
	}
	_, err = f.Write(packed.Bytes())
	return err
}

type tarEntry struct {
	name         string
	body         string
	typ          byte
	link         string
	major, minor int64
}

var cleanTar = []tarEntry{
	{name: "docs/", typ: tar.TypeDir},
	{name: "docs/notes.txt", body: "an ordinary entry\n"},
	{name: "docs/pixel.png", body: string(pngSized(2, 1))},
}

func tarOf(entries ...tarEntry) []byte {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{
			Typeflag: e.typ, Name: e.name, Linkname: e.link, Mode: 0o644, ModTime: fixedModTime,
			Devmajor: e.major, Devminor: e.minor, Format: tar.FormatUSTAR,
		}
		switch e.typ {
		case 0:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(e.body))
		case tar.TypeDir:
			h.Mode = 0o755
		}
		if err := w.WriteHeader(h); err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func gzipOf(name string, b []byte) []byte {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		panic(err)
	}
	w.Name, w.ModTime = name, fixedModTime
	if _, err := w.Write(b); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func sparseBomb() []byte {
	const real, stored = 4 << 30, 512
	const (
		modeAt, uidAt, gidAt, storedSizeAt, mtimeAt = 100, 108, 116, 124, 136
		checksumAt, typeflagAt, magicAt             = 148, 156, 257
		sparseRegionOffsetAt, sparseRegionLengthAt  = 386, 398
		realSizeAt                                  = 483
		gnuMagicAndVersion, checksumSummedAsSpaces  = "ustar  \x00", "        "
	)
	h := make([]byte, 512)
	octal := func(off, n int, v int64) { copy(h[off:off+n], fmt.Sprintf("%0*o\x00", n-1, v)) }
	copy(h, "zeros.bin")
	octal(modeAt, 8, 0o644)
	octal(uidAt, 8, 0)
	octal(gidAt, 8, 0)
	octal(storedSizeAt, 12, stored)
	octal(mtimeAt, 12, fixedModTime.Unix())
	h[typeflagAt] = tar.TypeGNUSparse
	copy(h[magicAt:], gnuMagicAndVersion)
	octal(sparseRegionOffsetAt, 12, real-stored)
	octal(sparseRegionLengthAt, 12, stored)
	octal(realSizeAt, 12, real)
	copy(h[checksumAt:typeflagAt], checksumSummedAsSpaces)
	var sum int64
	for _, c := range h {
		sum += int64(c)
	}
	copy(h[checksumAt:typeflagAt], fmt.Sprintf("%06o\x00 ", sum))
	data := bytes.Repeat([]byte("x"), stored)
	endOfArchive := make([]byte, 1024)
	return slices.Concat(h, data, endOfArchive)
}

func pe() []byte {
	b := make([]byte, 128)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 64)
	copy(b[64:], "PE\x00\x00")
	return b
}

func png() []byte {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde")
}

func pngSized(width, height uint32) []byte {
	chunk := []byte("IHDR")
	chunk = binary.BigEndian.AppendUint32(chunk, width)
	chunk = binary.BigEndian.AppendUint32(chunk, height)
	chunk = append(chunk, 8, 2, 0, 0, 0)
	b := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	b = append(b, chunk...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(chunk))
}

func jpeg() []byte {
	return []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xff\xd9")
}

func encryptedOOXML() []byte {
	b := make([]byte, 1024)
	copy(b, "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")
	off := 512
	for _, name := range []string{"EncryptionInfo", "EncryptedPackage"} {
		for i := range len(name) {
			b[off+2*i] = name[i]
		}
		off += 128
	}
	return b
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.ms-word.document.macroEnabled.main+xml"/></Types>`

const packageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

const document = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>A synthesized hazard fixture.</w:t></w:r></w:p></w:body></w:document>`

const documentRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.invalid/page" TargetMode="External"/></Relationships>`

const settingsRelsLatin1 = `<?xml version="1.0" encoding="ISO-8859-1" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/attachedTemplate" Target="https://example.invalid/modèle.dotm" TargetMode="External"/></Relationships>`

const settingsRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/attachedTemplate" Target="https://example.invalid/template.dotm" TargetMode="External"/></Relationships>`

const settings = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:attachedTemplate r:id="rId1"/></w:settings>`

const javascriptPDF = `%PDF-1.7
1 0 obj
<< /Type /Catalog /Pages 2 0 R /OpenAction 4 0 R /AA << /O 5 0 R >> >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>
endobj
4 0 obj
<< /S /JavaScript /JS (app.alert\('opened'\);) >>
endobj
5 0 obj
<< /S /J#61vaScript /JS (app.alert\('again'\);) >>
endobj
trailer
<< /Root 1 0 R >>
%%EOF
`

const scriptSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="100" height="100" onload="alert(1)">
  <script>alert(2)</script>
  <defs><rect id="box" width="10" height="10"/></defs>
  <use href="#box"/>
  <image href="data:image/png;base64,iVBORw0KGgo=" width="1" height="1"/>
  <image xlink:href="https://example.invalid/tracker.png" width="1" height="1"/>
  <foreignObject width="10" height="10"><div xmlns="http://www.w3.org/1999/xhtml">markup</div></foreignObject>
</svg>
`
