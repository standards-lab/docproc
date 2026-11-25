package inspect_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

func storedZIP(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateHeader(&zip.FileHeader{Name: "payload.txt", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("carried\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func content(t *testing.T, data []byte) inspect.Report {
	t.Helper()
	r, err := inspect.Content(t.Context(), bytes.NewReader(data), int64(len(data)), route.Identify(data), inspect.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAppendedZIPInText(t *testing.T) {
	archive := storedZIP(t)
	for _, tc := range []struct {
		name   string
		head   []byte
		kind   route.Kind
		checks []inspect.Check
		within string
	}{
		{"shebang", []byte("#!/bin/bash\necho hello\nexit 0\n"), route.Shebang, []inspect.Check{inspect.Executable, inspect.Polyglot}, "a shebang"},
		{"no signature", []byte{0x13, 0x37, 0x00, 0xc4, 0x9e, 0x01, 0xfe, 0x42}, route.Unknown, []inspect.Check{inspect.Polyglot}, "content of no signature"},
		{"utf-8 bom", []byte("\xef\xbb\xbfNotes, as written.\n"), route.UTF8BOM, []inspect.Check{inspect.Polyglot}, "a utf-8 bom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append(append([]byte{}, tc.head...), archive...)
			r := content(t, data)
			if r.Kind != tc.kind {
				t.Fatalf("kind %q, want %q", r.Kind, tc.kind)
			}
			if got := checks(r); !slices.Equal(got, tc.checks) || r.Verdict != inspect.Proceed {
				t.Fatalf("%+v, want checks %v that proceed", r, tc.checks)
			}
			want := fmt.Sprintf("ZIP end-of-central-directory record at offset %d in %s", len(data)-22, tc.within)
			if e := evidence(r, inspect.Polyglot); e != want {
				t.Errorf("evidence %q, want %q", e, want)
			}
		})
	}
}

func TestQuotedSignaturesInText(t *testing.T) {
	for name, data := range map[string][]byte{
		"shebang quoting a PDF header": []byte("#!/bin/sh\nhead -c 5 report.pdf # prints %PDF-\n"),
		"prose quoting a PDF header":   []byte("A PDF file opens with the bytes %PDF-1.7 and then its objects.\n"),
		"prose quoting a ZIP end record": []byte("A ZIP archive ends with PK\x05\x06, the end-of-central-directory " +
			"signature, followed by the record's fields and its comment, as the specification says.\n"),
	} {
		if r := content(t, data); has(r, inspect.Polyglot) {
			t.Errorf("%s: %+v, want no polyglot flag", name, r)
		}
	}
}
