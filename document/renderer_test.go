//go:build linux

package document_test

import (
	"errors"
	"testing"

	"github.com/standards-lab/docproc/document"
)

func TestCheckRender(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		page, dpi int
		want      error
	}{
		{"dpi zero", 1, 0, document.ErrRequest},
		{"dpi negative", 1, -150, document.ErrRequest},
		{"dpi zero before page zero", 0, 0, document.ErrRequest},
		{"page zero", 0, 150, document.ErrRequest},
		{"page negative", -1, 150, document.ErrRequest},
		{"first page", 1, 150, nil},
		{"a page no count bounds", 1 << 20, 72, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := document.CheckRender(tc.page, tc.dpi)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("CheckRender(page %d, %d dpi) returned %v, want %v", tc.page, tc.dpi, err, tc.want)
			}
		})
	}
}

func TestHasText(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"":                        false,
		" \n\t \r\n":              false,
		"\f":                      false,
		"      The Harbor Survey": true,
		"\n\nx\n":                 true,
	} {
		if got := document.HasText(text); got != want {
			t.Errorf("HasText(%q) = %v, want %v", text, got, want)
		}
	}
}
