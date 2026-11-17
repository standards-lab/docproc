//go:build ignore

// generate writes attachments.pdf beside this file: a one-page PDF that embeds a file each way a
// PDF holds one, written by hand, every byte fixed, so a rerun writes the same bytes. Run it from
// document/testdata/attachments with `mise exec -- go run generate.go`; the check never runs it,
// and reads the committed file.
//
// Its embedded files:
//
//   - notes.txt, in the catalog's name tree of embedded files, holding notesText;
//   - data.csv, behind a file attachment annotation on the page, holding dataText;
//   - clip.avi, behind a Screen annotation's rendition, the media a page plays rather than a file
//     it attaches, holding clipText, which a reader of attachments doesn't list.
package main

import (
	"bytes"
	"fmt"
	"os"
)

// The embedded files' contents.
const (
	notesText = "Embedded through the name tree.\n"
	dataText  = "name,count\nharbor,3\n"
	clipText  = "RIFF\x00\x00\x00\x00AVI not a real clip\n"
)

func main() {
	if err := os.WriteFile("attachments.pdf", build(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

// page is the page's content: a heading.
const page = "BT /Helv 16 Tf 72 720 Td (Attachments) Tj ET\n"

// build returns the PDF: its objects, numbered from 1 in order, then its cross-reference table
// and trailer.
func build() []byte {
	objects := []string{
		// 1: the catalog, its name tree holding notes.txt.
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(notes.txt) 5 0 R] >> >> >>",
		// 2: the page tree.
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: the page, with the file attachment and Screen annotations.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /Helv 11 0 R >> >> /Annots [7 0 R 9 0 R] >>",
		// 4: the page's content.
		stream("", page),
		// 5 and 6: notes.txt's file specification and its content.
		"<< /Type /Filespec /F (notes.txt) /UF (notes.txt) /EF << /F 6 0 R >> >>",
		stream("/Type /EmbeddedFile ", notesText),
		// 7: the file attachment annotation, and 8, data.csv's file specification, its content 12.
		"<< /Type /Annot /Subtype /FileAttachment /Rect [72 650 92 670] /FS 8 0 R /Name /PushPin >>",
		"<< /Type /Filespec /F (data.csv) /UF (data.csv) /EF << /F 12 0 R >> >>",
		// 9: the Screen annotation, playing 10, a media rendition of clip.avi.
		"<< /Type /Annot /Subtype /Screen /Rect [72 400 272 600] /P 3 0 R " +
			"/A << /Type /Action /S /Rendition /OP 0 /AN 9 0 R /R 10 0 R >> >>",
		"<< /Type /Rendition /S /MR /C << /Type /MediaClip /S /MCD /CT (video/avi) " +
			"/D << /Type /Filespec /F (clip.avi) /UF (clip.avi) /EF << /F 13 0 R >> >> >> >>",
		// 11: the font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 12 and 13: data.csv's and clip.avi's content.
		stream("/Type /EmbeddedFile ", dataText),
		stream("/Type /EmbeddedFile ", clipText),
	}

	var b bytes.Buffer
	// The header, and a comment of bytes past 127 that marks the file binary.
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
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

// stream returns a stream object of content, uncompressed, its dictionary dict and its length.
func stream(dict, content string) string {
	return fmt.Sprintf("<< %s/Length %d >>\nstream\n%sendstream", dict, len(content), content)
}
