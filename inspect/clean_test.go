//go:build linux

package inspect_test

import (
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/inspect"
)

// cleanDirs are the directories of the repository's clean fixtures: the office, document,
// picture, tool, and email packages' testdata.
var cleanDirs = []string{"../office/testdata", "../document/testdata", "../picture/testdata", "../tool/testdata", "../email/testdata"}

// cleanSkipped are the files under cleanDirs that aren't clean fixtures: picture's
// script-bearing.svg, ~/docs' SVG hazard, copied as the conversions' security fixture.
var cleanSkipped = map[string]bool{"../picture/testdata/script-bearing.svg": true}

// cleanFlags are the clean fixtures that carry what a check reads, and its checks. A linked
// picture is an external relationship, fetched when a reader renders the document, as an SVG's
// external href is, so it is active content though the fixture is clean; an external
// hyperlink, which word-with-attachments.docx holds three of, isn't. Its OLE object is one of
// the embeddings the checks name, as are document's attachments.pdf's embedded files.
var cleanFlags = map[string][]inspect.Check{
	"../office/testdata/ooxml/media.libreoffice.docx":     {inspect.ExternalRelationship},
	"../office/testdata/ooxml/word-with-attachments.docx": {inspect.OLEObject},
	"../document/testdata/attachments/attachments.pdf":    {inspect.PDFEmbeddedFile},
	"../document/testdata/portfolio/portfolio.pdf":        {inspect.PDFEmbeddedFile},
}

// TestCleanFixtures inspects the repository's clean fixtures, every file under cleanDirs but the
// generator scripts, which are executable by design, and cleanSkipped: each yields no flag, but
// cleanFlags' files, which yield exactly their checks, and every verdict is proceed. A cleanFlags
// file the walk doesn't reach, renamed or removed, fails, as does a walk that reaches nothing.
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
