package document

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// GhostscriptMinimum is the oldest Ghostscript the renderer accepts.
const GhostscriptMinimum = "10.0"

// GhostscriptSpec declares Ghostscript's gs, minimum 10.0, for either runner's constructor.
// gs --version prints the version alone, as "10.08.0", and the pattern takes it whole. A caller
// that needs another program path or a KillAfter period sets them on the Spec returned.
func GhostscriptSpec() tool.Spec {
	return tool.Spec{
		Program:        "gs",
		VersionArgs:    []string{"--version"},
		VersionPattern: regexp.MustCompile(`^(\d+(?:\.\d+)*)`),
		Minimum:        GhostscriptMinimum,
	}
}

// Ghostscript is the PDF renderer: it counts, renders, and reads the text layer of a PDF's pages
// with Ghostscript's gs, one process per call; the Preparer's shared processes take many pages
// each, through gs's page list. Ghostscript is AGPL-licensed; the renderer runs gs
// as a separate program through a tool.Tool and never links it.
//
// gs reads the PDF by its path, which Count and Render make absolute, and each run's working
// directory is the PDF's directory. The container runner mounts a run's working directory at the
// same absolute path, so the path names the same file through either runner.
type Ghostscript struct {
	gs *tool.Tool
}

// NewGhostscript builds the released renderer over cfg's Ghostscript; it reads no other field. A
// nil Ghostscript is the caller's mistake, and panics naming the field. It refuses one whose
// version is older than GhostscriptMinimum, whatever minimum its own Spec declared, with a
// *tool.VersionError wrapping tool.ErrTooOld.
func NewGhostscript(cfg Config) (*Ghostscript, error) {
	if cfg.Ghostscript == nil {
		panic("document: Config.Ghostscript is nil")
	}
	if err := cfg.Ghostscript.AtLeast(GhostscriptMinimum); err != nil {
		return nil, err
	}
	return &Ghostscript{gs: cfg.Ghostscript}, nil
}

// Count returns the number of pages in the PDF at pdf, as gs's own PDF interpreter counts them
// with pdfpagecount. gs runs with -dSAFER, permitted to read that one file.
func (g *Ghostscript) Count(ctx context.Context, pdf string) (int, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return 0, fmt.Errorf("document: ghostscript: %w", err)
	}
	var out bytes.Buffer
	if err := g.gs.Run(ctx, tool.Invocation{Args: countArgs(abs), Dir: filepath.Dir(abs), Stdout: &out}); err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil {
		return 0, fmt.Errorf("document: ghostscript: page count of %s: gs printed %q", abs, out.String())
	}
	return n, nil
}

// pageSize is a page's size in points as gs renders it: its media box, its rotation applied.
type pageSize struct {
	width, height float64
}

// pixels returns how many pixels gs renders the page at dpi: each side's points times dpi over
// 72, rounded half up, as gs sizes its device, multiplied.
func (s pageSize) pixels(dpi int) int64 {
	side := func(points float64) int64 { return int64(math.Floor(points*float64(dpi)/72 + 0.5)) }
	return side(s.width) * side(s.height)
}

// fit returns the largest DPI, at most dpi, at which the page renders in at most limit pixels,
// and false when not even 1 dpi does. A page's pixels never fall as its DPI rises, so the DPIs
// that fit are the ones below some bound, which fit searches for.
func (s pageSize) fit(dpi int, limit int64) (int, bool) {
	// The first DPI from 1 that doesn't fit; every DPI below it does.
	over := 1 + sort.Search(dpi, func(i int) bool { return s.pixels(i+1) > limit })
	return over - 1, over > 1
}

// survey is what the Preparer reads of a PDF before its pages: a size for each page, in page
// order, zero sizes when it didn't ask for them, and whether the PDF is a portfolio.
type survey struct {
	sizes []pageSize
	// portfolio is whether the PDF's catalog has a /Collection, which gs reads as a portfolio,
	// rendering, and reading the text of, its embedded documents' pages in place of its own: the
	// run's page range applied to each document in turn, so a run's output for a page is every
	// document's page of that number, none of it the page's alone.
	portfolio bool
}

// survey reads the PDF at pdf in one gs run: whether it is a portfolio, by the test gs's own
// runpdf makes of it, its PDFInfo holding /Collection; its page count; and, with sizes, each
// page's size, the page set up as rendering does, its media box and rotation applied, and the
// device's page size printed, so a page's size is the one Render renders. gs runs with -dSAFER,
// permitted to read that one file.
func (g *Ghostscript) survey(ctx context.Context, pdf string, sizes bool) (survey, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return survey{}, fmt.Errorf("document: ghostscript: %w", err)
	}
	var out bytes.Buffer
	if err := g.gs.Run(ctx, tool.Invocation{Args: surveyArgs(abs, sizes), Dir: filepath.Dir(abs), Stdout: &out}); err != nil {
		return survey{}, err
	}
	sv, err := parseSurvey(out.String(), sizes)
	if err != nil {
		return survey{}, fmt.Errorf("document: ghostscript: survey of %s: %w", abs, err)
	}
	return sv, nil
}

// parseSurvey reads surveyArgs' output: true or false, whether the PDF is a portfolio, and the
// page count, each on its own line, then, with sizes, each page's width and height in points, one
// page a line.
func parseSurvey(out string, sizes bool) (survey, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return survey{}, fmt.Errorf("gs printed %q", out)
	}
	portfolio, perr := strconv.ParseBool(strings.TrimSpace(lines[0]))
	n, err := strconv.Atoi(strings.TrimSpace(lines[1]))
	if perr != nil || err != nil || n < 0 || sizes && len(lines)-2 != n || !sizes && len(lines) != 2 {
		return survey{}, fmt.Errorf("gs printed %q", out)
	}
	sv := survey{sizes: make([]pageSize, n), portfolio: portfolio}
	if !sizes {
		return sv, nil
	}
	for i, line := range lines[2:] {
		f := strings.Fields(line)
		if len(f) != 2 {
			return survey{}, fmt.Errorf("page %d: gs printed %q", i+1, line)
		}
		w, werr := strconv.ParseFloat(f[0], 64)
		h, herr := strconv.ParseFloat(f[1], 64)
		if werr != nil || herr != nil || !(w >= 0) || !(h >= 0) {
			return survey{}, fmt.Errorf("page %d: gs printed %q", i+1, line)
		}
		sv.sizes[i] = pageSize{w, h}
	}
	return sv, nil
}

// Render writes page, 1-based, of the PDF at pdf to w as PNG at dpi, one gs run for the page. It
// refuses a DPI of zero or less and a page below 1 before gs runs, as CheckRender does. A gs that
// exits nonzero gives the Tool's error, a *tool.ExitError, and w may hold part of a PNG then.
//
// gs asked for a page past the last exits zero, writing its message to stdout in place of a PNG:
// "Requested FirstPage is greater than the number of pages in the file". Render writes nothing to
// w unless stdout begins as a PNG, and returns gs's message as its error when it doesn't.
func (g *Ghostscript) Render(ctx context.Context, pdf string, page, dpi int, w io.Writer) error {
	if err := CheckRender(page, dpi); err != nil {
		return err
	}
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return fmt.Errorf("document: ghostscript: %w", err)
	}
	return g.png(ctx, tool.Invocation{Args: renderArgs(abs, page, dpi), Dir: filepath.Dir(abs)}, abs, page, w)
}

// png runs gs as inv says, its stdout a page's PNG, and writes the PNG to w, holding back a stdout
// that doesn't begin as a PNG and returning it as the error.
func (g *Ghostscript) png(ctx context.Context, inv tool.Invocation, pdf string, page int, w io.Writer) error {
	out := &pngGuard{w: w}
	inv.Stdout = out
	if err := g.gs.Run(ctx, inv); err != nil {
		return err
	}
	if !out.png {
		return fmt.Errorf("document: ghostscript: page %d of %s: no PNG on stdout: %s", page, pdf, strings.TrimSpace(string(out.head)))
	}
	return nil
}

// pastLast is the start of the message gs prints for a FirstPage past a PDF's last page.
const pastLast = "Requested FirstPage is greater than the number of pages"

// textStderrLimit bounds how much of gs's stderr Text keeps, for its message and its errors.
const textStderrLimit = 4 << 10

// Text returns the text of page, 1-based, of the PDF at pdf, as its text layer holds it, one gs
// run with the txtwrite device: the page's text laid out in lines, gs's own spacing kept. A page
// with no text layer, such as a scanned page, returns blank text, which HasText reports as none.
// It refuses a page below 1 before gs runs, as CheckRender does. A gs that exits nonzero gives
// the Tool's error, a *tool.ExitError carrying gs's stderr.
//
// gs runs with -sstdout=%stderr, so its own messages go to stderr and stdout holds the text
// alone. A page past the last prints gs's message there, "Requested FirstPage is greater than
// the number of pages in the file", and exits zero; Text returns it as an error wrapping
// ErrRequest.
func (g *Ghostscript) Text(ctx context.Context, pdf string, page int) (string, error) {
	if page < 1 {
		return "", fmt.Errorf("document: page %d is below 1: %w", page, ErrRequest)
	}
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return "", fmt.Errorf("document: ghostscript: %w", err)
	}
	var out strings.Builder
	stderr := cli.LimitedBuffer{Limit: textStderrLimit}
	err = g.gs.Run(ctx, tool.Invocation{Args: textArgs(abs, page), Dir: filepath.Dir(abs), Stdout: &out, Stderr: &stderr})
	if err != nil {
		// Run keeps stderr for its *tool.ExitError only when the caller doesn't take it.
		if exit, ok := errors.AsType[*tool.ExitError](err); ok && exit.Message == "" {
			exit.Message = stderr.String()
		}
		return "", err
	}
	if msg := strings.TrimSpace(stderr.String()); strings.Contains(msg, pastLast) {
		return "", fmt.Errorf("document: ghostscript: page %d of %s: %s: %w", page, abs, msg, ErrRequest)
	}
	return out.String(), nil
}

// errOutput marks a shared run's output that doesn't hold its pages as asked: a stdout that isn't
// a stream of whole PNGs, one a page, or a text file missing. It is the content's failure, as
// output that doesn't parse is, so the run's pages fall back to a process each.
var errOutput = errors.New("gs's output doesn't hold the pages asked for")

// renderPages renders pages, 1-based and in order, of the PDF at pdf at dpi, in one gs run, its
// stdout the pages' PNGs one after another, which it splits by their chunks and hands to sink, a
// PNG at a time. It returns how many PNGs sink was handed whole, and the run's error: gs's own,
// the Tool's, or one wrapping errOutput when stdout isn't a PNG a page.
//
// The run's bound is the Tool's Timeout read as an idle bound, between gs's writes, rather than a
// bound on the whole run: a unit's bound is a page's, and one run renders many. Time sink spends
// blocked, waiting for the consumer, isn't idleness.
func (g *Ghostscript) renderPages(ctx context.Context, pdf string, pages []int, dpi int, sink pngSink) (int, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return 0, fmt.Errorf("document: ghostscript: %w", err)
	}
	split := &pngSplit{n: len(pages), sink: sink}
	err = g.gs.Run(ctx, tool.Invocation{Args: sharedRenderArgs(abs, pages, dpi), Dir: filepath.Dir(abs), Stdout: split, Idle: g.gs.Timeout()})
	whole, serr := split.finish()
	if err == nil && serr != nil {
		err = fmt.Errorf("document: ghostscript: pages %s of %s: %w", pageList(pages), abs, serr)
	}
	return whole, err
}

// sharedText reports whether textPages' files can be read back: through the exec runner they
// can, written wherever the caller says; the container runner mounts only a run's working
// directory, the PDF's, and textPages never writes there, so a container's text is read a page a
// run, through Text.
func (g *Ghostscript) sharedText() bool {
	_, container := g.gs.Image()
	return !container
}

// textPages writes the text of pages, 1-based and in order, of the PDF at pdf into dir, the
// caller's, in one gs run with the txtwrite device: the run's i-th page, from 1, to textFile(dir,
// i), as Text reads one page. Every page's file is there once it returns nil; a file missing is
// an error wrapping errOutput.
//
// gs runs without -q, so it writes "Page N" to stderr as each page starts, and the run's bound
// is the Tool's Timeout read as an idle bound, as renderPages' is.
func (g *Ghostscript) textPages(ctx context.Context, pdf string, pages []int, dir string) error {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return fmt.Errorf("document: ghostscript: %w", err)
	}
	// gs reads a % in OutputFile as a format's, which %% escapes.
	out := filepath.Join(strings.ReplaceAll(dir, "%", "%%"), "page-%d.txt")
	if err := g.gs.Run(ctx, tool.Invocation{Args: sharedTextArgs(abs, pages, out), Dir: filepath.Dir(abs), Idle: g.gs.Timeout()}); err != nil {
		return err
	}
	for i := range pages {
		if _, err := os.Stat(textFile(dir, i+1)); err != nil {
			return fmt.Errorf("document: ghostscript: text of page %d of %s: %w: %w", pages[i], abs, errOutput, err)
		}
	}
	return nil
}

// textFile is the file in dir textPages writes a run's i-th page's text to, i from 1.
func textFile(dir string, i int) string {
	return filepath.Join(dir, "page-"+strconv.Itoa(i)+".txt")
}

// pageList returns pages, ascending, as gs's PageList takes them: each run of consecutive pages
// a range, such as 1-915, and a page alone its number, joined by commas.
func pageList(pages []int) string {
	var b strings.Builder
	for i := 0; i < len(pages); {
		j := i
		for j+1 < len(pages) && pages[j+1] == pages[j]+1 {
			j++
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(pages[i]))
		if j > i {
			b.WriteString("-" + strconv.Itoa(pages[j]))
		}
		i = j + 1
	}
	return b.String()
}

// pageListFlag is the start of a shared run's page-list argument, the list after it.
const pageListFlag = "-sPageList="

// argLimit is the longest argument gs takes, in bytes: its argument buffer holds 2048 with the
// terminating NUL, and gs refuses a longer one as "Command too long" and exits 1, as gs 10.08.0
// does at 2048. A shared run's page list is held to it, as pageListLength measures it.
const argLimit = 2047

// pageListLength is the length of a page list as pageList writes it, measured a page at a time,
// so a batch's list is known as it grows without writing it out again.
type pageListLength struct {
	// done is the length of the list's runs before the last, with the comma after each; first
	// and last are the last run's pages; n is how many pages the list holds.
	done, first, last, n int
}

// with returns the length of the page-list argument, pageListFlag's bytes among them, once page,
// past the list's last, is added.
func (l *pageListLength) with(page int) int {
	switch {
	case l.n == 0:
		return len(pageListFlag) + runLength(page, page)
	case page == l.last+1:
		return len(pageListFlag) + l.done + runLength(l.first, page)
	}
	return len(pageListFlag) + l.done + runLength(l.first, l.last) + 1 + runLength(page, page)
}

// add adds page, past the list's last, to the list.
func (l *pageListLength) add(page int) {
	switch {
	case l.n == 0:
		l.first, l.last = page, page
	case page == l.last+1:
		l.last = page
	default:
		l.done += runLength(l.first, l.last) + 1
		l.first, l.last = page, page
	}
	l.n++
}

// runLength returns the length of a run of pages from first to last in a page list: the page's
// number when they are one page, and the range otherwise.
func runLength(first, last int) int {
	if first == last {
		return len(strconv.Itoa(first))
	}
	return len(strconv.Itoa(first)) + 1 + len(strconv.Itoa(last))
}

// sharedRenderArgs returns gs's arguments for pages of the PDF at pdf at dpi, as renderArgs' for
// one page but for the page list: gs writes each page's PNG to stdout in turn.
func sharedRenderArgs(pdf string, pages []int, dpi int) []string {
	return []string{"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sDEVICE=png16m",
		"-dTextAlphaBits=4", "-dGraphicsAlphaBits=4", "-r" + strconv.Itoa(dpi),
		pageListFlag + pageList(pages), "-sOutputFile=-", pdf}
}

// sharedTextArgs returns gs's arguments for the text of pages of the PDF at pdf, a file a page at
// out, whose %d gs numbers from 1 in the run; without -q, gs's messages, a "Page N" as each page
// starts among them, go to stderr.
func sharedTextArgs(pdf string, pages []int, out string) []string {
	return []string{"-dNOPAUSE", "-dBATCH", "-dSAFER", "-sstdout=%stderr", "-sDEVICE=txtwrite",
		pageListFlag + pageList(pages), "-sOutputFile=" + out, pdf}
}

// renderArgs returns gs's arguments for page of the PDF at pdf at dpi. png16m is 24-bit RGB; the
// alpha bits anti-alias text and graphics, as poppler's pdftoppm and pdftocairo do by default and
// gs doesn't. -sOutputFile=- writes the PNG to stdout.
func renderArgs(pdf string, page, dpi int) []string {
	return []string{"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sDEVICE=png16m",
		"-dTextAlphaBits=4", "-dGraphicsAlphaBits=4", "-r" + strconv.Itoa(dpi),
		"-dFirstPage=" + strconv.Itoa(page), "-dLastPage=" + strconv.Itoa(page), "-sOutputFile=-", pdf}
}

// textArgs returns gs's arguments for the text of page of the PDF at pdf: the txtwrite device,
// its text to stdout, and gs's own messages to stderr.
func textArgs(pdf string, page int) []string {
	return []string{"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sstdout=%stderr", "-sDEVICE=txtwrite",
		"-dFirstPage=" + strconv.Itoa(page), "-dLastPage=" + strconv.Itoa(page), "-sOutputFile=-", pdf}
}

// countArgs returns gs's arguments to print the page count of the PDF at pdf. The path is a
// PostScript string there, whose backslashes and parentheses are escaped.
func countArgs(pdf string) []string {
	return []string{"-q", "-dNODISPLAY", "-dSAFER", "--permit-file-read=" + pdf,
		"-c", "(" + psString(pdf) + ") (r) file runpdfbegin pdfpagecount = quit"}
}

// surveyArgs returns gs's arguments to print whether the PDF at pdf is a portfolio, true or false,
// as gs's runpdf tests it, PDFInfo, a dictionary once the PDF opens, holding /Collection; then its
// page count; then, with sizes, each page's size: pdfshowpage_setpage sets the device's page size
// to the page's, as rendering does, which is printed as its width and height. The path is escaped
// as countArgs escapes it.
func surveyArgs(pdf string, sizes bool) []string {
	script := "(" + psString(pdf) + ") (r) file runpdfbegin " +
		"/PDFInfo where { /PDFInfo get dup type /dicttype eq { /Collection known } { pop false } ifelse } { false } ifelse = "
	if sizes {
		script += "pdfpagecount dup = 1 1 3 -1 roll { " +
			"pdfgetpage pdfshowpage_setpage pop currentpagedevice /PageSize get aload pop exch =only ( ) print = " +
			"} for runpdfend quit"
	} else {
		script += "pdfpagecount = runpdfend quit"
	}
	return []string{"-q", "-dNODISPLAY", "-dSAFER", "--permit-file-read=" + pdf, "-c", script}
}

// psString escapes s for a PostScript string: its backslashes and parentheses.
func psString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}

// pngGuardLimit bounds how much of a stdout that isn't a PNG pngGuard keeps for the error.
const pngGuardLimit = 4 << 10

// pngGuard passes a PNG through to w, and holds back a stdout that doesn't begin with the PNG
// signature, keeping its first pngGuardLimit bytes, so a gs message never reaches w as a page.
type pngGuard struct {
	w io.Writer
	// head holds stdout until its first eight bytes are known, and all of it, to the limit, once
	// they aren't the signature.
	head []byte
	// png is whether stdout began with the signature, and decided whether that is known.
	png, decided bool
}

func (g *pngGuard) Write(p []byte) (int, error) {
	if g.png {
		return g.w.Write(p)
	}
	if g.decided {
		g.keep(p)
		return len(p), nil
	}
	g.keep(p)
	n := min(len(g.head), len(route.PNGSignature))
	if string(g.head[:n]) != route.PNGSignature[:n] {
		g.decided = true
		g.head = g.head[:min(len(g.head), pngGuardLimit)]
		return len(p), nil
	}
	if n < len(route.PNGSignature) {
		return len(p), nil
	}
	g.png, g.decided = true, true
	// head holds all of stdout so far, which is at most the signature plus p.
	if _, err := g.w.Write(g.head); err != nil {
		return 0, err
	}
	g.head = nil
	return len(p), nil
}

// keep appends p to head, up to pngGuardLimit for a stdout that isn't a PNG. Until the signature
// is decided head keeps everything, which is at most seven bytes plus p.
func (g *pngGuard) keep(p []byte) {
	if g.decided {
		p = p[:min(len(p), max(pngGuardLimit-len(g.head), 0))]
	}
	g.head = append(g.head, p...)
}

// pngSink takes the PNGs a pngSplit splits from a shared run's stdout, the i-th from 0, in turn.
type pngSink interface {
	// begin starts the i-th PNG and returns the writer its bytes go to, which never fails, a
	// page's own failure kept by the sink; an error stops the run's stdout, and is for the run's
	// end alone, its context ended.
	begin(i int) (io.Writer, error)
	// end ends the i-th PNG, written whole.
	end(i int)
	// abandon ends the i-th PNG unfinished: the run failed or its stdout stopped parsing first.
	abandon(i int)
}

// pngSplit splits a stdout of PNGs one after another, as gs writes a page list's, into each PNG
// by its own structure: the eight-byte signature, then chunks, each a four-byte big-endian length,
// a four-byte type, that many bytes of data, and a four-byte CRC, through the IEND chunk. It reads
// no chunk's data and checks no CRC: gs's PNGs are passed through as written, byte for byte.
//
// Output past n PNGs, or that isn't a PNG where one begins, stops the split: the rest of stdout is
// discarded, never written to a page, and finish reports it.
type pngSplit struct {
	n    int
	sink pngSink
	// i is the PNG being split, or the next, and w its writer, nil between PNGs.
	i int
	w io.Writer
	// head gathers the signature, or a chunk's length and type, until have is eight.
	head [8]byte
	have int
	// inChunk is whether head is a chunk's, the signature read; rest is what is left of a chunk's
	// data and CRC once its head is read, and iend whether the chunk is IEND.
	inChunk bool
	rest    int64
	iend    bool
	// err is the reason the split stopped.
	err error
}

func (s *pngSplit) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 && s.err == nil {
		if s.w == nil {
			if s.i == s.n {
				s.err = fmt.Errorf("%w: output past %d PNGs", errOutput, s.n)
				break
			}
			w, err := s.sink.begin(s.i)
			if err != nil {
				return 0, err
			}
			s.w, s.have, s.inChunk, s.rest = w, 0, false, 0
		}
		if s.rest > 0 {
			k := int(min(int64(len(p)), s.rest))
			_, _ = s.w.Write(p[:k])
			p, s.rest = p[k:], s.rest-int64(k)
			if s.rest == 0 && s.iend {
				s.sink.end(s.i)
				s.i, s.w = s.i+1, nil
			}
			continue
		}
		k := copy(s.head[s.have:], p)
		_, _ = s.w.Write(p[:k])
		p, s.have = p[k:], s.have+k
		if s.have < len(s.head) {
			continue
		}
		s.have = 0
		if !s.inChunk {
			if string(s.head[:]) != route.PNGSignature {
				s.err = fmt.Errorf("%w: PNG %d of %d doesn't begin with PNG's signature", errOutput, s.i+1, s.n)
			}
			s.inChunk = true
			continue
		}
		// A chunk's length is at most 2^31-1 bytes, so its data and CRC fit an int64.
		s.rest = int64(binary.BigEndian.Uint32(s.head[:4])) + 4
		s.iend = string(s.head[4:]) == "IEND"
	}
	return total, nil
}

// finish ends the split once the run has returned: a PNG left unfinished is abandoned. It returns
// how many PNGs were handed over whole, and why the split stopped, or, for a stdout that ended
// short of n PNGs, an error saying so; nil when it held n whole PNGs and nothing else.
func (s *pngSplit) finish() (int, error) {
	if s.w != nil {
		s.sink.abandon(s.i)
		s.w = nil
	}
	if s.err == nil && s.i < s.n {
		s.err = fmt.Errorf("%w: %d of %d PNGs on stdout", errOutput, s.i, s.n)
	}
	return s.i, s.err
}
