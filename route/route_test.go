package route_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/route"
)

var (
	pdfBytes  = "%PDF-1.7\n%filler"
	pngBytes  = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
	jpegBytes = "\xff\xd8\xff\xe0\x00\x10JFIF\x00"
	gif87     = "GIF87a\x01\x00\x01\x00"
	gif89     = "GIF89a\x01\x00\x01\x00"
	tiffLE    = "II*\x00\x08\x00\x00\x00"
	tiffBE    = "MM\x00*\x00\x00\x00\x08"
	zipBytes  = "PK\x03\x04\x14\x00\x06\x00[Content_Types].xml"
	ole2Bytes = "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1\x00\x00\x00\x00"
	wmfPlace  = "\xd7\xcd\xc6\x9a\x00\x00\x00\x00"
	peBytes   = pe(0x40)
)

func pe(at uint32) string {
	b := make([]byte, 0x80)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], at)
	copy(b[0x40:], "PE\x00\x00")
	return string(b)
}

func bmp(dib uint32) string {
	b := make([]byte, 54)
	copy(b, "BM")
	binary.LittleEndian.PutUint32(b[14:], dib)
	return string(b)
}

func emf(typ uint32, sig string) string {
	b := make([]byte, 88)
	binary.LittleEndian.PutUint32(b, typ)
	binary.LittleEndian.PutUint32(b[4:], 108)
	copy(b[40:], sig)
	return string(b)
}

func wmf(typ, size, version uint16) string {
	b := make([]byte, 18)
	binary.LittleEndian.PutUint16(b, typ)
	binary.LittleEndian.PutUint16(b[2:], size)
	binary.LittleEndian.PutUint16(b[4:], version)
	return string(b)
}

func writeContent(t *testing.T, dir, content, ext string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	name := hex.EncodeToString(sum[:]) + ext
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestFileSignatures(t *testing.T) {
	type tc struct {
		name     string
		content  string
		filename string
		mime     string
		want     route.Route
	}
	cases := []tc{
		{"pdf", pdfBytes, "report.pdf", "application/pdf", route.Document},
		{"png", pngBytes, "a.png", "image/png", route.Vision},
		{"jpeg", jpegBytes, "a.jpg", "image/jpeg", route.Vision},
		{"gif87a", gif87, "a.gif", "image/gif", route.Vision},
		{"gif89a", gif89, "a.gif", "image/gif", route.Vision},
		{"bmp", bmp(40), "a.bmp", "image/bmp", route.Vision},
		{"bmp core header", bmp(12), "a.bmp", "", route.Vision},
		{"bmp v5 header", bmp(124), "a.bmp", "", route.Vision},
		{"tiff little-endian", tiffLE, "a.tif", "image/tiff", route.VisionConverted},
		{"tiff big-endian", tiffBE, "a.tiff", "image/tiff", route.VisionConverted},
		{"emf", emf(1, " EMF"), "a.emf", "image/x-emf", route.VisionConverted},
		{"wmf placeable", wmfPlace, "a.wmf", "image/x-wmf", route.VisionConverted},
		{"wmf memory 3.0", wmf(1, 9, 0x0300), "a.wmf", "", route.VisionConverted},
		{"wmf disk 1.0", wmf(2, 9, 0x0100), "a.wmf", "", route.VisionConverted},
		{"ole2 msg", ole2Bytes, "forwarded.msg", "application/vnd.ms-outlook", route.Nested},
		{"ole2 MSG upper case", ole2Bytes, "FORWARDED.MSG", "", route.Nested},

		{"bmp with no dib header", "BM" + strings.Repeat("x", 30), "a.bmp", "image/bmp", route.Unrouted},
		{"emf type without signature", emf(1, "NOPE"), "a.emf", "image/x-emf", route.Unrouted},
		{"emf signature on another record", emf(2, " EMF"), "a.emf", "", route.Unrouted},
		{"wmf version 2", wmf(1, 9, 0x0200), "a.wmf", "", route.Unrouted},
		{"wmf type 3", wmf(3, 9, 0x0300), "a.wmf", "", route.Unrouted},
		{"wmf header size 8", wmf(1, 8, 0x0300), "a.wmf", "", route.Unrouted},
		{"pdf signature cut short", "%PDF", "a.pdf", "application/pdf", route.Unrouted},
		{"zip without an ooxml extension", zipBytes, "archive.zip", "application/zip", route.Archive},
		{"zip named for a macro slide show office doesn't read", zipBytes, "a.ppsm", "", route.Archive},
		{"zip named for a legacy format", zipBytes, "a.doc", "application/msword", route.Archive},
		{"ole2 without an extension", ole2Bytes, "attachment", "", route.Unrouted},
		{"ole2 named for ooxml without an EncryptedPackage stream", ole2Bytes, "secret.docx", "", route.Unrouted},
		{"ole2 named .bin", ole2Bytes, "a.bin", "application/msword", route.Unrouted},
		{"legacy name without the signature", "plain text", "notes.doc", "application/msword", route.Unrouted},
		{"ooxml name without the signature", "plain text", "notes.docx", "", route.Unrouted},
		{"msg name without the signature", "plain text", "note.msg", "application/vnd.ms-outlook", route.Unrouted},
		{"eml", "From: a@example.org\r\nSubject: x\r\n\r\nbody", "fwd.eml", "message/rfc822", route.Nested},
		{"text", "plain text", "a.txt", "text/plain", route.Text},
		{"empty", "", "a.pdf", "application/pdf", route.Unrouted},

		{"pdf stored as png", pdfBytes, "a.png", "image/png", route.Document},
		{"png stored as pdf", pngBytes, "report.pdf", "application/pdf", route.Vision},
		{"jpeg stored as docx", jpegBytes, "a.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", route.Vision},
		{"text stored as pdf", "plain text", "a.pdf", "application/pdf", route.Unrouted},
	}
	for _, ext := range []string{".docx", ".docm", ".dotx", ".dotm", ".xlsx", ".xlsm", ".xltx", ".xltm", ".pptx", ".pptm", ".potx", ".potm", ".ppsx", ".DOCX", ".XlSm"} {
		cases = append(cases, tc{"zip " + ext, zipBytes, "doc" + ext, "application/octet-stream", route.Office})
	}
	for _, ext := range []string{".doc", ".dot", ".xls", ".xlt", ".ppt", ".pps", ".pot", ".DOC", ".Xls", ".PPS"} {
		cases = append(cases, tc{"ole2 " + ext, ole2Bytes, "doc" + ext, "application/octet-stream", route.Office})
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, c.content, filepath.Ext(c.filename))
			got, why, err := route.File(filepath.Join(dir, file), c.filename, c.mime)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || why == "" {
				t.Errorf("route %q (%s), want %q", got, why, c.want)
			}
		})
	}
}

const headerShaped = "From: ann@example.org\r\nSubject: x\r\n\r\nbody"

func TestFileMessages(t *testing.T) {
	const notShaped = "but not header-shaped"
	long := strings.Repeat("x", 64*1024)
	cases := []struct {
		name, content, filename, mime string
		want                          route.Route
		why                           string
	}{
		{"eml extension alone", headerShaped, "fwd.eml", "application/octet-stream", route.Nested, ".eml extension, header-shaped"},
		{"eml extension in upper case", headerShaped, "FWD.EML", "", route.Nested, ".eml extension, header-shaped"},
		{"rfc822 type alone", headerShaped, "Fwd", "message/rfc822", route.Nested, "message/rfc822 type, header-shaped"},
		{"global type alone", headerShaped, "fwd.bin", "message/global", route.Nested, "message/global type, header-shaped"},
		{"rfc822 type in odd case with parameters", headerShaped, "", "Message/RFC822; name=\"fwd\"", route.Nested, "message/rfc822 type, header-shaped"},
		{"global type with a malformed parameter", headerShaped, "fwd", "MESSAGE/Global; name", route.Nested, "message/global type, header-shaped"},

		{"date field after a folded one", "Received: from a\r\n by b; Mon, 12 Jan 2026\r\nDate: x\r\n", "a.eml", "", route.Nested, "header-shaped"},
		{"space before the colon, lower-case from, bare newlines", "Subject : hi\nfrom: ann@example.org\n", "a.eml", "", route.Nested, "header-shaped"},
		{"date field without a newline", "date:x", "a.eml", "", route.Nested, "header-shaped"},
		{"from field past 64 KiB on the first line", "From: " + long + "\r\n", "a.eml", "", route.Nested, "header-shaped"},
		{"mbox separator first, an mbox", "From ann@example.org Sat Jan  3 01:05:34 1996\nFrom: ann@example.org\n", "a.eml", "message/rfc822", route.Nested, `mbox "From " line`},
		{"mbox separator first, named .txt", "From ann@example.org Sat Jan  3 01:05:34 1996\nFrom: ann@example.org\n", "a.txt", "message/rfc822", route.Unrouted, notShaped},
		{"plain text", "plain text", "a.eml", "message/rfc822", route.Unrouted, notShaped},
		{"empty", "", "a.eml", "", route.Unrouted, notShaped},
		{"blank line first", "\r\nFrom: ann@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},
		{"folded line first", " folded\r\nFrom: ann@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},
		{"from field after the blank line", "Subject: hi\r\n\r\nFrom: ann@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},
		{"from field after a line not a field", "Subject: hi\r\nnot a field\r\nFrom: ann@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},
		{"from field after a line past 64 KiB", "Subject: " + long + "\r\nFrom: ann@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},
		{"neither from nor date", "Subject: hi\r\nTo: bob@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},
		{"field without a name", ": no name\r\nFrom: ann@example.org\r\n", "a.eml", "", route.Unrouted, notShaped},

		{"header-shaped text", headerShaped, "a.txt", "text/plain", route.Text, "text by its .txt extension"},
		{"header-shaped, no name or type", headerShaped, "", "", route.Unrouted, "no routed signature"},
		{"another message type", headerShaped, "a.txt", "message/partial", route.Text, "text by its .txt extension"},
		{"rfc822 as a parameter's value", headerShaped, "a", "text/plain; x=message/rfc822", route.Unrouted, "no routed signature"},

		{"pdf named and stored as a message", pdfBytes, "a.eml", "message/rfc822", route.Document, "PDF signature"},
		{"zip named as a message", zipBytes, "a.eml", "message/rfc822", route.Archive, "ZIP signature without an OOXML extension, [Content_Types].xml, or ODF mimetype"},
		{"ole2 stored as a message", ole2Bytes, "a.msg", "message/rfc822", route.Nested, "OLE2 signature, .msg extension"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, c.content, filepath.Ext(c.filename))
			got, why, err := route.File(filepath.Join(dir, file), c.filename, c.mime)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || !strings.HasSuffix(why, c.why) {
				t.Errorf("route %q (%s), want %q (...%s)", got, why, c.want, c.why)
			}
		})
	}
}

func TestFileStoredMedia(t *testing.T) {
	cases := []struct {
		name, content, filename, mime string
		want                          route.Route
	}{
		{"onenote audio recording", "\x00\x01opaque", "recording.bin", "audio", route.Audio},
		{"onenote video recording", "\x00\x01opaque", "recording", "video", route.Video},
		{"a full audio type in odd case", "\x00\x01opaque", "a", "Audio/OGG", route.Audio},
		{"a video type with parameters", "\x00\x01opaque", "a", "video/webm; codecs=vp9", route.Video},
		{"another top-level type", "\x00\x01opaque", "a", "image/png", route.Unrouted},
		{"a text extension first", "spoken words", "a.txt", "audio", route.Text},
		{"a signature first", pdfBytes, "a", "video", route.Document},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, c.content, filepath.Ext(c.filename))
			got, why, err := route.File(filepath.Join(dir, file), c.filename, c.mime)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("route %q (%s), want %q", got, why, c.want)
			}
		})
	}
}
