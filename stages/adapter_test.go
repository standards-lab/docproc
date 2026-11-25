//go:build linux

package stages_test

import (
	"bytes"
	"cmp"
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

func newDispatcher(t *testing.T, stages dispatch.Stages, index dispatch.Index) *dispatch.Dispatcher {
	t.Helper()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: stages, Index: index, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type gsSettings struct {
	Pages int `json:"pages"`
	Empty int `json:"empty,omitzero"`
	Fail  int `json:"fail,omitzero"`
}

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
		if settings.Pages > 0 && strings.Contains(args[len(args)-1], "/Collection") {
			fmt.Println(false)
		}
		return 0
	}
	for _, page := range tooltest.GSPages(args) {
		if page == settings.Fail {
			fmt.Fprint(os.Stderr, "told to fail")
			return 1
		}
		if _, err := os.Stdout.Write(tooltest.PNG(fmt.Sprintf("page %d", page))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	return 0
}

type pdftotextSettings struct {
	Empty   int    `json:"empty,omitzero"`
	Version string `json:"version,omitzero"`
}

func pdftotextStandin(r tooltest.Run) int {
	var settings pdftotextSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 1 && args[0] == "-v" {
		fmt.Fprintf(os.Stderr, "pdftotext version %s\n", cmp.Or(settings.Version, "26.10.0"))
		return 0
	}
	page, err := strconv.Atoi(args[slices.Index(args, "-f")+1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	text := fmt.Sprintf("page %d\f", page)
	if page == settings.Empty {
		text = "  \n\f"
	}
	if _, err := os.Stdout.WriteString(text); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

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
	textSpec := document.PDFToTextSpec()
	textSpec.Program = tooltest.Link(t, t.TempDir(), "pdftotext", pdftotextSettings{Empty: s.Empty})
	pdftotext, err := tool.NewExec(t.Context(), textSpec)
	if err != nil {
		t.Fatal(err)
	}
	text, err := document.NewTextReader(document.Config{PDFToText: pdftotext})
	if err != nil {
		t.Fatal(err)
	}
	return document.NewPreparer(document.Config{Renderer: g, Text: text})
}

func testDocumentStage(t *testing.T, prep *document.Preparer) *stages.DocumentStage {
	t.Helper()
	return stages.NewDocumentStage(stages.DocumentConfig{
		Preparer: prep, QPDF: &fakeQPDF{}, PDFDetach: &fakeDetach{}, Version: "gs 10.08.0",
		Request: document.ImagesRequest{Text: true, Concurrency: 1},
	})
}

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
