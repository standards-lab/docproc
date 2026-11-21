//go:build linux

package inspect_test

import (
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/inspect"
)

var cleanDirs = []string{"../office/testdata", "../document/testdata", "../picture/testdata", "../tool/testdata", "../email/testdata"}

var cleanSkipped = map[string]bool{"../picture/testdata/script-bearing.svg": true}

var cleanFlags = map[string][]inspect.Check{
	"../office/testdata/ooxml/media.libreoffice.docx":     {inspect.ExternalRelationship},
	"../office/testdata/ooxml/word-with-attachments.docx": {inspect.OLEObject},
	"../document/testdata/attachments/attachments.pdf":    {inspect.PDFEmbeddedFile},
	"../document/testdata/portfolio/portfolio.pdf":        {inspect.PDFEmbeddedFile},
	"../document/testdata/malformed/compacted-syntax.pdf": {inspect.PDFAA, inspect.PDFJavaScript, inspect.PDFJS},
}

func TestCleanFixtures(t *testing.T) {
	n := 0
	seen := map[string]bool{}
	for _, dir := range cleanDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) == ".sh" || cleanSkipped[path] {
				return err
			}
			n++
			seen[path] = true
			r := mustInspect(t, path, d.Name(), inspect.Options{})
			if got, want := checks(r), cleanFlags[path]; !slices.Equal(got, want) {
				t.Errorf("%s: checks %v, want %v (%+v)", path, got, want, r.Flags)
			}
			if r.Verdict != inspect.Proceed {
				t.Errorf("%s: verdict %s", path, r.Verdict)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for path := range cleanFlags {
		if !seen[path] {
			t.Errorf("%s: no such clean fixture", path)
		}
	}
	if n == 0 {
		t.Fatal("no clean fixture inspected")
	}
	t.Logf("inspected %d clean fixtures", n)
}
