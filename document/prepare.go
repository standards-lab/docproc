package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/standards-lab/docproc/tool"
)

// DefaultDPI is the DPI a request of zero DPI renders its pages at.
const DefaultDPI = 150

// Preparer prepares a PDF's pages for a model's vision: each page rendered to a PNG file, and its
// text layer read when a request says to. The composition root builds one from its Ghostscript,
// and its methods are safe for concurrent use. It runs no inference: a layer above sends the pages
// it yields to a model.
type Preparer struct {
	render *Ghostscript
}

// NewPreparer builds a Preparer over cfg's Renderer; it reads no other field. A nil Renderer is
// the caller's mistake, and panics naming the field.
func NewPreparer(cfg Config) *Preparer {
	if cfg.Renderer == nil {
		panic("document: Config.Renderer is nil")
	}
	return &Preparer{render: cfg.Renderer}
}

// ImagesRequest is one PDF's run through a Preparer's Images.
type ImagesRequest struct {
	// Dir is the directory each page's PNG is written into, as page-N.png, N its number, which
	// the caller owns. It is required; an empty Dir is refused rather than taken as the system's
	// temporary directory.
	Dir string `json:"-"`
	// DPI is the DPI each page is rendered at. Zero means DefaultDPI; a negative DPI is refused.
	DPI int `json:"dpi"`
	// Concurrency is how many pages are held at once, each from the start of its preparation
	// until the consumer takes its outcome; it is at least 1, and anything less is refused.
	Concurrency int `json:"-"`
	// PagesPerProcess is the most pages one gs process renders, and reads the text of, for a
	// lane of pages that share processes, as Images says. Zero means a default of 32; a negative
	// PagesPerProcess is refused. Like Concurrency, it changes how the pages are prepared, never
	// what they are, so it is outside the request's identity.
	PagesPerProcess int `json:"-"`
	// Text reads each page's text layer, before rendering it, into its PageImage's Text.
	Text bool `json:"text"`
	// MaxPixels is the most pixels a page's PNG may hold, its width times its height: a page that
	// would hold more at DPI is rendered at the largest DPI at which it holds no more, and fails
	// when not even 1 dpi does. Zero is no bound; a negative MaxPixels is refused.
	MaxPixels int64 `json:"max_pixels"`
}

// PageImage is one prepared page: its number, 1-based, the PDF's page count, the path of its PNG
// file in the request's Dir and the DPI it was rendered at, and its text layer when the request
// reads it. The file is the caller's once the page is yielded. Path is "" and DPI 0 for a page
// that failed.
type PageImage struct {
	Number int
	Count  int
	Path   string
	DPI    int
	// Text is the page's text layer, as Ghostscript's Text reads it, for a request with Text; ""
	// otherwise. A page without a text layer has text HasText reports as none.
	Text string
}

// Images returns the stream of the pages of the PDF at path, which the caller owns, prepared as
// req says. The stream is lazy: nothing runs, not even the request's validation, until it is
// ranged over.
//
// Ranging over it validates req, counts the pages, with each page's size when req has a
// MaxPixels, and then, for every page from 1 to the count, reads its text layer when req says to,
// and renders the page at req.DPI, or the largest DPI under req.MaxPixels, to its file in
// req.Dir. At most req.Concurrency pages are held at once, a page's slot released only when the
// consumer takes its outcome, so the pages rendered ahead of the consumer are bounded. Pages are
// yielded in page order, a page that finishes early held until the pages before it are yielded.
//
// The pages share gs processes. With concurrency C, the pages are dealt to C lanes, lane k taking
// pages k, k+C, k+2C, and so on; each lane runs one gs process at a time over up to
// req.PagesPerProcess of its pages, through gs's page list, the PNGs split from its stdout, and,
// when req reads text, one process before it that writes the pages' text, a file a page, in a
// temporary directory of the Preparer's own, never the PDF's nor req.Dir, read back a page at a
// time and removed. A page MaxPixels renders at a lower DPI gets a process of its own, since gs
// renders one resolution a run. Each page's PNG and text are byte for byte what a process of its
// own gives. A shared process's bound is the gs Tool's Timeout read as an idle bound, between
// its writes, and never counting time spent waiting on the consumer; a process of a page's own
// takes it whole, as a unit's.
//
// A shared process that fails as the content's, gs exiting nonzero, dying by its own fault,
// passing its bound, or writing output that doesn't hold its pages, keeps the pages it handed
// over, and each page it didn't is prepared in processes of its own, so only a page that fails
// there fails. One that fails as the run's, its context ended or a signal docproc didn't send,
// fails each page it didn't hand over with its error.
//
// A page that fails, in reading its text or rendering, yields the PageImage's number and count
// with the error, which names the page and wraps its cause, its file removed, and the stream
// carries on. A refused request or a failed count yields one error with a zero PageImage, and the
// stream ends. When ctx ends, the stream yields ctx's error once, with a zero PageImage, and ends.
//
// Breaking out of the range, or ctx ending, cancels the pages in flight, and the loop returns once
// their work has ended, every page file not yet yielded removed.
func (p *Preparer) Images(ctx context.Context, path string, req ImagesRequest) iter.Seq2[PageImage, error] {
	return func(yield func(PageImage, error) bool) {
		if err := check(&req); err != nil {
			yield(PageImage{}, err)
			return
		}
		if err := ctx.Err(); err != nil {
			yield(PageImage{}, err)
			return
		}
		p.pages(ctx, path, req, yield)
	}
}

// defaultPagesPerProcess is the most pages one gs process renders for a request of zero
// PagesPerProcess.
const defaultPagesPerProcess = 32

// outcome is one page's file, its DPI, and its text, or its error, as its lane hands it to the
// stream.
type outcome struct {
	path string
	dpi  int
	text string
	err  error
}

// pages counts the pages of the PDF at path and prepares them, yielding each in page order, req
// already checked and defaulted. It returns once every page's work has ended.
//
// The pages are dealt to req.Concurrency lanes, lane k taking pages k, k+C, k+2C, and so on, C
// the concurrency, each lane preparing its own in order, one gs process at a time, as lane does.
// Each page's outcome goes to a channel of its page's own, buffered so a lane never waits on the
// consumer to hand one over. A page is held from the moment its lane starts writing it until the
// consumer takes its outcome, and may start only once the page C before it is taken: its turn.
// That page is its own lane's last, so each lane holds at most one page, and the stream at most C,
// a later page that finishes first waiting in its channel. Once the consumer stops, the outcomes
// it never took have their files removed.
func (p *Preparer) pages(ctx context.Context, path string, req ImagesRequest, yield func(PageImage, error) bool) {
	sizes, err := p.count(ctx, path, req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			err = cerr
		}
		yield(PageImage{}, err)
		return
	}
	n := len(sizes)
	r := &run{p: p, path: path, n: n, sizes: sizes, req: req, turns: make([]chan struct{}, n), done: make([]chan outcome, n)}
	if req.Text && p.render.sharedText() {
		// The run's scratch, which the shared text runs write in: the Preparer's own, never the
		// input's directory nor the caller's.
		if r.scratch, err = os.MkdirTemp("", "docproc-text-*"); err != nil {
			yield(PageImage{}, fmt.Errorf("document: text scratch: %w", err))
			return
		}
		defer func() { _ = os.RemoveAll(r.scratch) }()
	}
	work, cancel := context.WithCancel(ctx)
	r.ctx = work
	for i := range n {
		r.done[i] = make(chan outcome, 1)
		r.turns[i] = make(chan struct{})
		if i < req.Concurrency {
			close(r.turns[i])
		}
	}
	var wg sync.WaitGroup
	taken := 0
	defer func() {
		cancel()
		wg.Wait()
		for _, c := range r.done[taken:] {
			select {
			case o := <-c:
				remove(o.path)
			default:
			}
		}
	}()
	for k := range min(req.Concurrency, n) {
		wg.Go(func() { r.lane(k + 1) })
	}
	for i := range n {
		var o outcome
		select {
		case o = <-r.done[i]:
			taken++
			if next := i + req.Concurrency; next < n {
				close(r.turns[next])
			}
		case <-ctx.Done():
		}
		// A page that failed because ctx ended is ctx's error, not the page's.
		if err := ctx.Err(); err != nil {
			remove(o.path)
			yield(PageImage{}, err)
			return
		}
		if !yield(PageImage{Number: i + 1, Count: n, Path: o.path, DPI: o.dpi, Text: o.text}, o.err) {
			return
		}
	}
}

// count returns a size for each page of the PDF at path: each page's own when req has a MaxPixels,
// which needs it, and zero sizes, from the page count alone, when it doesn't.
func (p *Preparer) count(ctx context.Context, path string, req ImagesRequest) ([]pageSize, error) {
	if req.MaxPixels > 0 {
		return p.render.sizes(ctx, path)
	}
	n, err := p.render.Count(ctx, path)
	if err != nil {
		return nil, err
	}
	return make([]pageSize, n), nil
}

// run is one stream's preparation of the n pages of the PDF at path, their sizes sizes, under
// req, already checked: its context, which ends when the stream does, each page's turn, closed
// once it may start, and each page's outcome channel. scratch is the directory the shared text
// runs write in, "" when they don't run.
type run struct {
	p       *Preparer
	ctx     context.Context
	path    string
	n       int
	sizes   []pageSize
	req     ImagesRequest
	scratch string
	turns   []chan struct{}
	done    []chan outcome
}

// lane prepares the pages of lane k, from 1: pages k, k+C, k+2C, and so on, C the concurrency, in
// order. The pages that render at the request's DPI go through shared processes, as shared
// prepares them, up to the request's PagesPerProcess in each, a run of them broken by any page
// that doesn't; a page MaxPixels renders at a lower DPI, or that fits at none, gets a process of
// its own, as single prepares it, since gs renders one resolution a run.
//
// When a shared process fails as the content's, by contentFailure, the pages it handed over
// stand, and each page it didn't is prepared by single, alone, so only a page that fails there
// fails. Any other failure is the run's: each page it didn't hand over fails with it, and the
// stage above ends on the first. A lane returns once its pages are all handed over, or its context
// has ended.
func (r *run) lane(k int) {
	var batch []int
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		pages := batch
		batch = nil
		rest, step, err := r.shared(pages)
		switch {
		case err == nil:
			return true
		case r.ctx.Err() != nil:
			return false
		case contentFailure(err):
			for _, page := range rest {
				if !r.single(page) {
					return false
				}
			}
		default:
			for _, page := range rest {
				r.done[page-1] <- outcome{err: fmt.Errorf("document: page %d of %d: %s: %w", page, r.n, step, err)}
			}
		}
		return true
	}
	for page := k; page <= r.n; page += r.req.Concurrency {
		if r.req.MaxPixels > 0 {
			if dpi, ok := r.sizes[page-1].fit(r.req.DPI, r.req.MaxPixels); !ok || dpi != r.req.DPI {
				if !flush() || !r.single(page) {
					return
				}
				continue
			}
		}
		if batch = append(batch, page); len(batch) == r.req.PagesPerProcess && !flush() {
			return
		}
	}
	flush()
}

// turn waits for page's turn, and reports false when the run's context ends first.
func (r *run) turn(page int) bool {
	select {
	case <-r.turns[page-1]:
		return true
	case <-r.ctx.Done():
		return false
	}
}

// single prepares page in a process of its own, once it is its turn, as page does, and hands over
// its outcome. It reports false when the run's context ended first.
func (r *run) single(page int) bool {
	if !r.turn(page) {
		return false
	}
	r.done[page-1] <- r.p.page(r.ctx, r.path, page, r.n, r.sizes[page-1], r.req)
	return true
}

// shared prepares pages, in order and all at the request's DPI, through shared processes: when
// the request reads text, one gs run writes every page's text, then one gs run renders every page,
// each PNG, split from its stdout, written to its page's file once it is the page's turn and
// handed over with the page's text. It returns the pages it didn't hand over, the step that
// failed, "text" or "render", and its error; nil once every page is handed over. A page whose own
// file fails, such as one already in the directory, is handed over as failed, as page fails it.
func (r *run) shared(pages []int) ([]int, string, error) {
	texts, err := r.texts(pages)
	if err != nil {
		return pages, "text", err
	}
	defer texts.close()
	sink := &pageSink{r: r, pages: pages, texts: texts}
	whole, err := r.p.render.renderPages(r.ctx, r.path, pages, r.req.DPI, sink)
	return pages[whole:], "render", err
}

// texts reads the text of pages when the request says to: in one shared gs run, its files in a
// directory of the run's scratch, through the exec runner; a page a run, through Text, through the
// container runner, whose runs can't write outside the PDF's directory.
func (r *run) texts(pages []int) (*pageTexts, error) {
	t := &pageTexts{}
	switch {
	case !r.req.Text:
	case r.scratch == "":
		t.text = make([]string, len(pages))
		for i, page := range pages {
			var err error
			if t.text[i], err = r.p.render.Text(r.ctx, r.path, page); err != nil {
				return nil, err
			}
		}
	default:
		dir, err := os.MkdirTemp(r.scratch, "run-*")
		if err != nil {
			return nil, fmt.Errorf("document: text scratch: %w", err)
		}
		if err := r.p.render.textPages(r.ctx, r.path, pages, dir); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		t.dir = dir
	}
	return t, nil
}

// pageTexts are a shared process's pages' texts: in memory, in text; in files, a page each, in
// dir, as textPages writes them; or none, both empty, for a request without Text.
type pageTexts struct {
	text []string
	dir  string
}

// read returns the text of the run's i-th page, from 0, removing its file.
func (t *pageTexts) read(i int) (string, error) {
	switch {
	case t.text != nil:
		return t.text[i], nil
	case t.dir == "":
		return "", nil
	}
	file := textFile(t.dir, i+1)
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(b), os.Remove(file)
}

// close removes the texts' directory, with the files of the pages never handed over.
func (t *pageTexts) close() {
	if t.dir != "" {
		_ = os.RemoveAll(t.dir)
	}
}

// pageSink writes each PNG of a shared process to its page's file, page-N.png in the request's
// Dir, once it is the page's turn, and hands the page over with its text once the PNG is whole.
type pageSink struct {
	r     *run
	pages []int
	texts *pageTexts
	// f is the page's file, nil when it couldn't be created, file its path, and err the page's
	// own failure: its file's creation, a write, or its close.
	f    *os.File
	file string
	err  error
}

func (s *pageSink) begin(i int) (io.Writer, error) {
	page := s.pages[i]
	if !s.r.turn(page) {
		return nil, s.r.ctx.Err()
	}
	s.file = filepath.Join(s.r.req.Dir, "page-"+strconv.Itoa(page)+".png")
	s.f, s.err = os.OpenFile(s.file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	return s, nil
}

// Write writes p to the page's file until a write fails, keeping the failure, and never fails
// itself, so one page's failure never stops the process's other pages.
func (s *pageSink) Write(p []byte) (int, error) {
	if s.err == nil {
		_, s.err = s.f.Write(p)
	}
	return len(p), nil
}

func (s *pageSink) end(i int) {
	page := s.pages[i]
	step, err := "render", s.close()
	var text string
	if err == nil {
		step = "text"
		text, err = s.texts.read(i)
	}
	if err != nil {
		s.removeFile()
		s.r.done[page-1] <- outcome{err: fmt.Errorf("document: page %d of %d: %s: %w", page, s.r.n, step, err)}
		return
	}
	s.r.done[page-1] <- outcome{path: s.file, dpi: s.r.req.DPI, text: text}
}

func (s *pageSink) abandon(int) {
	_ = s.close()
	s.removeFile()
}

// close closes the page's file, when it was created, and returns the page's failure.
func (s *pageSink) close() error {
	err := s.err
	if s.f != nil {
		err = errors.Join(err, s.f.Close())
	}
	return err
}

// removeFile removes the page's file when the sink created it; a file that was already there,
// failing the page, is the caller's.
func (s *pageSink) removeFile() {
	if s.f != nil {
		remove(s.file)
	}
}

// faultSignals are the signals gs dies by through its own fault, which the stage above reads as
// the content's: a bad access, an abort, a bus error, an arithmetic fault, or an illegal
// instruction, an allocation refused under the memory bound among them.
var faultSignals = []string{"SIGSEGV", "SIGABRT", "SIGBUS", "SIGFPE", "SIGILL"}

// contentFailure reports whether err, a shared process's failure, is the content's, as the stage
// above classifies a gs run's, so its pages fall back to a process each: gs exiting nonzero,
// dying by its own fault, or passing its bound, or output that doesn't hold its pages. The rest
// are the run's, a context ended, a signal docproc didn't send, a program that didn't start, or
// docproc's own side failing, which a page's own process wouldn't change.
func contentFailure(err error) bool {
	if errors.Is(err, errOutput) {
		return true
	}
	exit, ok := errors.AsType[*tool.ExitError](err)
	return ok && (exit.TimedOut || exit.Signal == "" || slices.Contains(faultSignals, exit.Signal))
}

// page prepares page of the n pages of the PDF at path, its size size, req already checked, in
// processes of its own: it reads the page's text when req says to, and renders the page to its
// file in req.Dir, at req.DPI or the largest DPI under req.MaxPixels. Its error names the page,
// and a page that fails leaves no file. Its runs take the Tool's Timeout whole, a unit's.
func (p *Preparer) page(ctx context.Context, path string, page, n int, size pageSize, req ImagesRequest) outcome {
	fail := func(step string, err error) outcome {
		return outcome{err: fmt.Errorf("document: page %d of %d: %s: %w", page, n, step, err)}
	}
	var text string
	if req.Text {
		var err error
		if text, err = p.render.Text(ctx, path, page); err != nil {
			return fail("text", err)
		}
	}
	dpi := req.DPI
	if req.MaxPixels > 0 {
		fits, ok := size.fit(dpi, req.MaxPixels)
		if !ok {
			return fail("render", fmt.Errorf("%gx%g points past %d pixels at 1 dpi", size.width, size.height, req.MaxPixels))
		}
		dpi = fits
	}
	png := filepath.Join(req.Dir, "page-"+strconv.Itoa(page)+".png")
	f, err := os.OpenFile(png, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fail("render", err)
	}
	err = p.render.Render(ctx, path, page, dpi, f)
	if err = errors.Join(err, f.Close()); err != nil {
		remove(png)
		return fail("render", err)
	}
	return outcome{path: png, dpi: dpi, text: text}
}

// remove removes the page file at path, when there is one. The file is the stream's own, written
// in this run, so failing to remove it leaves a file in the caller's directory and nothing else.
func remove(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// check refuses a request Images can't run, before anything runs, and sets its defaults: a DPI of
// DefaultDPI for zero, and a PagesPerProcess of defaultPagesPerProcess for zero.
func check(req *ImagesRequest) error {
	switch {
	case req.Dir == "":
		return fmt.Errorf("document: request has no directory: %w", ErrRequest)
	case req.DPI < 0:
		return fmt.Errorf("document: request's %d dpi is negative: %w", req.DPI, ErrRequest)
	case req.Concurrency < 1:
		return fmt.Errorf("document: request's concurrency %d is below 1: %w", req.Concurrency, ErrRequest)
	case req.MaxPixels < 0:
		return fmt.Errorf("document: request's MaxPixels %d is negative: %w", req.MaxPixels, ErrRequest)
	case req.PagesPerProcess < 0:
		return fmt.Errorf("document: request's PagesPerProcess %d is negative: %w", req.PagesPerProcess, ErrRequest)
	}
	if req.DPI == 0 {
		req.DPI = DefaultDPI
	}
	if req.PagesPerProcess == 0 {
		req.PagesPerProcess = defaultPagesPerProcess
	}
	return nil
}
