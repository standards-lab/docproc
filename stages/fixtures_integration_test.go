//go:build linux && integration

package stages_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/internal/goldentest"
	"github.com/standards-lab/docproc/stages"
)

// fixturesGolden is the whole-fixture test's golden: one line per committed fixture, its path from
// the module's root, its route, and its outcome, tab-separated, in path order.
const fixturesGolden = "testdata/fixtures.tsv"

// committedFixtures returns every file under each of the base module's packages' testdata
// directories, the stages package's own golden aside, as paths from this package's directory, in
// path order: the module's committed fixtures, whatever their kind.
func committedFixtures(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (path == filepath.Join("..", "tools") || strings.HasPrefix(d.Name(), ".") && path != ".."):
			return filepath.SkipDir
		case d.IsDir() || !slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "testdata"):
			return nil
		case path == filepath.Join("..", "stages", fixturesGolden):
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(paths)
	return paths
}

// TestEveryFixture dispatches every committed fixture through Build's Set over this machine's
// tools, the same-checkout docproc tools among them, under a host's bomb rule and entry bound, so
// a bomb fixture is held to it rather than expanded, and checks each fixture's route and outcome
// against the golden, as text: the whole module's fixtures read as a host reads them, through
// stages and dispatch, four at once, as a host's pool workers run them. The bomb rule matters to
// the others too: an expanded bomb fills the working directory's filesystem, and soffice, its
// user installation not written, then fails whatever legacy document runs beside it.
func TestEveryFixture(t *testing.T) {
	set := builtSet(t, stages.Options{Inspect: inspect.Options{BombRatio: 100, BombSize: 64 << 20, Entries: 10000}})
	d, err := dispatch.New(dispatch.Config{
		Stages: set.Stages, Inspect: set.Inspect, Index: dispatch.NewMemoryIndex(0), Content: dispatch.NewMemoryContent(),
		Puts: 4, Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixtures := committedFixtures(t)
	got := map[string]string{}
	for ev, err := range d.Pool(t.Context(), func(yield func(dispatch.Input) bool) {
		for _, path := range fixtures {
			if !yield(dispatch.Input{Path: path}) {
				return
			}
		}
	}, 4) {
		if err != nil {
			t.Fatalf("%s: %v", ev.Input.Path, err)
		}
		e := ev.Event
		if e.Part.Depth != 0 || e.Output != nil || e.Warning != nil {
			continue
		}
		rel, _ := strings.CutPrefix(filepath.ToSlash(ev.Input.Path), "../")
		got[rel] = string(e.Part.ContentType.Route) + "\t" + string(e.Outcome)
		if e.Outcome == dispatch.Failed {
			t.Logf("%s: %s", rel, e.Reason)
		}
	}
	var b strings.Builder
	for _, path := range fixtures {
		rel, _ := strings.CutPrefix(filepath.ToSlash(path), "../")
		outcome, ok := got[rel]
		if !ok {
			t.Errorf("%s: no event of its own", rel)
		}
		b.WriteString(rel + "\t" + outcome + "\n")
	}
	if os.Getenv(goldentest.RecordEnv) == "1" {
		if err := os.WriteFile(fixturesGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(fixturesGolden)
	if err != nil {
		t.Fatal(err)
	}
	gotLines, wantLines := strings.Split(b.String(), "\n"), strings.Split(string(want), "\n")
	for _, line := range wantLines {
		if !slices.Contains(gotLines, line) {
			t.Errorf("want %q", line)
		}
	}
	for _, line := range gotLines {
		if !slices.Contains(wantLines, line) {
			t.Errorf(" got %q", line)
		}
	}
}
