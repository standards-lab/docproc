//go:build linux

package stages_test

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

type pictureSettings struct {
	Formats  string            `json:"formats"`
	Pages    int               `json:"pages"`
	EXIF     map[string]string `json:"exif,omitzero"`
	EXIFFail string            `json:"exif_fail,omitzero"`
}

const pictureListing = `   Format  Module    Mode  Description
-------------------------------------------------------------------------------
     AVIF  HEIC      rw+   AV1 Image File Format (1.23.6)
     HEIC  HEIC      rw+   High Efficiency Image Format (1.23.6)
     WEBP* WEBP      rw+   WebP Image Format (libwebp 1.6.0 [0210])
`

const (
	pictureTIFF = "II*\x00\x08\x00\x00\x00" + "a stand-in TIFF"
	pictureHEIC = "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"
	pictureWebP = "RIFF\x10\x00\x00\x00WEBPVP8 "
	pictureAVIF = "\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif"
)

func pictureStandin(r tooltest.Run) int {
	var settings pictureSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	switch {
	case len(args) == 1 && args[0] == "-version":
		fmt.Println("Version: ImageMagick 7.1.2-34 Q16-HDRI x86_64 https://imagemagick.org")
		return 0
	case slices.Equal(args, []string{"-list", "format", "null:"}):
		fmt.Print(settings.Formats)
		return 0
	case len(args) > 0 && args[len(args)-1] == "info:":
		return pictureEXIFStandin(settings)
	case len(args) < 2:
		fmt.Fprintf(os.Stderr, "stand-in: unexpected arguments %q\n", args)
		return 2
	}
	_, out, _ := strings.Cut(args[len(args)-1], ":")
	if !slices.Contains(args, "+adjoin") {
		if err := os.WriteFile(out, []byte(route.PNGSignature+args[0]), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	for i := range settings.Pages {
		if err := os.WriteFile(fmt.Sprintf(out, i), []byte(fmt.Sprintf("%spage %d", route.PNGSignature, i+1)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	return 0
}

func sofficeStandin(r tooltest.Run) int {
	if len(r.Args) == 1 && r.Args[0] == "--version" {
		fmt.Println("LibreOffice 26.8.1.1 680(Build:1)")
		return 0
	}
	fmt.Fprintf(os.Stderr, "stand-in: unexpected arguments %q\n", r.Args)
	return 2
}

func standinPictureStage(t *testing.T, listing string, pages int) *stages.PictureStage {
	t.Helper()
	return pictureStageOver(t, pictureSettings{Formats: listing, Pages: pages})
}

func pictureStageOver(t *testing.T, settings pictureSettings) *stages.PictureStage {
	t.Helper()
	mg, r := standinEXIF(t, settings)
	spec := picture.LibreOfficeSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "soffice", struct{}{})
	lo, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	c, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: lo})
	if err != nil {
		t.Fatal(err)
	}
	s := stages.NewPictureStage(stages.PictureConfig{Converter: c, EXIF: r, Version: stages.Versions(mg)})
	return s
}

func TestPictureStageTIFFPages(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, dispatch.Stages{route.VisionConverted: standinPictureStage(t, pictureListing, 3)}, dispatch.NewMemoryIndex(0))
	var held []string
	events := dispatchAll(t, d, input(t, "scan.tif", pictureTIFF), func(ev *dispatch.Event) {
		if ev.Output != nil {
			b, err := os.ReadFile(ev.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, string(b))
		}
	})
	wantSteps(t, events, []step{{"scan.tif", 0, dispatch.Prepared}})
	outputs := events[0].Outputs
	if len(outputs) != 3 {
		t.Fatalf("%d outputs, want 3", len(outputs))
	}
	for i, o := range outputs {
		n := strconv.Itoa(i + 1)
		want := map[string]string{"from": "tiff", "page": n, "count": "3"}
		if o.Kind != dispatch.ImageOutput || o.Name != "page "+n || o.MediaType != "image/png" || !maps.Equal(o.Meta, want) {
			t.Errorf("output %d: %s %q %s %v; want an image named %q, meta %v", i, o.Kind, o.Name, o.MediaType, o.Meta, "page "+n, want)
		}
		if held[i] != route.PNGSignature+"page "+n {
			t.Errorf("output %d holds %q, want page %s", i, held[i], n)
		}
	}
}

func TestPictureStageRasters(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, dispatch.Stages{route.VisionConverted: standinPictureStage(t, pictureListing, 0)}, dispatch.NewMemoryIndex(0))
	for name, c := range map[string]struct {
		content string
		kind    route.Kind
	}{
		"x.heic": {pictureHEIC, route.HEIC},
		"x.webp": {pictureWebP, route.WebP},
		"x.avif": {pictureAVIF, route.AVIF},
	} {
		var held string
		events := dispatchAll(t, d, input(t, name, c.content), func(ev *dispatch.Event) {
			if ev.Output != nil {
				b, err := os.ReadFile(ev.Output.Path)
				if err != nil {
					t.Fatal(err)
				}
				held = string(b)
			}
		})
		wantSteps(t, events, []step{{name, 0, dispatch.Prepared}})
		o := events[0].Outputs
		if len(o) != 1 || o[0].Kind != dispatch.ImageOutput || o[0].Name != "picture" || !maps.Equal(o[0].Meta, map[string]string{"from": string(c.kind)}) {
			t.Errorf("%s: outputs %+v", name, o)
		}
		if coder := string(c.kind) + ":"; !strings.HasPrefix(held, route.PNGSignature+coder) || !strings.HasSuffix(held, "[0]") {
			t.Errorf("%s: PNG holds %q; want its first image read through %s", name, held, coder)
		}
	}
}

func TestPictureNoReader(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, dispatch.Stages{route.VisionConverted: standinPictureStage(t, "", 0)}, dispatch.NewMemoryIndex(0))
	for name, c := range map[string]struct{ content, what string }{
		"x.heic": {pictureHEIC, "a HEIC"},
		"x.webp": {pictureWebP, "a WebP"},
		"x.avif": {pictureAVIF, "an AVIF"},
	} {
		ev := dispatchAll(t, d, input(t, name, c.content), nil)[0]
		want := "no processor yet: picture: " + name + " is " + c.what + " picture: no magick reader"
		if ev.Outcome != dispatch.NoProcessor || !strings.Contains(ev.Reason, want) {
			t.Errorf("%s: %s, %q; want no processor, %q", name, ev.Outcome, ev.Reason, want)
		}
	}
}
