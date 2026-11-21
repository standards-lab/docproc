package sevenzip

import (
	"context"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"iter"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
)

// ErrContent is the cause of an item whose content isn't the size or the CRC-32 its listing
// declares.
var ErrContent = errors.New("content doesn't match its listing")

// ReadRequest names an archive to read whole: its file, the kind to read it as, and its listing,
// as List gave it for the same file and kind.
type ReadRequest struct {
	// Path is the archive's file; it is only read.
	Path string
	// Kind is what route identified the archive as, as ListRequest's Kind.
	Kind route.Kind
	// Listing is the archive's listing, whose items Read yields in its order.
	Listing Listing
}

// Item is one item of an archive as Read yields it: its listing's Entry, and its content.
type Item struct {
	Entry
	// Content reads the item's content, nothing for a directory. Its last Read returns io.EOF
	// once the content is the size and CRC-32 the listing declares, and an error matching
	// ErrContent when it isn't; a failed run's error otherwise, such as one matching ErrEncrypted.
	// It is valid until the next item is yielded, and a caller reads it, or doesn't, from the
	// goroutine that ranges over Read.
	Content io.Reader
}

// Read yields each item of the archive req names, in its listing's order, with its content. One
// 7zz run, e -so, writes the content of every item the run holds to its standard output, which
// Read splits by the listing's sizes, in the listing's order, holding each item to its size and
// CRC-32. The run holds each file item that is sized, declares a CRC-32 or is empty, and isn't
// encrypted; every other item that isn't a directory falls back to the one-item run Extract
// makes, started when its content is first read, and is excluded from the whole run by its
// listed name, -x!, matched whole.
//
// The whole run is never relied on past what it delivers whole. An item of the run its caller
// doesn't read to its end, or whose content doesn't match its listing, stops the run, and every
// later item falls back to its one-item run; so does every item after the run ends early, by 7zz's
// failure or a run that can't start. An item the run holds that shares its listed name with an
// excluded item, or lies under one, as "a/b" under "a", reads alone, since the exclusion removes
// it too. An excluded item whose listed name holds a "_", which may be a line break 7zz lists as
// one, or that has no name, can't be excluded by its name with certainty, and no whole run is
// made: every item reads alone.
//
// Read yields an error, and ends, only when ctx ends or req is refused, matching ErrRequest.
func (r *Reader) Read(ctx context.Context, req ReadRequest) iter.Seq2[Item, error] {
	return func(yield func(Item, error) bool) {
		path, format, err := check(req.Path, req.Kind)
		if err != nil {
			yield(Item{}, err)
			return
		}
		entries := req.Listing.Entries
		inRun, exclude := wholePlan(entries)
		var whole *wholeRun
		if inRun != nil {
			whole = r.startWhole(ctx, path, format, exclude)
			defer whole.stop()
		}
		one := func(name string) ExtractRequest { return ExtractRequest{Path: req.Path, Kind: req.Kind, Name: name} }
		var prev *content
		defer func() {
			if prev != nil {
				prev.finish()
			}
		}()
		for i, e := range entries {
			if prev != nil {
				prev.finish()
				prev = nil
			}
			if err := ctx.Err(); err != nil {
				yield(Item{}, err)
				return
			}
			c := &content{r: r, ctx: ctx, e: e, req: one(e.Path), crc: crc32.NewIEEE()}
			if inRun != nil && inRun[i] {
				c.whole = whole
			}
			prev = c
			if !yield(Item{Entry: e, Content: c}, nil) {
				return
			}
		}
	}
}

func heldWhole(e Entry) bool {
	return !e.Folder && !e.Encrypted && e.Sized && (e.HasCRC || e.Size == 0) &&
		e.HardLink == "" && !e.IsSymlink() && !e.IsDevice()
}

func wholePlan(entries []Entry) ([]bool, []string) {
	inRun := make([]bool, len(entries))
	excluded := map[string]bool{}
	var exclude []string
	held := 0
	for i, e := range entries {
		switch {
		case heldWhole(e):
			inRun[i] = true
			held++
		case e.Folder:
		case e.Path == "", strings.Contains(e.Path, "_"):
			return nil, nil
		case !excluded[e.Path]:
			excluded[e.Path] = true
			exclude = append(exclude, e.Path)
		}
	}
	for i, e := range entries {
		if inRun[i] && shadowed(e.Path, excluded) {
			inRun[i] = false
			held--
		}
	}
	if held == 0 {
		return nil, nil
	}
	return inRun, exclude
}

func shadowed(name string, excluded map[string]bool) bool {
	for p := name; ; {
		if excluded[p] {
			return true
		}
		i := strings.LastIndexByte(p, '/')
		if i < 0 {
			return false
		}
		p = p[:i]
	}
}

type wholeRun struct {
	pr     *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	ended  bool
}

var errStopped = errors.New("the whole run was stopped")

func (r *Reader) startWhole(ctx context.Context, path, format string, exclude []string) *wholeRun {
	runCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	w := &wholeRun{pr: pr, cancel: cancel, done: make(chan struct{})}
	args := append([]string{"e", "-so", "-spd", "-bd", "-t" + format}, common...)
	for _, name := range exclude {
		args = append(args, "-x!"+name)
	}
	args = append(args, "--", path)
	go func() {
		defer close(w.done)
		stderr := &cli.LimitedBuffer{Limit: stderrLimit}
		w.err = r.run(runCtx, args, pw, stderr)
		pw.CloseWithError(w.err)
	}()
	return w
}

func (w *wholeRun) stop() {
	w.ended = true
	w.pr.CloseWithError(errStopped)
	w.cancel()
	<-w.done
}

func (w *wholeRun) outcome() error {
	w.stop()
	return w.err
}

type content struct {
	r      *Reader
	ctx    context.Context
	e      Entry
	req    ExtractRequest
	whole  *wholeRun
	n      uint64
	crc    hash.Hash32
	single *singleRun
	err    error
}

func (c *content) Read(p []byte) (int, error) {
	switch {
	case c.err != nil:
		return 0, c.err
	case c.e.Folder:
		c.err = io.EOF
		return 0, c.err
	case c.whole != nil && c.whole.ended && c.n == 0:
		c.whole = nil
	}
	if c.whole != nil {
		return c.readWhole(p)
	}
	return c.readSingle(p)
}

func (c *content) readWhole(p []byte) (int, error) {
	left := c.e.Size - c.n
	if left == 0 {
		c.err = c.verify()
		if c.err != io.EOF {
			c.whole.stop()
		}
		return 0, c.err
	}
	if uint64(len(p)) > left {
		p = p[:left]
	}
	n, err := c.whole.pr.Read(p)
	c.n += uint64(n)
	c.crc.Write(p[:n])
	if err == nil {
		return n, nil
	}
	runErr := c.whole.outcome()
	if c.n == 0 {
		c.whole = nil
		return c.readSingle(p)
	}
	c.err = fmt.Errorf("sevenzip: %q: %w: %d bytes, %d listed", c.e.Path, ErrContent, c.n, c.e.Size)
	if runErr != nil {
		c.err = fmt.Errorf("%w: %w", c.err, runErr)
	}
	return n, c.err
}

func (c *content) verify() error {
	switch {
	case c.e.Sized && c.n != c.e.Size:
		return fmt.Errorf("sevenzip: %q: %w: %d bytes, %d listed", c.e.Path, ErrContent, c.n, c.e.Size)
	case c.e.HasCRC && c.crc.Sum32() != c.e.CRC:
		return fmt.Errorf("sevenzip: %q: %w: CRC-32 %08x, %08x listed", c.e.Path, ErrContent, c.crc.Sum32(), c.e.CRC)
	}
	return io.EOF
}

func (c *content) readSingle(p []byte) (int, error) {
	if c.single == nil {
		c.single = c.r.startSingle(c.ctx, c.req)
	}
	n, err := c.single.pr.Read(p)
	c.n += uint64(n)
	c.crc.Write(p[:n])
	switch {
	case err == io.EOF:
		c.err = c.verify()
		return n, c.err
	case err != nil:
		c.err = err
		return n, err
	}
	return n, nil
}

func (c *content) finish() {
	if c.whole != nil && !c.whole.ended && c.err == nil && c.verify() != io.EOF {
		c.whole.stop()
	}
	if c.single != nil {
		c.single.stop()
	}
}

type singleRun struct {
	pr     *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *Reader) startSingle(ctx context.Context, req ExtractRequest) *singleRun {
	runCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	s := &singleRun{pr: pr, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		pw.CloseWithError(r.Extract(runCtx, req, pw))
	}()
	return s
}

func (s *singleRun) stop() {
	s.pr.CloseWithError(errStopped)
	s.cancel()
	<-s.done
}
