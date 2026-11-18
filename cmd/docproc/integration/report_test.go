//go:build integration

package integration_test

import (
	"errors"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docproc/cmd/docproc/integration"
)

// docproc's report as the built binary renders it, over a run directory a built run wrote: its
// pages read back from the run's report directory, every link on them followed to a file.

// indexPage is the report's index page's file name, report.IndexPage's.
const indexPage = "index.html"

// lastLine returns the last line of out, which ends in a newline; "" when out holds none.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	return lines[len(lines)-1]
}

// reportDir returns the absolute report directory of the run id of the run directory d at out.
func reportDir(t *testing.T, d *rundir.Dir, out, id string) string {
	t.Helper()
	rel, err := d.ReportDir(id)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(out, filepath.FromSlash(rel))
}

// recorded returns, from the run id's record, the inputs it names and the distinct contents the
// inputs that reached their own event hold, each by its own part's SHA-256: the contents the
// report gives a page each. An input whose dispatch stopped with an error line holds none, as the
// report counts it. A record that ends unended, rundir.ErrNotEnded, ends the read and no more.
func recorded(t *testing.T, d *rundir.Dir, id string) (inputs map[string]bool, contents map[string]bool) {
	t.Helper()
	own := map[string]string{}
	stopped := map[string]bool{}
	inputs = map[string]bool{}
	for e, err := range d.Record(id) {
		if err != nil {
			if !errors.Is(err, rundir.ErrNotEnded) {
				t.Fatalf("Record: %v", err)
			}
			break
		}
		inputs[e.Input] = true
		switch {
		case e.Kind == schema.ErrorEvent:
			stopped[e.Input] = true
		case e.Kind == schema.PartEvent && e.Part.ID == "/":
			own[e.Input] = e.Part.SHA256
		}
	}
	contents = map[string]bool{}
	for path, sha := range own {
		if !stopped[path] && sha != "" {
			contents[sha] = true
		}
	}
	return inputs, contents
}

// linkAttr matches a page's href and src attributes, their values quoted as html/template quotes
// them.
var linkAttr = regexp.MustCompile(`\s(?:href|src)="([^"]*)"`)

// checkLinks reads the page at path and follows every href and src on it: a relative link
// resolves against the page's directory to a regular file, and a file URL, an input's source,
// names one of inputs. It returns how many links the page holds.
func checkLinks(t *testing.T, path string, inputs map[string]bool) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	links := linkAttr.FindAllStringSubmatch(string(b), -1)
	for _, m := range links {
		link := html.UnescapeString(m[1])
		u, err := url.Parse(link)
		if err != nil {
			t.Errorf("%s links %q, not a URL: %v", path, link, err)
			continue
		}
		var file string
		switch u.Scheme {
		case "file":
			if !inputs[u.Path] {
				t.Errorf("%s links %q, the source of no input of the run", path, link)
				continue
			}
			file = u.Path
		case "":
			if u.Host != "" || filepath.IsAbs(u.Path) {
				t.Errorf("%s links %q, not relative to its directory", path, link)
				continue
			}
			file = filepath.Join(filepath.Dir(path), filepath.FromSlash(u.Path))
		default:
			t.Errorf("%s links %q, outside the run directory and its inputs", path, link)
			continue
		}
		if fi, err := os.Stat(file); err != nil || !fi.Mode().IsRegular() {
			t.Errorf("%s links %q, resolving to %s, not a file: %v", path, link, file, err)
		}
	}
	return len(links)
}

// docproc run --report over every committed fixture renders the run's report once it has ended
// and prints its index's path, as the last line of its stdout: the report directory holds the
// index and one page for each distinct content the run's inputs hold, as its record counts them,
// and every link on every page resolves to a file, a relative one against the page's directory,
// an input's file URL to that input.
func TestRun_Report(t *testing.T) {
	fixtures := integration.Fixtures(t)
	out := filepath.Join(t.TempDir(), "out")

	res := integration.Run(t, runArgs(fixtures, out, "--report")...)

	if res.Code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", res.Code, process.ExitOK, res.Stderr)
	}
	d, id := onlyRun(t, out)
	dir := reportDir(t, d, out, id)
	if got, want := lastLine(res.Stdout), filepath.Join(dir, indexPage); got != want {
		t.Errorf("stdout's last line = %q, want the index's path %q", got, want)
	}
	inputs, contents := recorded(t, d, id)
	if len(contents) == 0 {
		t.Fatal("the record holds no content: the run reached no input's own event")
	}
	want := []string{indexPage}
	for sha := range contents {
		want = append(want, sha+".html")
	}
	slices.Sort(want)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("report directory holds %d files, want the index and %d content pages:\ngot  %q\nwant %q", len(got), len(contents), got, want)
	}
	for _, name := range got {
		page := filepath.Join(dir, name)
		if n := checkLinks(t, page, inputs); n == 0 {
			t.Errorf("%s holds no link", page)
		}
	}
	// Each content page links each input holding it to its source.
	sources := 0
	for sha := range contents {
		b, err := os.ReadFile(filepath.Join(dir, sha+".html"))
		if err != nil {
			continue // its absence is reported above
		}
		sources += strings.Count(string(b), `href="file://`)
	}
	if sources < len(contents) {
		t.Errorf("content pages link %d input sources, want at least one per content, %d", sources, len(contents))
	}
}
