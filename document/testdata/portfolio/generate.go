//go:build ignore

// generate writes portfolio.pdf beside this file: a PDF portfolio, a two-page cover document
// whose catalog has a /Collection, embedding two PDFs of two pages each, written by hand, every
// byte fixed, so a rerun writes the same bytes. Run it from document/testdata/portfolio with
// `mise exec -- go run generate.go`; the check never runs it, and reads the committed file.
//
// gs renders a portfolio named on its command line as its embedded documents in place of its own
// pages, each of them through the run's page range, so such a run for the cover's page 1 writes
// both documents' page 1, two PNGs, and a page list of 1 and 2 writes four. A page's own run, the
// document package's Render and Text, reads it through the non-portfolio half of gs's runpdf, and
// writes the cover's page alone.
package main

import (
	"bytes"
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("portfolio.pdf", portfolio(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

// portfolio returns the portfolio: the cover's catalog, with its /Collection and its name tree
// of the two embedded documents, its page tree and two pages, and the documents' file
// specifications and contents.
func portfolio() []byte {
	first, second := document("Harbor survey"), document("Tide tables")
	return pdf([]string{
		// 1: the catalog.
		"<< /Type /Catalog /Pages 2 0 R /Collection << /Type /Collection /View /D >> " +
			"/Names << /EmbeddedFiles << /Names [(harbor.pdf) 7 0 R (tides.pdf) 8 0 R] >> >> >>",
		// 2: the page tree, 3 and 4 its pages, 5 and 6 their contents.
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		page(5, 11), page(6, 11),
		stream("", text("Portfolio cover, page 1")),
		stream("", text("Portfolio cover, page 2")),
		// 7 and 8: the documents' file specifications, 9 and 10 their contents.
		"<< /Type /Filespec /F (harbor.pdf) /UF (harbor.pdf) /EF << /F 9 0 R >> >>",
		"<< /Type /Filespec /F (tides.pdf) /UF (tides.pdf) /EF << /F 10 0 R >> >>",
		stream("/Type /EmbeddedFile /Subtype /application#2Fpdf ", string(first)),
		stream("/Type /EmbeddedFile /Subtype /application#2Fpdf ", string(second)),
		// 11: the font.
		font,
	})
}

// document returns an embedded document: its catalog, its page tree and two pages, their
// contents, each a line naming title and the page, and its font.
func document(title string) []byte {
	return pdf([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		page(5, 7), page(6, 7),
		stream("", text(title+", page 1")),
		stream("", text(title+", page 2")),
		font,
	})
}

// font is the Helvetica every page writes in.
const font = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"

// page returns a letter page of the page tree, object 2, its content the object contents and its
// font the object font.
func page(contents, font int) string {
	return fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R "+
		"/Resources << /Font << /Helv %d 0 R >> >> >>", contents, font)
}

// text returns a page's content: line, as a heading.
func text(line string) string {
	return fmt.Sprintf("BT /Helv 16 Tf 72 720 Td (%s) Tj ET\n", line)
}

// pdf returns a PDF of objects, numbered from 1 in order, then its cross-reference table and
// trailer.
func pdf(objects []string) []byte {
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
