//go:build integration && linux

package document_test

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

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

func TestGhostscriptCountHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	for set, want := range map[string]int{"clean": 6, "scanned": 7} {
		n, err := g.Count(t.Context(), fixtureCopy(t, set+".pdf"))
		if err != nil || n != want {
			t.Errorf("Count(%s.pdf) returned %d, %v, want %d", set, n, err, want)
		}
	}
}

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

func TestGhostscriptPortfolioHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	b, err := os.ReadFile(filepath.Join("testdata", "portfolio", "portfolio.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), `in (a)\b`)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(dir, `cover (1)\.pdf`)
	if err := os.WriteFile(pdf, b, 0o600); err != nil {
		t.Fatal(err)
	}
	pngs, texts := make([][]byte, 2), make([]string, 2)
	for page := 1; page <= 2; page++ {
		var out bytes.Buffer
		if err := g.Render(t.Context(), pdf, page, 150, &out); err != nil {
			t.Fatalf("Render(portfolio.pdf page %d): %v", page, err)
		}
		r := bytes.NewReader(out.Bytes())
		img, err := png.Decode(r)
		if err != nil {
			t.Fatalf("portfolio.pdf page %d isn't a PNG: %v", page, err)
		}
		if b := img.Bounds(); b.Dx() != 1275 || b.Dy() != 1650 || r.Len() != 0 {
			t.Errorf("portfolio.pdf page %d is %dx%d with %d bytes after its PNG, want one PNG, letter at 150 dpi, 1275x1650", page, b.Dx(), b.Dy(), r.Len())
		}
		text, err := g.Text(t.Context(), pdf, page)
		if want := "Portfolio cover, page " + strconv.Itoa(page); err != nil || collapse(text) != want {
			t.Errorf("Text(portfolio.pdf page %d) = %q, %v, want the cover's page alone, %q", page, text, err, want)
		}
		pngs[page-1], texts[page-1] = out.Bytes(), text
	}
	err = g.Render(t.Context(), pdf, 3, 150, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "Requested FirstPage is greater than the number of pages in the file: 2") {
		t.Errorf("Render(portfolio.pdf page 3) of 2 pages returned %v, want gs's message", err)
	}
	_, err = g.Text(t.Context(), pdf, 3)
	if !errors.Is(err, document.ErrRequest) || !strings.Contains(err.Error(), "Requested FirstPage is greater than the number of pages in the file: 2") {
		t.Errorf("Text(portfolio.pdf page 3) of 2 pages returned %v, want document.ErrRequest with gs's message", err)
	}
	p := document.NewPreparer(document.Config{Renderer: g})
	yielded := 0
	for img, err := range p.Images(t.Context(), pdf, document.ImagesRequest{Dir: t.TempDir(), Concurrency: 2, Text: true}) {
		if err != nil {
			t.Fatalf("Images(portfolio.pdf): page %d: %v", img.Number, err)
		}
		yielded++
		b, err := os.ReadFile(img.Path)
		if err != nil || img.Number != yielded || !bytes.Equal(b, pngs[img.Number-1]) || img.Text != texts[img.Number-1] {
			t.Errorf("Images(portfolio.pdf): yield %d, page %d: its PNG or text isn't the cover's page (%v)", yielded, img.Number, err)
		}
	}
	if yielded != 2 {
		t.Errorf("Images(portfolio.pdf) yielded %d pages, want 2", yielded)
	}
}

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

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

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

func charErrors(truth, got string) (edits, length int) {
	t := []rune(collapse(truth))
	return editDistance(t, []rune(collapse(got))), len(t)
}

func wordErrors(truth, got string) (edits, length int) {
	t := strings.Fields(truth)
	return editDistance(t, strings.Fields(got)), len(t)
}

func rate(edits, length int) float64 {
	if length == 0 {
		return 0
	}
	return float64(edits) / float64(length)
}
