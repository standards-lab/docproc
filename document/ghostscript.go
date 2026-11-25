package document

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"slices"
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

// Ghostscript is the PDF renderer: it counts and renders a PDF's pages with Ghostscript's gs, one
// process per call; the Preparer's shared processes take many pages each, through gs's page list.
// Ghostscript is AGPL-licensed; the renderer runs gs as a separate program through a tool.Tool and
// never links it.
//
// gs reads the PDF by its path, which Count and Render make absolute, and each run's working
// directory is the PDF's directory. The container runner mounts a run's working directory at the
// same absolute path, so the path names the same file through either runner. The Preparer's own
// runs, its survey and its shared processes, take the working directory it gives them, a scratch
// directory of its own through the exec runner.
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

type pageSize struct {
	width, height float64
}

func (s pageSize) pixels(dpi int) int64 {
	side := func(points float64) int64 { return int64(math.Floor(points*float64(dpi)/72 + 0.5)) }
	return side(s.width) * side(s.height)
}

func (s pageSize) fit(dpi int, limit int64) (int, bool) {
	over := 1 + sort.Search(dpi, func(i int) bool { return s.pixels(i+1) > limit })
	return over - 1, over > 1
}

type survey struct {
	sizes     []pageSize
	portfolio bool
}

func (g *Ghostscript) survey(ctx context.Context, pdf, dir string, sizes bool) (survey, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return survey{}, fmt.Errorf("document: ghostscript: %w", err)
	}
	var out bytes.Buffer
	if err := g.gs.Run(ctx, tool.Invocation{Args: surveyArgs(abs, sizes), Dir: workDir(dir, abs), Stdout: &out}); err != nil {
		return survey{}, err
	}
	sv, err := parseSurvey(out.String(), sizes)
	if err != nil {
		what := "page count"
		if sizes {
			what = "page sizes"
		}
		return survey{}, fmt.Errorf("document: ghostscript: %s of %s: %w", what, abs, err)
	}
	return sv, nil
}

func parseSurvey(out string, sizes bool) (survey, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	n, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || n < 0 {
		return survey{}, fmt.Errorf("gs printed %q", out)
	}
	var sv survey
	if n > 0 {
		if len(lines) < 2 || lines[1] != "true" && lines[1] != "false" {
			return survey{}, fmt.Errorf("gs printed %q", out)
		}
		sv.portfolio = lines[1] == "true"
		out = strings.Replace(out, "\n"+lines[1]+"\n", "\n", 1)
		lines = slices.Delete(lines, 1, 2)
	}
	if sizes && len(lines)-1 != n || !sizes && len(lines) != 1 {
		return survey{}, fmt.Errorf("gs printed %q", out)
	}
	sv.sizes = make([]pageSize, n)
	if !sizes {
		return sv, nil
	}
	for i, line := range lines[1:] {
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
// exits nonzero gives the Tool's error, a *tool.ExitError, and w may hold part of a PNG then. gs
// reads the PDF through the non-portfolio branch of its runpdf, so a PDF portfolio's page is its
// cover's own, one PNG, not its embedded documents' pages.
//
// gs runs with -sstdout=%stderr, so its own messages go to stderr and stdout holds the PNG alone.
// gs asked for a page past the last exits zero, writing no PNG and its message, "Requested
// FirstPage is greater than the number of pages in the file", to stderr. Render writes nothing to
// w unless stdout begins as a PNG, and returns gs's message as its error when it doesn't. A page
// gs renders with a message, such as "**** Error: Page drawing error occurred.", renders, and the
// message is dropped, as the stderr of every run that succeeds is.
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

func (g *Ghostscript) png(ctx context.Context, inv tool.Invocation, pdf string, page int, w io.Writer) error {
	out := &pngGuard{w: w}
	stderr := cli.LimitedBuffer{Limit: gsStderrLimit}
	inv.Stdout, inv.Stderr = out, &stderr
	if err := g.gs.Run(ctx, inv); err != nil {
		if exit, ok := errors.AsType[*tool.ExitError](err); ok && exit.Message == "" {
			exit.Message = stderr.String()
		}
		return err
	}
	if !out.png {
		return fmt.Errorf("document: ghostscript: page %d of %s: no PNG on stdout: %s", page, pdf, strings.TrimSpace(stderr.String()))
	}
	return nil
}

const gsStderrLimit = 4 << 10

var errOutput = errors.New("gs's output doesn't hold the pages asked for")

func (g *Ghostscript) renderPages(ctx context.Context, pdf, dir string, pages []int, dpi int, sink pngSink) (int, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return 0, fmt.Errorf("document: ghostscript: %w", err)
	}
	split := &pngSplit{n: len(pages), sink: sink}
	err = g.gs.Run(ctx, tool.Invocation{Args: sharedRenderArgs(abs, pages, dpi), Dir: workDir(dir, abs), Stdout: split, Idle: g.gs.Timeout()})
	whole, serr := split.finish()
	if err == nil && serr != nil {
		err = fmt.Errorf("document: ghostscript: pages %s of %s: %w", pageList(pages), abs, serr)
	}
	return whole, err
}

func (g *Ghostscript) ownDir() bool {
	_, container := g.gs.Image()
	return !container
}

func workDir(dir, pdf string) string {
	if dir != "" {
		return dir
	}
	return filepath.Dir(pdf)
}

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

const pageListFlag = "-sPageList="

const argLimit = 2047

func listFits(pages []int, page int) bool {
	return len(pageListFlag)+len(pageList(append(slices.Clip(pages), page))) <= argLimit
}

func sharedRenderArgs(pdf string, pages []int, dpi int) []string {
	return []string{"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sstdout=%stderr", "-sDEVICE=png16m",
		"-dTextAlphaBits=4", "-dGraphicsAlphaBits=4", "-r" + strconv.Itoa(dpi),
		pageListFlag + pageList(pages), "-sOutputFile=-", pdf}
}

func renderArgs(pdf string, page, dpi int) []string {
	return append([]string{"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sstdout=%stderr", "-sDEVICE=png16m",
		"-dTextAlphaBits=4", "-dGraphicsAlphaBits=4", "-r" + strconv.Itoa(dpi),
		"-dFirstPage=" + strconv.Itoa(page), "-dLastPage=" + strconv.Itoa(page), "-sOutputFile=-"}, ownPageArgs(pdf)...)
}

// See docs/api.md, "Running the tools".
const ownPages = "runpdfbegin process_trailer_attrs runpdfpagerange dopdfpages runpdfend"

func ownPageArgs(pdf string) []string {
	return []string{"--permit-file-read=" + pdf, "-c", "(" + psString(pdf) + ") (r) file " + ownPages}
}

func countArgs(pdf string) []string {
	return []string{"-q", "-dNODISPLAY", "-dSAFER", "--permit-file-read=" + pdf,
		"-c", "(" + psString(pdf) + ") (r) file runpdfbegin pdfpagecount = quit"}
}

func surveyArgs(pdf string, sizes bool) []string {
	script := "(" + psString(pdf) + ") (r) file runpdfbegin pdfpagecount dup = " +
		"dup 0 gt { /PDFInfo where { /PDFInfo get dup type /dicttype eq { /Collection known } { pop false } ifelse } { false } ifelse = } if "
	if sizes {
		script += "1 1 3 -1 roll { " +
			"pdfgetpage pdfshowpage_setpage pop currentpagedevice /PageSize get aload pop exch =only ( ) print = " +
			"} for runpdfend quit"
	} else {
		script += "pop runpdfend quit"
	}
	return []string{"-q", "-dNODISPLAY", "-dSAFER", "--permit-file-read=" + pdf, "-c", script}
}

func psString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}

type pngGuard struct {
	w            io.Writer
	head         []byte
	png, decided bool
}

func (g *pngGuard) Write(p []byte) (int, error) {
	if g.png {
		return g.w.Write(p)
	}
	if g.decided {
		return len(p), nil
	}
	g.head = append(g.head, p...)
	n := min(len(g.head), len(route.PNGSignature))
	if string(g.head[:n]) != route.PNGSignature[:n] {
		g.decided, g.head = true, nil
		return len(p), nil
	}
	if n < len(route.PNGSignature) {
		return len(p), nil
	}
	g.png, g.decided = true, true
	if _, err := g.w.Write(g.head); err != nil {
		return 0, err
	}
	g.head = nil
	return len(p), nil
}

type pngSink interface {
	begin(i int) (io.Writer, error)
	end(i int)
	abandon(i int)
}

type pngSplit struct {
	n       int
	sink    pngSink
	i       int
	w       io.Writer
	head    [8]byte
	have    int
	inChunk bool
	rest    int64
	iend    bool
	err     error
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
		s.rest = int64(binary.BigEndian.Uint32(s.head[:4])) + 4
		s.iend = string(s.head[4:]) == "IEND"
	}
	return total, nil
}

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
