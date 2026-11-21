package dispatch_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

type container struct {
	world   map[string]string
	entered chan string
	release chan struct{}

	mu        sync.Mutex
	calls     map[string]int
	remaining map[string]remaining
}

type remaining struct {
	n  int64
	ok bool
}

func newContainer(world map[string]string) *container {
	return &container{world: world, calls: map[string]int{}, remaining: map[string]remaining{}}
}

func (c *container) remainingOf(name string) (remaining, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.remaining[name]
	return r, ok
}

func (c *container) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "container", Version: "1", Config: "test"}
}

func (c *container) called(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[name]
}

func (c *container) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		c.mu.Lock()
		c.calls[p.Name]++
		c.mu.Unlock()
		b, err := os.ReadFile(p.Path)
		if err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		out := filepath.Join(dir, "lines.txt")
		if err := os.WriteFile(out, fmt.Appendf(nil, "%d", len(lines)), 0o644); err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		o := dispatch.Output{Kind: dispatch.TextOutput, Name: "lines", MediaType: "text/plain", Path: out}
		if !yield(dispatch.OutputItem(o), nil) {
			return
		}
		for i, line := range lines {
			verb, arg, _ := strings.Cut(line, " ")
			where := fmt.Sprintf("line %d", i+1)
			switch verb {
			case "file", "copy":
				var content []byte
				if verb == "file" {
					content = []byte(c.world[arg])
				} else if content, err = os.ReadFile(filepath.Join("..", "inspect", "testdata", arg)); err != nil {
					yield(dispatch.Item{}, err)
					return
				}
				path := filepath.Join(dir, arg)
				if err := os.WriteFile(path, content, 0o644); err != nil {
					yield(dispatch.Item{}, err)
					return
				}
				if !yield(dispatch.PartItem(dispatch.Found{Path: path, Name: arg, Where: where}), nil) {
					return
				}
			case "remaining":
				n, ok := dispatch.Remaining(ctx)
				c.mu.Lock()
				c.remaining[p.Name] = remaining{n, ok}
				c.mu.Unlock()
			case "limited":
				f := dispatch.Found{Name: arg, Where: where, Outcome: dispatch.Limited, Reason: "the stage's own words"}
				if !yield(dispatch.PartItem(f), nil) {
					return
				}
			case "fail":
				yield(dispatch.Item{}, errors.New("container: told to fail"))
				return
			case "noproc":
				yield(dispatch.Item{}, fmt.Errorf("container: %w for this", dispatch.ErrNoProcessor))
				return
			case "encrypted":
				yield(dispatch.Item{}, fmt.Errorf("container: %w content", dispatch.ErrEncrypted))
				return
			case "block":
				c.entered <- p.Name
				select {
				case <-c.release:
				case <-ctx.Done():
					yield(dispatch.Item{}, ctx.Err())
					return
				}
			}
		}
	}
}

type leaf struct {
	name string
	err  error

	mu    sync.Mutex
	calls int
}

func (l *leaf) Identity() dispatch.Identity {
	return dispatch.Identity{Name: l.name, Version: "1", Config: "test"}
}

func (l *leaf) called() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func (l *leaf) Prepare(_ context.Context, p dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		l.mu.Lock()
		l.calls++
		l.mu.Unlock()
		if l.err != nil {
			yield(dispatch.Item{}, l.err)
			return
		}
		yield(dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.RecordOutput, Value: fmt.Appendf(nil, `{"name":%q}`, p.Name),
			Meta: map[string]string{"stage": l.name},
		}), nil)
	}
}

type fixture struct {
	d       *dispatch.Dispatcher
	index   dispatch.Index
	text    *container
	pdf     *leaf
	archive *leaf
	dir     string
}

func newFixture(t *testing.T, world map[string]string, index dispatch.Index, limits dispatch.Limits) *fixture {
	t.Helper()
	if index == nil {
		index = dispatch.NewMemoryIndex(0)
	}
	f := &fixture{
		index:   index,
		text:    newContainer(world),
		pdf:     &leaf{name: "pdf"},
		archive: &leaf{name: "archive", err: errors.New("archive: can't read entry")},
		dir:     t.TempDir(),
	}
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages: dispatch.Stages{route.Text: f.text, route.Document: f.pdf, route.Archive: f.archive},
		Index:  index, Dir: f.dir, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	return f
}

func input(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type step struct {
	name    string
	depth   int
	outcome dispatch.Outcome
}

func (s step) String() string { return fmt.Sprintf("%s@%d:%s", s.name, s.depth, s.outcome) }

type dispatched struct {
	dispatch.Event
	Outputs []dispatch.Output
}

type gatherer struct {
	pending map[string][]dispatch.Output
}

func (g *gatherer) add(t *testing.T, ev dispatch.Event) (dispatched, bool) {
	t.Helper()
	if g.pending == nil {
		g.pending = map[string][]dispatch.Output{}
	}
	if ev.Output != nil {
		g.pending[ev.Part.Path] = append(g.pending[ev.Part.Path], *ev.Output)
		return dispatched{}, false
	}
	outputs := g.pending[ev.Part.Path]
	delete(g.pending, ev.Part.Path)
	if len(outputs) != ev.Outputs {
		t.Errorf("%s: its event counts %d outputs, after %d output events", ev.Part.Name, ev.Outputs, len(outputs))
	}
	return dispatched{Event: ev, Outputs: outputs}, true
}

func collect(t *testing.T, seq iter.Seq2[dispatch.Event, error], each func(*dispatch.Event)) []dispatched {
	t.Helper()
	var g gatherer
	var events []dispatched
	for ev, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		if each != nil {
			each(&ev)
		}
		if d, ok := g.add(t, ev); ok {
			events = append(events, d)
		}
	}
	return events
}

func dispatchAll(t *testing.T, d *dispatch.Dispatcher, path string, each func(*dispatch.Event)) []dispatched {
	t.Helper()
	return collect(t, d.Dispatch(t.Context(), dispatch.Input{Path: path}), each)
}

func steps(events []dispatched) []step {
	out := make([]step, 0, len(events))
	for _, ev := range events {
		out = append(out, step{ev.Part.Name, ev.Part.Depth, ev.Outcome})
	}
	return out
}

func wantSteps(t *testing.T, events []dispatched, want []step) {
	t.Helper()
	if got := steps(events); !slices.Equal(got, want) {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}
}

func hasFlag(flags []inspect.Flag, c inspect.Check) bool {
	return slices.ContainsFunc(flags, func(f inspect.Flag) bool { return f.Check == c })
}

func byName(t *testing.T, events []dispatched, name string) dispatched {
	t.Helper()
	for _, ev := range events {
		if ev.Part.Name == name {
			return ev
		}
	}
	t.Fatalf("no event for %s", name)
	return dispatched{}
}

func logged(t *testing.T, index dispatch.Index, key dispatch.Key) []dispatch.Recorded {
	t.Helper()
	var got []dispatch.Recorded
	for r, err := range index.Items(t.Context(), key) {
		if err != nil {
			t.Fatalf("items: %v", err)
		}
		got = append(got, r)
	}
	return got
}

func children(t *testing.T, index dispatch.Index, key dispatch.Key) []dispatch.Child {
	t.Helper()
	var got []dispatch.Child
	for _, r := range logged(t, index, key) {
		if r.Child != nil {
			got = append(got, *r.Child)
		}
	}
	return got
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantEmpty(t *testing.T, work string) {
	t.Helper()
	if left := files(t, work); len(left) > 0 {
		t.Errorf("files left in the working directory: %q", left)
	}
}
