//go:build integration && linux

package document_test

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/standards-lab/docproc/document"
)

// pngSize returns the width and height of the PNG file at path.
func pngSize(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("%s isn't a PNG: %v", path, err)
	}
	return cfg.Width, cfg.Height
}

// TestImagesHost checks the released preparation on the host's gs: Images prepares every page of
// scanned.pdf, in page order, as a PNG file of letter size at 150 dpi, and of clean.pdf with its
// text layer, and records each run's wall time.
func TestImagesHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	p := document.NewPreparer(document.Config{Renderer: g})
	for _, tc := range []struct {
		set   string
		pages int
		text  bool
	}{{"scanned", 7, false}, {"clean", 6, true}} {
		req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: 4, Text: tc.text}
		start := time.Now()
		next := 1
		for img, err := range p.Images(t.Context(), filepath.Join("testdata", tc.set+".pdf"), req) {
			if err != nil {
				t.Fatalf("%s.pdf page %d: %v", tc.set, img.Number, err)
			}
			if img.Number != next || img.Count != tc.pages || img.DPI != 150 {
				t.Errorf("%s.pdf yield %d is page %d of %d at %d dpi, want page %d of %d at 150", tc.set, next, img.Number, img.Count, img.DPI, next, tc.pages)
			}
			if w, h := pngSize(t, img.Path); w != 1275 || h != 1650 {
				t.Errorf("%s.pdf page %d is %dx%d, want letter at 150 dpi, 1275x1650", tc.set, img.Number, w, h)
			}
			if document.HasText(img.Text) != tc.text {
				t.Errorf("%s.pdf page %d has a text layer %t, want %t", tc.set, img.Number, !tc.text, tc.text)
			}
			next++
		}
		if next-1 != tc.pages {
			t.Errorf("%s.pdf: the stream yielded %d pages, want %d", tc.set, next-1, tc.pages)
		}
		t.Logf("recorded: Images %s.pdf, text %t, %d pages at concurrency 4: %v", tc.set, tc.text, tc.pages, time.Since(start).Round(time.Millisecond))
	}
}

// TestImagesMaxPixelsHost checks MaxPixels on the host's gs: one pixel short of a letter page's
// 1275x1650 at 150 dpi, clean.pdf's pages render at 149 dpi, 1267x1639, within the bound, as the
// page sizes the count run read predict; and at exactly that many pixels, at 150 dpi.
func TestImagesMaxPixelsHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	p := document.NewPreparer(document.Config{Renderer: g})
	for _, tc := range []struct {
		max  int64
		dpi  int
		w, h int
	}{{1275*1650 - 1, 149, 1267, 1639}, {1275 * 1650, 150, 1275, 1650}} {
		req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: 2, MaxPixels: tc.max}
		for img, err := range p.Images(t.Context(), filepath.Join("testdata", "clean.pdf"), req) {
			if err != nil {
				t.Fatalf("page %d: %v", img.Number, err)
			}
			w, h := pngSize(t, img.Path)
			if img.DPI != tc.dpi || w != tc.w || h != tc.h || int64(w*h) > tc.max {
				t.Errorf("MaxPixels %d: page %d at %d dpi is %dx%d, want %d dpi, %dx%d", tc.max, img.Number, img.DPI, w, h, tc.dpi, tc.w, tc.h)
			}
		}
	}
}
