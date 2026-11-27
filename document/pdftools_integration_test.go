//go:build integration && linux

package document_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

func hostQPDF(t *testing.T) *document.QPDF {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), document.QPDFSpec())
	if err != nil {
		t.Fatal(err)
	}
	q, err := document.NewQPDF(document.Config{QPDF: tl})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func hostDetacher(t *testing.T) *document.Detacher {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), document.PDFDetachSpec())
	if err != nil {
		t.Fatal(err)
	}
	d, err := document.NewDetacher(document.Config{PDFDetach: tl})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func encrypted(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	pdfs := map[string][]string{
		"open-password.pdf":        {"--encrypt", "--user-password=user", "--owner-password=owner", "--bits=256", "--"},
		"open-password-aes128.pdf": {"--encrypt", "--user-password=user", "--owner-password=owner", "--bits=128", "--use-aes=y", "--"},
		"permissions.pdf":          {"--encrypt", "--user-password=", "--owner-password=owner", "--bits=256", "--extract=n", "--modify=none", "--"},
	}
	paths := map[string]string{}
	for name, args := range pdfs {
		out := filepath.Join(dir, name)
		cmd := exec.CommandContext(t.Context(), "qpdf", append(append([]string{filepath.Join("testdata", "clean.pdf")}, args...), out)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("qpdf making %s: %v: %s", name, err, b)
		}
		paths[name] = out
	}
	return paths
}

func TestFormReal(t *testing.T) {
	q := hostQPDF(t)
	for name, want := range map[string][]document.FormField{
		"acroform.pdf": acroformFields,
		"signed.pdf":   signedFields,
	} {
		f, err := q.Form(t.Context(), formFixture(name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(f.Fields, want) {
			t.Errorf("%s's fields\n got %+v\nwant %+v", name, f.Fields, want)
		}
		b, _ := json.Marshal(f)
		t.Logf("%s: %s", name, b)
	}
	clean, err := q.Form(t.Context(), filepath.Join("testdata", "clean.pdf"))
	if err != nil || len(clean.Fields) != 0 {
		t.Errorf("clean.pdf, which has no AcroForm: %+v, %v", clean, err)
	}
}

func TestRequiresPasswordReal(t *testing.T) {
	q := hostQPDF(t)
	pdfs := encrypted(t)
	for name, want := range map[string]bool{
		pdfs["open-password.pdf"]:              true,
		pdfs["open-password-aes128.pdf"]:       true,
		pdfs["permissions.pdf"]:                false,
		filepath.Join("testdata", "clean.pdf"): false,
	} {
		got, err := q.RequiresPassword(t.Context(), name)
		if err != nil || got != want {
			t.Errorf("%s: %t, %v; want %t", filepath.Base(name), got, err, want)
		}
	}
	if _, err := q.RequiresPassword(t.Context(), filepath.Join(t.TempDir(), "missing.pdf")); err == nil {
		t.Error("a missing file was answered")
	}
}

var attachmentsFixture = filepath.Join("testdata", "attachments", "attachments.pdf")

func TestDetacherReal(t *testing.T) {
	d := hostDetacher(t)
	list, err := d.List(t.Context(), attachmentsFixture)
	want := []document.Attachment{{Number: 1, Name: "notes.txt"}, {Number: 2, Name: "data.csv"}}
	if err != nil || !reflect.DeepEqual(list, want) {
		t.Fatalf("List: %+v, %v; want %+v", list, err, want)
	}
	for i, content := range []string{"Embedded through the name tree.\n", "name,count\nharbor,3\n"} {
		var b bytes.Buffer
		if err := d.Save(t.Context(), attachmentsFixture, i+1, &b); err != nil || b.String() != content {
			t.Errorf("Save(%d): %q, %v; want %q", i+1, b.String(), err, content)
		}
	}
	if err := d.Save(t.Context(), attachmentsFixture, 3, &bytes.Buffer{}); !errors.Is(err, document.ErrNoAttachment) {
		t.Errorf("Save(3): %v, want ErrNoAttachment", err)
	}
	list, err = d.List(t.Context(), filepath.Join("testdata", "clean.pdf"))
	if err != nil || len(list) != 0 {
		t.Errorf("clean.pdf: %+v, %v; want none", list, err)
	}
}

func hostTextReader(t *testing.T) *document.TextReader {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), document.PDFToTextSpec())
	if err != nil {
		t.Fatal(err)
	}
	r, err := document.NewTextReader(document.Config{PDFToText: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTextReaderReal(t *testing.T) {
	r := hostTextReader(t)
	pdf := fixtureCopy(t, "clean.pdf")
	truth := truthOf(t, "clean")
	var edits, chars, wedits, words int
	for i := range truth {
		text, err := r.Text(t.Context(), pdf, i+1)
		if err != nil {
			t.Fatalf("Text(clean.pdf page %d): %v", i+1, err)
		}
		if !document.HasText(text) {
			t.Errorf("clean.pdf page %d has no text layer, want one", i+1)
		}
		e, n := charErrors(truth[i], text)
		we, wn := wordErrors(truth[i], text)
		edits, chars, wedits, words = edits+e, chars+n, wedits+we, words+wn
		if found := wordsFound(truth[i], text); found < 0.95 {
			t.Errorf("clean.pdf page %d's text holds %.4f of its truth's words, want nearly all: %q", i+1, found, text)
		}
		if strings.Contains(text, "  ") {
			t.Errorf("clean.pdf page %d holds a run of spaces: %q", i+1, text)
		}
		if strings.HasSuffix(text, "\f") {
			t.Errorf("clean.pdf page %d ends in a form feed", i+1)
		}
		t.Logf("recorded: pdftotext clean.pdf page %d: CER %.4f, WER %.4f, %.4f of the truth's words found, %d bytes",
			i+1, rate(e, n), rate(we, wn), wordsFound(truth[i], text), len(text))
	}
	t.Logf("recorded: pdftotext clean.pdf, %d pages: CER %.4f, WER %.4f", len(truth), rate(edits, chars), rate(wedits, words))
	page2, err := r.Text(t.Context(), pdf, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, want := strings.Fields(page2), strings.Fields(truth[1])
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("clean.pdf page 2's words\n got %q\nwant %q", got, want)
	}
	scanned := fixtureCopy(t, "scanned.pdf")
	for page := 1; page <= 7; page++ {
		text, err := r.Text(t.Context(), scanned, page)
		if err != nil {
			t.Fatalf("Text(scanned.pdf page %d): %v", page, err)
		}
		if document.HasText(text) {
			t.Errorf("scanned.pdf page %d has text layer %q, want none", page, text)
		}
	}
	if _, err := r.Text(t.Context(), pdf, len(truth)+1); !errors.Is(err, document.ErrRequest) {
		t.Errorf("Text(clean.pdf page %d) of %d pages returned %v, want document.ErrRequest", len(truth)+1, len(truth), err)
	}
	permissions := encrypted(t)["permissions.pdf"]
	for i := range truth {
		text, err := r.Text(t.Context(), permissions, i+1)
		if err != nil || !document.HasText(text) {
			t.Errorf("permissions.pdf page %d: %q, %v; want its text", i+1, text, err)
		}
	}
}

func TestInfoReaderReal(t *testing.T) {
	tl, err := tool.NewExec(t.Context(), document.PDFInfoSpec())
	if err != nil {
		t.Fatal(err)
	}
	r, err := document.NewInfoReader(document.Config{PDFInfo: tl})
	if err != nil {
		t.Fatal(err)
	}
	for name, pdf := range map[string]string{
		"info":  filepath.Join("testdata", "info", "info.pdf"),
		"clean": filepath.Join("testdata", "clean.pdf"),
	} {
		got, err := r.Metadata(t.Context(), pdf)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		standin, st := standinInfoReader(t, "26.10.0")
		st.set(func(s *settings) { s.Info = pdfinfoGoldens(t, name) })
		want, err := standin.Metadata(t.Context(), pdf)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the host's pdfinfo read\n %+v\nthe goldens\n %+v", name, got, want)
		}
		if name == "info" {
			wantInfoFixture(t, got, want.XMP.Packet)
		}
	}
	pdfs := encrypted(t)
	if _, err := r.Metadata(t.Context(), pdfs["open-password.pdf"]); err == nil || !strings.Contains(err.Error(), "password") {
		t.Errorf("open-password.pdf: %v, want pdfinfo's password failure", err)
	}
	if got, err := r.Metadata(t.Context(), pdfs["permissions.pdf"]); err != nil || got.Info == nil || got.Info.Title != "document fixtures" {
		t.Errorf("permissions.pdf: %+v, %v; want its information dictionary", got, err)
	}
}
