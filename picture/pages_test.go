//go:build linux

package picture_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

var (
	heicData = "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic" + "filler"
	webpData = "RIFF\x10\x00\x00\x00WEBPVP8 " + "filler"
	avifData = "\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif" + "filler"
)

func pageNames(stem string, n int) []string {
	var names []string
	for i := range n {
		names = append(names, fmt.Sprintf("%s-page%d.png", stem, i+1))
	}
	return names
}

func TestConvertPagesTIFF(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc, content, name, stem string
		pages                     int
	}{
		{"three pages, named by its SHA-256", tiffLE, "", sha(tiffLE), 3},
		{"one page, named otherwise", tiffBE, "scan.dat", "scan_1.b", 1},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			st.set(func(s *settings) { s.Pages = tc.pages })
			path := writePicture(t, tc.content, tc.name)
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: route.TIFF, Stem: tc.stem})
			if err != nil {
				t.Fatal(err)
			}
			names := pageNames(tc.stem, tc.pages)
			var want []string
			for _, n := range names {
				want = append(want, filepath.Join(dir, n))
			}
			if !slices.Equal(pngs, want) {
				t.Fatalf("PNGs: got %q, want %q", pngs, want)
			}
			for i, png := range pngs {
				page := fmt.Sprintf("%s%s%s page %d", pngSignature, convertedPrefix, tc.content, i+1)
				if b, err := os.ReadFile(png); err != nil || string(b) != page {
					t.Errorf("page %d holds %q, %v; want %q", i+1, b, err, page)
				}
			}
			unchanged(t, path, tc.content)
			if got := entries(t, dir); !slices.Equal(got, names) {
				t.Errorf("working directory holds %q, want the pages alone", got)
			}
			all := st.runs()
			if got := programs(all); !slices.Equal(got, []string{"magick"}) {
				t.Fatalf("runs: got %q, want magick alone", got)
			}
			stem := regexp.QuoteMeta(tc.stem)
			args := []string{`tiff:` + own(dir) + stem + `\.tiff`, `\+adjoin`, `-define`, `png:exclude-chunks=date,time`, `png:` + stem + `-%d\.png`}
			if !matchArgs(all[0].Args, args) {
				t.Errorf("magick's arguments: got %q, want %q", all[0].Args, args)
			}
			if !regexp.MustCompile(`^`+own(dir)+`?$`).MatchString(all[0].Dir) || all[0].Input != tc.content {
				t.Errorf("magick ran in %s on %q; want in the copy's directory on the TIFF", all[0].Dir, all[0].Input)
			}
		})
	}
}

func TestConvertPagesFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc string
		set  func(*settings)
		want string
	}{
		{"magick exits 1", func(s *settings) { s.Fail = "magick: improper image header\n" },
			"picture: magick: scan.tiff: tool: magick: exit code 1: magick: improper image header"},
		{"magick writes no PNG", func(s *settings) { s.Raw = true },
			"picture: magick: scan.tiff: scan-0.png isn't a PNG"},
		{"a page left out", func(s *settings) { s.Pages, s.Gap = 3, true },
			"picture: magick: scan.tiff: wrote 2 pages without page 2"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			st.set(tc.set)
			path := writePicture(t, tiffLE, "")
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: route.TIFF, Stem: "scan"})
			if pngs != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, %v; want an error saying %q", pngs, err, tc.want)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
			unchanged(t, path, tiffLE)
		})
	}
}

func TestConvertRaster(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		content, coder, ext string
		kind                route.Kind
	}{
		{heicData, "heic", ".heic", route.HEIC},
		{webpData, "webp", ".webp", route.WebP},
		{avifData, "avif", ".avif", route.AVIF},
	} {
		t.Run(tc.coder, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			path := writePicture(t, tc.content, "photo.bin")
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: tc.kind, Stem: "photo"})
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{filepath.Join(dir, "photo.png")}; !slices.Equal(pngs, want) {
				t.Fatalf("PNGs: got %q, want %q", pngs, want)
			}
			if b, err := os.ReadFile(pngs[0]); err != nil || string(b) != pngSignature+convertedPrefix+tc.content {
				t.Errorf("PNG holds %q, %v", b, err)
			}
			if got := entries(t, dir); !slices.Equal(got, []string{"photo.png"}) {
				t.Errorf("working directory holds %q, want the PNG alone", got)
			}
			all := st.runs()
			if got := programs(all); !slices.Equal(got, []string{"magick"}) {
				t.Fatalf("runs: got %q, want one magick run", got)
			}
			want := []string{tc.coder + `:` + own(dir) + `photo` + regexp.QuoteMeta(tc.ext) + `\[0\]`, `-auto-orient`, `-define`, `png:exclude-chunks=date,time`, `png:` + own(dir) + `photo\.png`}
			if !matchArgs(all[0].Args, want) {
				t.Errorf("magick's arguments: got %q, want %q", all[0].Args, want)
			}
			if all[0].Dir != dir || all[0].Input != tc.content {
				t.Errorf("magick ran in %s on %q; want in %s on the picture", all[0].Dir, all[0].Input, dir)
			}
			unchanged(t, path, tc.content)
		})
	}
}

func TestConvertNoReader(t *testing.T) {
	t.Parallel()
	listing := "   Format  Module    Mode  Description\n" +
		"     HEIC  HEIC      -w+   High Efficiency Image Format (1.23.6)\n" +
		"     WEBP* WEBP      rw+   WebP Image Format (libwebp 1.6.0 [0210])\n"
	c, st := standinConverter(t, picture.Limits{}, func(s *settings) { s.Formats = &listing })
	st.set(func(s *settings) { s.Formats = nil })
	for _, tc := range []struct {
		content string
		kind    route.Kind
		want    string
	}{
		{heicData, route.HEIC, "is a HEIC picture: no magick reader"},
		{avifData, route.AVIF, "is an AVIF picture: no magick reader"},
	} {
		dir := t.TempDir()
		path := writePicture(t, tc.content, "")
		pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: tc.kind, Stem: "photo"})
		if pngs != nil || !errors.Is(err, picture.ErrNoReader) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ConvertPages: got %q, %v; want ErrNoReader saying %q", pngs, err, tc.want)
		}
		err = c.ConvertBatch(t.Context(), picture.Batch{Dir: dir, Pictures: []picture.Picture{
			{Path: path, Kind: tc.kind, PNG: filepath.Join(dir, "a.png")},
		}})
		if !errors.Is(err, picture.ErrNoReader) {
			t.Errorf("ConvertBatch: got %v, want ErrNoReader", err)
		}
		if got := entries(t, dir); len(got) != 0 {
			t.Errorf("working directory holds %q, want nothing", got)
		}
	}
	if got := st.runs(); len(got) != 0 {
		t.Errorf("runs: got %q, want none", programs(got))
	}
	if got := st.listings(); len(got) != 1 {
		t.Errorf("format listings: got %d, want one", len(got))
	}
	if _, err := c.ConvertPages(t.Context(), picture.Request{Dir: t.TempDir(), Path: writePicture(t, webpData, ""), Kind: route.WebP, Stem: "photo"}); err != nil {
		t.Errorf("a WebP: %v", err)
	}
}

func TestNewConverterListFails(t *testing.T) {
	t.Parallel()
	st := newStandins(t, "7.1.2-34", func(s *settings) { s.ListFail = "magick: unable to open module file\n" })
	c, err := picture.NewConverter(t.Context(), picture.Config{Magick: st.mustTool(picture.MagickSpec()), LibreOffice: st.mustTool(picture.LibreOfficeSpec())})
	if c != nil || err == nil || !strings.Contains(err.Error(), "picture: magick's format listing: tool: magick: exit code 1: magick: unable to open module file") {
		t.Errorf("got %v, %v; want the listing's error", c, err)
	}
}
