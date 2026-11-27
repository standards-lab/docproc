package apptest

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/docproc/stages"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
	"github.com/standards-lab/docproc/cmd/docproc/internal/run"
)

// Recorder records events in order. It is safe for concurrent use, as a
// value's Start or Shutdown may record from its own goroutine. The zero
// value is ready to use.
type Recorder struct {
	mu     sync.Mutex
	events []string
}

// Record appends event.
func (r *Recorder) Record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// Log returns a copy of the events recorded so far.
func (r *Recorder) Log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// Builds returns a Recorder of the name of each node a's runs build, in
// the order each Build begins them: depth-first from the roots, each node
// once per Build, a node whose constructor fails included. A run that
// builds nothing records nothing. Call it before a runs: the graph calls
// only the observers it has when a Build begins.
func Builds(a *app.App) *Recorder {
	r := &Recorder{}
	a.Graph().Observe(func(ref graph.Ref) { r.Record(ref.Name()) })
	return r
}

// ErrHalted is what a run of an App given to [Halt] fails with, labelled
// "lifecycle config".
var ErrHalted = errors.New("halted")

// Halt makes a's lifecycle configuration node fail with ErrHalted. The
// dispatcher adds that node to every Build of a command that declares
// nodes, so such a run fails its Build before anything starts, and a run
// whose constructors do no I/O does none at all. A run that builds nothing,
// such as help or version, is unaffected. Halt is a Replace, so it panics
// once a has run a Build.
func Halt(a *app.App) {
	a.Graph().Replace(a.Nodes().LifecycleConfig, func(*graph.Scope) (lifecycle.Config, error) {
		return lifecycle.Config{}, ErrHalted
	})
}

// Stages makes build a's stage builder, in place of stages.Build over the host's PATH, so a test
// decides the stages a run dispatches to, stand-in stages among them. build is given the run's
// options, its --exclude routes included. Stages is a Replace, so it panics once a has run a
// Build.
func Stages(a *app.App, build run.StageBuilder) {
	a.Graph().Replace(a.Nodes().StageBuilder, func(*graph.Scope) (run.StageBuilder, error) {
		return build, nil
	})
}

// Tools makes a's runs build docproc's stages, stages.Build, over the tools lookup finds instead
// of the host's PATH, the docker CLI included, so a test decides which tools a run has, stand-ins
// among them. Tools is a Replace, so it panics once a has run a Build.
func Tools(a *app.App, lookup stages.Lookup) {
	Stages(a, func(ctx context.Context, opts stages.Options) (stages.Set, error) {
		return stages.Build(ctx, lookup, opts)
	})
}

// ErrNoTool is what [NoTools] fails every lookup with.
var ErrNoTool = errors.New("no such tool")

// NoTools is the lookup of a host with no tool: it finds no program. Given to [Tools], it leaves
// out every stage that needs a tool, wiring the text stage alone, and runs nothing on the host.
func NoTools(string) (string, error) { return "", ErrNoTool }
