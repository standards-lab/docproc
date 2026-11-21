package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/standards-lab/docproc/internal/cli"
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
	// lane of pages that share processes, as Images says. Zero means a default of 8; a negative
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
// req.PagesPerProcess of its pages, and no more than gs takes in one argument's page list, through
// gs's page list, the PNGs split from its stdout, and, when req reads text, one process before it
// that writes the pages' text, a file a page, in a temporary directory of the Preparer's own,
// never the PDF's nor req.Dir, read back a page at a time and removed. A page MaxPixels renders at
// a lower DPI gets a process of its own, since gs renders one resolution a run, as does a lane's
// lone page, and every page of a PDF portfolio, whose shared processes' output gs writes from
// its embedded documents' pages rather than a page's own; a process of a page's own gives a
// portfolio's page as its cover's own, one PNG and that page's text. Each page's PNG and text are
// byte for byte what a process of its own gives. Through the exec runner, the run that counts the
// pages and the shared processes work in that temporary directory; a page's own processes work
// in the PDF's directory, as Render and Text do. Every run's TMPDIR, where gs keeps its temporary
// files, is the exec runner's own directory for the run, removed once gs is reaped, so a gs ended
// early leaves none beside the PDF. A shared process's bound is the gs Tool's Timeout read as an
// idle bound, between its writes, and never counting time spent waiting on the consumer; a
// process of a page's own takes it whole, as a unit's.
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

const defaultPagesPerProcess = 8

type outcome struct {
	path string
	dpi  int
	text string
	err  error
}

func (p *Preparer) pages(ctx context.Context, path string, req ImagesRequest, yield func(PageImage, error) bool) {
	var scratch string
	if p.render.ownDir() {
		var err error
		if scratch, err = os.MkdirTemp("", "docproc-gs-*"); err != nil {
			yield(PageImage{}, fmt.Errorf("document: gs scratch: %w", err))
			return
		}
		defer func() { _ = os.RemoveAll(scratch) }()
	}
	sv, err := p.render.survey(ctx, path, scratch, req.MaxPixels > 0)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			err = cerr
		}
		yield(PageImage{}, err)
		return
	}
	n := len(sv.sizes)
	r := &run{p: p, path: path, n: n, sizes: sv.sizes, portfolio: sv.portfolio, req: req, scratch: scratch, turns: make([]chan struct{}, n), done: make([]chan outcome, n)}
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

// See docs/api.md, "Running the tools".
type run struct {
	p         *Preparer
	ctx       context.Context
	path      string
	n         int
	sizes     []pageSize
	portfolio bool
	req       ImagesRequest
	scratch   string
	turns     []chan struct{}
	done      []chan outcome
}

func (r *run) lane(k int) {
	var batch []int
	flush := func() bool {
		pages := batch
		batch = nil
		switch len(pages) {
		case 0:
			return true
		case 1:
			return r.single(pages[0])
		}
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
		if !r.shares(page) {
			if !flush() || !r.single(page) {
				return
			}
			continue
		}
		if len(batch) > 0 && !listFits(batch, page) && !flush() {
			return
		}
		batch = append(batch, page)
		if len(batch) == r.req.PagesPerProcess && !flush() {
			return
		}
	}
	flush()
}

func (r *run) shares(page int) bool {
	if r.portfolio {
		return false
	}
	if r.req.MaxPixels > 0 {
		dpi, ok := r.sizes[page-1].fit(r.req.DPI, r.req.MaxPixels)
		return ok && dpi == r.req.DPI
	}
	return true
}

func (r *run) turn(page int) bool {
	select {
	case <-r.turns[page-1]:
		return true
	case <-r.ctx.Done():
		return false
	}
}

func (r *run) single(page int) bool {
	if !r.turn(page) {
		return false
	}
	r.done[page-1] <- r.p.page(r.ctx, r.path, page, r.n, r.sizes[page-1], r.req)
	return true
}

func (r *run) shared(pages []int) ([]int, string, error) {
	texts, err := r.texts(pages)
	if err != nil {
		return pages, "text", err
	}
	defer texts.close()
	sink := &pageSink{r: r, pages: pages, texts: texts}
	whole, err := r.p.render.renderPages(r.ctx, r.path, r.scratch, pages, r.req.DPI, sink)
	return pages[whole:], "render", err
}

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

type pageTexts struct {
	text []string
	dir  string
}

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

func (t *pageTexts) close() {
	if t.dir != "" {
		_ = os.RemoveAll(t.dir)
	}
}

type pageSink struct {
	r     *run
	pages []int
	texts *pageTexts
	f     *os.File
	file  string
	err   error
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

func (s *pageSink) close() error {
	err := s.err
	if s.f != nil {
		err = errors.Join(err, s.f.Close())
	}
	return err
}

func (s *pageSink) removeFile() {
	if s.f != nil {
		remove(s.file)
	}
}

func contentFailure(err error) bool {
	if errors.Is(err, errOutput) {
		return true
	}
	exit, ok := errors.AsType[*tool.ExitError](err)
	return ok && (exit.TimedOut || exit.Signal == "" || cli.OwnFault(exit.Signal))
}

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

func remove(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

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
