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

const fixturesGolden = "testdata/fixtures.tsv"

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

func copyFixtures(t *testing.T, fixtures []string) map[string]string {
	t.Helper()
	root := t.TempDir()
	copies := map[string]string{"": root}
	for _, path := range fixtures {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := strings.CutPrefix(filepath.ToSlash(path), "../")
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
		copies[path] = dst
	}
	return copies
}

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
	copies := copyFixtures(t, fixtures)
	got := map[string]string{}
	for ev, err := range d.Pool(t.Context(), func(yield func(dispatch.Input) bool) {
		for _, path := range fixtures {
			if !yield(dispatch.Input{Path: copies[path]}) {
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
		rel, _ := filepath.Rel(copies[""], ev.Input.Path)
		rel = filepath.ToSlash(rel)
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
