//go:build linux

package document_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

// fixture is a Preparer over the stand-in gs at 10.08.0, the stand-in, a PDF path, and a request
// whose pages go to a directory of the test's own.
type fixture struct {
	prep *document.Preparer
	gs   *standin
	pdf  string
	req  document.ImagesRequest
}

// newFixture builds a fixture whose stand-in gs counts pages pages, at concurrency 1. The
// stand-in reads no PDF, so the file is empty.
func newFixture(t *testing.T, pages int) *fixture {
	t.Helper()
	return newTimedFixture(t, pages, 0)
}

// newTimedFixture is newFixture with timeout the Spec's Timeout, a unit's bound.
func newTimedFixture(t *testing.T, pages int, timeout time.Duration) *fixture {
	t.Helper()
	st := newStandin(t, "gs", "10.08.0")
	st.set(func(s *settings) { s.Pages = pages })
	spec := document.GhostscriptSpec()
	spec.Timeout = timeout
	g, err := document.NewGhostscript(document.Config{Ghostscript: st.mustTool(spec)})
	if err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(pdf, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return &fixture{
		prep: document.NewPreparer(document.Config{Renderer: g}),
		gs:   st, pdf: pdf,
		req: document.ImagesRequest{Dir: t.TempDir(), Concurrency: 1},
	}
}

// images ranges over the fixture's stream to its end, returning every page and error it yields.
func (f *fixture) images(ctx context.Context) ([]document.PageImage, []error) {
	var pages []document.PageImage
	var errs []error
	for p, err := range f.prep.Images(ctx, f.pdf, f.req) {
		pages = append(pages, p)
		errs = append(errs, err)
	}
	return pages, errs
}

// rendered returns the page and the DPI of every page a render run the stand-in gs logged names,
// in the order the runs began, each run's pages in its order.
func (f *fixture) rendered() (pages, dpi []int) {
	for _, r := range f.gs.runs() {
		if !slices.Contains(r.Args, "-sDEVICE=png16m") {
			continue
		}
		var at int
		for _, a := range r.Args {
			if v, ok := strings.CutPrefix(a, "-r"); ok {
				at, _ = strconv.Atoi(v)
			}
		}
		for _, page := range pageArgs(r.Args) {
			pages = append(pages, page)
			dpi = append(dpi, at)
		}
	}
	return pages, dpi
}

// processes returns the pages of each run of the stand-in gs with device, "png16m" or
// "txtwrite", as gs names them, "-sPageList=1,4" for a shared process and "-dFirstPage=2" for a
// page's own, sorted, since lanes start their processes in no fixed order.
func (f *fixture) processes(device string) []string {
	var got []string
	for _, r := range f.gs.runs() {
		if !slices.Contains(r.Args, "-sDEVICE="+device) {
			continue
		}
		for _, a := range r.Args {
			if strings.HasPrefix(a, "-sPageList=") || strings.HasPrefix(a, "-dFirstPage=") {
				got = append(got, a)
			}
		}
	}
	slices.Sort(got)
	return got
}

// files returns the names of the files in dir, sorted.
func files(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// zero reports whether p is the zero PageImage a stream's own error comes with.
func zero(p document.PageImage) bool { return p == document.PageImage{} }

// TestNewPreparerNil: a Config with no Renderer is the caller's mistake, and panics naming the
// field.
func TestNewPreparerNil(t *testing.T) {
	t.Parallel()
	wantPanic(t, "document: Config.Renderer is nil", func() { document.NewPreparer(document.Config{}) })
}

// TestImagesLazy checks that nothing runs until the stream is ranged over.
func TestImagesLazy(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 3)
	pages := f.prep.Images(t.Context(), f.pdf, f.req)
	if runs := f.gs.runs(); len(runs) != 0 {
		t.Errorf("gs ran %q before the stream was ranged over", runs)
	}
	for _, err := range pages {
		if err != nil {
			t.Error(err)
		}
	}
	if p, _ := f.rendered(); len(p) != 3 {
		t.Errorf("ranging over the stream rendered pages %v, want 3", p)
	}
}

// TestImagesPages checks that each page is yielded in page order as a PNG file in the request's
// directory, page-N.png holding the page gs rendered, with its number, the count, and the
// default DPI, when later pages finish first: page 1 is the slowest to render.
func TestImagesPages(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 4)
	f.gs.set(func(s *settings) { s.Delay = map[int]int{1: 200, 2: 100} })
	f.req.Concurrency = 4
	pages, errs := f.images(t.Context())
	if len(pages) != 4 {
		t.Fatalf("the stream yielded %d pages, want 4", len(pages))
	}
	for i, p := range pages {
		want := document.PageImage{Number: i + 1, Count: 4, Path: filepath.Join(f.req.Dir, fmt.Sprintf("page-%d.png", i+1)), DPI: 150}
		if errs[i] != nil || p != want {
			t.Errorf("yield %d: %+v, %v; want %+v", i+1, p, errs[i], want)
			continue
		}
		if b, err := os.ReadFile(p.Path); err != nil || !bytes.Equal(b, standinPage(p.Number, 150)) {
			t.Errorf("page %d's file holds %q, %v; want the stand-in's page", p.Number, b, err)
		}
	}
	if _, dpi := f.rendered(); !slices.Equal(dpi, []int{150, 150, 150, 150}) {
		t.Errorf("rendered at %v dpi, want 150 for each page", dpi)
	}
}

// TestImagesDPI checks a request's DPI reaches every render and its PageImage.
func TestImagesDPI(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 2)
	f.req.DPI = 300
	pages, _ := f.images(t.Context())
	if _, dpi := f.rendered(); !slices.Equal(dpi, []int{300, 300}) || pages[0].DPI != 300 {
		t.Errorf("rendered at %v dpi, page 1's DPI %d; want 300", dpi, pages[0].DPI)
	}
}

// TestImagesSlots checks a page's slot is released only when its outcome is taken: at
// concurrency 2, while the consumer holds page 1, pages 2 and 3 are written, page 2 waiting
// untaken in its slot, and page 4 isn't written until page 2 is taken, though its lane's process
// has rendered it. The test waits for page 3's file before it looks for page 4's, so a slow
// render can't hide a slot released too early.
func TestImagesSlots(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 6)
	f.req.Concurrency = 2
	for p, err := range f.prep.Images(t.Context(), f.pdf, f.req) {
		if err != nil {
			t.Fatal(err)
		}
		if p.Number != 1 {
			continue
		}
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			if slices.Contains(files(t, f.req.Dir), "page-3.png") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("page 3 wasn't written within 10 s while page 1 was held")
			}
		}
		time.Sleep(300 * time.Millisecond)
		if got := files(t, f.req.Dir); !slices.Equal(got, []string{"page-1.png", "page-2.png", "page-3.png"}) {
			t.Errorf("while page 1 was held, the directory held %q, want pages 1 to 3", got)
		}
	}
	if rendered, _ := f.rendered(); len(rendered) != 6 {
		t.Errorf("rendered pages %v, want 6", rendered)
	}
}

// TestImagesFailedPage checks a page that fails to render yields its number and count with an
// error naming the page and wrapping gs's, no file of it left, and the stream carries on.
func TestImagesFailedPage(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 3)
	f.gs.set(func(s *settings) { s.FailPage = 2 })
	f.req.Concurrency = 3
	pages, errs := f.images(t.Context())
	if len(pages) != 3 || errs[0] != nil || errs[2] != nil {
		t.Fatalf("the stream yielded %+v, %v; want 3 pages, page 2 alone failing", pages, errs)
	}
	if p := pages[1]; p != (document.PageImage{Number: 2, Count: 3}) || !strings.Contains(errs[1].Error(), "page 2 of 3: render") ||
		!strings.Contains(errs[1].Error(), "told to fail") {
		t.Errorf("page 2: %+v, %v", p, errs[1])
	}
	if got := files(t, f.req.Dir); !slices.Equal(got, []string{"page-1.png", "page-3.png"}) {
		t.Errorf("the directory holds %q, want pages 1 and 3", got)
	}
}

// TestImagesExistingFile checks a page whose file is already in the directory fails, the file
// left as it was.
func TestImagesExistingFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 2)
	existing := filepath.Join(f.req.Dir, "page-2.png")
	if err := os.WriteFile(existing, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errs := f.images(t.Context())
	if errs[0] != nil || !errors.Is(errs[1], os.ErrExist) {
		t.Errorf("errors %v, want page 2 alone failing, its file existing", errs)
	}
	if b, _ := os.ReadFile(existing); string(b) != "mine" {
		t.Errorf("page-2.png holds %q, want the caller's", b)
	}
}

// TestImagesCountFails checks a failed count is the stream's one error, with a zero PageImage.
func TestImagesCountFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 3)
	f.gs.set(func(s *settings) { s.Fail = "Error: /invalidfileaccess" })
	pages, errs := f.images(t.Context())
	if len(pages) != 1 || !zero(pages[0]) || errs[0] == nil {
		t.Errorf("the stream yielded %+v, %v; want one error", pages, errs)
	}
}

// TestImagesBreak checks breaking out of the stream ends the work in flight, and leaves only the
// pages yielded in the directory: the pages rendered ahead and never taken are removed.
func TestImagesBreak(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 6)
	f.gs.set(func(s *settings) { s.Delay = map[int]int{1: 200} })
	f.req.Concurrency = 4
	for p, err := range f.prep.Images(t.Context(), f.pdf, f.req) {
		if err != nil || p.Number != 1 {
			t.Fatalf("first yield %+v, %v", p, err)
		}
		break
	}
	if got := files(t, f.req.Dir); !slices.Equal(got, []string{"page-1.png"}) {
		t.Errorf("after a break at page 1 the directory holds %q, want page 1's alone", got)
	}
}

// TestImagesContextEnds checks an ended context is the stream's one error, once, with a zero
// PageImage, the pages not yet taken removed; and that a context ended before the range runs
// nothing.
func TestImagesContextEnds(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 6)
	f.gs.set(func(s *settings) { s.Delay = map[int]int{2: 5000} })
	f.req.Concurrency = 4
	ctx, cancel := context.WithCancel(t.Context())
	var got []error
	for p, err := range f.prep.Images(ctx, f.pdf, f.req) {
		got = append(got, err)
		if p.Number == 1 {
			cancel()
		}
		if err != nil && !zero(p) {
			t.Errorf("an error came with %+v", p)
		}
	}
	if len(got) != 2 || got[0] != nil || !errors.Is(got[1], context.Canceled) {
		t.Errorf("the stream yielded %v, want page 1 and then context.Canceled", got)
	}
	if names := files(t, f.req.Dir); !slices.Equal(names, []string{"page-1.png"}) {
		t.Errorf("the directory holds %q, want page 1's alone", names)
	}

	f = newFixture(t, 3)
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	pages, errs := f.images(ended)
	if len(pages) != 1 || !errors.Is(errs[0], context.Canceled) || len(f.gs.runs()) != 0 {
		t.Errorf("an ended context: %+v, %v, runs %q", pages, errs, f.gs.runs())
	}
}

// TestImagesRefusals checks a request Images can't run is refused, wrapping ErrRequest, before gs
// runs: no directory, a negative DPI, a concurrency below 1, which has no default, a negative
// MaxPixels, and a negative PagesPerProcess.
func TestImagesRefusals(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*document.ImagesRequest){
		"no directory":         func(r *document.ImagesRequest) { r.Dir = "" },
		"negative dpi":         func(r *document.ImagesRequest) { r.DPI = -1 },
		"zero concurrency":     func(r *document.ImagesRequest) { r.Concurrency = 0 },
		"negative concurrency": func(r *document.ImagesRequest) { r.Concurrency = -1 },
		"negative max pixels":  func(r *document.ImagesRequest) { r.MaxPixels = -1 },
		"negative pages":       func(r *document.ImagesRequest) { r.PagesPerProcess = -1 },
	} {
		f := newFixture(t, 3)
		change(&f.req)
		pages, errs := f.images(t.Context())
		if len(pages) != 1 || !zero(pages[0]) || !errors.Is(errs[0], document.ErrRequest) {
			t.Errorf("%s: %+v, %v; want one ErrRequest", name, pages, errs)
		}
		if runs := f.gs.runs(); len(runs) != 0 {
			t.Errorf("%s: gs ran %q", name, runs)
		}
	}
}

// TestImagesText checks a request with Text reads every page's text layer into its PageImage,
// in a shared process before the shared process that renders the pages, and renders every page as
// usual.
func TestImagesText(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 2)
	f.req.Text = true
	pages, errs := f.images(t.Context())
	for i, p := range pages {
		if errs[i] != nil || p.Text != standinText(i+1) || p.Path == "" {
			t.Errorf("page %d: %+v, %v; want its text and its file", i+1, p, errs[i])
		}
	}
	var devices []string
	for _, r := range f.gs.runs() {
		devices = append(devices, r.Args[slices.IndexFunc(r.Args, func(a string) bool {
			return strings.HasPrefix(a, "-sDEVICE=") || a == "-dNODISPLAY"
		})])
	}
	// The pages' text before their render.
	if want := []string{"-dNODISPLAY", "-sDEVICE=txtwrite", "-sDEVICE=png16m"}; !slices.Equal(devices, want) {
		t.Errorf("gs ran %q, want %q", devices, want)
	}
}

// TestImagesTextFails checks a shared text process that fails falls back to a process a page,
// its text and its render each, and a page whose text fails there fails, unrendered, and the
// stream carries on.
func TestImagesTextFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 3)
	f.gs.set(func(s *settings) { s.FailText = 2 })
	f.req.Text = true
	pages, errs := f.images(t.Context())
	if len(pages) != 3 || errs[0] != nil || errs[2] != nil || pages[0].Text != standinText(1) || pages[2].Text != standinText(3) {
		t.Fatalf("the stream yielded %+v, %v; want 3 pages, page 2 alone failing", pages, errs)
	}
	if err := errs[1]; err == nil || !strings.Contains(err.Error(), "page 2 of 3: text") || pages[1].Path != "" {
		t.Errorf("page 2: %+v, %v; want its text failing", pages[1], err)
	}
	if got, want := f.processes("txtwrite"), []string{"-dFirstPage=1", "-dFirstPage=2", "-dFirstPage=3", "-sPageList=1,2,3"}; !slices.Equal(got, want) {
		t.Errorf("text processes %q, want %q", got, want)
	}
	if got, want := f.processes("png16m"), []string{"-dFirstPage=1", "-dFirstPage=3"}; !slices.Equal(got, want) {
		t.Errorf("render processes %q, want %q", got, want)
	}
}

// TestImagesMaxPixels checks a request's MaxPixels: the pages' sizes are read in the one gs run
// that counts them, and a page past it at the request's DPI renders at the largest DPI that
// fits, a letter page's 1275x1650 at 150 dpi, 2103750 pixels, at 149 dpi with one pixel fewer,
// rounded as gs rounds its device's sides; a page within it at the request's DPI, a rotated page
// read as its rotated size; and a page past it at 1 dpi fails.
func TestImagesMaxPixels(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 0)
	f.gs.set(func(s *settings) { s.Sizes = []string{"612 792", "612.0 396.0", "792 612", "1e9 1e9"} })
	f.req.MaxPixels = 1275*1650 - 1
	pages, errs := f.images(t.Context())
	if len(pages) != 4 {
		t.Fatalf("the stream yielded %+v, %v; want 4 pages", pages, errs)
	}
	for i, want := range []int{149, 150, 149} {
		if errs[i] != nil || pages[i].DPI != want {
			t.Errorf("page %d: %+v, %v; want %d dpi", i+1, pages[i], errs[i], want)
		}
	}
	if errs[3] == nil || pages[3].Path != "" || !strings.Contains(errs[3].Error(), "page 4 of 4") {
		t.Errorf("page 4, past MaxPixels at 1 dpi: %+v, %v; want it failed", pages[3], errs[3])
	}
	if _, dpi := f.rendered(); !slices.Equal(dpi, []int{149, 150, 149}) {
		t.Errorf("rendered at %v dpi, want 149, 150, and 149", dpi)
	}
	runs := f.gs.runs()
	if script := runs[0].Args[len(runs[0].Args)-1]; !slices.Contains(runs[0].Args, "-dNODISPLAY") ||
		!strings.Contains(script, "pdfshowpage_setpage") || !strings.Contains(script, "pdfpagecount") {
		t.Errorf("the first run %q, want the count and sizes run", runs[0].Args)
	}
	if n := len(slices.DeleteFunc(runs, func(r standinRun) bool { return !slices.Contains(r.Args, "-dNODISPLAY") })); n != 1 {
		t.Errorf("%d count runs, want the one sizes run", n)
	}

	// Exactly the pixels of 150 dpi is within the bound.
	f = newFixture(t, 1)
	f.req.MaxPixels = 1275 * 1650
	if pages, errs := f.images(t.Context()); errs[0] != nil || pages[0].DPI != 150 {
		t.Errorf("MaxPixels exactly a letter page's at 150 dpi: %+v, %v", pages, errs)
	}

	// Output the sizes run can't read fails the count.
	f = newFixture(t, 0)
	f.gs.set(func(s *settings) { s.Sizes = []string{"612"} })
	f.req.MaxPixels = 1
	if pages, errs := f.images(t.Context()); len(pages) != 1 || !zero(pages[0]) || errs[0] == nil {
		t.Errorf("an unreadable size: %+v, %v; want the stream's one error", pages, errs)
	}
}

// TestImagesShared checks the pages share gs processes: with concurrency C, lane k takes pages k,
// k+C, k+2C, and so on, up to PagesPerProcess of them a process, through gs's page list, its text
// in one process before its render in another; and every page, yielded in page order, holds the
// PNG and text a process of its own gives, at PagesPerProcess 1, 2, 4, which divides neither 7
// pages nor a lane's 3, and 10, past the count, at concurrency 1 and 3. Zero PagesPerProcess is
// the default, past 7 pages.
func TestImagesShared(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		per, concurrency int
		want             []string
	}{
		{1, 1, []string{"1", "2", "3", "4", "5", "6", "7"}},
		{2, 1, []string{"1,2", "3,4", "5,6", "7"}},
		{4, 1, []string{"1,2,3,4", "5,6,7"}},
		{10, 1, []string{"1,2,3,4,5,6,7"}},
		{0, 1, []string{"1,2,3,4,5,6,7"}},
		{1, 3, []string{"1", "2", "3", "4", "5", "6", "7"}},
		{2, 3, []string{"1,4", "7", "2,5", "3,6"}},
		{4, 3, []string{"1,4,7", "2,5", "3,6"}},
		{10, 3, []string{"1,4,7", "2,5", "3,6"}},
	} {
		f := newFixture(t, 7)
		f.req.PagesPerProcess, f.req.Concurrency, f.req.Text = tc.per, tc.concurrency, true
		pages, errs := f.images(t.Context())
		name := fmt.Sprintf("PagesPerProcess %d, concurrency %d", tc.per, tc.concurrency)
		if len(pages) != 7 {
			t.Fatalf("%s: the stream yielded %+v, %v; want 7 pages", name, pages, errs)
		}
		for i, p := range pages {
			want := document.PageImage{Number: i + 1, Count: 7, Path: filepath.Join(f.req.Dir, fmt.Sprintf("page-%d.png", i+1)), DPI: 150, Text: standinText(i + 1)}
			if errs[i] != nil || p != want {
				t.Errorf("%s: yield %d: %+v, %v; want %+v", name, i+1, p, errs[i], want)
				continue
			}
			if b, err := os.ReadFile(p.Path); err != nil || !bytes.Equal(b, standinPage(p.Number, 150)) {
				t.Errorf("%s: page %d's file holds %q, %v; want its own page", name, p.Number, b, err)
			}
		}
		var want []string
		for _, list := range tc.want {
			want = append(want, "-sPageList="+list)
		}
		slices.Sort(want)
		if got := f.processes("png16m"); !slices.Equal(got, want) {
			t.Errorf("%s: render processes %q, want %q", name, got, want)
		}
		if got := f.processes("txtwrite"); !slices.Equal(got, want) {
			t.Errorf("%s: text processes %q, want %q", name, got, want)
		}
	}
}

// TestImagesSharedTextFiles checks the shared text processes write their files in a directory
// of the stream's own, neither the PDF's nor the request's, a file a page numbered within the
// process, and that the directory is gone once the stream ends.
func TestImagesSharedTextFiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 5)
	f.req.Text, f.req.Concurrency, f.req.PagesPerProcess = true, 2, 2
	for _, err := range f.prep.Images(t.Context(), f.pdf, f.req) {
		if err != nil {
			t.Fatal(err)
		}
	}
	var outs []string
	for _, r := range f.gs.runs() {
		if slices.Contains(r.Args, "-sDEVICE=txtwrite") {
			for _, a := range r.Args {
				if v, ok := strings.CutPrefix(a, "-sOutputFile="); ok {
					outs = append(outs, v)
				}
			}
		}
	}
	if len(outs) != 3 {
		t.Fatalf("text processes wrote to %q, want 3", outs)
	}
	for _, out := range outs {
		dir := filepath.Dir(out)
		if filepath.Base(out) != "page-%d.txt" || dir == filepath.Dir(f.pdf) || dir == f.req.Dir ||
			strings.HasPrefix(dir, filepath.Dir(f.pdf)+string(filepath.Separator)) || strings.HasPrefix(dir, f.req.Dir+string(filepath.Separator)) {
			t.Errorf("a text process wrote to %s, want a file a page in a directory of the stream's own", out)
		}
		if _, err := os.Stat(filepath.Dir(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the text scratch %s is left: %v", filepath.Dir(dir), err)
		}
	}
	if got := files(t, filepath.Dir(f.pdf)); !slices.Equal(got, []string{"doc.pdf"}) {
		t.Errorf("the PDF's directory holds %q, want the PDF alone", got)
	}
}

// TestImagesSharedFallback checks a shared process that fails partway as the content's: the pages
// it handed over stand, each page it didn't gets processes of its own, its text and its render,
// and only the page that fails there fails, with the error naming gs's reason, its file removed.
func TestImagesSharedFallback(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 7)
	f.gs.set(func(s *settings) { s.FailPage = 4 })
	f.req.Text = true
	pages, errs := f.images(t.Context())
	if len(pages) != 7 {
		t.Fatalf("the stream yielded %+v, %v; want 7 pages", pages, errs)
	}
	for i, p := range pages {
		if i == 3 {
			if p != (document.PageImage{Number: 4, Count: 7}) || errs[i] == nil ||
				!strings.Contains(errs[i].Error(), "page 4 of 7: render") || !strings.Contains(errs[i].Error(), "told to fail") {
				t.Errorf("page 4: %+v, %v; want it failing with gs's reason", p, errs[i])
			}
			continue
		}
		if b, err := os.ReadFile(p.Path); errs[i] != nil || err != nil || !bytes.Equal(b, standinPage(i+1, 150)) || p.Text != standinText(i+1) {
			t.Errorf("page %d: %+v, %v, file %q; want its own page and text", i+1, p, errs[i], b)
		}
	}
	if got, want := f.processes("png16m"), []string{"-dFirstPage=4", "-dFirstPage=5", "-dFirstPage=6", "-dFirstPage=7", "-sPageList=1,2,3,4,5,6,7"}; !slices.Equal(got, want) {
		t.Errorf("render processes %q, want %q", got, want)
	}
	if got, want := f.processes("txtwrite"), []string{"-dFirstPage=4", "-dFirstPage=5", "-dFirstPage=6", "-dFirstPage=7", "-sPageList=1,2,3,4,5,6,7"}; !slices.Equal(got, want) {
		t.Errorf("text processes %q, want %q", got, want)
	}
	if got := files(t, f.req.Dir); !slices.Equal(got, []string{"page-1.png", "page-2.png", "page-3.png", "page-5.png", "page-6.png", "page-7.png"}) {
		t.Errorf("the directory holds %q, want every page's but page 4's", got)
	}
}

// TestImagesSharedRunFailure checks a shared process that fails as the run's, gs killed by a
// SIGKILL docproc didn't send, falls back to nothing: the pages it handed over stand, and each
// page it didn't fails with its error, no process of its own run.
func TestImagesSharedRunFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t, 5)
	f.gs.set(func(s *settings) { s.KillPage = 3 })
	pages, errs := f.images(t.Context())
	if len(pages) != 5 || errs[0] != nil || errs[1] != nil {
		t.Fatalf("the stream yielded %+v, %v; want 5 pages, the first two prepared", pages, errs)
	}
	for i := 2; i < 5; i++ {
		var exit *tool.ExitError
		if !errors.As(errs[i], &exit) || exit.Signal != "SIGKILL" || pages[i].Path != "" ||
			!strings.Contains(errs[i].Error(), fmt.Sprintf("page %d of 5: render", i+1)) {
			t.Errorf("page %d: %+v, %v; want gs's SIGKILL", i+1, pages[i], errs[i])
		}
	}
	if got, want := f.processes("png16m"), []string{"-sPageList=1,2,3,4,5"}; !slices.Equal(got, want) {
		t.Errorf("render processes %q, want %q", got, want)
	}
	if got := files(t, f.req.Dir); !slices.Equal(got, []string{"page-1.png", "page-2.png"}) {
		t.Errorf("the directory holds %q, want pages 1 and 2", got)
	}
}

// TestImagesMaxPixelsOwnProcess checks a page MaxPixels renders at a lower DPI gets a process of
// its own, at that DPI, the lane's run of pages at the request's DPI broken around it, at
// concurrency 1 and 2: page 3, a letter page, at 149 dpi, the half-letter pages at 150.
func TestImagesMaxPixelsOwnProcess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		concurrency int
		want        []string
	}{
		{1, []string{"-dFirstPage=3", "-sPageList=1,2", "-sPageList=4,5"}},
		{2, []string{"-dFirstPage=3", "-sPageList=1", "-sPageList=2,4", "-sPageList=5"}},
	} {
		f := newFixture(t, 0)
		f.gs.set(func(s *settings) { s.Sizes = []string{"612 396", "612 396", "612 792", "612 396", "612 396"} })
		f.req.MaxPixels, f.req.Concurrency = 1275*1650-1, tc.concurrency
		pages, errs := f.images(t.Context())
		for i, p := range pages {
			dpi := 150
			if i == 2 {
				dpi = 149
			}
			if b, err := os.ReadFile(p.Path); errs[i] != nil || err != nil || p.DPI != dpi || !bytes.Equal(b, standinPage(i+1, dpi)) {
				t.Errorf("concurrency %d: page %d: %+v, %v; want it at %d dpi", tc.concurrency, i+1, p, errs[i], dpi)
			}
		}
		if got := f.processes("png16m"); !slices.Equal(got, tc.want) {
			t.Errorf("concurrency %d: render processes %q, want %q", tc.concurrency, got, tc.want)
		}
	}
}

// TestImagesIdleBound checks a shared process's bound is the Spec's Timeout read as an idle bound
// between its pages: a process whose pages each take most of the bound, and whose consumer holds
// a page for three times the bound, runs to its end, the time it waits on the consumer not idle;
// and a page that takes past the bound ends its shared process, the pages before it standing, and
// fails in its own process, which takes the bound whole, the pages after it rendered in their own.
func TestImagesIdleBound(t *testing.T) {
	t.Parallel()
	const bound = time.Second
	f := newTimedFixture(t, 4, bound)
	f.gs.set(func(s *settings) { s.Delay = map[int]int{1: 600, 2: 600, 3: 600, 4: 600} })
	for p, err := range f.prep.Images(t.Context(), f.pdf, f.req) {
		if err != nil {
			t.Fatalf("page %d: %v", p.Number, err)
		}
		if p.Number == 1 {
			time.Sleep(3 * bound)
		}
	}
	if got, want := f.processes("png16m"), []string{"-sPageList=1,2,3,4"}; !slices.Equal(got, want) {
		t.Errorf("render processes %q, want %q", got, want)
	}

	f = newTimedFixture(t, 4, bound)
	f.gs.set(func(s *settings) { s.Delay = map[int]int{3: 2500} })
	pages, errs := f.images(t.Context())
	if len(pages) != 4 || errs[0] != nil || errs[1] != nil || errs[3] != nil {
		t.Fatalf("the stream yielded %+v, %v; want page 3 alone failing", pages, errs)
	}
	if exit, ok := errors.AsType[*tool.ExitError](errs[2]); !ok || !exit.TimedOut || !strings.Contains(errs[2].Error(), "page 3 of 4: render") {
		t.Errorf("page 3: %v; want its own process timed out", errs[2])
	}
	if got, want := f.processes("png16m"), []string{"-dFirstPage=3", "-dFirstPage=4", "-sPageList=1,2,3,4"}; !slices.Equal(got, want) {
		t.Errorf("render processes %q, want %q", got, want)
	}
}
