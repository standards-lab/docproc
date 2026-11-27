//go:build ignore

// generate writes info.pdf beside this file, which document/testdata/README.md describes. Run it
// from document/testdata/info with `mise exec -- go run generate.go`; the check never runs it, and
// reads the committed file.
package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

const headerMarkedBinary = "%PDF-1.6\n%\xe2\xe3\xcf\xd3\n"

func main() {
	if err := os.WriteFile("info.pdf", build(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

const page = "BT /Helv 16 Tf 72 720 Td (Harbor survey) Tj ET\n"

const packet = `<?xpacket begin="` + "\uFEFF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/">
   <dc:title><rdf:Alt>
    <rdf:li xml:lang="de">Hafenvermessung</rdf:li>
    <rdf:li xml:lang="x-default">Harbor survey</rdf:li>
   </rdf:Alt></dc:title>
   <dc:creator><rdf:Seq>
    <rdf:li>Ada Lovelace</rdf:li>
    <rdf:li>Charles Babbage</rdf:li>
   </rdf:Seq></dc:creator>
   <dc:description><rdf:Alt>
    <rdf:li xml:lang="x-default">Soundings of the inner harbor, Zürich edition</rdf:li>
   </rdf:Alt></dc:description>
   <dc:subject><rdf:Bag>
    <rdf:li>harbor</rdf:li>
    <rdf:li>survey</rdf:li>
   </rdf:Bag></dc:subject>
   <dc:language><rdf:Bag>
    <rdf:li>en</rdf:li>
    <rdf:li>de</rdf:li>
   </rdf:Bag></dc:language>
   <xmp:CreateDate>2026-03-15T09:30:00+05:30</xmp:CreateDate>
   <xmp:ModifyDate>2026-04-01T12:00:00Z</xmp:ModifyDate>
   <xmp:CreatorTool>generate.go</xmp:CreatorTool>
  </rdf:Description>
  <rdf:Description rdf:about=""
    xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
    xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
    pdf:Producer="docproc fixtures"
    pdf:Keywords="harbor, survey"
    xmpMM:DocumentID="uuid:6f1c2a3e-4b5d-4e6f-8a9b-0c1d2e3f4a5b"/>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="r"?>`

func utf16(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, r := range s {
		fmt.Fprintf(&b, "%04X", r)
	}
	b.WriteString(">")
	return b.String()
}

func build() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /Helv 6 0 R >> >> >>",
		stream("", page),
		stream("/Type /Metadata /Subtype /XML ", packet),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Title " + utf16("Harbor survey, Zürich") + " /Author (Ada Lovelace) /Subject (Soundings) " +
			"/Keywords (harbor, survey) /Creator (generate.go) /Producer (docproc fixtures) " +
			"/CreationDate (D:20260315093000+05'30') /ModDate (D:20260401120000Z) /Trapped /False " +
			"/Project (docproc) /Reviewed (D:20260402) /Pages 3 >>",
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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func stream(dict, content string) string {
	return fmt.Sprintf("<< %s/Length %d >>\nstream\n%sendstream", dict, len(content), content)
}
