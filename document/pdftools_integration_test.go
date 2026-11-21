//go:build integration && linux

package document_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
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
