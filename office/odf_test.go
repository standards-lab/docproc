//go:build linux

package office_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/office"
)

const odfType = "application/vnd.oasis.opendocument."

const encryptedManifest = `<?xml version="1.0" encoding="UTF-8"?>
<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">
 <manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>
 <manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml" manifest:size="400">
  <manifest:encryption-data manifest:checksum-type="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#sha256-1k" manifest:checksum="AAAA">
   <manifest:algorithm manifest:algorithm-name="http://www.w3.org/2001/04/xmlenc#aes256-cbc" manifest:initialisation-vector="AAAA"/>
  </manifest:encryption-data>
 </manifest:file-entry>
</manifest:manifest>
`

const plainManifest = `<?xml version="1.0" encoding="UTF-8"?>
<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">
 <manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>
 <manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>
 <encryption-data/>
</manifest:manifest>
`

func odfPackage(t *testing.T, mediaType string, entries ...string) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	raw, err := w.CreateRaw(&zip.FileHeader{
		Name: "mimetype", Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte(mediaType)),
		CompressedSize64: uint64(len(mediaType)), UncompressedSize64: uint64(len(mediaType)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write([]byte(mediaType)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(entries); i += 2 {
		f, err := w.Create(entries[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(entries[i+1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestConvertConverted(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	manifest := []string{"content.xml", "<office:document-content/>", "META-INF/manifest.xml", plainManifest}
	xlsb := "PK\x03\x04 an Excel binary workbook"
	for _, tc := range []struct {
		desc, doc, name, from, to, filter, infilter string
	}{
		{"odt", odfPackage(t, odfType+"text", manifest...), "report.odt", "odt", "docx", "MS Word 2007 XML", ""},
		{"ott", odfPackage(t, odfType+"text-template", manifest...), "letter.ott", "ott", "docx", "MS Word 2007 XML", ""},
		{"odt with no manifest", odfPackage(t, odfType+"text", "content.xml", "<x/>"), "report.odt", "odt", "docx", "MS Word 2007 XML", ""},
		{"odt named .zip", odfPackage(t, odfType+"text", manifest...), "report.zip", "odt", "docx", "MS Word 2007 XML", ""},
		{"ods", odfPackage(t, odfType+"spreadsheet", manifest...), "budget.ods", "ods", "xlsx", "Calc MS Excel 2007 XML", ""},
		{"ots", odfPackage(t, odfType+"spreadsheet-template", manifest...), "budget.ots", "ots", "xlsx", "Calc MS Excel 2007 XML", ""},
		{"odp", odfPackage(t, odfType+"presentation", manifest...), "deck.odp", "odp", "pptx", "Impress MS PowerPoint 2007 XML", ""},
		{"otp", odfPackage(t, odfType+"presentation-template", manifest...), "deck.otp", "otp", "pptx", "Impress MS PowerPoint 2007 XML", ""},
		{"odg", odfPackage(t, odfType+"graphics", manifest...), "plan.odg", "odg", "pptx", "Impress MS PowerPoint 2007 XML", "impress8"},
		{"otg", odfPackage(t, odfType+"graphics-template", manifest...), "plan.otg", "otg", "pptx", "Impress MS PowerPoint 2007 XML", "impress8"},
		{"wpd", "\xffWPC\x10\x00\x00\x00\x01\x0a a WordPerfect document", "memo.wpd", "wpd", "docx", "MS Word 2007 XML", "WordPerfect"},
		{"wpd with no name", "\xffWPC\x10\x00\x00\x00\x01\x0a a WordPerfect document", "", "wpd", "docx", "MS Word 2007 XML", "WordPerfect"},
		{"xlsb", xlsb, "book.xlsb", "xlsb", "xlsx", "Calc MS Excel 2007 XML", ""},
		{"XLSB", xlsb, "BOOK.XLSB", "xlsb", "xlsx", "Calc MS Excel 2007 XML", ""},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			dir := t.TempDir()
			before := len(ins.runs())
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", tc.doc), Dir: dir, Name: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			if got := document(t, recs).ConvertedFrom; got != tc.from {
				t.Errorf("converted from: got %q, want %q", got, tc.from)
			}
			all := ins.runs()[before:]
			if len(all) != 2 || all[0].Program != "soffice" || all[1].Program != "docproc-ooxml" {
				t.Fatalf("runs: got %+v, want soffice, then docproc-ooxml", all)
			}
			so, of := all[0], all[1]
			if so.Input != tc.doc || of.Input != convertedPrefix+tc.doc {
				t.Errorf("inputs: soffice %q, docproc-ooxml %q; want the document, then soffice's output", so.Input, of.Input)
			}
			work := regexp.QuoteMeta(dir) + `/libreoffice-[^/]+`
			want := []string{`-env:UserInstallation=file://` + work + `/profile`, `--headless`, `--norestore`}
			if tc.infilter != "" {
				want = append(want, `--infilter=`+tc.infilter)
			}
			want = append(want, `--convert-to`, regexp.QuoteMeta(tc.to+":"+tc.filter), `--outdir`, work+`/out`, work+`/in/document\.`+tc.from)
			if !matchArgs(so.Args, want) {
				t.Errorf("soffice's arguments: got %q, want %q", so.Args, want)
			}
			if input := of.Args[len(of.Args)-1]; !strings.HasSuffix(input, "/out/document."+tc.to) {
				t.Errorf("docproc-ooxml's input %s, want soffice's %s", input, tc.to)
			}
		})
	}
}

func TestConvertODFUnread(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	encrypted, err := os.ReadFile(filepath.Join("testdata", "odf", "generated", "encrypted.odt"))
	if err != nil {
		t.Fatal(err)
	}
	truncated := odfPackage(t, odfType+"text", "content.xml", strings.Repeat("x", 200))
	for _, tc := range []struct {
		desc, doc string
		want      error
		reason    string
	}{
		{"formula", odfPackage(t, odfType+"formula", "META-INF/manifest.xml", plainManifest), dispatch.ErrNoProcessor, "no processor yet for an ODF package of type " + odfType + "formula"},
		{"chart", odfPackage(t, odfType+"chart"), dispatch.ErrNoProcessor, odfType + "chart"},
		{"database", odfPackage(t, odfType+"base"), dispatch.ErrNoProcessor, odfType + "base"},
		{"image", odfPackage(t, odfType+"image"), dispatch.ErrNoProcessor, odfType + "image"},
		{"encrypted formula", odfPackage(t, odfType+"formula", "META-INF/manifest.xml", encryptedManifest), dispatch.ErrNoProcessor, odfType + "formula"},
		{"encrypted, per entry", odfPackage(t, odfType+"text", "META-INF/manifest.xml", encryptedManifest), dispatch.ErrEncrypted, "an ODF package whose manifest holds encryption data"},
		{"encrypted, LibreOffice's", string(encrypted), dispatch.ErrEncrypted, "an ODF package whose manifest holds encryption data"},
		{"truncated", truncated[:len(truncated)-30], dispatch.ErrRefused, "an unreadable ODF package"},
		{"malformed manifest", odfPackage(t, odfType+"text", "META-INF/manifest.xml", "<manifest:manifest"), dispatch.ErrRefused, "an unreadable ODF package"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			before := len(ins.runs())
			dir := t.TempDir()
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", tc.doc), Dir: dir, Name: "a.odt"})
			if len(recs) != 0 || !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("got %d records, %v; want %v saying %q", len(recs), err, tc.want, tc.reason)
			}
			if runs := ins.runs()[before:]; len(runs) != 0 {
				t.Errorf("runs %+v, want none", runs)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
		})
	}
}
