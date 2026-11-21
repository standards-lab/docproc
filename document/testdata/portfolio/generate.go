//go:build ignore

// generate writes portfolio.pdf beside this file, which document/testdata/README.md describes. Run it from
// document/testdata/portfolio with `mise exec -- go run generate.go`; the check never runs it,
// and reads the committed file.
package main

import (
	"bytes"
	"fmt"
	"os"
)

const headerMarkedBinary = "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"

func main() {
	if err := os.WriteFile("portfolio.pdf", portfolio(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func portfolio() []byte {
	first, second := document("Harbor survey"), document("Tide tables")
	return pdf([]string{
		"<< /Type /Catalog /Pages 2 0 R /Collection << /Type /Collection /View /D >> " +
			"/Names << /EmbeddedFiles << /Names [(harbor.pdf) 7 0 R (tides.pdf) 8 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		page(5, 11), page(6, 11),
		stream("", text("Portfolio cover, page 1")),
		stream("", text("Portfolio cover, page 2")),
		"<< /Type /Filespec /F (harbor.pdf) /UF (harbor.pdf) /EF << /F 9 0 R >> >>",
		"<< /Type /Filespec /F (tides.pdf) /UF (tides.pdf) /EF << /F 10 0 R >> >>",
		stream("/Type /EmbeddedFile /Subtype /application#2Fpdf ", string(first)),
		stream("/Type /EmbeddedFile /Subtype /application#2Fpdf ", string(second)),
		font,
	})
}

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

const font = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"

func page(contents, font int) string {
	return fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R "+
		"/Resources << /Font << /Helv %d 0 R >> >> >>", contents, font)
}

func text(line string) string {
	return fmt.Sprintf("BT /Helv 16 Tf 72 720 Td (%s) Tj ET\n", line)
}

func pdf(objects []string) []byte {
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
