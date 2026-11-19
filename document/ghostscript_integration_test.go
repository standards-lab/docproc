//go:build integration && linux

package document_test

import (
	"bytes"
	"errors"
	"image/png"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

// hostGhostscript builds the released renderer over the host's gs through the exec runner,
// which refuses one older than 10.0.
func hostGhostscript(t *testing.T) (*document.Ghostscript, *tool.Tool) {
	t.Helper()
	gs, err := tool.NewExec(t.Context(), document.GhostscriptSpec())
	if err != nil {
		t.Fatal(err)
	}
	g, err := document.NewGhostscript(document.Config{Ghostscript: gs})
	if err != nil {
		t.Fatal(err)
	}
	return g, gs
}

func TestGhostscriptVersion(t *testing.T) {
	_, gs := hostGhostscript(t)
	t.Logf("%s %s", gs.Name(), gs.Version())
}

// TestGhostscriptCountHost checks the host's gs counts the fixtures' pages.
func TestGhostscriptCountHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	for set, want := range map[string]int{"clean": 6, "scanned": 7} {
		n, err := g.Count(t.Context(), fixtureCopy(t, set+".pdf"))
		if err != nil || n != want {
			t.Errorf("Count(%s.pdf) returned %d, %v, want %d", set, n, err, want)
		}
	}
}

// TestGhostscriptRenderHost checks a page of each fixture renders as a PNG of letter size at
// 150 dpi, 1275x1650, and that a page past the last is an error that writes nothing.
func TestGhostscriptRenderHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	for set, page := range map[string]int{"clean": 1, "scanned": 7} {
		var out bytes.Buffer
		if err := g.Render(t.Context(), fixtureCopy(t, set+".pdf"), page, 150, &out); err != nil {
			t.Fatalf("Render(%s.pdf page %d): %v", set, page, err)
		}
		img, err := png.Decode(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Fatalf("%s.pdf page %d isn't a PNG: %v", set, page, err)
		}
		if b := img.Bounds(); b.Dx() != 1275 || b.Dy() != 1650 {
			t.Errorf("%s.pdf page %d is %dx%d, want letter at 150 dpi, 1275x1650", set, page, b.Dx(), b.Dy())
		}
		t.Logf("%s.pdf page %d: %d PNG bytes, %v", set, page, out.Len(), img.Bounds())
	}
	var out bytes.Buffer
	err := g.Render(t.Context(), fixtureCopy(t, "clean.pdf"), 7, 150, &out)
	if err == nil {
		t.Error("Render(clean.pdf page 7) of 6 pages returned no error")
	}
	if out.Len() != 0 {
		t.Errorf("Render(clean.pdf page 7) of 6 pages wrote %d bytes, want none", out.Len())
	}
	t.Logf("page past the last: %v", err)
}

// TestGhostscriptRenderMessageHost checks the host's gs renders the malformed fixture's page,
// which it renders with "**** Error: Page drawing error occurred.": the message goes to stderr,
// and Render writes the page's PNG alone.
func TestGhostscriptRenderMessageHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	var out bytes.Buffer
	if err := g.Render(t.Context(), fixtureCopy(t, "malformed/compacted-syntax.pdf"), 1, 150, &out); err != nil {
		t.Fatalf("Render(compacted-syntax.pdf page 1): %v", err)
	}
	img, err := png.Decode(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("compacted-syntax.pdf page 1 isn't a PNG: %v", err)
	}
	t.Logf("compacted-syntax.pdf page 1: %d PNG bytes, %v", out.Len(), img.Bounds())
}

// TestGhostscriptTextHost checks the host's gs reads clean.pdf's text layer, every page of it
// having one, and finds none on scanned.pdf's pages, their text an image, and records each clean
// page's character and word error rates against its ground truth. A page past the last is
// ErrRequest.
func TestGhostscriptTextHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	pdf := fixtureCopy(t, "clean.pdf")
	var edits, chars, wedits, words int
	for i, truth := range truthOf(t, "clean") {
		text, err := g.Text(t.Context(), pdf, i+1)
		if err != nil {
			t.Fatalf("Text(clean.pdf page %d): %v", i+1, err)
		}
		if !document.HasText(text) {
			t.Errorf("clean.pdf page %d has no text layer, want one", i+1)
			continue
		}
		e, n := charErrors(truth, text)
		we, wn := wordErrors(truth, text)
		found := wordsFound(truth, text)
		edits, chars, wedits, words = edits+e, chars+n, wedits+we, words+wn
		// txtwrite keeps the page's layout, so two columns read across both, line by line, and a
		// form's labels and values sit apart: the error rates, which count order, are recorded,
		// and the words are asserted, whatever their order.
		if found < 0.95 {
			t.Errorf("clean.pdf page %d's text layer holds %.4f of its truth's words, want nearly all: %q", i+1, found, text)
		}
		t.Logf("recorded: text layer clean.pdf page %d: CER %.4f, WER %.4f, %.4f of the truth's words found, %d bytes",
			i+1, rate(e, n), rate(we, wn), found, len(text))
	}
	t.Logf("recorded: text layer clean.pdf, 6 pages: CER %.4f, WER %.4f", rate(edits, chars), rate(wedits, words))
	scanned := fixtureCopy(t, "scanned.pdf")
	for page := 1; page <= 7; page++ {
		text, err := g.Text(t.Context(), scanned, page)
		if err != nil {
			t.Fatalf("Text(scanned.pdf page %d): %v", page, err)
		}
		if document.HasText(text) {
			t.Errorf("scanned.pdf page %d has text layer %q, want none", page, text)
		}
	}
	if _, err := g.Text(t.Context(), pdf, 7); !errors.Is(err, document.ErrRequest) {
		t.Errorf("Text(clean.pdf page 7) of 6 pages returned %v, want document.ErrRequest", err)
	}
}

// wordsFound returns the fraction of truth's words that got holds, whatever their order, each
// word of got counted once: 1 when got holds every word of truth, as often as truth does.
func wordsFound(truth, got string) float64 {
	have := map[string]int{}
	for _, w := range strings.Fields(got) {
		have[w]++
	}
	want := strings.Fields(truth)
	found := 0
	for _, w := range want {
		if have[w] > 0 {
			have[w]--
			found++
		}
	}
	return rate(found, len(want))
}

// collapse returns s with every run of whitespace made one space, and none at either end.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// editDistance returns the Levenshtein distance from a to b: the fewest insertions, deletions,
// and substitutions of one element that turn a into b. It keeps one row of the table.
func editDistance[T comparable](a, b []T) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diag := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			above := row[j]
			row[j] = min(above+1, row[j-1]+1, diag+cost)
			diag = above
		}
	}
	return row[len(b)]
}

// charErrors returns the character edit distance from truth to got and truth's length in
// characters, both texts collapsed, so line breaks and spacing never count as errors, and
// compared rune by rune, case counting. Their ratio is the character error rate; summed over
// pages, the rate of the pages together.
func charErrors(truth, got string) (edits, length int) {
	t := []rune(collapse(truth))
	return editDistance(t, []rune(collapse(got))), len(t)
}

// wordErrors returns the word edit distance from truth to got and truth's length in words, words
// being runs of non-whitespace. Their ratio is the word error rate.
func wordErrors(truth, got string) (edits, length int) {
	t := strings.Fields(truth)
	return editDistance(t, strings.Fields(got)), len(t)
}

// rate returns edits over length, and 0 for a length of 0.
func rate(edits, length int) float64 {
	if length == 0 {
		return 0
	}
	return float64(edits) / float64(length)
}
