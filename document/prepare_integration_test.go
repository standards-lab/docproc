//go:build integration && linux

package document_test

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/tool"
)

func pngSize(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("%s isn't a PNG: %v", path, err)
	}
	return cfg.Width, cfg.Height
}

func TestImagesHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	p := document.NewPreparer(document.Config{Renderer: g, Text: hostTextReader(t)})
	for _, tc := range []struct {
		set   string
		pages int
		text  bool
	}{{"scanned", 7, false}, {"clean", 6, true}} {
		req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: 4, Text: tc.text}
		start := time.Now()
		next := 1
		for img, err := range p.Images(t.Context(), fixtureCopy(t, tc.set+".pdf"), req) {
			if err != nil {
				t.Fatalf("%s.pdf page %d: %v", tc.set, img.Number, err)
			}
			if img.Number != next || img.Count != tc.pages || img.DPI != 150 {
				t.Errorf("%s.pdf yield %d is page %d of %d at %d dpi, want page %d of %d at 150", tc.set, next, img.Number, img.Count, img.DPI, next, tc.pages)
			}
			if w, h := pngSize(t, img.Path); w != 1275 || h != 1650 {
				t.Errorf("%s.pdf page %d is %dx%d, want letter at 150 dpi, 1275x1650", tc.set, img.Number, w, h)
			}
			if document.HasText(img.Text) != tc.text {
				t.Errorf("%s.pdf page %d has a text layer %t, want %t", tc.set, img.Number, !tc.text, tc.text)
			}
			next++
		}
		if next-1 != tc.pages {
			t.Errorf("%s.pdf: the stream yielded %d pages, want %d", tc.set, next-1, tc.pages)
		}
		t.Logf("recorded: Images %s.pdf, text %t, %d pages at concurrency 4: %v", tc.set, tc.text, tc.pages, time.Since(start).Round(time.Millisecond))
	}
}

func TestImagesMaxPixelsHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	p := document.NewPreparer(document.Config{Renderer: g, Text: hostTextReader(t)})
	for _, tc := range []struct {
		max  int64
		dpi  int
		w, h int
	}{{1275*1650 - 1, 149, 1267, 1639}, {1275 * 1650, 150, 1275, 1650}} {
		req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: 2, MaxPixels: tc.max}
		for img, err := range p.Images(t.Context(), fixtureCopy(t, "clean.pdf"), req) {
			if err != nil {
				t.Fatalf("page %d: %v", img.Number, err)
			}
			w, h := pngSize(t, img.Path)
			if img.DPI != tc.dpi || w != tc.w || h != tc.h || int64(w*h) > tc.max {
				t.Errorf("MaxPixels %d: page %d at %d dpi is %dx%d, want %d dpi, %dx%d", tc.max, img.Number, img.DPI, w, h, tc.dpi, tc.w, tc.h)
			}
		}
	}
}

func TestImagesSharedHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	tr := hostTextReader(t)
	p := document.NewPreparer(document.Config{Renderer: g, Text: tr})
	for _, set := range []struct {
		pdf       string
		maxPixels int64
		dpi       int
	}{
		{"clean", 0, 150}, {"degraded", 0, 150}, {"scanned", 0, 150}, {"malformed/compacted-syntax", 0, 150},
		{"forms/acroform", 0, 150}, {"forms/signed", 0, 150}, {"attachments/attachments", 0, 150},
		{"clean", 1275*1650 - 1, 149}, {"portfolio/portfolio", 0, 150},
	} {
		pdf := fixtureCopy(t, set.pdf+".pdf")
		n, err := g.Count(t.Context(), pdf)
		if err != nil {
			t.Fatal(err)
		}
		pngs, texts := make([][]byte, n), make([]string, n)
		for i := range n {
			var out bytes.Buffer
			if err := g.Render(t.Context(), pdf, i+1, set.dpi, &out); err != nil {
				t.Fatal(err)
			}
			if texts[i], err = tr.Text(t.Context(), pdf, i+1); err != nil {
				t.Fatal(err)
			}
			pngs[i] = out.Bytes()
		}
		for _, per := range []int{1, 2, 4, 32} {
			for _, concurrency := range []int{1, 3} {
				name := fmt.Sprintf("%s.pdf, MaxPixels %d, PagesPerProcess %d, concurrency %d", set.pdf, set.maxPixels, per, concurrency)
				req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: concurrency, PagesPerProcess: per, Text: true, MaxPixels: set.maxPixels}
				yielded := 0
				for img, err := range p.Images(t.Context(), pdf, req) {
					if err != nil {
						t.Fatalf("%s: page %d: %v", name, img.Number, err)
					}
					yielded++
					b, err := os.ReadFile(img.Path)
					if err != nil || img.Number != yielded || img.DPI != set.dpi || !bytes.Equal(b, pngs[img.Number-1]) || img.Text != texts[img.Number-1] {
						t.Errorf("%s: yield %d, page %d at %d dpi: its PNG or text isn't its own process's (%v)", name, yielded, img.Number, img.DPI, err)
					}
				}
				if yielded != n {
					t.Errorf("%s: the stream yielded %d pages, want %d", name, yielded, n)
				}
			}
		}
	}
}

func annotsNullPDF(pages int) []byte {
	content := "0 0 1 rg 10 10 50 50 re f"
	objs := []string{"<</Type/Catalog/Pages 2 0 R>>"}
	kids := make([]string, pages)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	objs = append(objs, fmt.Sprintf("<</Type/Pages/Count %d/Kids[%s]>>", pages, strings.Join(kids, " ")))
	for range pages {
		objs = append(objs, fmt.Sprintf("<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]/Annots null/Contents %d 0 R>>", 3+pages))
	}
	objs = append(objs, fmt.Sprintf("<</Length %d>>stream\n%s\nendstream", len(content), content))
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<</Root 1 0 R/Size %d>>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestImagesSharedMessageHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	pdf := filepath.Join(t.TempDir(), "annots-null.pdf")
	if err := os.WriteFile(pdf, annotsNullPDF(3), 0o600); err != nil {
		t.Fatal(err)
	}
	p := document.NewPreparer(document.Config{Renderer: g, Text: hostTextReader(t)})
	req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: 1, PagesPerProcess: 3}
	yielded := 0
	for img, err := range p.Images(t.Context(), pdf, req) {
		if err != nil {
			t.Fatalf("page %d: %v", img.Number, err)
		}
		yielded++
		var want bytes.Buffer
		if err := g.Render(t.Context(), pdf, img.Number, img.DPI, &want); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(img.Path); err != nil || !bytes.Equal(b, want.Bytes()) {
			t.Errorf("page %d isn't Render's (%v)", img.Number, err)
		}
	}
	if yielded != 3 {
		t.Errorf("the stream yielded %d pages, want 3", yielded)
	}
}

func TestImagesPageListHost(t *testing.T) {
	host, err := exec.LookPath("gs")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pdf := filepath.Join(dir, "numbers.pdf")
	write := exec.CommandContext(t.Context(), "gs", "-q", "-dSAFER", "-sDEVICE=pdfwrite", "-o", pdf, "-c",
		"/Helvetica findfont 72 scalefont setfont 1 1 1200 { 72 400 moveto 10 string cvs show showpage } for")
	if out, err := write.CombinedOutput(); err != nil {
		t.Fatalf("writing the PDF: %v: %s", err, out)
	}
	log := filepath.Join(dir, "args.log")
	shim := filepath.Join(dir, "gs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\nexec " + host + " \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := document.GhostscriptSpec()
	spec.Program = shim
	gs, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	g, err := document.NewGhostscript(document.Config{Ghostscript: gs})
	if err != nil {
		t.Fatal(err)
	}
	p := document.NewPreparer(document.Config{Renderer: g, Text: hostTextReader(t)})
	const dpi = 10
	sampled := []int{1, 2, 600, 601, 1035, 1036, 1037, 1038, 1199, 1200}
	for _, concurrency := range []int{1, 2} {
		if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		req := document.ImagesRequest{Dir: t.TempDir(), DPI: dpi, Concurrency: concurrency, PagesPerProcess: 2000}
		yielded := 0
		for img, err := range p.Images(t.Context(), pdf, req) {
			if err != nil {
				t.Fatalf("concurrency %d: page %d: %v", concurrency, img.Number, err)
			}
			yielded++
			if !slices.Contains(sampled, img.Number) {
				continue
			}
			var want bytes.Buffer
			if err := g.Render(t.Context(), pdf, img.Number, dpi, &want); err != nil {
				t.Fatal(err)
			}
			if b, err := os.ReadFile(img.Path); err != nil || !bytes.Equal(b, want.Bytes()) {
				t.Errorf("concurrency %d: page %d isn't Render's (%v)", concurrency, img.Number, err)
			}
		}
		if yielded != 1200 {
			t.Errorf("concurrency %d: the stream yielded %d pages, want 1200", concurrency, yielded)
		}
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		var lists []string
		var own []int
		for line := range strings.Lines(string(b)) {
			if !strings.Contains(line, "-sDEVICE=png16m") {
				continue
			}
			for f := range strings.FieldsSeq(line) {
				if len(f) > 2047 {
					t.Errorf("concurrency %d: a gs argument of %d bytes, past gs's 2047", concurrency, len(f))
				}
				if v, ok := strings.CutPrefix(f, "-sPageList="); ok {
					lists = append(lists, v)
				}
				if v, ok := strings.CutPrefix(f, "-dFirstPage="); ok {
					n, _ := strconv.Atoi(v)
					own = append(own, n)
				}
			}
		}
		if !slices.Equal(own, sampled) {
			t.Errorf("concurrency %d: pages rendered in a process of their own %v, want the sampled %v", concurrency, own, sampled)
		}
		want := map[int]int{1: 1, 2: 4}[concurrency]
		if len(lists) != want || concurrency == 1 && lists[0] != "1-1200" {
			t.Errorf("concurrency %d: %d shared render processes, want %d: %.80q", concurrency, len(lists), want, lists)
		}
	}
}

func TestImagesUnopenedHost(t *testing.T) {
	g, _ := hostGhostscript(t)
	p := document.NewPreparer(document.Config{Renderer: g, Text: hostTextReader(t)})
	pdf := filepath.Join(t.TempDir(), "unopened.pdf")
	if err := os.WriteFile(pdf, []byte("not a PDF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := g.Count(t.Context(), pdf)
	if err == nil || !strings.Contains(err.Error(), "Couldn't initialise file") {
		t.Fatalf("Count returned %v, want gs's message quoted", err)
	}
	count := err.Error()
	for _, tc := range []struct {
		maxPixels int64
		want      string
	}{{0, count}, {1 << 30, strings.Replace(count, ": page count of ", ": page sizes of ", 1)}} {
		req := document.ImagesRequest{Dir: t.TempDir(), Concurrency: 2, MaxPixels: tc.maxPixels}
		var errs []string
		for _, err := range p.Images(t.Context(), pdf, req) {
			errs = append(errs, fmt.Sprint(err))
		}
		if len(errs) != 1 || errs[0] != tc.want {
			t.Errorf("MaxPixels %d: the stream yielded %q, want the one error %s", tc.maxPixels, errs, tc.want)
		}
	}
}
