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

// exportPictures are the pictures TestPictureConvertReal extracts from the committed fixtures,
// one of each converted kind: the TIFF fixture's TIFF, an OLE object's EMF preview, and a sheet's
// WMF.
var exportPictures = []struct {
	fixture, entry string
}{
	{"pictures/tiff.docx", "word/media/image1.tiff"},
	{"ooxml/word-with-attachments.docx", "word/media/image4.emf"},
	{"ooxml/with-drawing.xlsx", "xl/media/image5.wmf"},
}

// TestPictureConvertReal extracts a TIFF, an EMF, and a WMF from the committed fixtures, writes
// each as mail names an attachment, its SHA-256 and stored extension, outside the working
// directory, and converts it through the picture package's ConvertBatch alone with the real
// soffice in the default image and the host's magick, twice, the second run in a later second
// than the first ended in. Each gives the working directory's file named by its SHA-256 and .png, which image/png
// decodes, the same bytes both times and the same bytes as Convert hands the picture on as within
// its document, since both run the picture package's one code path; the original is kept and
// nothing else is left.
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

// pictureKinds are the kinds of exportPictures' entries, by extension.
var pictureKinds = map[string]route.Kind{".tiff": route.TIFF, ".emf": route.EMF, ".wmf": route.WMF}

// convertOne converts the picture at path, read as the kind its extension names, through pc's
// ConvertBatch alone, a TIFF's first image as a document's TIFF is, into dir's PNG named by its
// SHA-256, its name's stem, at scale, and returns the PNG's path.
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

// sha returns the lowercase hex SHA-256 of content.
func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// extract returns the bytes of the entry named entry in the ZIP package at path.
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
