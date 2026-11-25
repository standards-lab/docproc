//go:build linux

package document_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

func standinTextReader(t *testing.T, version string) (*document.TextReader, *standin) {
	t.Helper()
	st := newStandin(t, "pdftotext", version)
	r, err := document.NewTextReader(document.Config{PDFToText: st.mustTool(document.PDFToTextSpec())})
	if err != nil {
		t.Fatal(err)
	}
	return r, st
}

func TestNewTextReader(t *testing.T) {
	t.Parallel()
	wantPanic(t, "document: Config.PDFToText is nil", func() { _, _ = document.NewTextReader(document.Config{}) })
	spec := document.PDFToTextSpec()
	spec.Minimum = "1"
	tl, err := newStandin(t, "pdftotext", "22.01.0").tool(spec)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Version().String() != "22.1.0" {
		t.Errorf("version %s, want 22.1.0", tl.Version())
	}
	_, err = document.NewTextReader(document.Config{PDFToText: tl})
	if _, ok := errors.AsType[*tool.VersionError](err); !ok || !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewTextReader over 22.01.0: %v, want a *tool.VersionError wrapping ErrTooOld", err)
	}
	if r, _ := standinTextReader(t, "26.10.0"); r == nil {
		t.Error("no TextReader over 26.10.0")
	}
}

func TestTextReaderText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		page   int
		stdout string
		want   string
	}{
		{1, "The Harbor Survey\n\nLeft column.\n\nRight column.\n\n\f", "The Harbor Survey\n\nLeft column.\n\nRight column.\n\n"},
		{2, "\f", ""},
		{3, "two pages\f\f", "two pages\f"},
		{4, "no form feed\n", "no form feed\n"},
		{12, "ofÏce\ttoday\n\f", "ofÏce\ttoday\n"},
	} {
		r, st := standinTextReader(t, "26.10.0")
		pdf := filepath.Join(t.TempDir(), "x.pdf")
		st.set(func(s *settings) { s.Stdout = new([]byte(tc.stdout)) })
		got, err := r.Text(t.Context(), pdf, tc.page)
		if err != nil || got != tc.want {
			t.Errorf("Text(page %d) = %q, %v, want %q", tc.page, got, err, tc.want)
		}
		n := strconv.Itoa(tc.page)
		want := []string{"-enc", "UTF-8", "-f", n, "-l", n, pdf, "-"}
		runs := st.runs()
		if len(runs) != 1 || !slices.Equal(runs[0].Args, want) || runs[0].Dir != filepath.Dir(pdf) {
			t.Errorf("Text(page %d) ran pdftotext as %+v, want one run with %q in %s", tc.page, runs, want, filepath.Dir(pdf))
		}
	}
}

func TestTextReaderRefuses(t *testing.T) {
	t.Parallel()
	r, st := standinTextReader(t, "26.10.0")
	for _, page := range []int{0, -1} {
		if _, err := r.Text(t.Context(), "x.pdf", page); !errors.Is(err, document.ErrRequest) {
			t.Errorf("Text(page %d) returned %v, want document.ErrRequest", page, err)
		}
	}
	if got := st.runs(); len(got) != 0 {
		t.Errorf("pdftotext ran %+v, want no run", got)
	}
}

func TestTextReaderPastLast(t *testing.T) {
	t.Parallel()
	r, st := standinTextReader(t, "26.10.0")
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	st.set(func(s *settings) {
		s.Stderr = "Command Line Error: Wrong page range given: the first page (9) can not be after the last page (6).\n"
		s.Exit = 99
	})
	_, err := r.Text(t.Context(), pdf, 9)
	if !errors.Is(err, document.ErrRequest) || !strings.Contains(err.Error(), "page 9 of "+pdf) {
		t.Errorf("Text(page 9) of 6 returned %v, want document.ErrRequest naming page 9 of %s", err, pdf)
	}
}

func TestTextReaderFails(t *testing.T) {
	t.Parallel()
	r, st := standinTextReader(t, "26.10.0")
	pdf := filepath.Join(t.TempDir(), "x.pdf")
	long := "Syntax Error: " + strings.Repeat("x", 5000)
	st.set(func(s *settings) {
		s.Stderr = long
		s.Exit = 1
	})
	_, err := r.Text(t.Context(), pdf, 1)
	exit, ok := errors.AsType[*tool.ExitError](err)
	if !ok || exit.Code != 1 || exit.Message != long[:4<<10] || errors.Is(err, document.ErrRequest) {
		t.Errorf("Text returned %v, want a *tool.ExitError with code 1 and pdftotext's first 4 KiB of stderr", err)
	}
	st.set(func(s *settings) {
		s.Stderr = "I/O Error: Couldn't open file\n"
		s.Exit = 99
	})
	_, err = r.Text(t.Context(), pdf, 1)
	if exit, ok := errors.AsType[*tool.ExitError](err); !ok || exit.Code != 99 || errors.Is(err, document.ErrRequest) {
		t.Errorf("Text over an exit 99 that isn't a page range returned %v, want a *tool.ExitError", err)
	}
}

func TestTextReaderCanceled(t *testing.T) {
	t.Parallel()
	r, _ := standinTextReader(t, "26.10.0")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := r.Text(ctx, filepath.Join(t.TempDir(), "x.pdf"), 1)
	if _, ok := errors.AsType[*tool.ExitError](err); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("Text with an ended context returned %v, want context.Canceled", err)
	}
}
