//go:build linux

package stages_test

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
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

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

// gsStandin is the stand-in gs. With --version alone it prints 10.08.0. A survey run, with
// -dNODISPLAY, prints false, no portfolio, when its PostScript asks for /Collection, then its
// settings' Pages. Any other run's pages are its -sPageList's, a page or a range of them between
// each comma, or its -dFirstPage alone, each in turn: a run of the txtwrite device writes "page N" for each, but a
// blank line for Empty, to stdout for an OutputFile of -, and otherwise to a file of the
// OutputFile, its %d the page's place in the run from 1; and a render writes each page's PNG,
// naming "page N", but fails at Fail, the pages before it written.
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
		if strings.Contains(args[len(args)-1], "/Collection") {
			fmt.Println(false)
		}
		fmt.Println(settings.Pages)
		return 0
	}
	var pages []int
	out := "-"
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-dFirstPage="); ok {
			n, _ := strconv.Atoi(v)
			pages = []int{n}
		}
		if v, ok := strings.CutPrefix(a, "-sPageList="); ok {
			for f := range strings.SplitSeq(v, ",") {
				first, last, _ := strings.Cut(f, "-")
				from, _ := strconv.Atoi(first)
				to := from
				if last != "" {
					to, _ = strconv.Atoi(last)
				}
				for n := from; n <= to; n++ {
					pages = append(pages, n)
				}
			}
		}
		if v, ok := strings.CutPrefix(a, "-sOutputFile="); ok {
			out = v
		}
	}
	for i, page := range pages {
		var b []byte
		switch {
		case slices.Contains(args, "-sDEVICE=txtwrite") && page == settings.Empty:
			b = []byte("  \n")
		case slices.Contains(args, "-sDEVICE=txtwrite"):
			b = fmt.Appendf(nil, "page %d", page)
		case page == settings.Fail:
			fmt.Fprint(os.Stderr, "told to fail")
			return 1
		default:
			b = tooltest.PNG(fmt.Sprintf("page %d", page))
		}
		var err error
		if out == "-" {
			_, err = os.Stdout.Write(b)
		} else {
			err = os.WriteFile(fmt.Sprintf(out, i+1), b, 0o600)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
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

// testDocumentStage returns a document stage over prep, reading the pages' text, one page at a
// time, with a qpdf that finds no password and no form and a pdfdetach that lists nothing.
func testDocumentStage(t *testing.T, prep *document.Preparer) *stages.DocumentStage {
	t.Helper()
	return stages.NewDocumentStage(stages.DocumentConfig{
		Preparer: prep, QPDF: &fakeQPDF{}, PDFDetach: &fakeDetach{}, Version: "gs 10.08.0",
		Request: document.ImagesRequest{Text: true, Concurrency: 1},
	})
}

// TestDocumentStage: a PDF's pages, over the stand-in gs, are a text output and an image output
// each, in page order, named by the page and carrying its number and the count, and the image its
// DPI, the PNG a file in the part's directory; a page with no text layer has no text output; and a
// page gs fails to render is a warning naming the page and gs, the part prepared with the others.
func TestDocumentStage(t *testing.T) {
	pdf := input(t, "three.pdf", "%PDF-1.7\n%%EOF\n")
	d := newDispatcher(t, dispatch.Stages{route.Document: testDocumentStage(t, standinPreparer(t, gsSettings{Pages: 3, Empty: 2}))}, dispatch.NewMemoryIndex(0))
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

	d = newDispatcher(t, dispatch.Stages{route.Document: testDocumentStage(t, standinPreparer(t, gsSettings{Pages: 3, Fail: 2}))}, dispatch.NewMemoryIndex(0))
	var warnings []dispatch.Warning
	ev := dispatchAll(t, d, pdf, func(ev *dispatch.Event) {
		if ev.Warning != nil {
			warnings = append(warnings, *ev.Warning)
		}
	})[0]
	if ev.Outcome != dispatch.Prepared || len(ev.Outputs) != 4 {
		t.Errorf("a failing page: %s, %q, %d outputs", ev.Outcome, ev.Reason, len(ev.Outputs))
	}
	if len(warnings) != 1 || warnings[0].Tool != "gs" || warnings[0].Where != "page 2" || !strings.Contains(warnings[0].What, "told to fail") {
		t.Errorf("a failing page's warnings: %+v", warnings)
	}
}
