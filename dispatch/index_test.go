package dispatch_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
)

// indexes returns a constructor for each shipped index. dispatchtest's suites hold both to the
// contract; the tests here are the walk's over them and each one's own.
func indexes() map[string]func(*testing.T) dispatch.Index {
	return map[string]func(*testing.T) dispatch.Index{
		"memory": func(*testing.T) dispatch.Index { return dispatch.NewMemoryIndex(0) },
		"file": func(t *testing.T) dispatch.Index {
			idx, err := dispatch.NewFileIndex(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return idx
		},
	}
}

// waitSignal is an index that tells waiting when a Wait begins.
type waitSignal struct {
	dispatch.Index
	waiting chan struct{}
}

func (w *waitSignal) Wait(ctx context.Context, key dispatch.Key) (dispatch.Entry, bool, error) {
	select {
	case w.waiting <- struct{}{}:
	default:
	}
	return w.Index.Wait(ctx, key)
}

// TestConcurrentDispatch: two dispatches of the same content over one index, run together: one
// claims and prepares it, the other waits on the claim, then reports it seen, with the outputs
// the first recorded; the stage runs once.
func TestConcurrentDispatch(t *testing.T) {
	for name, newIndex := range indexes() {
		t.Run(name, func(t *testing.T) {
			idx := &waitSignal{Index: newIndex(t), waiting: make(chan struct{}, 1)}
			f := newFixture(t, map[string]string{}, idx, dispatch.Limits{})
			f.text.entered = make(chan string, 1)
			f.text.release = make(chan struct{})
			content := "block"
			results := make(chan []dispatched, 2)
			run := func(path string) {
				var g gatherer
				var events []dispatched
				for ev, err := range f.d.Dispatch(t.Context(), dispatch.Input{Path: path}) {
					if err != nil {
						t.Error(err)
						break
					}
					if d, ok := g.add(t, ev); ok {
						events = append(events, d)
					}
				}
				results <- events
			}
			go run(input(t, "first.txt", content))
			<-f.text.entered
			go run(input(t, "second.txt", content))
			// A second dispatch that enters the stage, rather than waiting on the claim, is a
			// duplicate prepare.
			select {
			case <-idx.waiting:
			case name := <-f.text.entered:
				close(f.text.release)
				t.Fatalf("%s entered the stage while another dispatch held the claim", name)
			}
			close(f.text.release)
			outcomes := map[dispatch.Outcome]int{}
			for range 2 {
				events := <-results
				if len(events) != 1 {
					t.Fatalf("events: %v", steps(events))
				}
				outcomes[events[0].Outcome]++
				if events[0].Outcome == dispatch.Seen && len(events[0].Outputs) != 1 {
					t.Errorf("seen without the recorded outputs: %+v", events[0])
				}
			}
			if outcomes[dispatch.Prepared] != 1 || outcomes[dispatch.Seen] != 1 {
				t.Fatalf("outcomes: %v", outcomes)
			}
			if n := f.text.called("first.txt") + f.text.called("second.txt"); n != 1 {
				t.Fatalf("stage ran %d times", n)
			}
		})
	}
}

// storeFiles returns the paths of the files under dir, relative to it, sorted.
func storeFiles(t *testing.T, dir string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		got = append(got, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

// fileKey is the key of the file index's tests.
var fileKey = dispatch.Key{
	SHA256: strings.Repeat("ab", 32), Stage: dispatch.Identity{Name: "s", Version: "1", Config: "c"},
}

// fileWarning is a log item for the file index's tests.
func fileWarning(where string) dispatch.Recorded {
	return dispatch.Recorded{Warning: &dispatch.Warning{Tool: "t", Where: where, What: "w"}}
}

// TestFileIndexLayout: a claim's log is written under a temporary name beside its entry, in the
// directory of the key's name's first two hex digits, and renamed at Record; the entry it replaces
// loses its log, and a FileIndex opened again over the directory reads the entry and its log.
func TestFileIndexLayout(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	idx, err := dispatch.NewFileIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	record := func(where string) {
		t.Helper()
		token, lease, ok, err := idx.Claim(ctx, fileKey)
		if err != nil || !ok || lease != 0 {
			t.Fatalf("claim: %v, %s, %v, want a claim with no lease", ok, lease, err)
		}
		if err := idx.Append(ctx, fileKey, token, fileWarning(where)); err != nil {
			t.Fatal(err)
		}
		got := storeFiles(t, dir)
		if len(got) != 1 || !strings.HasPrefix(filepath.Base(got[0]), ".tmp-") {
			t.Fatalf("files while claimed: %v, want one temporary log", got)
		}
		if err := idx.Record(ctx, fileKey, token, dispatch.Entry{Outcome: dispatch.Prepared}); err != nil {
			t.Fatal(err)
		}
		got = storeFiles(t, dir)
		if len(got) != 2 || !slices.ContainsFunc(got, func(p string) bool { return strings.HasSuffix(p, ".json") }) ||
			!slices.ContainsFunc(got, func(p string) bool { return strings.HasSuffix(p, ".jsonl") }) {
			t.Fatalf("files once recorded: %v, want an entry and its log", got)
		}
		for _, p := range got {
			if shard := filepath.Dir(p); len(shard) != 2 || !strings.HasPrefix(filepath.Base(p), shard) {
				t.Errorf("%s isn't in the directory of its name's first two hex digits", p)
			}
		}
	}
	record("first")
	if err := idx.Drop(ctx, fileKey); err != nil {
		t.Fatal(err)
	}
	if got := storeFiles(t, dir); len(got) != 0 {
		t.Fatalf("files after a drop: %v, want none", got)
	}
	record("second")

	again, err := dispatch.NewFileIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := again.Lookup(ctx, fileKey); err != nil || !ok {
		t.Fatalf("lookup in the index opened again: %v, %v", ok, err)
	}
	var got []string
	for r, err := range again.Items(ctx, fileKey) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, r.Warning.Where)
	}
	if !slices.Equal(got, []string{"second"}) {
		t.Errorf("items in the index opened again: %v", got)
	}
}

// TestFileIndexLeftovers: the temporary logs and entries a process that died left are removed
// when a FileIndex opens the directory, and a release removes its claim's log.
func TestFileIndexLeftovers(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	idx, err := dispatch.NewFileIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	token, _, _, err := idx.Claim(ctx, fileKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Append(ctx, fileKey, token, fileWarning("dies")); err != nil {
		t.Fatal(err)
	}
	logs, err := filepath.Glob(filepath.Join(dir, "*", ".tmp-*"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("temporary logs: %v, %v, want the claim's", logs, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(logs[0]), ".tmp-123"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := storeFiles(t, dir); len(got) != 2 {
		t.Fatalf("files before opening again: %v", got)
	}
	if _, err := dispatch.NewFileIndex(dir); err != nil {
		t.Fatal(err)
	}
	if got := storeFiles(t, dir); len(got) != 0 {
		t.Errorf("files once opened again: %v, want none", got)
	}

	fresh, err := dispatch.NewFileIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, _, _, err = fresh.Claim(ctx, fileKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Append(ctx, fileKey, token, fileWarning("released")); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Release(ctx, fileKey, token); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := fresh.Lookup(ctx, fileKey); err != nil || ok {
		t.Errorf("lookup after a release: %v, %v", ok, err)
	}
}

// TestNewFileIndex: an empty directory is refused.
func TestNewFileIndex(t *testing.T) {
	if _, err := dispatch.NewFileIndex(""); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("NewFileIndex(\"\"): %v, want ErrRequest", err)
	}
}

// TestContentFile: the content store's layout, the name's first two hex digits, then the whole
// name; a name that isn't a SHA-256 has none.
func TestContentFile(t *testing.T) {
	sum := strings.Repeat("0f", 32)
	if got, want := dispatch.ContentFile(sum), filepath.Join("0f", sum); got != want {
		t.Errorf("ContentFile: %q, want %q", got, want)
	}
	for _, bad := range []string{"", "0f", strings.ToUpper(sum), "../" + sum[3:]} {
		if got := dispatch.ContentFile(bad); got != "" {
			t.Errorf("ContentFile(%q): %q, want none", bad, got)
		}
	}
}

// TestFileContentRenew: a content put again is rewritten once its file is older than the renew
// age, its age restarting, and skipped while it is younger, or always with no renew age; a put
// leaves no temporary file.
func TestFileContentRenew(t *testing.T) {
	ctx := t.Context()
	b := []byte("renewed")
	sum := sha(b)
	old := time.Now().Add(-2 * time.Hour)
	for _, c := range []struct {
		renew   time.Duration
		renewed bool
	}{{0, false}, {time.Hour, true}, {3 * time.Hour, false}} {
		dir := t.TempDir()
		store, err := dispatch.NewFileContent(dir, c.renew)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, sum, strings.NewReader(string(b))); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, dispatch.ContentFile(sum))
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		read := &countingReader{r: strings.NewReader(string(b))}
		if err := store.Put(ctx, sum, read); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if renewed := info.ModTime().After(old.Add(time.Minute)); renewed != c.renewed || (read.n > 0) != c.renewed {
			t.Errorf("renew age %s over a content 2h old: renewed %v, read %d bytes, want renewed %v",
				c.renew, renewed, read.n, c.renewed)
		}
		if got := storeFiles(t, dir); !slices.Equal(got, []string{dispatch.ContentFile(sum)}) {
			t.Errorf("files: %v, want the content's alone", got)
		}
	}
}

// TestNewFileContent: an empty directory and a negative renew age are refused.
func TestNewFileContent(t *testing.T) {
	if _, err := dispatch.NewFileContent("", 0); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("NewFileContent(\"\"): %v, want ErrRequest", err)
	}
	if _, err := dispatch.NewFileContent(t.TempDir(), -time.Second); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("NewFileContent with a negative renew age: %v, want ErrRequest", err)
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}
