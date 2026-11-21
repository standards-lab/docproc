//go:build integration && linux

package office_test

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

var pictureFixtures = []string{
	"ooxml/*.docx", "ooxml/*.xlsx", "ooxml/*.pptx", "pictures/*.docx",
	"legacy/generated/*.doc", "legacy/generated/*.xls", "legacy/generated/*.ppt",
	"legacy/real/*.doc", "legacy/real/*.xls", "legacy/real/*.ppt",
}

func TestPictures(t *testing.T) {
	full := fullConverter(t)

	var paths []string
	for _, pattern := range pictureFixtures {
		matches, err := filepath.Glob(filepath.Join("testdata", pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, matches...)
	}
	kinds := map[string]int{}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			res, again, elapsed := convertTimed(t, full, path)
			timing := "conversion"
			var converted []office.Medium
			seen := map[string]bool{}
			for _, m := range res.doc.Media {
				kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(m.File)), ".")
				if kind == "tif" {
					kind = "tiff"
				}
				if m.Kind != office.Picture || kind != "emf" && kind != "wmf" && kind != "tiff" || seen[m.File] {
					continue
				}
				seen[m.File] = true
				kinds[kind]++
				media := filepath.Dir(m.Path)
				png := filepath.Join(media, strings.TrimSuffix(m.File, filepath.Ext(m.File))+".png")
				if !m.Converted || m.Path != png {
					t.Errorf("%s: handed on as %q, converted %t; want %s", m.File, m.Path, m.Converted, png)
					continue
				}
				if strings.Contains(res.markdown, m.File) {
					t.Errorf("%s: the Markdown still names the stored picture", m.File)
				}
				if !strings.Contains(res.markdown, filepath.Base(png)) {
					t.Errorf("%s: the Markdown doesn't link its PNG", m.File)
				}
				converted = append(converted, m)
				samePNG(t, m, again)
			}
			for _, m := range converted {
				logPicture(t, name, m, fmt.Sprintf("%s %5d ms for %d", timing, elapsed.Milliseconds(), len(converted)))
			}
		})
	}
	t.Logf("pictures: %d emf, %d wmf, %d tiff", kinds["emf"], kinds["wmf"], kinds["tiff"])
	for _, kind := range []string{"emf", "wmf", "tiff"} {
		if kinds[kind] == 0 {
			t.Errorf("no fixture holds a %s picture", kind)
		}
	}
}

func fullConverter(t *testing.T) *office.Converter {
	t.Helper()
	c, _ := fullConverters(t)
	return c
}

func fullConverters(t *testing.T) (*office.Converter, *picture.Converter) {
	t.Helper()
	o, err := tool.NewExec(t.Context(), office.OfficeSpec())
	if err != nil {
		t.Fatalf("docproc-ooxml, built from tools/ooxml, on PATH: %v", err)
	}
	lo, err := tool.NewContainer(t.Context(), "", tool.Image{Ref: picture.LibreOfficeImage}, picture.LibreOfficeSpec())
	if err != nil {
		t.Fatal(err)
	}
	mg, err := tool.NewExec(t.Context(), picture.MagickSpec())
	if err != nil {
		t.Fatalf("magick, a host prerequisite for TIFF: %v", err)
	}
	t.Logf("soffice %s in %s, magick %s", lo.Version(), picture.LibreOfficeImage, mg.Version())
	pc, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: lo})
	if err != nil {
		t.Fatal(err)
	}
	c, err := office.NewConverter(t.Context(), office.Config{Office: o, Pictures: pc})
	if err != nil {
		t.Fatal(err)
	}
	return c, pc
}

func convertTimed(t *testing.T, c *office.Converter, path string) (converted, converted, time.Duration) {
	t.Helper()
	var results []converted
	var fastest time.Duration
	for i := range 2 {
		if i > 0 {
			time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
		}
		start := time.Now()
		res := convertPath(t, c, path)
		elapsed := time.Since(start)
		if results == nil {
			fastest = elapsed
		}
		results = append(results, res)
		fastest = min(fastest, elapsed)
	}
	return results[0], results[1], fastest
}

func samePNG(t *testing.T, m office.Medium, again converted) {
	t.Helper()
	i := slices.IndexFunc(again.doc.Media, func(a office.Medium) bool { return a.File == m.File })
	if i < 0 || !again.doc.Media[i].Converted {
		t.Errorf("%s: not converted the second time", m.File)
		return
	}
	first, err := os.ReadFile(m.Path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(again.doc.Media[i].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("%s: converted twice, its PNGs differ (%d and %d B); a conversion isn't reproducible", m.File, len(first), len(second))
	}
}

func logPicture(t *testing.T, fixture string, m office.Medium, timing string) {
	t.Helper()
	stored, err := os.Stat(filepath.Join(filepath.Dir(m.Path), m.File))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(m.Path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Errorf("%s: its PNG doesn't decode: %v", m.File, err)
		return
	}
	b := img.Bounds()
	if kind := strings.ToLower(filepath.Ext(m.File)); (kind == ".emf" || kind == ".wmf") &&
		(b.Dx() >= picture.DefaultMetafileScale*794 || b.Dy() >= picture.DefaultMetafileScale*1123) {
		t.Errorf("%s: its PNG is %dx%d, the page it was rendered on, not trimmed to the picture", m.File, b.Dx(), b.Dy())
	}
	t.Logf("picture: %-34s %s… %-4s %7d B -> png %4dx%-4d %7d B  %s",
		fixture, m.File[:12], strings.TrimPrefix(filepath.Ext(m.File), "."), stored.Size(),
		b.Dx(), b.Dy(), len(data), timing)
}
