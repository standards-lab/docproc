package document

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

// PDFToTextMinimum is the oldest pdftotext the package accepts: poppler 22.02.
const PDFToTextMinimum = "22.02"

// PDFToTextSpec declares poppler's pdftotext, minimum 22.02, for either runner's constructor.
// pdftotext -v prints "pdftotext version 26.10.0" and its copyright to stderr and exits 0. A
// caller that needs another program path or a KillAfter period sets them on the Spec returned.
func PDFToTextSpec() tool.Spec {
	return tool.Spec{
		Program:        "pdftotext",
		VersionArgs:    []string{"-v"},
		VersionPattern: regexp.MustCompile(`pdftotext version (\d+(?:\.\d+)*)`),
		Minimum:        PDFToTextMinimum,
	}
}

// TextReader reads a PDF page's text layer through poppler's pdftotext in its reading-order mode,
// neither -layout nor -raw, so a multi-column page reads column by column and no run of spaces
// pads it. Each Text is one pdftotext run, which reads the PDF by its absolute path in the PDF's
// directory. Its methods are safe for concurrent use.
type TextReader struct {
	pdftotext *tool.Tool
}

// NewTextReader builds a TextReader over cfg's PDFToText; it reads no other field. A nil
// PDFToText is the caller's mistake, and panics naming the field. It refuses one older than
// PDFToTextMinimum, whatever minimum its own Spec declared, with a *tool.VersionError wrapping
// tool.ErrTooOld.
func NewTextReader(cfg Config) (*TextReader, error) {
	if cfg.PDFToText == nil {
		panic("document: Config.PDFToText is nil")
	}
	if err := cfg.PDFToText.AtLeast(PDFToTextMinimum); err != nil {
		return nil, err
	}
	return &TextReader{pdftotext: cfg.PDFToText}, nil
}

const pdftotextStderrLimit = 4 << 10

const pdftotextPastLast = "Wrong page range given"

// Text returns the text of page, 1-based, of the PDF at pdf, as its text layer holds it, through
// pdftotext -enc UTF-8 -f N -l N, written to stdout, the form feed pdftotext ends the page with
// dropped. A page with no text layer, such as a scanned page, returns blank text, which HasText
// reports as none. It refuses a page below 1 before pdftotext runs, wrapping ErrRequest. A page
// past the last, which pdftotext refuses with exit 99 and "Wrong page range given", returns an
// error naming the page and the PDF, wrapping ErrRequest. A pdftotext that exits nonzero
// otherwise gives the Tool's error, a *tool.ExitError carrying the first 4 KiB of pdftotext's
// stderr.
func (r *TextReader) Text(ctx context.Context, pdf string, page int) (string, error) {
	if page < 1 {
		return "", fmt.Errorf("document: page %d is below 1: %w", page, ErrRequest)
	}
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return "", fmt.Errorf("document: pdftotext: %w", err)
	}
	var out strings.Builder
	stderr := cli.LimitedBuffer{Limit: pdftotextStderrLimit}
	n := strconv.Itoa(page)
	err = r.pdftotext.Run(ctx, tool.Invocation{
		Args: []string{"-enc", "UTF-8", "-f", n, "-l", n, abs, "-"}, Dir: filepath.Dir(abs),
		Stdout: &out, Stderr: &stderr,
	})
	if err != nil {
		exit, ok := errors.AsType[*tool.ExitError](err)
		if ok && exit.Code == 99 && strings.Contains(stderr.String(), pdftotextPastLast) {
			msg := strings.TrimSpace(stderr.String())
			return "", fmt.Errorf("document: pdftotext: page %d of %s: %s: %w", page, abs, msg, ErrRequest)
		}
		if ok && exit.Message == "" {
			exit.Message = stderr.String()
		}
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\f"), nil
}
