//go:build integration && linux

package picture_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// hostLimits are the limits the integration tier's Converter passes every magick run: far past
// what its fixtures need, so each converts under them.
var hostLimits = picture.Limits{Area: 64 << 20, Width: 16384, Height: 16384, Memory: 512 << 20, Map: 1 << 30, Disk: 2 << 30}

// hostLimitArgs are hostLimits as magick's -limit options, in the order every run carries them.
var hostLimitArgs = []string{"-limit", "area", "67108864", "-limit", "width", "16384", "-limit", "height", "16384",
	"-limit", "memory", "536870912", "-limit", "map", "1073741824", "-limit", "disk", "2147483648"}

// recorder is the host's magick behind a shell script that appends each run's arguments to a log,
// one line each, NUL-separated, then runs magick, so a test reads what every real run was given.
type recorder struct {
	t   *testing.T
	log string
}

// newRecorder writes the recording script into a directory of its own and returns it with the
// Spec that runs it as magick.
func newRecorder(t *testing.T) (*recorder, tool.Spec) {
	t.Helper()
	magick, err := exec.LookPath("magick")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r := &recorder{t: t, log: filepath.Join(dir, "runs.log")}
	script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do printf '%%s\\0' \"$a\"; done >> '%s'\necho >> '%s'\nexec '%s' \"$@\"\n", r.log, r.log, magick)
	path := filepath.Join(dir, "magick")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := picture.MagickSpec()
	spec.Program = path
	return r, spec
}

// runs returns every recorded run's arguments, in order, the version probe left out.
func (r *recorder) runs() [][]string {
	r.t.Helper()
	b, err := os.ReadFile(r.log)
	if err != nil {
		r.t.Fatal(err)
	}
	var runs [][]string
	for line := range strings.Lines(string(b)) {
		args := strings.Split(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\x00"), "\x00")
		if len(args) == 1 && args[0] == "-version" {
			continue
		}
		runs = append(runs, args)
	}
	return runs
}

// limited fails the test unless every recorded magick run, the format listing included, began
// with hostLimitArgs, and returns how many there were.
func (r *recorder) limited() int {
	r.t.Helper()
	runs := r.runs()
	for _, args := range runs {
		if len(args) < len(hostLimitArgs) || !slices.Equal(args[:len(hostLimitArgs)], hostLimitArgs) {
			r.t.Errorf("magick ran with %q, not beginning with the configured limits %q", args, hostLimitArgs)
		}
	}
	return len(runs)
}

// hostConverter builds a Converter over the host's magick, behind a recorder, and soffice, both
// through the exec runner, with hostLimits, and checks when the test ends that every magick run
// carried them.
func hostConverter(t *testing.T) *picture.Converter {
	t.Helper()
	r, spec := newRecorder(t)
	mg, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	so, err := tool.NewExec(t.Context(), picture.LibreOfficeSpec())
	if err != nil {
		t.Fatal(err)
	}
	c, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: so, Limits: hostLimits})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		n := r.limited()
		t.Logf("recorded: %d magick runs, each beginning with the configured limits", n)
	})
	return c
}

// convertHost copies the SVG data into a directory of its own as name, converts it through c at
// density into a working directory of its own, twice, the second run in a later second than the
// first ended in, and returns the PNG decoded, failing unless both runs gave the same bytes, the
// working directory holds the PNG alone, and the original is kept.
func convertHost(t *testing.T, c *picture.Converter, name string, data []byte, density int) image.Image {
	t.Helper()
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
		out, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: route.SVG, Stem: sha(string(data)), Density: density})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{filepath.Join(dir, sha(string(data))+".png")}; !slices.Equal(out, want) {
			t.Errorf("PNG: got %q, want %q", out, want)
		}
		if got := entries(t, dir); len(got) != 1 {
			t.Errorf("working directory holds %q, want the PNG alone", got)
		}
		b, err := os.ReadFile(out[0])
		if err != nil {
			t.Fatal(err)
		}
		pngs = append(pngs, b)
	}
	if !bytes.Equal(pngs[0], pngs[1]) {
		t.Errorf("converted twice, its PNGs differ (%d and %d B); the conversion isn't reproducible", len(pngs[0]), len(pngs[1]))
	}
	unchanged(t, path, string(data))
	img, err := png.Decode(bytes.NewReader(pngs[0]))
	if err != nil {
		t.Fatalf("its PNG doesn't decode: %v", err)
	}
	t.Logf("recorded: %s %d B at density %d -> png %dx%d %d B", name, len(data), density, img.Bounds().Dx(), img.Bounds().Dy(), len(pngs[0]))
	return img
}

// census counts img's pixels: opaque white, dark (each channel below a quarter), and red (red
// high, green and blue low), and the pixels that aren't opaque.
func census(img image.Image) (white, dark, red, translucent int) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
			switch {
			case c.A != 0xffff:
				translucent++
			case c.R == 0xffff && c.G == 0xffff && c.B == 0xffff:
				white++
			case c.R < 0x4000 && c.G < 0x4000 && c.B < 0x4000:
				dark++
			case c.R > 0xc000 && c.G < 0x4000 && c.B < 0x4000:
				red++
			}
		}
	}
	return white, dark, red, translucent
}

// TestConvertSVGHost converts the package's own SVG fixtures, shapes and text and an ink-like
// drawing, on the host's magick, under the configured limits: each is rendered at the default
// density, 150 dpi, and at a set one, 300 dpi, its size in CSS pixels or millimetres scaled to it,
// flattened onto opaque white with print on it, and the same bytes converted twice.
func TestConvertSVGHost(t *testing.T) {
	c := hostConverter(t)
	for _, tc := range []struct {
		name          string
		density       int
		width, height int
	}{
		{"shapes.svg", 0, 500, 250},
		{"ink.svg", 0, 236, 118},
		{"shapes.svg", 300, 1000, 500},
		{"ink.svg", 300, 472, 236},
	} {
		t.Run(fmt.Sprintf("%s at %d", tc.name, tc.density), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			img := convertHost(t, c, tc.name, data, tc.density)
			if b := img.Bounds(); b.Dx() != tc.width || b.Dy() != tc.height {
				t.Errorf("PNG is %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.width, tc.height)
			}
			white, dark, _, translucent := census(img)
			if translucent != 0 || white < img.Bounds().Dx()*img.Bounds().Dy()/2 || dark == 0 {
				t.Errorf("PNG has %d white, %d dark, and %d translucent pixels; want it flattened onto white with print on it", white, dark, translucent)
			}
		})
	}
}

// TestConvertSVGScriptHost converts ~/docs' script-bearing.svg, committed as testdata: an SVG with
// an onload handler, an inline script, and an image whose href is http://example.invalid. It
// converts, at 150 dpi, to a PNG of its text alone: librsvg, the rsvg coder the conversion names,
// has no script engine, so neither the handler nor the script runs, and its resolver loads no
// URL but a data: URL or a file beside the SVG, the copy alone in its directory here, so the
// image is never fetched and renders as nothing.
func TestConvertSVGScriptHost(t *testing.T) {
	c := hostConverter(t)
	data, err := os.ReadFile(filepath.Join("testdata", "script-bearing.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img := convertHost(t, c, "script-bearing.svg", data, 0)
	if b := img.Bounds(); b.Dx() != 625 || b.Dy() != 187 {
		t.Errorf("PNG is %dx%d, want 625x187, 400x120 at 150 dpi, rounded down", b.Dx(), b.Dy())
	}
	if _, dark, _, translucent := census(img); dark == 0 || translucent != 0 {
		t.Errorf("PNG has %d dark and %d translucent pixels; want its text, opaque", dark, translucent)
	}
}

// TestConvertSVGReferencesHost: an SVG whose images name a file beside it, a file by its
// absolute path, and a URL on a listener of this test's own converts to a PNG that draws none
// of them, and nothing connects to the listener. The pictures are red, so one drawn would show.
func TestConvertSVGReferencesHost(t *testing.T) {
	c := hostConverter(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var connections atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			_ = conn.Close()
		}
	}()
	red := redPNG(t)
	beside := t.TempDir()
	if err := os.WriteFile(filepath.Join(beside, "beside.png"), red, 0o600); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.png")
	if err := os.WriteFile(elsewhere, red, 0o600); err != nil {
		t.Fatal(err)
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="96" height="32">`+
		`<image href="beside.png" x="0" y="0" width="32" height="32"/>`+
		`<image xlink:href="file://%s" x="32" y="0" width="32" height="32"/>`+
		`<image href="http://%s/remote.png" x="64" y="0" width="32" height="32"/>`+
		`</svg>`, elsewhere, ln.Addr())
	path := filepath.Join(beside, "references.svg")
	if err := os.WriteFile(path, []byte(svg), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := c.ConvertPages(t.Context(), picture.Request{Dir: t.TempDir(), Path: path, Kind: route.SVG, Stem: "references"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out[0])
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, reds, _ := census(img); reds != 0 {
		t.Errorf("PNG has %d red pixels; an image the SVG names was drawn", reds)
	}
	if n := connections.Load(); n != 0 {
		t.Errorf("%d connections to the URL the SVG names; want none", n)
	}
	if strings.Contains(string(b), "remote") {
		t.Errorf("the PNG names the URL")
	}
}

// redPNG returns a 32x32 PNG of opaque red.
func redPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{0xff, 0, 0, 0xff})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestLimitsHost: a limit the picture passes fails the conversion on the host's magick with its
// own message, so the configured limits reach magick: shapes.svg at 150 dpi is 500 pixels wide,
// past a width limit of 400, and leaves nothing in the working directory.
func TestLimitsHost(t *testing.T) {
	r, spec := newRecorder(t)
	mg, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	so, err := tool.NewExec(t.Context(), picture.LibreOfficeSpec())
	if err != nil {
		t.Fatal(err)
	}
	c, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: so, Limits: picture.Limits{Width: 400}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: filepath.Join("testdata", "shapes.svg"), Kind: route.SVG, Stem: "shapes"})
	if pngs != nil || err == nil || !strings.Contains(err.Error(), "width or height exceeds limit") {
		t.Fatalf("got %q, %v; want magick's width limit refusing the picture", pngs, err)
	}
	t.Logf("recorded: %v", err)
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("working directory holds %q, want nothing", got)
	}
	for _, args := range r.runs() {
		if !slices.Equal(args[:3], []string{"-limit", "width", "400"}) {
			t.Errorf("magick ran with %q, want the width limit first", args)
		}
	}
}
