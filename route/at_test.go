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

// zipOf returns a ZIP of the named entries, each holding its own name, in order.
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

// TestAtContent: a ZIP holding [Content_Types].xml is an OOXML package, office's, whatever its
// name and wherever the entry is in its central directory, its reason naming the entry, and one
// that holds it only in a deeper folder, or holds none, is an archive by its name; a PDF header
// after white space or a byte order mark is a PDF; content whose head is text, named with a text
// extension, is text before a short signature, but not before a binary head; and a header of
// From and Date, or From and Received, makes content of no signature and no text extension a
// message, while From or Date alone, a text extension, or a first line that is no field don't.
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

		{"shebang text named .txt", "#!/bin/sh is how a script begins\n", "notes.txt", route.Text, "text by its .txt extension, before a short shebang signature"},
		{"shebang text named .md", "#! /usr/bin/env python3\n", "notes.md", route.Text, "text by its .md extension, before a short shebang signature"},
		{"gzip named .txt", "\x1f\x8b\x08\x00\x00\x00\x00\x00", "a.txt", route.Archive, "gzip signature"},
		{"jpeg named .log", jpegBytes, "a.log", route.Vision, "JPEG signature"},
		{"id3 tag named .txt", "ID3\x04\x00\x00\x00\x00\x00\x22", "a.txt", route.Audio, "MP3 signature"},
		{"shebang named .sh", "#!/bin/sh\n", "a.sh", route.Unrouted, `"#!" line, an executable script`},

		{"from and date, no name", "From: ann@example.org\r\nDate: Mon, 12 Jan 2026 10:00:00 +0000\r\n\r\nbody", "", route.Nested, "a message by its header, From and Date"},
		{"received and from, named .bin", "Received: from a by b\r\nFrom: ann@example.org\r\n\r\nbody", "upload.bin", route.Nested, "a message by its header, From and Received"},
		{"from and date after a byte order mark", "\xef\xbb\xbfDate: x\r\nFrom: ann@example.org\r\n", "", route.Nested, "a message by its header, From and Date"},
		{"from alone", "From: ann@example.org\r\nSubject: x\r\n", "", route.Unrouted, "no routed signature"},
		{"date alone", "Date: x\r\nSubject: x\r\n", "", route.Unrouted, "no routed signature"},
		{"from and date named .txt", "From: ann@example.org\r\nDate: x\r\n", "a.txt", route.Text, "text by its .txt extension"},
		{"from and date after prose", "Dear Ann,\r\nFrom: ann@example.org\r\nDate: x\r\n", "", route.Unrouted, "no routed signature"},
		{"from and date past the blank line", "Subject: x\r\n\r\nFrom: ann@example.org\r\nDate: x\r\n", "", route.Unrouted, "no routed signature"},
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
			// At over the same bytes, given a head, routes them alike.
			r := strings.NewReader(c.content)
			head := []byte(c.content[:min(len(c.content), route.HeadLen)])
			if got2, why2, err := route.At(r, int64(len(c.content)), head, c.filename, ""); err != nil || got2 != got || why2 != why {
				t.Errorf("At: route %q (%s), %v; File %q (%s)", got2, why2, err, got, why)
			}
		})
	}
}

// TestExt: an extension is lowercased, with trailing dots and spaces trimmed, of a name or a path
// by either separator.
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
}
