package report_test

import (
	"errors"
	"html"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/goldentest"
	"github.com/standards-lab/docproc/rundir"

	"github.com/standards-lab/docproc/cmd/docproc/internal/report"
)

const (
	ended   = "019a5f3c-1d2e-7a10-8b00-0000000000a1"
	unended = "019a5f3c-1d2e-7a10-8b00-0000000000a2"
)

const (
	oddSHA     = "956e499b5e521771b3584eaa57b659d9bae2cebed6c9b9a66b1bde9ef33ab03e"
	exeSHA     = "d9f0fc6ffd4c580cd4bd9f56d16d92f1e74ab539039d33d33ab256c8995b5b9d"
	pdfSHA     = "f90199938cc629a711b4ce5a349f5606d96acde9ef44abfab0d167826f4b60df"
	archiveSHA = "f04403b02fdd738868d5c683872140987b664d03f8a29aec13ee88f3b271d014"
)

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

func render(t *testing.T, d *rundir.Dir, root, id string) map[string]string {
	t.Helper()
	if err := report.Render(t.Context(), d, root, id); err != nil {
		t.Fatal(err)
	}
	return pages(t, reportDir(t, d, root, id))
}

func reportDir(t *testing.T, d *rundir.Dir, root, id string) string {
	t.Helper()
	rel, err := d.ReportDir(id)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

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
	locked := row(got["index.html"], "/docs/locked.pdf")
	if strings.Contains(locked, "href") || !strings.Contains(locked, `data-outcome="error"`) ||
		!strings.Contains(locked, "permission denied") {
		t.Errorf("the index's row of /docs/locked.pdf isn't unlinked, as an error, with its error: %q", locked)
	}
	if link := row(got["index.html"], "/docs/b/copy of report.pdf"); !strings.Contains(link, `href="`+pdfSHA+`.html"`) {
		t.Errorf("the index's row of the PDF's copy doesn't link the PDF's page: %q", link)
	}
}

func row(page, path string) string {
	for line := range strings.Lines(page) {
		if strings.HasPrefix(line, "<tr><td>") && strings.Contains(line, "<code>"+path+"</code>") {
			return line
		}
	}
	return ""
}

func TestRender_Tree(t *testing.T) {
	d, root := fixture(t)
	page := render(t, d, root, ended)[archiveSHA+".html"]
	inner := strings.Index(page, "<code>/inner.zip</code>")
	voice := strings.Index(page, "<code>/inner.zip/voice.wav</code>")
	lost := strings.Index(page, "<code>/missing/lost.txt</code>")
	if inner < 0 || voice < 0 || lost < 0 {
		t.Fatalf("the archive's page lacks a part:\n%s", page)
	}
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

func TestRender_Unended(t *testing.T) {
	d, root := fixture(t)
	got := render(t, d, root, unended)
	if !strings.Contains(got["index.html"], "not ended") {
		t.Error("the unended run's index doesn't say it has not ended")
	}
	if row(got["index.html"], "/docs/c.txt") == "" || row(got["index.html"], "/docs/d.txt") != "" {
		t.Error("the unended run's index doesn't list its whole lines' input alone")
	}
	if strings.Contains(render(t, d, root, ended)["index.html"], "not ended") {
		t.Error("the ended run's index says it has not ended")
	}
}

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

func TestRender_Escapes(t *testing.T) {
	d, root := fixture(t)
	got := render(t, d, root, ended)
	odd := got[oddSHA+".html"]
	for _, want := range []string{
		"<pre>&lt;/pre&gt;&lt;script&gt;alert(1)&lt;/script&gt;\n&lt;!-- hostile --&gt;\n</pre>",
		"<code>/docs/&lt;i&gt;odd&lt;/i&gt;.txt</code>",
	} {
		if !strings.Contains(odd, want) {
			t.Errorf("the hostile input's page lacks %q", want)
		}
	}
	if want := "docproc-mail: exit 3: &lt;/pre&gt;&lt;script&gt;bad&lt;/script&gt;"; !strings.Contains(got[archiveSHA+".html"], want) {
		t.Errorf("the archive's page lacks the failed part's reason, escaped, %q", want)
	}
}

func TestRender_Outputs(t *testing.T) {
	d, root := fixture(t)
	got := render(t, d, root, ended)
	pdf, archive := got[pdfSHA+".html"], got[archiveSHA+".html"]
	for _, want := range []string{
		"<pre>Quarterly report\nA line with &lt;/pre&gt;&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; more.\n\nThe end, no newline after it.</pre>",
		`<img src="../../../content/43/4371149be76808ede2e39736bd07c9a9209f1d6207cfb3a530c7a2e84ab1a5a2"`,
		"&lt;b&gt;Ada&lt;/b&gt;",
		`<a href="file:///docs/b/copy%20of%20report.pdf">`,
	} {
		if !strings.Contains(pdf, want) {
			t.Errorf("the PDF's page lacks %q", want)
		}
	}
	for _, want := range []string{
		"<pre># Notes\n\n*Keep* &lt;this&gt; &amp; that.\n\n- one\n- two\n</pre>",
		`<audio controls preload="none" src="../../../content/`,
		`">content</a>`,
		"<code>quarantine/" + exeSHA + "</code>",
	} {
		if !strings.Contains(archive, want) {
			t.Errorf("the archive's page lacks %q", want)
		}
	}
	if strings.Contains(archive, `href="../../../quarantine`) {
		t.Error("the archive's page links the kept file")
	}
}

var attrs = regexp.MustCompile(`(?:href|src)="([^"]*)"`)

func TestRender_Links(t *testing.T) {
	d, root := fixture(t)
	got := render(t, d, root, ended)
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(moved, "runs", ended, "report")
	links := 0
	for name, page := range got {
		for _, m := range attrs.FindAllStringSubmatch(page, -1) {
			ref := html.UnescapeString(m[1])
			if strings.HasPrefix(ref, "file:///docs/") {
				continue
			}
			u, err := url.Parse(ref)
			if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "/") {
				t.Errorf("%s: link %q isn't relative", name, ref)
				continue
			}
			target := filepath.Join(dir, filepath.FromSlash(u.Path))
			if !strings.HasPrefix(target, moved+string(filepath.Separator)) {
				t.Errorf("%s: link %q leaves the run directory", name, ref)
			}
			if info, err := os.Stat(target); err != nil || !info.Mode().IsRegular() {
				t.Errorf("%s: link %q resolves to no file: %v", name, ref, err)
			}
			links++
		}
	}
	if links == 0 {
		t.Error("the pages hold no link")
	}
}

func TestRender_Header(t *testing.T) {
	d, root := fixture(t)
	index := render(t, d, root, ended)["index.html"]
	for _, want := range []string{
		"<h1>docproc run <code>" + ended + "</code></h1>",
		"<dt>Start</dt><dd class=\"mono\">2026-11-07T15:00:00.123456789Z</dd>",
		"<dt>End</dt><dd class=\"mono\">2026-11-07T15:00:04.123456789Z <span class=\"muted\">(4s)</span></dd>",
		"command <code>(devel)</code>, library <code>v0.3.0</code>",
		"<dt>Workers</dt><dd>2</dd>",
		"<dt>Arguments</dt><dd><code>/docs</code></dd>",
		"<dt>Collected</dt><dd>5 inputs</dd>",
		"<td class=\"error\">soffice: not found</td>",
		"<tr><td>video</td><td>excluded</td>",
		"<dt>Error</dt><dd class=\"error\">dispatch: index: write index/ab: no space left on device &lt;full&gt;</dd>",
		"<tr><td>archive</td><td><span class=\"badge\" data-outcome=\"prepared\">prepared</span></td><td class=\"n\">1</td><td class=\"n\">2</td></tr>",
		"<tr><td>-</td><td><span class=\"badge\" data-outcome=\"error\">error</span></td><td class=\"n\">1</td><td class=\"n\">1</td></tr>",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("the index lacks %q", want)
		}
	}
}

func TestRender_SelfContained(t *testing.T) {
	d, root := fixture(t)
	for name, page := range render(t, d, root, ended) {
		for _, bad := range []string{"<script", "<link", "@import", "url(", "http:", "https:"} {
			if strings.Contains(strings.ToLower(page), bad) {
				t.Errorf("%s holds %q", name, bad)
			}
		}
		if !strings.Contains(page, "prefers-color-scheme: dark") {
			t.Errorf("%s doesn't follow the reader's scheme", name)
		}
	}
}

func TestRender_Missing(t *testing.T) {
	d, root := fixture(t)
	const png = "4371149be76808ede2e39736bd07c9a9209f1d6207cfb3a530c7a2e84ab1a5a2"
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rundir.ContentPath(png)))); err != nil {
		t.Fatal(err)
	}
	pdf := render(t, d, root, ended)[pdfSHA+".html"]
	if strings.Contains(pdf, "<img") || !strings.Contains(pdf, "The content store holds no content by this SHA-256.") {
		t.Error("the PDF's page shows or links the missing image, or doesn't say it is missing")
	}
	if !strings.Contains(pdf, "Quarterly report") {
		t.Error("the PDF's page lacks its text, which the store holds")
	}
}
