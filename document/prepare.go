package document

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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
	Dir string
	// DPI is the DPI each page is rendered at. Zero means DefaultDPI; a negative DPI is refused.
	DPI int
	// Concurrency is how many pages are held at once, each from the start of its preparation
	// until the consumer takes its outcome; it is at least 1, and anything less is refused.
	Concurrency int
	// Text reads each page's text layer, before rendering it, into its PageImage's Text.
	Text bool
	// MaxPixels is the most pixels a page's PNG may hold, its width times its height: a page that
	// would hold more at DPI is rendered at the largest DPI at which it holds no more, and fails
	// when not even 1 dpi does. Zero is no bound; a negative MaxPixels is refused.
	MaxPixels int64
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

// outcome is one page's file, its DPI, and its text, or its error, as its worker hands it to the
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
// A dispatcher starts a worker per page, in page order, each taking one of req.Concurrency slots
// before it starts; each worker hands its outcome to a channel of its page's own, buffered so it
// never waits on the consumer. The consumer reads the channels in page order and frees a page's
// slot when it reads its outcome, so a later page that finishes first waits in its channel holding
// its slot. Once the consumer stops, the outcomes it never took have their files removed.
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
	work, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	done := make([]chan outcome, n)
	for i := range done {
		done[i] = make(chan outcome, 1)
	}
	taken := 0
	defer func() {
		cancel()
		wg.Wait()
		for _, c := range done[taken:] {
			select {
			case o := <-c:
				remove(o.path)
			default:
			}
		}
	}()
	slots := make(chan struct{}, req.Concurrency)
	wg.Go(func() {
		for i := range n {
			select {
			case slots <- struct{}{}:
			case <-work.Done():
				return
			}
			wg.Go(func() {
				done[i] <- p.page(work, path, i+1, n, sizes[i], req)
			})
		}
	})
	for i := range n {
		var o outcome
		select {
		case o = <-done[i]:
			<-slots
			taken++
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

// page prepares page of the n pages of the PDF at path, its size size, req already checked: it
// reads the page's text when req says to, and renders the page to its file in req.Dir, at req.DPI
// or the largest DPI under req.MaxPixels. Its error names the page, and a page that fails leaves
// no file.
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

// check refuses a request Images can't run, before anything runs, and sets its default: a DPI of
// DefaultDPI for zero.
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
	}
	if req.DPI == 0 {
		req.DPI = DefaultDPI
	}
	return nil
}
