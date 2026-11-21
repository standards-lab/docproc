//go:build integration && linux

package office_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

var exportPictures = []struct {
	fixture, entry string
}{
	{"pictures/tiff.docx", "word/media/image1.tiff"},
	{"ooxml/word-with-attachments.docx", "word/media/image4.emf"},
	{"ooxml/with-drawing.xlsx", "xl/media/image5.wmf"},
}

func TestPictureConvertReal(t *testing.T) {
	c, pc := fullConverters(t)
	for _, p := range exportPictures {
		t.Run(filepath.Base(p.entry), func(t *testing.T) {
			data := extract(t, filepath.Join("testdata", p.fixture), p.entry)
			name := sha(string(data)) + filepath.Ext(p.entry)
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			var pngs [][]byte
			for i := range 2 {
				if i > 0 {
					time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
				}
				dir := t.TempDir()
				out, err := convertOne(t, pc, dir, path, 0)
				if err != nil {
					t.Fatal(err)
				}
				stem := sha(string(data))
				if want := filepath.Join(dir, stem+".png"); out != want {
					t.Errorf("PNG: got %s, want %s", out, want)
				}
				if got := entries(t, dir); !slices.Equal(got, []string{stem + ".png"}) {
					t.Errorf("working directory holds %q, want the PNG alone", got)
				}
				b, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(b))
				if err != nil {
					t.Fatalf("its PNG doesn't decode: %v", err)
				}
				if i == 0 {
					t.Logf("picture: %s %7d B -> png %4dx%-4d %7d B", name[:12]+"…"+filepath.Ext(name),
						len(data), img.Bounds().Dx(), img.Bounds().Dy(), len(b))
				}
				pngs = append(pngs, b)
			}
			if !bytes.Equal(pngs[0], pngs[1]) {
				t.Errorf("converted twice, its PNGs differ (%d and %d B); the conversion isn't reproducible", len(pngs[0]), len(pngs[1]))
			}
			if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, data) {
				t.Errorf("the original wasn't kept: %v", err)
			}

			res := convertPath(t, c, filepath.Join("testdata", p.fixture))
			i := slices.IndexFunc(res.doc.Media, func(m office.Medium) bool { return m.File == name })
			if i < 0 || !res.doc.Media[i].Converted {
				t.Fatalf("Convert didn't convert %s", name)
			}
			within, err := os.ReadFile(res.doc.Media[i].Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(within, pngs[0]) {
				t.Errorf("picture's Convert's PNG (%d B) isn't office's Convert's (%d B)", len(pngs[0]), len(within))
			}
		})
	}
}

var pictureKinds = map[string]route.Kind{".tiff": route.TIFF, ".emf": route.EMF, ".wmf": route.WMF}

func convertOne(t *testing.T, pc *picture.Converter, dir, path string, scale int) (string, error) {
	t.Helper()
	base := filepath.Base(path)
	png := filepath.Join(dir, strings.TrimSuffix(base, filepath.Ext(base))+".png")
	err := pc.ConvertBatch(t.Context(), picture.Batch{
		Dir: dir, MetafileScale: scale,
		Pictures: []picture.Picture{{Path: path, Kind: pictureKinds[filepath.Ext(base)], PNG: png}},
	})
	return png, err
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func extract(t *testing.T, path, entry string) []byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	f, err := r.Open(entry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
