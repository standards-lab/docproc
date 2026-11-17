package stages_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
)

// input writes content to a file named name in a directory of its own, returning its path.
func input(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// step is one event, as the tests compare them: the part's name, its depth, and its outcome.
type step struct {
	name    string
	depth   int
	outcome dispatch.Outcome
}

func (s step) String() string { return fmt.Sprintf("%s@%d:%s", s.name, s.depth, s.outcome) }

// dispatched is a part's own event as the tests compare them, with its outputs, gathered from
// its output events as the caller's loop body left them.
type dispatched struct {
	dispatch.Event
	Outputs  []dispatch.Output
	Warnings []dispatch.Warning
}

// gatherer gathers each part's output and warning events into its own event, by the part's path, which is
// unique within one dispatch.
type gatherer struct {
	pending  map[string][]dispatch.Output
	warnings map[string][]dispatch.Warning
}

// add takes ev, returning the part's dispatched event and true when ev is a part's own event; it
// fails the test when a part's event's output count isn't its output events'.
func (g *gatherer) add(t *testing.T, ev dispatch.Event) (dispatched, bool) {
	t.Helper()
	if g.pending == nil {
		g.pending = map[string][]dispatch.Output{}
		g.warnings = map[string][]dispatch.Warning{}
	}
	if ev.Warning != nil {
		g.warnings[ev.Part.Path] = append(g.warnings[ev.Part.Path], *ev.Warning)
		return dispatched{}, false
	}
	if ev.Output != nil {
		g.pending[ev.Part.Path] = append(g.pending[ev.Part.Path], *ev.Output)
		return dispatched{}, false
	}
	outputs, warnings := g.pending[ev.Part.Path], g.warnings[ev.Part.Path]
	delete(g.pending, ev.Part.Path)
	delete(g.warnings, ev.Part.Path)
	if len(outputs) != ev.Outputs {
		t.Errorf("%s: its event counts %d outputs, after %d output events", ev.Part.Name, ev.Outputs, len(outputs))
	}
	return dispatched{Event: ev, Outputs: outputs, Warnings: warnings}, true
}

// collect ranges over seq, calling each, when not nil, in the loop's body for every event, its
// output events among them, and returns the parts' events, each with its outputs; an error fails
// the test.
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

// dispatchAll dispatches path, as collect says.
func dispatchAll(t *testing.T, d *dispatch.Dispatcher, path string, each func(*dispatch.Event)) []dispatched {
	t.Helper()
	return collect(t, d.Dispatch(t.Context(), dispatch.Input{Path: path}), each)
}

// steps returns events' steps.
func steps(events []dispatched) []step {
	out := make([]step, 0, len(events))
	for _, ev := range events {
		out = append(out, step{ev.Part.Name, ev.Part.Depth, ev.Outcome})
	}
	return out
}

// wantSteps fails the test unless events' steps are want.
func wantSteps(t *testing.T, events []dispatched, want []step) {
	t.Helper()
	if got := steps(events); !slices.Equal(got, want) {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}
}

// hasFlag reports whether flags hold one of check c.
func hasFlag(flags []inspect.Flag, c inspect.Check) bool {
	return slices.ContainsFunc(flags, func(f inspect.Flag) bool { return f.Check == c })
}

// byName returns the first event of the part named name.
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

// outputNamed returns ev's first output of kind named name.
func outputNamed(t *testing.T, ev dispatched, kind dispatch.OutputKind, name string) dispatch.Output {
	t.Helper()
	for _, o := range ev.Outputs {
		if o.Kind == kind && o.Name == name {
			return o
		}
	}
	t.Fatalf("%s: no %s output named %q among %d", ev.Part.Name, kind, name, len(ev.Outputs))
	return dispatch.Output{}
}

// wheres returns the Where of each event below the input, in order.
func wheres(events []dispatched) []string {
	var out []string
	for _, ev := range events {
		if ev.Part.Depth > 0 {
			out = append(out, ev.Part.Where)
		}
	}
	return out
}

// sum returns content's lowercase hex SHA-256.
func sum(content string) string {
	s := sha256.Sum256([]byte(content))
	return hex.EncodeToString(s[:])
}
