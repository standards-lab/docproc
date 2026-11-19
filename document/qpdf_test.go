//go:build linux

package document_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

// formFixture returns the path of one of the forms fixtures, under testdata/forms.
func formFixture(name string) string { return filepath.Join("testdata", "forms", name) }

// acroformFields are the fields of the generated acroform.pdf, as the generator's doc lists
// them, in the AcroForm's order: what Form reads from qpdf's JSON of it.
var acroformFields = []document.FormField{
	{Name: "applicant.name", Type: document.TextField, Value: "Ada Lovelace"},
	{Name: "applicant.city", Type: document.TextField, Value: "Zürich"},
	{Name: "subscribe", Type: document.CheckboxField, Value: "Yes", Checked: new(true)},
	{Name: "newsletter", Type: document.CheckboxField, Value: "Off", Checked: new(false)},
	{Name: "contact", Type: document.RadioField, Value: "Phone", Checked: new(true)},
	{
		Name: "colour", Type: document.ChoiceField, Value: "Green",
		Options: []string{"Red", "Green", "Blue"}, Selected: []string{"Green"},
	},
	{
		Name: "languages", Type: document.ChoiceField,
		Options: []string{"English", "French", "German"}, Selected: []string{"en", "de"},
	},
}

// standinQPDF links the stand-in qpdf at version and builds a QPDF over it, returning the
// stand-in.
func standinQPDF(t *testing.T, version string) (*document.QPDF, *standin) {
	t.Helper()
	st := newStandin(t, "qpdf", version)
	q, err := document.NewQPDF(document.Config{QPDF: st.mustTool(document.QPDFSpec())})
	if err != nil {
		t.Fatal(err)
	}
	return q, st
}

// TestFixtureSums holds the forms, attachments, and portfolio fixtures to the SHA-256 the testdata README gives them.
func TestFixtureSums(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("testdata", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"forms/acroform.pdf", "forms/acroform.json", "forms/signed.pdf", "forms/signed.json", "attachments/attachments.pdf", "portfolio/portfolio.pdf"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if line := hex.EncodeToString(sum[:]) + "  " + name; !bytes.Contains(readme, []byte(line)) {
			t.Errorf("the testdata README has no line %q", line)
		}
	}
}

// TestNewQPDF: a nil QPDF panics naming the field, and a qpdf older than 11.0, whatever its
// Spec's minimum, is refused with a *tool.VersionError wrapping tool.ErrTooOld; qpdf's version
// line gives its version.
func TestNewQPDF(t *testing.T) {
	t.Parallel()
	wantPanic(t, "document: Config.QPDF is nil", func() { _, _ = document.NewQPDF(document.Config{}) })
	spec := document.QPDFSpec()
	spec.Minimum = "1"
	tl, err := newStandin(t, "qpdf", "10.6.3").tool(spec)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Version().String() != "10.6.3" {
		t.Errorf("version %s, want 10.6.3", tl.Version())
	}
	if _, err := document.NewQPDF(document.Config{QPDF: tl}); !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewQPDF over 10.6.3: %v, want ErrTooOld", err)
	}
}

// TestRequiresPassword: qpdf --requires-password's exit code is the answer, 0 a password needed,
// 2 not encrypted and 3 encrypted but opening without one, warnings aside; a run exiting 2 with
// qpdf's error, as it does for a file it can't open, fails with it, and any other exit fails.
// The PDF is named by its absolute path, in its own directory.
func TestRequiresPassword(t *testing.T) {
	t.Parallel()
	q, st := standinQPDF(t, "12.4.2")
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	for _, c := range []struct {
		exit   int
		stderr string
		want   bool
		fails  string
	}{
		{exit: 0, want: true},
		{exit: 2},
		{exit: 2, stderr: "WARNING: x.pdf: file is damaged\nWARNING: x.pdf: Attempting to reconstruct cross-reference table\n"},
		{exit: 3},
		{exit: 2, stderr: "WARNING: x.pdf: can't find PDF header\nqpdf: x.pdf: unable to find trailer dictionary while recovering damaged file\n", fails: "unable to find trailer dictionary"},
		{exit: 1, stderr: "qpdf: something else\n", fails: "something else"},
	} {
		st.set(func(s *settings) { s.Exit = c.exit })
		st.set(func(s *settings) { s.Stderr = c.stderr })
		got, err := q.RequiresPassword(t.Context(), pdf)
		switch {
		case c.fails != "":
			if err == nil || !strings.Contains(err.Error(), c.fails) {
				t.Errorf("exit %d, %q: %v, want an error naming %q", c.exit, c.stderr, err, c.fails)
			}
		case err != nil || got != c.want:
			t.Errorf("exit %d, %q: %t, %v; want %t", c.exit, c.stderr, got, err, c.want)
		}
	}
	last := st.runs()[len(st.runs())-1]
	if !slices.Equal(last.Args, []string{"--requires-password", pdf}) || last.Dir != filepath.Dir(pdf) {
		t.Errorf("run %q in %s", last.Args, last.Dir)
	}
}

// TestForm: qpdf's JSON of the generated acroform.pdf, recorded beside it, reads as the
// generator's fields exactly, a radio group's two widgets as one field; a run ending in warnings,
// exit 3, has succeeded; a PDF with no AcroForm has no fields; and a run that fails, or JSON that
// isn't version 2, fails.
func TestForm(t *testing.T) {
	t.Parallel()
	q, st := standinQPDF(t, "12.4.2")
	b, err := os.ReadFile(formFixture("acroform.json"))
	if err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	for _, exit := range []int{0, 3} {
		st.set(func(s *settings) { s.Stdout = new(b) })
		st.set(func(s *settings) { s.Exit = exit })
		f, err := q.Form(t.Context(), pdf)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.Fields, acroformFields) {
			t.Errorf("exit %d: fields\n got %+v\nwant %+v", exit, f.Fields, acroformFields)
		}
	}
	if args := st.runs()[0].Args; !slices.Equal(args, []string{"--json=2", "--json-key=acroform", pdf}) {
		t.Errorf("args %q", args)
	}

	st.set(func(s *settings) { s.Exit = 0 })
	st.set(func(s *settings) {
		s.Stdout = new([]byte(`{"version": 2, "acroform": {"fields": [], "hasacroform": false, "needappearances": false}}`))
	})
	if f, err := q.Form(t.Context(), pdf); err != nil || f.Fields == nil || len(f.Fields) != 0 {
		t.Errorf("no AcroForm: %+v, %v; want no fields", f, err)
	}
	st.set(func(s *settings) { s.Stdout = new([]byte(`{"version": 1, "acroform": {}}`)) })
	if _, err := q.Form(t.Context(), pdf); err == nil || !strings.Contains(err.Error(), "version 1") {
		t.Errorf("JSON version 1: %v", err)
	}
	st.set(func(s *settings) { s.Stdout = new([]byte("")) })
	st.set(func(s *settings) { s.Exit = 2 })
	st.set(func(s *settings) { s.Stderr = "qpdf: x.pdf: invalid password\n" })
	if _, err := q.Form(t.Context(), pdf); err == nil || !strings.Contains(err.Error(), "invalid password") {
		t.Errorf("a failed run: %v", err)
	}
}

// TestFormValues: qpdf's JSON version 2 encodings read as text: a binary string's bytes, a name's
// text with its slash or "n:/" taken off; a pushbutton, a signature, and a field of a type none
// names are typed so; and a field's value never fails the form: a number, a dictionary, a
// reference, a string of no encoding Form knows, a binary string whose hex doesn't decode, and an
// array's entries of those are no value, the field kept.
func TestFormValues(t *testing.T) {
	t.Parallel()
	q, st := standinQPDF(t, "12.4.2")
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	st.set(func(s *settings) {
		s.Stdout = new([]byte(`{"version": 2, "acroform": {"hasacroform": true, "fields": [
		{"fullname": "bin", "fieldtype": "/Tx", "istext": true, "value": "b:48c3a9ff"},
		{"fullname": "box", "fieldtype": "/Btn", "ischeckbox": true, "value": "n:/On"},
		{"fullname": "push", "fieldtype": "/Btn", "fieldflags": 65536, "value": null},
		{"fullname": "sig", "fieldtype": "/Sig", "value": null},
		{"fullname": "odd", "fieldtype": "/Xx", "value": "u:x"},
		{"fullname": "number", "fieldtype": "/Tx", "istext": true, "value": 7},
		{"fullname": "dict", "fieldtype": "/Tx", "istext": true, "value": {"/A": 1}},
		{"fullname": "ref", "fieldtype": "/Tx", "istext": true, "value": "3 0 R"},
		{"fullname": "badhex", "fieldtype": "/Tx", "istext": true, "value": "b:zz"},
		{"fullname": "list", "fieldtype": "/Ch", "ischoice": true, "value": ["u:a", 2, "4 0 R", "/b"]}
	]}}`))
	})
	f, err := q.Form(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	want := []document.FormField{
		{Name: "bin", Type: document.TextField, Value: "Hé\uFFFD"},
		{Name: "box", Type: document.CheckboxField, Value: "On", Checked: new(true)},
		{Name: "push", Type: document.PushbuttonField},
		{Name: "sig", Type: document.SignatureField, Signed: new(false)},
		{Name: "odd", Type: "Xx", Value: "x"},
		{Name: "number", Type: document.TextField},
		{Name: "dict", Type: document.TextField},
		{Name: "ref", Type: document.TextField},
		{Name: "badhex", Type: document.TextField},
		{Name: "list", Type: document.ChoiceField, Selected: []string{"a", "b"}},
	}
	if !reflect.DeepEqual(f.Fields, want) {
		t.Errorf("fields\n got %+v\nwant %+v", f.Fields, want)
	}
	st.set(func(s *settings) { s.Stdout = new([]byte(`{"version": 2, "acroform": {`)) })
	if _, err := q.Form(t.Context(), pdf); err == nil {
		t.Error("JSON that doesn't parse was read")
	}
}

// signedFields are the fields of signed.pdf, a page of a signed US government PDF: its one
// signature field, signed, its value the reference "12 0 R" to its signature dictionary.
var signedFields = []document.FormField{
	{Name: "USGPOSignature", Type: document.SignatureField, Signed: new(true)},
}

// TestFormSigned: qpdf's JSON of signed.pdf, recorded beside it verbatim, reads as its one signed
// signature field, whose value, a reference, fails nothing.
func TestFormSigned(t *testing.T) {
	t.Parallel()
	q, st := standinQPDF(t, "12.4.2")
	b, err := os.ReadFile(formFixture("signed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"fieldtype": "/Sig"`)) || !bytes.Contains(b, []byte(`"value": "12 0 R"`)) {
		t.Fatal("signed.json holds no /Sig field of value 12 0 R")
	}
	// qpdf exits 3 on this PDF, its warnings no failure.
	st.set(func(s *settings) { s.Stdout = new(b) })
	st.set(func(s *settings) { s.Exit = 3 })
	f, err := q.Form(t.Context(), filepath.Join(t.TempDir(), "signed.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Fields, signedFields) {
		t.Errorf("fields\n got %+v\nwant %+v", f.Fields, signedFields)
	}
}
