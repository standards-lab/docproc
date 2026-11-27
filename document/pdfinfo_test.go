//go:build linux

package document_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/tool"
)

func standinInfoReader(t *testing.T, version string) (*document.InfoReader, *standin) {
	t.Helper()
	st := newStandin(t, "pdfinfo", version)
	r, err := document.NewInfoReader(document.Config{PDFInfo: st.mustTool(document.PDFInfoSpec())})
	if err != nil {
		t.Fatal(err)
	}
	return r, st
}

func TestNewInfoReader(t *testing.T) {
	t.Parallel()
	wantPanic(t, "document: Config.PDFInfo is nil", func() { _, _ = document.NewInfoReader(document.Config{}) })
	spec := document.PDFInfoSpec()
	spec.Minimum = "1"
	tl, err := newStandin(t, "pdfinfo", "22.01.0").tool(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.NewInfoReader(document.Config{PDFInfo: tl}); !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewInfoReader over 22.01.0: %v, want ErrTooOld", err)
	}
}

func pdfinfoGoldens(t *testing.T, name string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for mode, ext := range map[string]string{"custom": "custom.txt", "meta": "meta.xml", "summary": "summary.txt"} {
		b, err := os.ReadFile(filepath.Join("testdata", "info", name+"."+ext))
		if err != nil {
			t.Fatal(err)
		}
		out[mode] = string(b)
	}
	return out
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func wantInfoFixture(t *testing.T, got metadata.PDF, packet string) {
	t.Helper()
	wantInfo := &metadata.PDFInfo{
		Title: "Harbor survey, Zürich", Author: "Ada Lovelace", Subject: "Soundings", Keywords: "harbor, survey",
		Creator: "generate.go", Producer: "docproc fixtures",
		CreationDate: utc("2026-03-15T04:00:00Z"), ModDate: utc("2026-04-01T12:00:00Z"),
		Custom: metadata.Properties{{Key: "Project", Value: "docproc"}, {Key: "Reviewed", Value: "D:20260402"}},
	}
	wantXMP := &metadata.XMP{
		Title: "Harbor survey", Creators: []string{"Ada Lovelace", "Charles Babbage"},
		Description: "Soundings of the inner harbor, Zürich edition", Subjects: []string{"harbor", "survey"},
		Languages: []string{"en", "de"}, CreateDate: utc("2026-03-15T04:00:00Z"), ModifyDate: utc("2026-04-01T12:00:00Z"),
		CreatorTool: "generate.go", Producer: "docproc fixtures", Keywords: "harbor, survey",
		DocumentID: "uuid:6f1c2a3e-4b5d-4e6f-8a9b-0c1d2e3f4a5b", Packet: packet,
	}
	if got.Version != "1.6" {
		t.Errorf("version %q, want 1.6", got.Version)
	}
	if !reflect.DeepEqual(got.Info, wantInfo) {
		t.Errorf("info\n %+v\nwant\n %+v", got.Info, wantInfo)
	}
	if !reflect.DeepEqual(got.XMP, wantXMP) {
		t.Errorf("xmp\n %+v\nwant\n %+v", got.XMP, wantXMP)
	}
	for _, d := range []time.Time{got.Info.CreationDate, got.Info.ModDate, got.XMP.CreateDate, got.XMP.ModifyDate} {
		if d.Location() != time.UTC {
			t.Errorf("%v isn't in UTC", d)
		}
	}
}

func TestInfoReaderMetadata(t *testing.T) {
	t.Parallel()
	r, st := standinInfoReader(t, "26.10.0")
	goldens := pdfinfoGoldens(t, "info")
	st.set(func(s *settings) { s.Info = goldens })
	pdf := filepath.Join("testdata", "info", "info.pdf")
	got, err := r.Metadata(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	wantInfoFixture(t, got, goldens["meta"])

	abs, err := filepath.Abs(pdf)
	if err != nil {
		t.Fatal(err)
	}
	want := []standinRun{
		{Dir: filepath.Dir(abs), Args: []string{"-isodates", "-enc", "UTF-8", "-custom", abs}},
		{Dir: filepath.Dir(abs), Args: []string{"-meta", abs}},
		{Dir: filepath.Dir(abs), Args: []string{"-enc", "UTF-8", abs}},
	}
	if runs := st.runs(); !reflect.DeepEqual(runs, want) {
		t.Errorf("runs %+v, want %+v", runs, want)
	}
}

func TestInfoReaderClean(t *testing.T) {
	t.Parallel()
	r, st := standinInfoReader(t, "26.10.0")
	goldens := pdfinfoGoldens(t, "clean")
	st.set(func(s *settings) { s.Info = goldens })
	got, err := r.Metadata(t.Context(), filepath.Join("testdata", "clean.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	want := metadata.PDF{
		Version: "1.7",
		Info:    &metadata.PDFInfo{Title: "document fixtures", Creator: "Typst 0.15.1"},
		XMP: &metadata.XMP{
			Title: "document fixtures", Languages: []string{"en"}, CreatorTool: "Typst 0.15.1",
			DocumentID: "eT6gjrd4sCliyxkT8QXOGA==", Packet: goldens["meta"],
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata\n %+v\nwant\n %+v", got, want)
	}
}

func TestInfoReaderLines(t *testing.T) {
	t.Parallel()
	r, st := standinInfoReader(t, "26.10.0")
	custom := strings.Join([]string{
		"A Spaced:Key:    v",
		"CreationDate:    garbage",
		"Lead:               lead",
		"ModDate:         2026-03-15T09:30:00",
		"Subject:         ",
		"VeryLongCustomKeyNameHere:x",
		"Title:           Line one?Line two",
		"",
	}, "\n")
	st.set(func(s *settings) { s.Info = map[string]string{"custom": custom, "summary": "Pages:           1\n"} })
	got, err := r.Metadata(t.Context(), filepath.Join(t.TempDir(), "x.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	want := metadata.PDF{Info: &metadata.PDFInfo{
		Title: "Line one?Line two", ModDate: utc("2026-03-15T09:30:00Z"),
		Custom: metadata.Properties{
			{Key: "A Spaced:Key", Value: "v"}, {Key: "CreationDate", Value: "garbage"},
			{Key: "Lead", Value: "   lead"}, {Key: "VeryLongCustomKeyNameHere", Value: "x"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata\n %+v\nwant\n %+v", got, want)
	}
}

func TestInfoReaderNone(t *testing.T) {
	t.Parallel()
	r, st := standinInfoReader(t, "26.10.0")
	st.set(func(s *settings) { s.Info = map[string]string{"summary": "Pages:           1\nPDF version:     1.4\n"} })
	got, err := r.Metadata(t.Context(), filepath.Join(t.TempDir(), "x.pdf"))
	if err != nil || !reflect.DeepEqual(got, metadata.PDF{Version: "1.4"}) {
		t.Errorf("metadata %+v, %v; want the version alone", got, err)
	}
}

func TestInfoReaderFails(t *testing.T) {
	t.Parallel()
	r, st := standinInfoReader(t, "26.10.0")
	st.set(func(s *settings) {
		s.Exit = 1
		s.Stderr = "Command Line Error: Incorrect password\n"
	})
	_, err := r.Metadata(t.Context(), filepath.Join(t.TempDir(), "locked.pdf"))
	exit, ok := errors.AsType[*tool.ExitError](err)
	if !ok || exit.Code != 1 || !strings.Contains(err.Error(), "-custom locked.pdf: Command Line Error: Incorrect password") {
		t.Errorf("error %v, want pdfinfo's exit 1 naming its message", err)
	}
}

func TestInfoReaderXMPUnparsed(t *testing.T) {
	t.Parallel()
	r, st := standinInfoReader(t, "26.10.0")
	const packet = "<x:xmpmeta><rdf:RDF>"
	st.set(func(s *settings) { s.Info = map[string]string{"custom": "Title:           T\n", "meta": packet} })
	got, err := r.Metadata(t.Context(), filepath.Join(t.TempDir(), "x.pdf"))
	if !errors.Is(err, document.ErrXMP) {
		t.Errorf("error %v, want ErrXMP", err)
	}
	want := metadata.PDF{Info: &metadata.PDFInfo{Title: "T"}, XMP: &metadata.XMP{Packet: packet}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata %+v, want %+v", got, want)
	}
}

func TestParseXMP(t *testing.T) {
	t.Parallel()
	const head = `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" ` +
		`xmlns:pdf="http://ns.adobe.com/pdf/1.3/" xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/">`
	const tail = `</rdf:RDF></x:xmpmeta>`
	for _, c := range []struct {
		name string
		body string
		want metadata.XMP
	}{
		{
			"attributes",
			`<rdf:Description rdf:about="" xmp:CreatorTool="Tool" pdf:Producer="Prod" pdf:Keywords="a, b" ` +
				`xmpMM:DocumentID="id-1" xmp:CreateDate="2026-01-02T03:04:05-02:00"/>`,
			metadata.XMP{CreatorTool: "Tool", Producer: "Prod", Keywords: "a, b", DocumentID: "id-1", CreateDate: utc("2026-01-02T05:04:05Z")},
		},
		{
			"an alternative with no x-default reads its first",
			`<rdf:Description><dc:title><rdf:Alt><rdf:li xml:lang="fr">Port</rdf:li><rdf:li xml:lang="de">Hafen</rdf:li></rdf:Alt></dc:title></rdf:Description>`,
			metadata.XMP{Title: "Port"},
		},
		{
			"empty values are absent",
			`<rdf:Description><dc:creator><rdf:Seq><rdf:li></rdf:li><rdf:li> B </rdf:li></rdf:Seq></dc:creator>` +
				`<dc:subject><rdf:Bag/></dc:subject><xmp:CreatorTool>  </xmp:CreatorTool></rdf:Description>`,
			metadata.XMP{Creators: []string{"B"}},
		},
		{
			"dates of a year, a month, a minute, and no zone",
			`<rdf:Description><xmp:CreateDate>2026</xmp:CreateDate><xmp:ModifyDate>2026-02-03T04:05</xmp:ModifyDate></rdf:Description>`,
			metadata.XMP{CreateDate: utc("2026-01-01T00:00:00Z"), ModifyDate: utc("2026-02-03T04:05:00Z")},
		},
		{
			"a date that isn't one is absent",
			`<rdf:Description><xmp:CreateDate>yesterday</xmp:CreateDate><xmp:ModifyDate>2026-07</xmp:ModifyDate></rdf:Description>`,
			metadata.XMP{ModifyDate: utc("2026-07-01T00:00:00Z")},
		},
		{
			"descriptions merge, properties of other schemas ignored",
			`<rdf:Description><dc:description><rdf:Alt><rdf:li xml:lang="x-default">D</rdf:li></rdf:Alt></dc:description></rdf:Description>` +
				`<rdf:Description xmlns:o="urn:other"><o:title>not dc</o:title><dc:language><rdf:Bag><rdf:li>en</rdf:li></rdf:Bag></dc:language></rdf:Description>`,
			metadata.XMP{Description: "D", Languages: []string{"en"}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			packet := head + c.body + tail
			got, err := document.ParseXMP([]byte(packet))
			if err != nil {
				t.Fatal(err)
			}
			c.want.Packet = packet
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("got\n %+v\nwant\n %+v", *got, c.want)
			}
		})
	}
}

func TestParseXMPInvalidUTF8(t *testing.T) {
	t.Parallel()
	got, err := document.ParseXMP([]byte("<a>\xff</a>"))
	if err == nil {
		t.Errorf("no error, read %+v", got)
	}
	if got.Packet != "<a>�</a>" || got.Creators != nil {
		t.Errorf("packet %q, want its byte replaced", got.Packet)
	}
}
