package route_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/route"
)

func zipOf(t *testing.T, names ...string) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range names {
		f, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestAtContent(t *testing.T) {
	const ooxml = "ZIP signature with a [Content_Types].xml entry, an OOXML package"
	many := make([]string, 0, 2001)
	for i := range 2000 {
		many = append(many, fmt.Sprintf("word/media/image%d.png", i))
	}
	cases := []struct {
		name, content, filename string
		want                    route.Route
		why                     string
	}{
		{"content types first, named .zip", zipOf(t, "[Content_Types].xml", "word/document.xml"), "a.zip", route.Office, ooxml},
		{"content types last, no name", zipOf(t, "word/document.xml", "[Content_Types].xml"), "", route.Office, ooxml},
		{"content types after 2,000 entries", zipOf(t, append(many, "[Content_Types].xml")...), "upload.bin", route.Office, ooxml},
		{"content types in a folder", zipOf(t, "docs/[Content_Types].xml"), "a.zip", route.Archive, "ZIP signature without an OOXML extension, [Content_Types].xml, or ODF mimetype"},
		{"no content types, named .docx", zipOf(t, "word/document.xml"), "a.docx", route.Office, "ZIP signature, .docx extension"},
		{"no central directory", "PK\x03\x04 and nothing after", "a.zip", route.Archive, "ZIP signature without an OOXML extension, [Content_Types].xml, or ODF mimetype"},

		{"pdf after white space, named .bin", "\r\n  " + pdfBytes, "a.bin", route.Document, "PDF signature"},
		{"pdf after a byte order mark", "\xef\xbb\xbf" + pdfBytes, "", route.Document, "PDF signature"},

		{"shebang text named .txt", "#!/bin/sh is how a script begins\n", "notes.txt", route.Text, `"#!" line, a script`},
		{"shebang text named .md", "#! /usr/bin/env python3\n", "notes.md", route.Text, `"#!" line, a script`},
		{"gzip named .txt", "\x1f\x8b\x08\x00\x00\x00\x00\x00", "a.txt", route.Archive, "gzip signature"},
		{"jpeg named .log", jpegBytes, "a.log", route.Vision, "JPEG signature"},
		{"id3 tag named .txt", "ID3\x04\x00\x00\x00\x00\x00\x22", "a.txt", route.Audio, "MP3 signature"},
		{"shebang named .sh", "#!/bin/sh\n", "a.sh", route.Text, `"#!" line, a script`},
		{"shebang without a name", "#!/bin/sh\necho hi\n", "", route.Text, `"#!" line, a script`},

		{"from and date, no name", "From: ann@example.org\r\nDate: Mon, 12 Jan 2026 10:00:00 +0000\r\n\r\nbody", "", route.Nested, "a message by its header, From and Date"},
		{"received and from, named .bin", "Received: from a by b\r\nFrom: ann@example.org\r\n\r\nbody", "upload.bin", route.Nested, "a message by its header, From and Received"},
		{"from and date after a byte order mark", "\xef\xbb\xbfDate: x\r\nFrom: ann@example.org\r\n", "", route.Nested, "a message by its header, From and Date"},
		{"from alone", "From: ann@example.org\r\nSubject: x\r\n", "", route.Text, "no routed signature, text by its head"},
		{"date alone", "Date: x\r\nSubject: x\r\n", "", route.Text, "no routed signature, text by its head"},
		{"from and date named .txt", "From: ann@example.org\r\nDate: x\r\n", "a.txt", route.Nested, "a message by its header, From and Date"},
		{"from and date after prose", "Dear Ann,\r\nFrom: ann@example.org\r\nDate: x\r\n", "", route.Text, "no routed signature, text by its head"},
		{"from and date past the blank line", "Subject: x\r\n\r\nFrom: ann@example.org\r\nDate: x\r\n", "", route.Text, "no routed signature, text by its head"},
		{"from and date after a utf-8 bom, no header", "\xef\xbb\xbfplain\r\nFrom: a\r\nDate: x\r\n", "", route.Text, "text by its UTF-8 byte order mark"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, c.content, filepath.Ext(c.filename))
			got, why, err := route.File(filepath.Join(dir, file), c.filename, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || why != c.why {
				t.Errorf("route %q (%s), want %q (%s)", got, why, c.want, c.why)
			}
			r := strings.NewReader(c.content)
			head := []byte(c.content[:min(len(c.content), route.HeadLen)])
			if got2, why2, err := route.At(r, int64(len(c.content)), head, c.filename, ""); err != nil || got2 != got || why2 != why {
				t.Errorf("At: route %q (%s), %v; File %q (%s)", got2, why2, err, got, why)
			}
		})
	}
}

func TestAtNearClaims(t *testing.T) {
	const (
		separator = "From ann@example.org Sat Jan  3 01:05:34 1996\n"
		mbox      = `mbox "From " line`
		noHeader  = `mbox "From " line without a message header`
		eml       = "a message by its .eml extension, but not header-shaped"
		rfc822    = "a message by its message/rfc822 type, but not header-shaped"
	)
	cases := []struct {
		name, content, filename, mime string
		want                          route.Route
		why                           string
	}{
		{"mbox with from and date", separator + "From: ann@example.org\nDate: x\n\nbody\n", "Inbox", "", route.Nested, mbox},
		{"mbox with from and received", separator + "Received: from a by b\nFrom: ann@example.org\n\nbody\n", "a.mbox", "application/mbox", route.Nested, mbox},
		{"mbox after a separator past 64 KiB", "From " + strings.Repeat("x", 64*1024) + "\nFrom: ann@example.org\nDate: x\n", "", "", route.Nested, mbox},
		{"mbox with crlf lines", "From ann@example.org\r\nFrom: ann@example.org\r\nDate: x\r\n", "", "", route.Nested, mbox},
		{"from prose without a header", "From the desk of Ann\nDear Bob,\n", "letter.txt", "", route.Text, noHeader + ", text by its head"},
		{"from line alone", "From ann", "", "", route.Text, noHeader + ", text by its head"},
		{"from and subject alone", separator + "From: ann@example.org\nSubject: x\n", "", "", route.Text, noHeader + ", text by its head"},
		{"date and received without from", separator + "Date: x\nReceived: from a by b\n", "", "", route.Text, noHeader + ", text by its head"},
		{"from and date after prose", separator + "Dear Bob,\nFrom: ann@example.org\nDate: x\n", "", "", route.Text, noHeader + ", text by its head"},
		{"from and date past the blank line", separator + "\nFrom: ann@example.org\nDate: x\n", "", "", route.Text, noHeader + ", text by its head"},
		{"from followed by nul bytes", "From \x00\x00\x00\x00", "", "", route.Unrouted, noHeader + ", not text"},
		{"from header with a nul in the body", separator + "Subject: x\n\n\x00", "a.mbox", "", route.Unrouted, noHeader + ", not text"},

		{"eml not header-shaped, text", "Dear Bob,\nhello\n", "a.eml", "", route.Text, eml + "; text by its head"},
		{"eml not header-shaped, a nul", "Dear Bob,\x00hello\n", "a.eml", "", route.Unrouted, eml},
		{"rfc822 not header-shaped, text", "Dear Bob,\nhello\n", "fwd", "message/rfc822", route.Text, rfc822 + "; text by its head"},
		{"rfc822 not header-shaped, a nul", "Dear Bob,\x00hello\n", "fwd", "message/rfc822", route.Unrouted, rfc822},
		{"eml header-shaped", "From: ann@example.org\r\nSubject: x\r\n\r\nbody", "a.eml", "", route.Nested, "a message by its .eml extension, header-shaped"},

		{"text stored as audio/mpeg", "the minutes of the meeting\n", "minutes.mp3", "audio/mpeg", route.Text, "no routed signature, text by its head"},
		{"a nul stored as audio/mpeg", "\x00\x01opaque", "a.mp3", "audio/mpeg", route.Audio, "no known signature, stored as audio/mpeg"},
		{"a nul stored as video/mp4", "\x00\x01opaque", "clip", "video/mp4", route.Video, "no known signature, stored as video/mp4"},
		{"a message stored as audio", "From: ann@example.org\r\nDate: x\r\n\r\nbody", "", "audio/wav", route.Nested, "a message by its header, From and Date"},
		{"a utf-8 bom stored as audio", "\xef\xbb\xbfwords", "", "audio/wav", route.Text, "text by its UTF-8 byte order mark"},
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
			head := []byte(c.content[:min(len(c.content), route.HeadLen)])
			got, why, err = route.At(strings.NewReader(c.content), int64(len(c.content)), head, c.filename, c.mime)
			if err != nil || got != c.want || why != c.why {
				t.Errorf("At: route %q (%s), %v; want %q (%s)", got, why, err, c.want, c.why)
			}
		})
	}
}

func TestAtXPS(t *testing.T) {
	const (
		ooxml   = "ZIP signature with a [Content_Types].xml entry, an OOXML package"
		whole   = "ZIP signature with a FixedDocumentSequence.fdseq entry, an XPS package"
		pieced  = "ZIP signature with a FixedDocumentSequence.fdseq/[0].piece entry, an XPS package"
		archive = "ZIP signature without an OOXML extension, [Content_Types].xml, or ODF mimetype"
	)
	wholeXPS := zipOf(t, "[Content_Types].xml", "_rels/.rels", "FixedDocumentSequence.fdseq",
		"Documents/1/FixedDocument.fdoc", "Documents/1/Pages/1.fpage")
	piecedXPS := zipOf(t, "[Content_Types].xml/[0].piece", "_rels/.rels/[0].piece",
		"FixedDocumentSequence.fdseq/[0].piece", "Documents/1/FixedDocument.fdoc/[0].piece",
		"Documents/1/Pages/1.fpage/[0].piece", "Documents/1/Pages/1.fpage/[1].last.piece",
		"[Content_Types].xml/[1].last.piece")
	cases := []struct {
		name, content, filename string
		want                    route.Route
		why                     string
	}{
		{"whole xps named .docx", wholeXPS, "testing - large.docx", route.Unrouted, whole},
		{"whole xps named .xps", wholeXPS, "a.xps", route.Unrouted, whole},
		{"pieced xps named .docx", piecedXPS, "testing - large.docx", route.Unrouted, pieced},
		{"a later piece alone, named .docx", zipOf(t, "FixedDocumentSequence.fdseq/[1].last.piece"), "a.docx", route.Office, "ZIP signature, .docx extension"},
		{"sequence in a folder, named .zip", zipOf(t, "docs/FixedDocumentSequence.fdseq"), "a.zip", route.Archive, archive},
		{"docx", zipOf(t, "[Content_Types].xml", "_rels/.rels", "word/document.xml"), "a.docx", route.Office, ooxml},
		{"xlsx", zipOf(t, "[Content_Types].xml", "_rels/.rels", "xl/workbook.xml"), "a.xlsx", route.Office, ooxml},
		{"pptx", zipOf(t, "[Content_Types].xml", "_rels/.rels", "ppt/presentation.xml"), "a.pptx", route.Office, ooxml},
		{"plain zip", zipOf(t, "notes.txt", "pictures/a.png"), "a.zip", route.Archive, archive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			head := []byte(c.content[:min(len(c.content), route.HeadLen)])
			got, why, err := route.At(strings.NewReader(c.content), int64(len(c.content)), head, c.filename, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || why != c.why {
				t.Errorf("route %q (%s), want %q (%s)", got, why, c.want, c.why)
			}
		})
	}
}

func TestExt(t *testing.T) {
	for name, want := range map[string]string{
		"Report.PDF":         ".pdf",
		"invoice.pdf.exe. ":  ".exe",
		`C:\docs\a.DOCX`:     ".docx",
		"dir.d/README":       "",
		`dir.d\README`:       "",
		"archive.tar.gz":     ".gz",
		"":                   "",
		"no extension . . .": "",
	} {
		if got := route.Ext(name); got != want {
			t.Errorf("Ext(%q) = %q, want %q", name, got, want)
		}
	}
	if !route.OOXMLExtension(".xlsb") || !route.OOXMLExtension(".docm") || route.OOXMLExtension(".doc") {
		t.Error("OOXMLExtension: .xlsb and .docm are OOXML packages' extensions, .doc isn't")
	}
	for ext, want := range map[string]string{
		".doc": "docx", ".dot": "docx", ".xls": "xlsx", ".xlt": "xlsx",
		".ppt": "pptx", ".pps": "pptx", ".pot": "pptx",
		".docx": "", ".rtf": "", ".DOC": "", "": "",
	} {
		if got := route.LegacyTarget(ext); got != want {
			t.Errorf("LegacyTarget(%q) = %q, want %q", ext, got, want)
		}
	}
}
