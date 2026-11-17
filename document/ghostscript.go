package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// GhostscriptMinimum is the oldest Ghostscript the released renderer accepts.
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

// Ghostscript is the released renderer, the renderer bake-off's winner: it counts, renders, and
// reads the text layer of a PDF's pages with Ghostscript's gs, one process per call. Ghostscript is AGPL-licensed; the
// renderer runs gs as a separate program through a tool.Tool and never links it.
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

// sizes returns the size of every page of the PDF at pdf, in page order, in one gs run that sets
// each page up as rendering does, its media box and rotation applied, and prints the device's
// page size, so a page's size is the one Render renders. gs runs with -dSAFER, permitted to read
// that one file.
func (g *Ghostscript) sizes(ctx context.Context, pdf string) ([]pageSize, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return nil, fmt.Errorf("document: ghostscript: %w", err)
	}
	var out bytes.Buffer
	if err := g.gs.Run(ctx, tool.Invocation{Args: sizesArgs(abs), Dir: filepath.Dir(abs), Stdout: &out}); err != nil {
		return nil, err
	}
	sizes, err := parseSizes(out.String())
	if err != nil {
		return nil, fmt.Errorf("document: ghostscript: page sizes of %s: %w", abs, err)
	}
	return sizes, nil
}

// parseSizes reads sizesArgs' output: the page count on its own line, then each page's width and
// height in points, one page a line.
func parseSizes(out string) ([]pageSize, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	n, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || n < 0 || len(lines)-1 != n {
		return nil, fmt.Errorf("gs printed %q", out)
	}
	sizes := make([]pageSize, n)
	for i, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("page %d: gs printed %q", i+1, line)
		}
		w, werr := strconv.ParseFloat(f[0], 64)
		h, herr := strconv.ParseFloat(f[1], 64)
		if werr != nil || herr != nil || !(w >= 0) || !(h >= 0) {
			return nil, fmt.Errorf("page %d: gs printed %q", i+1, line)
		}
		sizes[i] = pageSize{w, h}
	}
	return sizes, nil
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

// renderArgs returns gs's arguments for page of the PDF at pdf at dpi, as the bake-off measured
// them. png16m is 24-bit RGB; the alpha bits anti-alias text and graphics, as poppler's pdftoppm
// and pdftocairo do by default and gs doesn't. -sOutputFile=- writes the PNG to stdout.
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

// sizesArgs returns gs's arguments to print the page count of the PDF at pdf and each page's size:
// pdfshowpage_setpage sets the device's page size to the page's, as rendering does, which is
// printed as its width and height. The path is escaped as countArgs escapes it.
func sizesArgs(pdf string) []string {
	return []string{"-q", "-dNODISPLAY", "-dSAFER", "--permit-file-read=" + pdf,
		"-c", "(" + psString(pdf) + ") (r) file runpdfbegin pdfpagecount dup = 1 1 3 -1 roll { " +
			"pdfgetpage pdfshowpage_setpage pop currentpagedevice /PageSize get aload pop exch =only ( ) print = " +
			"} for runpdfend quit"}
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
