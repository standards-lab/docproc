//go:build integration && linux

package office_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/internal/goldentest"
)

const goldenPictures = "testdata/golden/pictures.sha256"

func TestGoldenPictures(t *testing.T) {
	c, pc := fullConverters(t)
	got := map[string]string{}
	for _, pattern := range pictureFixtures {
		paths, err := filepath.Glob(filepath.Join("testdata", pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			rel, _ := filepath.Rel("testdata", path)
			res := convertPath(t, c, path)
			for _, m := range res.doc.Media {
				if !m.Converted {
					continue
				}
				b, err := os.ReadFile(m.Path)
				if err != nil {
					t.Fatal(err)
				}
				got["convert/"+rel+"/"+m.File] = goldentest.Sum(b)
			}
		}
	}
	for _, p := range exportPictures {
		data := extract(t, filepath.Join("testdata", p.fixture), p.entry)
		path := filepath.Join(t.TempDir(), goldentest.Sum(data)+filepath.Ext(p.entry))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, scale := range []int{0, 1} {
			out, err := convertOne(t, pc, t.TempDir(), path, scale)
			if err != nil {
				t.Fatalf("%s: %v", p.entry, err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			key := "picture/" + p.fixture + "/" + p.entry
			if scale != 0 {
				key += "@scale1"
			}
			got[key] = goldentest.Sum(b)
		}
	}
	goldentest.Check(t, goldenPictures, got)
}
