//go:build linux

package dispatch_test

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// toollessPicture returns a picture stage over a Converter given no tool, which has no processor
// for any picture.
func toollessPicture(t *testing.T) *dispatch.PictureStage {
	t.Helper()
	c, err := picture.NewConverter(t.Context(), picture.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := dispatch.NewPictureStage(dispatch.PictureConfig{Converter: c, Version: "none"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newDispatcher returns a dispatcher over stages and index, in a working directory of its own.
func newDispatcher(t *testing.T, stages dispatch.Stages, index dispatch.Index) *dispatch.Dispatcher {
	t.Helper()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: stages, Index: index, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// gsSettings are the stand-in gs's: a PDF of Pages pages, the page Empty with no text layer,
// and the page Fail failing to render.
type gsSettings struct {
	Pages int `json:"pages"`
	Empty int `json:"empty,omitzero"`
	Fail  int `json:"fail,omitzero"`
}

// gsStandin is the stand-in gs. With --version alone it prints 10.08.0. A count run, with
// -dNODISPLAY, prints its settings' Pages; a run of the txtwrite device prints "page N", N its
// -dFirstPage, but a blank line for Empty; and a render prints PNG's signature and "page N", but
// fails for Fail.
func gsStandin(r tooltest.Run) int {
	var settings gsSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("10.08.0")
		return 0
	}
	if slices.Contains(args, "-dNODISPLAY") {
		fmt.Println(settings.Pages)
		return 0
	}
	page := 0
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-dFirstPage="); ok {
			page, _ = strconv.Atoi(v)
		}
	}
	switch {
	case slices.Contains(args, "-sDEVICE=txtwrite") && page == settings.Empty:
		fmt.Print("  \n")
	case slices.Contains(args, "-sDEVICE=txtwrite"):
		fmt.Printf("page %d", page)
	case page == settings.Fail:
		fmt.Fprint(os.Stderr, "told to fail")
		return 1
	default:
		fmt.Printf("%spage %d", route.PNGSignature, page)
	}
	return 0
}

// standinPreparer returns a Preparer over the stand-in gs, through the exec runner, with s its
// settings.
func standinPreparer(t *testing.T, s gsSettings) *document.Preparer {
	t.Helper()
	spec := document.GhostscriptSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "gs", s)
	gs, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	g, err := document.NewGhostscript(document.Config{Ghostscript: gs})
	if err != nil {
		t.Fatal(err)
	}
	return document.NewPreparer(document.Config{Renderer: g})
}

// documentStage returns a document stage over prep, reading the pages' text, one page at a time.
func documentStage(t *testing.T, prep *document.Preparer) *dispatch.DocumentStage {
	t.Helper()
	s, err := dispatch.NewDocumentStage(dispatch.DocumentConfig{
		Preparer: prep, Version: "gs 10.08.0", Request: document.ImagesRequest{Text: true, Concurrency: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestDocumentStage: a PDF's pages, over the stand-in gs, are a text output and an image output
// each, in page order, named by the page and carrying its number and the count, and the image its
// DPI, the PNG a file in the part's directory; a page with no text layer has no text output; and a
// page that fails fails the part once the others are prepared.
func TestDocumentStage(t *testing.T) {
	pdf := input(t, "three.pdf", "%PDF-1.7\n%%EOF\n")
	d := newDispatcher(t, dispatch.Stages{route.Document: documentStage(t, standinPreparer(t, gsSettings{Pages: 3, Empty: 2}))}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, pdf, func(ev *dispatch.Event) {
		if o := ev.Output; o != nil && o.Kind == dispatch.ImageOutput {
			b, err := os.ReadFile(o.Path)
			if err != nil || !bytes.HasPrefix(b, []byte(route.PNGSignature)) {
				t.Errorf("%s: %v, %d bytes", o.Name, err, len(b))
			}
		}
	})
	wantSteps(t, events, []step{{"three.pdf", 0, dispatch.Prepared}})
	var got []string
	for _, o := range events[0].Outputs {
		got = append(got, fmt.Sprintf("%s %s %s/%s %s %q", o.Kind, o.Name, o.Meta[dispatch.MetaPage], o.Meta[dispatch.MetaCount], o.Meta[dispatch.MetaDPI], o.Value))
	}
	want := []string{
		`text page 1 1/3  "page 1"`, `image page 1 1/3 150 ""`,
		`image page 2 2/3 150 ""`,
		`text page 3 3/3  "page 3"`, `image page 3 3/3 150 ""`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("outputs:\n got %q\nwant %q", got, want)
	}

	d = newDispatcher(t, dispatch.Stages{route.Document: documentStage(t, standinPreparer(t, gsSettings{Pages: 3, Fail: 2}))}, dispatch.NewMemoryIndex(0))
	ev := dispatchAll(t, d, pdf, nil)[0]
	if ev.Outcome != dispatch.Failed || !strings.Contains(ev.Reason, "1 of 3 pages failed") || len(ev.Outputs) != 4 {
		t.Errorf("a failing page: %s, %q, %d outputs", ev.Outcome, ev.Reason, len(ev.Outputs))
	}
}

// TestPictureNoProcessor: a Converter given no tool has no processor yet for a WebP, a HEIC, an
// AVIF, a TIFF, or an EMF; each reason says why.
func TestPictureNoProcessor(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.VisionConverted: toollessPicture(t)}, dispatch.NewMemoryIndex(0))
	for name, c := range map[string]struct{ content, reason string }{
		"x.webp": {"RIFF\x10\x00\x00\x00WEBPVP8 ", "is a WebP picture: no magick converter"},
		"x.heic": {"\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic", "is a HEIC picture: no magick converter"},
		"x.avif": {"\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif", "is an AVIF picture: no magick converter"},
		"x.tif":  {"II*\x00\x08\x00\x00\x00", "no magick converter"},
		"x.emf":  {"\x01\x00\x00\x00" + strings.Repeat("\x00", 36) + " EMF" + strings.Repeat("\x00", 40), "no LibreOffice converter"},
	} {
		ev := dispatchAll(t, d, input(t, name, c.content), nil)[0]
		if ev.Part.ContentType.Route != route.VisionConverted || ev.Outcome != dispatch.NoProcessor || !strings.Contains(ev.Reason, c.reason) {
			t.Errorf("%s: %s %s, %q; want no processor, %q", name, ev.Part.ContentType.Route, ev.Outcome, ev.Reason, c.reason)
		}
	}
}
