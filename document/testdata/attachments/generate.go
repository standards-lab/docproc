//go:build ignore

// generate writes attachments.pdf beside this file, which document/testdata/README.md describes. Run it from
// document/testdata/attachments with `mise exec -- go run generate.go`; the check never runs it,
// and reads the committed file.
package main

import (
	"bytes"
	"fmt"
	"os"
)

const (
	notesText = "Embedded through the name tree.\n"
	dataText  = "name,count\nharbor,3\n"
	clipText  = "RIFF\x00\x00\x00\x00AVI not a real clip\n"
)

const headerMarkedBinary = "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"

func main() {
	if err := os.WriteFile("attachments.pdf", build(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

const page = "BT /Helv 16 Tf 72 720 Td (Attachments) Tj ET\n"

func build() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(notes.txt) 5 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /Helv 11 0 R >> >> /Annots [7 0 R 9 0 R] >>",
		stream("", page),
		"<< /Type /Filespec /F (notes.txt) /UF (notes.txt) /EF << /F 6 0 R >> >>",
		stream("/Type /EmbeddedFile ", notesText),
		"<< /Type /Annot /Subtype /FileAttachment /Rect [72 650 92 670] /FS 8 0 R /Name /PushPin >>",
		"<< /Type /Filespec /F (data.csv) /UF (data.csv) /EF << /F 12 0 R >> >>",
		"<< /Type /Annot /Subtype /Screen /Rect [72 400 272 600] /P 3 0 R " +
			"/A << /Type /Action /S /Rendition /OP 0 /AN 9 0 R /R 10 0 R >> >>",
		"<< /Type /Rendition /S /MR /C << /Type /MediaClip /S /MCD /CT (video/avi) " +
			"/D << /Type /Filespec /F (clip.avi) /UF (clip.avi) /EF << /F 13 0 R >> >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		stream("/Type /EmbeddedFile ", dataText),
		stream("/Type /EmbeddedFile ", clipText),
	}

	var b bytes.Buffer
	b.WriteString(headerMarkedBinary)
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func stream(dict, content string) string {
	return fmt.Sprintf("<< %s/Length %d >>\nstream\n%sendstream", dict, len(content), content)
}
