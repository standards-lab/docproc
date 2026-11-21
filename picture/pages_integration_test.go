//go:build integration && linux

package picture_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

func convertPagesHost(t *testing.T, c *picture.Converter, path string, kind route.Kind) []image.Image {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var runs [][][]byte
	for range 2 {
		dir := t.TempDir()
		pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: kind, Stem: sha(string(data))})
		if err != nil {
			t.Fatal(err)
		}
		if got := entries(t, dir); len(got) != len(pngs) {
			t.Errorf("working directory holds %q, want the %d PNGs alone", got, len(pngs))
		}
		var run [][]byte
		for _, p := range pngs {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			run = append(run, b)
		}
		runs = append(runs, run)
	}
	if len(runs[0]) != len(runs[1]) {
		t.Fatalf("converted twice, %d and %d PNGs", len(runs[0]), len(runs[1]))
	}
	var imgs []image.Image
	for i, b := range runs[0] {
		if !bytes.Equal(b, runs[1][i]) {
			t.Errorf("PNG %d: converted twice, its bytes differ; the conversion isn't reproducible", i+1)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("PNG %d doesn't decode: %v", i+1, err)
		}
		imgs = append(imgs, img)
	}
	unchanged(t, path, string(data))
	return imgs
}

func sizes(imgs []image.Image) []image.Point {
	var out []image.Point
	for _, img := range imgs {
		out = append(out, img.Bounds().Size())
	}
	return out
}

func wantSizes(t *testing.T, imgs []image.Image, want ...image.Point) {
	t.Helper()
	got := sizes(imgs)
	if len(got) != len(want) {
		t.Fatalf("%d PNGs of sizes %v, want %d of %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("PNG %d is %v, want %v", i+1, got[i], want[i])
		}
	}
}

func near(img image.Image, x, y int, want color.NRGBA) bool {
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	d := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 8 }
	return c.A == 0xff && d(c.R, want.R) && d(c.G, want.G) && d(c.B, want.B)
}

var (
	white = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	red   = color.NRGBA{0xff, 0, 0, 0xff}
)

func TestConvertPagesTIFFHost(t *testing.T) {
	c := hostConverter(t)
	path := filepath.Join("testdata", "pages.tiff")
	imgs := convertPagesHost(t, c, path, route.TIFF)
	wantSizes(t, imgs, image.Pt(40, 30), image.Pt(50, 30), image.Pt(60, 30))
	for i, want := range []uint8{0xff, 0x80, 0} {
		if g := gray(imgs[i], 5, 5); max(g, want)-min(g, want) > 2 {
			t.Errorf("page %d is gray %d, want %d", i+1, g, want)
		}
	}

	imgs = convertPagesHost(t, c, filepath.Join("testdata", "scanned-g4.tiff"), route.TIFF)
	wantSizes(t, imgs, image.Pt(64, 48), image.Pt(72, 48), image.Pt(80, 48))
	for i, p := range []struct{ black, white image.Point }{
		{image.Pt(20, 20), image.Pt(50, 20)},
		{image.Pt(50, 20), image.Pt(20, 20)},
		{image.Pt(20, 20), image.Pt(-1, -1)},
	} {
		if gray(imgs[i], p.black.X, p.black.Y) != 0 || p.white.X >= 0 && gray(imgs[i], p.white.X, p.white.Y) != 0xff {
			t.Errorf("page %d: black at %v is %d, white at %v; want its own block", i+1, p.black, gray(imgs[i], p.black.X, p.black.Y), p.white)
		}
	}

	imgs = convertPagesHost(t, c, filepath.Join("testdata", "lzw-gray.tiff"), route.TIFF)
	wantSizes(t, imgs, image.Pt(30, 20), image.Pt(40, 20))
	if g := gray(imgs[0], 5, 5); max(g, 0x40)-min(g, 0x40) > 2 {
		t.Errorf("page 1 is gray %d, want 64", g)
	}

	png := filepath.Join(t.TempDir(), "first.png")
	if err := c.ConvertBatch(t.Context(), picture.Batch{Dir: t.TempDir(), Pictures: []picture.Picture{{Path: path, Kind: route.TIFF, PNG: png}}}); err != nil {
		t.Fatal(err)
	}
	if img := decodeFile(t, png); img.Bounds().Size() != image.Pt(40, 30) {
		t.Errorf("ConvertBatch's PNG is %v, want the first page's 40x30", img.Bounds().Size())
	}
}

func gray(img image.Image, x, y int) uint8 {
	return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
}

func decodeFile(t *testing.T, path string) image.Image {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestConvertRasterHost(t *testing.T) {
	c := hostConverter(t)
	kinds := map[string]route.Kind{".heic": route.HEIC, ".webp": route.WebP, ".avif": route.AVIF}
	for _, name := range []string{"two-images.heic", "oriented.webp"} {
		t.Run(name, func(t *testing.T) {
			imgs := convertPagesHost(t, c, filepath.Join("testdata", name), kinds[filepath.Ext(name)])
			wantSizes(t, imgs, image.Pt(32, 48))
			if !near(imgs[0], 16, 4, white) || !near(imgs[0], 16, 44, red) {
				t.Errorf("PNG is %v above %v, want white above red", imgs[0].At(16, 4), imgs[0].At(16, 44))
			}
		})
	}
	for _, name := range []string{"animated.webp", "animated.avif"} {
		t.Run(name, func(t *testing.T) {
			imgs := convertPagesHost(t, c, filepath.Join("testdata", name), kinds[filepath.Ext(name)])
			wantSizes(t, imgs, image.Pt(24, 16))
			if !near(imgs[0], 12, 8, red) {
				t.Errorf("PNG is %v, want the first frame's red", imgs[0].At(12, 8))
			}
		})
	}
}
