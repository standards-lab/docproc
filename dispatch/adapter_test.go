//go:build linux

package dispatch_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
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

// pages is a document.Renderer and TextExtractor over a PDF of n pages: page N renders as a
// 1x1 PNG and reads "page N" as its text, but for the page empty, which has no text layer, and
// the page fail, which fails to render.
type pages struct {
	n, empty, fail int
}

func (r *pages) Count(context.Context, string) (int, error) { return r.n, nil }

func (r *pages) Render(_ context.Context, _ string, page, dpi int, w io.Writer) error {
	if err := document.CheckRender(page, dpi); err != nil {
		return err
	}
	if page == r.fail {
		return errors.New("told to fail")
	}
	img := image.NewGray(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Gray{Y: uint8(page)})
	return png.Encode(w, img)
}

func (r *pages) Text(_ context.Context, _ string, page int) (string, error) {
	if page == r.empty {
		return "  \n", nil
	}
	return fmt.Sprintf("page %d", page), nil
}

// documentStage returns a document stage over render, reading the pages' text.
func documentStage(t *testing.T, render document.Renderer) *dispatch.DocumentStage {
	t.Helper()
	prep := document.NewPreparer(document.Config{Renderer: render})
	s, err := dispatch.NewDocumentStage(dispatch.DocumentConfig{
		Preparer: prep, Version: "pages 1", Request: document.ImagesRequest{Text: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestDocumentStage: a PDF's pages, over a renderer of its own, are a text output and an image
// output each, in page order, named by the page and carrying its number and the count, the PNG
// a file in the part's directory; a page with no text layer has no text output; and a page that
// fails fails the part once the others are prepared.
func TestDocumentStage(t *testing.T) {
	pdf := input(t, "three.pdf", "%PDF-1.7\n%%EOF\n")
	d := newDispatcher(t, dispatch.Stages{route.Document: documentStage(t, &pages{n: 3, empty: 2})}, dispatch.NewMemoryIndex(0))
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
		got = append(got, fmt.Sprintf("%s %s %s/%s %q", o.Kind, o.Name, o.Meta["page"], o.Meta["count"], o.Value))
	}
	want := []string{
		`text page 1 1/3 "page 1"`, `image page 1 1/3 ""`,
		`image page 2 2/3 ""`,
		`text page 3 3/3 "page 3"`, `image page 3 3/3 ""`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("outputs:\n got %q\nwant %q", got, want)
	}

	d = newDispatcher(t, dispatch.Stages{route.Document: documentStage(t, &pages{n: 3, fail: 2})}, dispatch.NewMemoryIndex(0))
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
