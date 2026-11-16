//go:build linux

package document_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

// standinDetacher links the stand-in pdfdetach at version and builds a Detacher over it,
// returning the stand-in.
func standinDetacher(t *testing.T, version string) (*document.Detacher, *standin) {
	t.Helper()
	st := newStandin(t, "pdfdetach", version)
	d, err := document.NewDetacher(document.Config{PDFDetach: st.mustTool(document.PDFDetachSpec())})
	if err != nil {
		t.Fatal(err)
	}
	return d, st
}

// TestNewDetacher: pdfdetach's version, printed by -v, which exits 99, is read; a nil PDFDetach
// panics naming the field, and a pdfdetach older than 22.02, whatever its Spec's minimum, is
// refused with a *tool.VersionError wrapping tool.ErrTooOld.
func TestNewDetacher(t *testing.T) {
	t.Parallel()
	wantPanic(t, "document: Config.PDFDetach is nil", func() { _, _ = document.NewDetacher(document.Config{}) })
	spec := document.PDFDetachSpec()
	spec.Minimum = "1"
	tl, err := newStandin(t, "pdfdetach", "22.01.0").tool(spec)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Version().String() != "22.1.0" {
		t.Errorf("version %s, want 22.1.0", tl.Version())
	}
	if _, err := document.NewDetacher(document.Config{PDFDetach: tl}); !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewDetacher over 22.01.0: %v, want ErrTooOld", err)
	}
	d, _ := standinDetacher(t, "26.10.0")
	if d == nil {
		t.Error("no Detacher over 26.10.0")
	}
}

// TestList: pdfdetach -list's count and numbered names read as attachments, in order, in UTF-8;
// a name with a line break in it is read whole; a listing whose count its lines don't match, or
// that begins with no count, fails, and so does a failed run, naming pdfdetach's message.
func TestList(t *testing.T) {
	t.Parallel()
	d, st := standinDetacher(t, "26.10.0")
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	st.set(func(s *settings) {
		s.Stdout = new([]byte("3 embedded files\n1: KSBASE.WQ2\n2: two\nlines.txt\n3: ../../etc/passwd\n"))
	})
	got, err := d.List(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	want := []document.Attachment{{1, "KSBASE.WQ2"}, {2, "two\nlines.txt"}, {3, "../../etc/passwd"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list %+v, want %+v", got, want)
	}
	if r := st.runs()[0]; !slices.Equal(r.Args, []string{"-list", "-enc", "UTF-8", pdf}) || r.Dir != filepath.Dir(pdf) {
		t.Errorf("run %q in %s", r.Args, r.Dir)
	}
	st.set(func(s *settings) { s.Stdout = new([]byte("0 embedded files\n")) })
	if got, err := d.List(t.Context(), pdf); err != nil || len(got) != 0 {
		t.Errorf("none: %+v, %v", got, err)
	}
	for _, out := range []string{"2 embedded files\n1: a\n", "nothing\n", "1 embedded files\nstray\n1: a\n"} {
		st.set(func(s *settings) { s.Stdout = new([]byte(out)) })
		if _, err := d.List(t.Context(), pdf); err == nil {
			t.Errorf("listing %q was read", out)
		}
	}
	st.set(func(s *settings) { s.Stdout = new([]byte("")) })
	st.set(func(s *settings) { s.Stderr = "Command Line Error: Incorrect password\n" })
	st.set(func(s *settings) { s.Exit = 1 })
	if _, err := d.List(t.Context(), pdf); err == nil || !strings.Contains(err.Error(), "Incorrect password") {
		t.Errorf("a failed run: %v", err)
	}
}

// TestSave: a file is saved to /dev/stdout, never to its stored name, and its bytes reach the
// writer; a number pdfdetach refuses, or one below 1, is ErrNoAttachment.
func TestSave(t *testing.T) {
	t.Parallel()
	d, st := standinDetacher(t, "26.10.0")
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	st.set(func(s *settings) { s.Stdout = new([]byte("the attachment's bytes")) })
	var b bytes.Buffer
	if err := d.Save(t.Context(), pdf, 2, &b); err != nil {
		t.Fatal(err)
	}
	if b.String() != "the attachment's bytes" {
		t.Errorf("saved %q", b.String())
	}
	if r := st.runs()[0]; !slices.Equal(r.Args, []string{"-save", "2", "-o", "/dev/stdout", pdf}) {
		t.Errorf("args %q", r.Args)
	}
	st.set(func(s *settings) { s.Stdout = new([]byte("")) })
	st.set(func(s *settings) { s.Stderr = "Command Line Error: Invalid file number\n" })
	st.set(func(s *settings) { s.Exit = 99 })
	if err := d.Save(t.Context(), pdf, 9, &b); !errors.Is(err, document.ErrNoAttachment) {
		t.Errorf("number 9: %v, want ErrNoAttachment", err)
	}
	if err := d.Save(t.Context(), pdf, 0, &b); !errors.Is(err, document.ErrNoAttachment) {
		t.Errorf("number 0: %v, want ErrNoAttachment", err)
	}
}
