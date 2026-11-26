//go:build integration && linux

package office_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/office"
)

var drawingText = []string{"A drawing for docproc", "An ellipse", "Boxes, an ellipse, and a line", "# Slide 2", "The second page of the drawing"}

func TestConvertedRoundTrip(t *testing.T) {
	c := containerConverter(t)
	odf := filepath.Join("testdata", "odf", "generated")
	for _, tc := range []struct {
		path, from, to, original string
		text                     []string
	}{
		{filepath.Join(odf, "tables.libreoffice.odt"), "odt", "docx", "tables.libreoffice.docx", nil},
		{filepath.Join(odf, "tables.libreoffice.ott"), "ott", "docx", "tables.libreoffice.docx", nil},
		{filepath.Join(odf, "cells.libreoffice.ods"), "ods", "xlsx", "cells.libreoffice.xlsx", nil},
		{filepath.Join(odf, "cells.libreoffice.ots"), "ots", "xlsx", "cells.libreoffice.xlsx", nil},
		{filepath.Join(odf, "slides.libreoffice.odp"), "odp", "pptx", "slides.libreoffice.pptx", nil},
		{filepath.Join(odf, "slides.libreoffice.otp"), "otp", "pptx", "slides.libreoffice.pptx", nil},
		{filepath.Join(odf, "drawing.odg"), "odg", "pptx", "", drawingText},
		{filepath.Join(odf, "drawing.otg"), "otg", "pptx", "", drawingText},
		{filepath.Join("testdata", "wordperfect", "testWordPerfect_6_61.wpd"), "wpd", "docx", "", []string{"Sluwe Sjaantje sloeg de slome slager"}},
		{filepath.Join("testdata", "xlsb", "testVarious.xlsb"), "xlsb", "xlsx", "", []string{"This is a string", "13.1211231321", "text test"}},
	} {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			res := convertPath(t, c, tc.path)
			if res.doc.Format != tc.to || res.doc.ConvertedFrom != tc.from {
				t.Fatalf("document %s, converted from %q; want %s from %s", res.doc.Format, res.doc.ConvertedFrom, tc.to, tc.from)
			}
			for _, w := range res.warnings {
				t.Logf("warning: %s: %s", w.Where, w.What)
			}
			if strings.TrimSpace(res.markdown) == "" {
				t.Fatal("no Markdown")
			}
			for _, s := range tc.text {
				if !strings.Contains(res.markdown, s) {
					t.Errorf("the Markdown lacks %q:\n%s", s, res.markdown)
				}
			}
			if tc.original == "" {
				return
			}
			want := elements(convertPath(t, c, filepath.Join("testdata", "ooxml", tc.original)))
			got := elements(res)
			for kind, keys := range want {
				if kind == "slide" || kind == "sheet" {
					if len(got[kind]) != len(keys) {
						t.Errorf("%s: %d, want the original's %d", kind, len(got[kind]), len(keys))
					}
				}
			}
			logLoss(t, filepath.Base(tc.path), "original", want, got, nil)
		})
	}
}

func TestConvertedEncrypted(t *testing.T) {
	c := containerConverter(t)
	for _, path := range []string{
		filepath.Join("testdata", "odf", "generated", "encrypted.odt"),
		filepath.Join("testdata", "xlsb", "protected_passtika.xlsb"),
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			recs, err := collect(t, c, office.Request{Path: path, Dir: t.TempDir(), Name: filepath.Base(path)})
			if len(recs) != 0 || !errors.Is(err, dispatch.ErrEncrypted) {
				t.Fatalf("got %d records, %v; want ErrEncrypted", len(recs), err)
			}
			t.Logf("refused: %v", err)
		})
	}
}
