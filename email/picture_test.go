//go:build linux

package email_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// standinPNG begins what the stand-in magick writes: PNG's signature and a marker, before its
// input.
const standinPNG = "\x89PNG\r\n\x1a\nconverted: "

// pictureStandin is the stand-in magick that the picture package's Converter is built over.
// With -version alone it prints magick's version line, 7.1.2-34, and with -list format a listing
// of no formats, which the Converter reads when it is built. Otherwise it writes standinPNG and its input, its first argument with any
// format prefix and frame suffix, "tiff:path[0]", to its output, its last argument, "png:path".
func pictureStandin(r tooltest.Run) int {
	name, args := r.Program, r.Args
	switch {
	case name == "magick" && len(args) == 1 && args[0] == "-version":
		fmt.Println("Version: ImageMagick 7.1.2-34 Q16-HDRI x86_64 https://imagemagick.org")
		return 0
	case name == "magick" && len(args) == 2 && args[0] == "-list" && args[1] == "format":
		fmt.Println("   Format  Module    Mode  Description")
		return 0
	case name != "magick" || len(args) < 2:
		fmt.Fprintf(os.Stderr, "stand-in %s: unexpected arguments %q\n", name, args)
		return 2
	}
	strip := func(arg string) string {
		_, path, _ := strings.Cut(arg, ":")
		return strings.TrimSuffix(path, "[0]")
	}
	input, err := os.ReadFile(strip(args[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := os.WriteFile(strip(args[len(args)-1]), append([]byte(standinPNG), input...), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

// pictureConverter builds a picture Converter over the stand-in magick, through the exec runner,
// with no LibreOffice, and returns it with magick's link.
func pictureConverter(t *testing.T) (*picture.Converter, string) {
	t.Helper()
	link := tooltest.Link(t, t.TempDir(), "magick", nil)
	spec := picture.MagickSpec()
	spec.Program = link
	mg, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	c, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg})
	if err != nil {
		t.Fatal(err)
	}
	return c, link
}

// TestRouteVisionConvertedPicture: a TIFF attachment, written into the attachments directory as
// mail names it, its SHA-256 and stored extension, routes route.VisionConverted, and the caller
// converts it through the picture package's Convert into a directory of its own: the PNG is that
// directory's file named by the attachment's SHA-256 and .png, from one magick run on the TIFF's
// bytes, and the attachment's file is kept as mail wrote it. How picture invokes magick is
// picture's own tests' to prove.
func TestRouteVisionConvertedPicture(t *testing.T) {
	t.Parallel()
	c, magick := pictureConverter(t)
	attachments := t.TempDir()
	file := writeContent(t, attachments, tiffLE, ".tif")
	a := &email.Attachment{File: file, Filename: "scan.tif", Method: "by_value", Path: filepath.Join(attachments, file)}
	rt, why, err := a.Route()
	if err != nil || rt != route.VisionConverted {
		t.Fatalf("route: got %s (%s), %v; want %s", rt, why, err, route.VisionConverted)
	}

	out := t.TempDir()
	png, err := c.Convert(t.Context(), picture.Request{Dir: out, Path: a.Path})
	if err != nil {
		t.Fatal(err)
	}
	stem := strings.TrimSuffix(file, ".tif")
	if want := filepath.Join(out, stem+".png"); png != want {
		t.Errorf("PNG: got %s, want %s", png, want)
	}
	if b, err := os.ReadFile(png); err != nil || string(b) != standinPNG+tiffLE {
		t.Errorf("PNG holds %q, %v; want the stand-in's conversion of the TIFF", b, err)
	}
	if b, err := os.ReadFile(a.Path); err != nil || string(b) != tiffLE {
		t.Errorf("the attachment's file holds %q, %v; want it kept", b, err)
	}
	if list, err := os.ReadDir(attachments); err != nil || len(list) != 1 || list[0].Name() != file {
		t.Errorf("attachments directory holds %v, %v; want the attachment alone", list, err)
	}
	// Beside the conversion, magick ran for its version and its format listing.
	if all := tooltest.Runs(t, magick); len(all) != 3 {
		t.Errorf("runs: got %q, want one magick run beside the version and the listing", all)
	}
}
