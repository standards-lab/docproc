package report_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/goldentest"
	"github.com/standards-lab/docproc/rundir"

	"github.com/standards-lab/docproc/cmd/docproc/internal/report"
)

// The fixture's runs, under testdata/rundir: ended, over five inputs, two of them one content, an
// archive with parts found two deep and a part whose parent had no line of its own, an input
// stopped before its own event, and an input whose name and outputs are hostile HTML; and unended,
// its last line torn.
const (
	ended   = "019a5f3c-1d2e-7a10-8b00-0000000000a1"
	unended = "019a5f3c-1d2e-7a10-8b00-0000000000a2"
)

// The fixture's contents: the PDF two inputs hold, and the archive.
const (
	pdfSHA     = "f90199938cc629a711b4ce5a349f5606d96acde9ef44abfab0d167826f4b60df"
	archiveSHA = "f04403b02fdd738868d5c683872140987b664d03f8a29aec13ee88f3b271d014"
)

// fixture returns a copy of the committed run directory, opened to read, and its root, so a
// render writes nothing under testdata.
func fixture(t *testing.T) (*rundir.Dir, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "rundir"))); err != nil {
		t.Fatal(err)
	}
	d, err := rundir.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return d, root
}

// render renders the run id of the run directory at root and returns its pages, by file name.
func render(t *testing.T, d *rundir.Dir, root, id string) map[string]string {
	t.Helper()
	if err := report.Render(t.Context(), d, root, id); err != nil {
		t.Fatal(err)
	}
	return pages(t, reportDir(t, d, root, id))
}

// reportDir returns the report directory of the run id, under root.
func reportDir(t *testing.T, d *rundir.Dir, root, id string) string {
	t.Helper()
	rel, err := d.ReportDir(id)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// pages returns every file in dir, by name.
func pages(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(b)
	}
	return got
}

// TestRender_Golden: each fixture run's pages, compared as text with the committed goldens under
// testdata/golden/<run>/, the same files and the same bytes. With goldentest.RecordEnv set to 1,
// the goldens are written.
func TestRender_Golden(t *testing.T) {
	d, root := fixture(t)
	for _, id := range []string{ended, unended} {
		got := render(t, d, root, id)
		dir := filepath.Join("testdata", "golden", id)
		if os.Getenv(goldentest.RecordEnv) == "1" {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range got {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		want := pages(t, dir)
		if g, w := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)); !slices.Equal(g, w) {
			t.Errorf("run %s: pages %v, want %v", id, g, w)
		}
		for name, body := range want {
			if got[name] != body {
				t.Errorf("run %s: %s differs from its golden; %s=1 rewrites it, and git diff shows how", id, name, goldentest.RecordEnv)
			}
		}
	}
}

// TestRender_Pages: the ended run's index and one page per distinct content: the PDF's two inputs
// share one page, the input stopped before its own event has none.
func TestRender_Pages(t *testing.T) {
	d, root := fixture(t)
	got := render(t, d, root, ended)
	names := slices.Sorted(maps.Keys(got))
	if len(names) != 4 || !slices.Contains(names, "index.html") || !slices.Contains(names, pdfSHA+".html") ||
		!slices.Contains(names, archiveSHA+".html") {
		t.Fatalf("pages %v, want index.html and three contents' pages, the PDF's and the archive's among them", names)
	}
	pdf := got[pdfSHA+".html"]
	for _, path := range []string{"/docs/a/report.pdf", "/docs/b/copy of report.pdf"} {
		if !strings.Contains(pdf, path) {
			t.Errorf("the PDF's page doesn't list its input %s", path)
		}
	}
	// The input stopped before its own event is on the index, unlinked.
	index := got["index.html"]
	if !strings.Contains(index, "<code>/docs/locked.pdf</code></td>") || !strings.Contains(index, "error") {
		t.Errorf("the index doesn't list /docs/locked.pdf, unlinked, as an error:\n%s", index)
	}
}

// TestRender_Tree: the archive's parts nest by their parent IDs, two deep, and the part whose
// parent had no line of its own stays at the top of the tree.
func TestRender_Tree(t *testing.T) {
	d, root := fixture(t)
	page := render(t, d, root, ended)[archiveSHA+".html"]
	inner := strings.Index(page, "<code>/inner.zip</code>")
	voice := strings.Index(page, "<code>/inner.zip/voice.wav</code>")
	lost := strings.Index(page, "<code>/missing/lost.txt</code>")
	if inner < 0 || voice < 0 || lost < 0 {
		t.Fatalf("the archive's page lacks a part:\n%s", page)
	}
	// Each nesting opens a list; voice.wav is one deeper than inner.zip, and lost.txt is back at
	// the top.
	depth := func(at int) int {
		return strings.Count(page[:at], "<ul") - strings.Count(page[:at], "</ul>")
	}
	if depth(voice) != depth(inner)+1 {
		t.Errorf("voice.wav at depth %d, want one under inner.zip's %d", depth(voice), depth(inner))
	}
	top := strings.Index(page, "<code>/</code>")
	if depth(lost) != depth(top) {
		t.Errorf("lost.txt at depth %d, want the tree's top, %d", depth(lost), depth(top))
	}
}

// TestRender_Deterministic: a second render of the same run directory, under UTC this time,
// renders the same bytes.
func TestRender_Deterministic(t *testing.T) {
	d, root := fixture(t)
	first := render(t, d, root, ended)
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
	if again := render(t, d, root, ended); !maps.Equal(again, first) {
		t.Error("a second render differs from the first")
	}
}

// TestRender_Replaces: a render replaces the earlier pages whole, a page no render writes among
// them, and removes the working directories a killed render left.
func TestRender_Replaces(t *testing.T) {
	d, root := fixture(t)
	dir := reportDir(t, d, root, ended)
	render(t, d, root, ended)
	if err := os.WriteFile(filepath.Join(dir, "stale.html"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(filepath.Dir(dir), ".report-killed")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatal(err)
	}
	got := render(t, d, root, ended)
	if _, ok := got["stale.html"]; ok {
		t.Error("a render kept a page it didn't write")
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"manifest.json", "record.jsonl", "report"}; !slices.Equal(names, want) {
		t.Errorf("the run's directory holds %v, want %v", names, want)
	}
}

// TestRender_Unended: an unended run renders what its record holds, its torn last line dropped,
// marked not ended.
func TestRender_Unended(t *testing.T) {
	d, root := fixture(t)
	got := render(t, d, root, unended)
	if !strings.Contains(got["index.html"], "not ended") {
		t.Error("the unended run's index doesn't say it has not ended")
	}
	if !strings.Contains(got["index.html"], "/docs/c.txt") || strings.Contains(got["index.html"], "/docs/d.txt") {
		t.Error("the unended run's index doesn't list its whole lines' input alone")
	}
}

// TestRender_NoRun: an id that isn't a run of the directory is refused, matching fs.ErrNotExist,
// and writes nothing.
func TestRender_NoRun(t *testing.T) {
	d, root := fixture(t)
	err := report.Render(t.Context(), d, root, "019a5f3c-1d2e-7a10-8b00-0000000000ff")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Render of no run: %v, want fs.ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Join(root, "runs", "019a5f3c-1d2e-7a10-8b00-0000000000ff")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Render of no run wrote its directory: %v", err)
	}
}
