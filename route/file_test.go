package route_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/route"
)

func TestFileRoutes(t *testing.T) {
	cases := []struct {
		name, content, filename, mime string
		want                          route.Route
		why                           string
	}{
		{"svg", svgPlain, "a.svg", "image/svg+xml", route.VisionConverted, "SVG root element"},
		{"svg named .xml", svgProlog, "diagram.xml", "text/xml", route.VisionConverted, "SVG root element"},
		{"heic", ftyp("heic", "mif1", "heic"), "a.heic", "image/heic", route.VisionConverted, "HEIC brand"},
		{"webp", riffForm("WEBP"), "a.webp", "image/webp", route.VisionConverted, "WebP signature"},
		{"avif", ftyp("avif", "avif", "mif1"), "a.avif", "", route.VisionConverted, "AVIF brand"},

		{"odf", odfText, "a.odt", "application/vnd.oasis.opendocument.text", route.Office, "ODF mimetype entry"},
		{"odf named .zip", odfText, "a.zip", "application/zip", route.Office, "ODF mimetype entry"},
		{"odf named .docx", odfText, "a.docx", "", route.Office, "ODF mimetype entry"},
		{"rtf", `{\rtf1\ansi`, "a.rtf", "application/rtf", route.Office, "RTF signature"},
		{"rtf named .doc", `{\rtf1\ansi`, "a.doc", "application/msword", route.Office, "RTF signature"},

		{"onestore section", oneSection, "Section.one", "application/onenote", route.OneNote, "OneStore signature"},
		{"onestore table of contents", oneTOC, "Open Notebook.onetoc2", "", route.OneNote, "OneStore signature"},
		{"onestore without a name", oneSection, "", "", route.OneNote, "OneStore signature"},
		{"cabinet named .onepkg", mscf, "Notebook.onepkg", "", route.OneNote, "cabinet signature, .onepkg extension"},
		{"cabinet named .ONEPKG", mscf, "NOTEBOOK.ONEPKG", "", route.OneNote, "cabinet signature, .onepkg extension"},
		{"cabinet named .cab", mscf, "drivers.cab", "application/vnd.ms-cab-compressed", route.Archive, "cabinet signature without a .onepkg extension"},

		{"zip", zipBytes, "a.zip", "application/zip", route.Archive, "ZIP signature without an OOXML extension, [Content_Types].xml, or ODF mimetype"},
		{"zip named .xlsb, which office doesn't read yet", zipBytes, "a.xlsb", "", route.Office, "ZIP signature, .xlsb extension, an Excel binary workbook"},
		{"zip named .XLSB", zipBytes, "A.XLSB", "", route.Office, "ZIP signature, .xlsb extension, an Excel binary workbook"},
		{"ole2 named .xlsb", ole2Bytes, "a.xlsb", "", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"wordperfect", wordPerfect, "a.wpd", "application/vnd.wordperfect", route.Office, "WordPerfect signature, a WordPerfect document"},
		{"wordperfect without a name", wordPerfect, "", "", route.Office, "WordPerfect signature, a WordPerfect document"},
		{"tar", tarHeader("a.txt", "ustar\x0000"), "a.tar", "application/x-tar", route.Archive, "tar signature"},
		{"tar named .txt", tarHeader("a.txt", "ustar  \x00"), "a.txt", "", route.Archive, "tar signature"},
		{"zip named .docx", zipBytes, "a.DOCX", "", route.Office, "ZIP signature, .docx extension"},
		{"gzip", "\x1f\x8b\x08\x00", "a.tar.gz", "application/gzip", route.Archive, "gzip signature"},
		{"zstd", "\x28\xb5\x2f\xfd\x04", "a.tar.zst", "", route.Archive, "zstd signature"},
		{"7z", "7z\xbc\xaf\x27\x1c\x00\x04", "a.7z", "", route.Archive, "7z signature"},
		{"rar4", rar4(0), "a.rar", "application/vnd.rar", route.Archive, "RAR4 signature"},
		{"rar4 volume", rar4(0x0001), "a.r00", "", route.Archive, "RAR4 signature"},
		{"rar5", rar5(0x03, 0x01, 0x00, 0x00), "a.rar", "", route.Archive, "RAR5 signature"},
		{"rar5 volume", rar5(0x03, 0x01, 0x00, 0x01), "a.part1.rar", "", route.Archive, "RAR5 signature"},
		{"rar5 with encrypted headers", rar5(0x0c, 0x04, 0x00, 0x00, 0x00), "a.rar", "", route.Archive, "RAR5 signature"},
		{"xz", "\xfd7zXZ\x00\x00\x04\xe6\xd6\xb4\x46", "a.tar.xz", "application/x-xz", route.Archive, "xz signature"},
		{"bzip2", "BZh91AY&SY\x8a\x1b", "a.tar.bz2", "application/x-bzip2", route.Archive, "bzip2 signature"},
		{"bzip2 empty stream", "BZh9\x17\x72\x45\x38\x50\x90\x00\x00\x00\x00", "a.bz2", "", route.Archive, "bzip2 signature"},
		{"bzh prose named .txt", "BZh9 is how a bzip2 stream begins", "a.txt", "", route.Text, "text by its .txt extension"},
		{"bzh prose without a text extension", "BZh9 is how a bzip2 stream begins", "a.bz", "", route.Unrouted, "no routed signature"},

		{"wav", riffForm("WAVE"), "a.wav", "audio/wav", route.Audio, "WAV signature"},
		{"flac", "fLaC\x00\x00\x00\x22", "a.flac", "", route.Audio, "FLAC signature"},
		{"mp3 id3", "ID3\x04\x00\x00\x00\x00\x00\x22", "a.mp3", "audio/mpeg", route.Audio, "MP3 signature"},
		{"mp3 frame", "\xff\xfb\x90\x64\x00", "a.mp3", "", route.Audio, "MP3 signature"},
		{"ogg", "OggS\x00\x02\x00\x00", "a.opus", "audio/ogg", route.Audio, "Ogg signature"},
		{"ogg vorbis", oggPage(oggBOS, vorbisPacket), "a.ogg", "audio/ogg", route.Audio, "Ogg signature"},
		{"ogg opus", oggPage(oggBOS, opusPacket), "a.opus", "", route.Audio, "Ogg signature"},
		{"m4a", ftyp("M4A ", "M4A ", "isom"), "a.m4a", "", route.Audio, "M4A brand"},

		{"mp4", ftyp("isom", "isom", "avc1"), "a.mp4", "video/mp4", route.Video, "MP4 brand"},
		{"matroska", ebml("matroska"), "a.mkv", "", route.Video, "Matroska signature"},
		{"webm", ebml("webm"), "a.webm", "video/webm", route.Video, "WebM signature"},
		{"avi", riffForm("AVI "), "a.avi", "", route.Video, "AVI signature"},
		{"mpeg-1 program stream", "\x00\x00\x01\xba\x21\x00\x01\x00\x01\x80\x1b\x91", "a.mpg", "video/mpeg", route.Video, "MPEG pack start code, a program stream"},
		{"mpeg-2 program stream", "\x00\x00\x01\xba\x44\x00\x04\x00\x04\x01\x01\x89\xc3\xf8", "a.vob", "", route.Video, "MPEG pack start code, a program stream"},
		{"mpeg program stream named .txt", "\x00\x00\x01\xba\x44\x00\x04\x00\x04\x01\x01\x89\xc3\xf8", "a.txt", "", route.Video, "MPEG pack start code, a program stream"},
		{"ogg theora", oggPage(oggBOS, theoraPacket), "a.ogv", "video/ogg", route.Video, "Ogg signature with a video stream"},
		{"ogg theora named .ogg", oggPage(oggBOS, vorbisPacket) + oggPage(oggBOS, theoraPacket), "a.ogg", "audio/ogg", route.Video, "Ogg signature with a video stream"},
		{"ogg vp8", oggPage(oggBOS, "OVP80\x01\x01\x00"), "a.ogv", "", route.Video, "Ogg signature with a video stream"},
		{"ogg dirac", oggPage(oggBOS, "BBCD\x00\x00\x00\x00"), "a.ogv", "", route.Video, "Ogg signature with a video stream"},
		{"ogg ogm video", oggPage(oggBOS, "\x01video\x00\x00\x00DIV3"), "a.ogm", "", route.Video, "Ogg signature with a video stream"},

		{"csv", "a,b\n1,2\n", "отчёт.csv", "text/csv", route.Text, "text by its .csv extension"},
		{"json in upper case", `{"a":1}`, "A.JSON", "", route.Text, "text by its .json extension"},
		{"ndjson", "{}\n{}\n", "a.ndjson", "", route.Text, "text by its .ndjson extension"},
		{"markdown", "# Title\n", "a.md", "", route.Text, "text by its .md extension"},
		{"subtitles", "1\n00:00:01,000 --> 00:00:02,000\nhi\n", "a.srt", "", route.Text, "text by its .srt extension"},
		{"xml", "<?xml version=\"1.0\"?><root/>", "a.xml", "", route.Text, "text by its .xml extension"},
		{"csv with a byte order mark", "\xef\xbb\xbfa,b\n", "a.csv", "", route.Text, "text by its .csv extension"},
		{"utf-8 bom without a name", "\xef\xbb\xbfplain text", "", "", route.Text, "text by its UTF-8 byte order mark"},
		{"utf-16 bom named .bin", "\xff\xfea\x00", "a.bin", "", route.Text, "text by its UTF-16 byte order mark"},
		{"utf-32 bom", "\x00\x00\xfe\xff\x00\x00\x00a", "a", "", route.Text, "text by its UTF-32 byte order mark"},
		{"html", "<!DOCTYPE html><html><body>hi</body></html>", "a.html", "text/html", route.Unrouted, "no routed signature"},
		{"htm in upper case", "<html><body>hi</body></html>", "A.HTM", "", route.Unrouted, "no routed signature"},
		{"xhtml", "<?xml version=\"1.0\"?><html xmlns=\"http://www.w3.org/1999/xhtml\"/>", "a.xhtml", "application/xhtml+xml", route.Unrouted, "no routed signature"},
		{"html with an inline svg", "<html><body><svg></svg></body></html>", "a.html", "text/html", route.Unrouted, "no routed signature"},
		{"svg named .html", svgProlog, "a.html", "text/html", route.VisionConverted, "SVG root element"},
		{"text of no text extension", "plain text", "a.typ", "text/plain", route.Unrouted, "no routed signature"},
		{"text stored as text/plain without a name", "plain text", "", "text/plain", route.Unrouted, "no routed signature"},
		{"pdf named .txt", pdfBytes, "a.txt", "text/plain", route.Document, "PDF signature"},

		{"eml with a byte order mark", "\xef\xbb\xbf" + headerShaped, "fwd.eml", "", route.Nested, "a message by its .eml extension, header-shaped"},
		{"rfc822 named .txt", headerShaped, "fwd.txt", "message/rfc822", route.Nested, "a message by its message/rfc822 type, header-shaped"},
		{"rfc822 named .txt, not header-shaped", "plain text", "fwd.txt", "message/rfc822", route.Unrouted, "a message by its message/rfc822 type, but not header-shaped"},

		{"pst", pstHeader("SM"), "archive.pst", "application/vnd.ms-outlook", route.Nested, "PST signature, a .pst"},
		{"pst without a name", pstHeader("SM"), "", "", route.Nested, "PST signature, a .pst"},
		{"ost", pstHeader("SO"), "offline.ost", "", route.Unrouted, "PST signature with an .ost's client magic, an offline store mail refuses"},
		{"ost named .pst", pstHeader("SO"), "archive.pst", "", route.Unrouted, "PST signature with an .ost's client magic, an offline store mail refuses"},
		{"mbox", mboxBytes, "Inbox", "", route.Nested, `mbox "From " line`},
		{"mbox named .mbox", mboxBytes, "a.mbox", "application/mbox", route.Nested, `mbox "From " line`},
		{"mbox named .eml", mboxBytes, "a.eml", "message/rfc822", route.Nested, `mbox "From " line`},
		{"mbox line named .txt, prose", "From the desk of Ann\n", "letter.txt", "text/plain", route.Text, "text by its .txt extension"},

		{"pe", peBytes, "setup.exe", "application/x-msdownload", route.Unrouted, "PE signature, an executable"},
		{"pe named .pdf", peBytes, "invoice.pdf", "application/pdf", route.Unrouted, "PE signature, an executable"},
		{"elf", "\x7fELF\x02\x01\x01", "a", "", route.Unrouted, "ELF signature, an executable"},
		{"mach-o", "\xcf\xfa\xed\xfe\x00", "a.dylib", "", route.Unrouted, "Mach-O signature, an executable"},
		{"shebang", "#!/bin/sh\necho hi\n", "run", "", route.Unrouted, `"#!" line, an executable script`},
		{"shebang named .sh", "#! /bin/sh\necho hi\n", "run.sh", "text/x-shellscript", route.Unrouted, `"#!" line, an executable script`},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, c.content, filepath.Ext(c.filename))
			got, why, err := route.File(filepath.Join(dir, file), c.filename, c.mime)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || why != c.why {
				t.Errorf("route %q (%s), want %q (%s)", got, why, c.want, c.why)
			}
		})
	}
}

func TestFileContainerExtensions(t *testing.T) {
	dir := t.TempDir()
	check := func(content, filename string, want route.Route) {
		t.Helper()
		file := writeContent(t, dir, content+filename, filepath.Ext(filename))
		if got, why, err := route.File(filepath.Join(dir, file), filename, ""); err != nil || got != want {
			t.Errorf("%s: route %q (%s), %v; want %q", filename, got, why, err, want)
		}
	}
	for _, ext := range []string{".docx", ".docm", ".dotx", ".dotm", ".xlsx", ".xlsm", ".xltx", ".xltm", ".pptx", ".pptm", ".potx", ".potm", ".ppsx"} {
		check(zipBytes, "a"+ext, route.Office)
		check(ole2Bytes, "a"+ext, route.Unrouted)
	}
	for _, ext := range []string{".doc", ".dot", ".xls", ".xlt", ".ppt", ".pps", ".pot"} {
		check(ole2Bytes, "a"+strings.ToUpper(ext), route.Office)
		check(zipBytes, "a"+ext, route.Archive)
	}
	check(ole2Bytes, "a.msg", route.Nested)
	check(zipBytes, "a.msg", route.Archive)
	check(ole2Bytes, "a.one", route.Unrouted)
	check(zipBytes, "a.onepkg", route.Archive)
}

func ole2With(size int, name string, offsets ...int) string {
	b := make([]byte, size)
	copy(b, ole2Bytes)
	for _, off := range offsets {
		for i := range len(name) {
			b[off+2*i] = name[i]
		}
	}
	return string(b)
}

func TestFileEncryptedPackage(t *testing.T) {
	const encryptedWhy = "OLE2 signature with an EncryptedPackage stream, %s extension, an encrypted OOXML package"
	cases := []struct {
		name, content, filename string
		want                    route.Route
		why                     string
	}{
		{"first entry", ole2With(1024, "EncryptedPackage", 512), "secret.xlsx", route.Office, fmt.Sprintf(encryptedWhy, ".xlsx")},
		{"second entry, upper case", ole2With(1024, "EncryptedPackage", 640), "SECRET.DOCX", route.Office, fmt.Sprintf(encryptedWhy, ".docx")},
		{"4096-byte sectors", ole2With(8192, "EncryptedPackage", 4096+384), "deck.pptm", route.Office, fmt.Sprintf(encryptedWhy, ".pptm")},
		{"binary workbook", ole2With(1024, "EncryptedPackage", 768), "book.xlsb", route.Office, fmt.Sprintf(encryptedWhy, ".xlsb")},
		{"legacy name", ole2With(1024, "EncryptedPackage", 512), "old.xls", route.Office, "OLE2 signature, .xls extension"},
		{"no ooxml name", ole2With(1024, "EncryptedPackage", 512), "secret.bin", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"unaligned", ole2With(1024, "EncryptedPackage", 514), "secret.xlsx", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"within the header", ole2With(1024, "EncryptedPackage", 128), "secret.xlsx", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"a longer name", ole2With(1024, "EncryptedPackageX", 512), "secret.xlsx", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"another stream", ole2With(1024, "EncryptionInfo", 512), "secret.xlsx", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"header alone", ole2Bytes, "secret.xlsx", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
		{"entry cut short", ole2With(512+40, "EncryptedPackage", 512), "secret.xlsx", route.Unrouted, "OLE2 signature without a legacy or .msg extension"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, c.content+c.filename, filepath.Ext(c.filename))
			got, why, err := route.File(filepath.Join(dir, file), c.filename, "")
			if err != nil || got != c.want || why != c.why {
				t.Errorf("route %q (%s), %v; want %q (%s)", got, why, err, c.want, c.why)
			}
		})
	}
	got, why, err := route.File("../inspect/testdata/encrypted.xlsx", "encrypted.xlsx", "")
	if err != nil || got != route.Office || why != fmt.Sprintf(encryptedWhy, ".xlsx") {
		t.Errorf("inspect's encrypted.xlsx: route %q (%s), %v; want office", got, why, err)
	}
}

func TestFileErrors(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := route.File(filepath.Join(dir, "missing"), "a.pdf", ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v, want fs.ErrNotExist", err)
	}
	sub := filepath.Join(dir, "a-directory.pdf")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, _, err := route.File(sub, "a.pdf", ""); err == nil {
		t.Errorf("directory: route %q, want an error", got)
	}
}
