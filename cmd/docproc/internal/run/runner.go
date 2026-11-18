package run

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/docproc/stages"
)

// puts is how many of a part's outputs a dispatch puts into the run directory's content store at
// once, dispatch.Config.Puts: a put is a copy on the local disk, which the workers' dispatches
// already keep busy, so one at a time is enough.
const puts = 1

// runner is one run of the command: its configuration, the terminal it prints to, the run
// directory it writes, its path arguments and the inputs collected from them, and how many are
// dispatched at once.
type runner struct {
	cfg     Config
	w       io.Writer
	out     string
	args    []string
	inputs  []input
	workers int
}

// run builds the stages, prints the stage table, creates the run directory, and begins a run in
// it whose manifest names its tools, its stage table, its docproc build, and its inputs; then it
// dispatches every input on the dispatcher's pool, writing one record line per event, and ends
// the run, printing the closing summary. Its working directory, under the run directory, is
// removed once the dispatch is done.
//
// An error that stops one input's dispatch is that input's error line, and the others go on. A
// failure of the index or the content store, a *dispatch.StoreError, stops the run, as do the
// context ending and the run directory or the terminal refusing a write: the run ends with that
// error, which run returns once the manifest names it.
func (r *runner) run(ctx context.Context) (err error) {
	set, err := r.cfg.Build(ctx, r.cfg.Stages)
	if err != nil {
		return err
	}
	if err := writeTable(r.w, set); err != nil {
		return err
	}
	dir, err := rundir.Create(r.out)
	if err != nil {
		return err
	}
	m := schema.Manifest{
		Docproc:   r.cfg.Docproc,
		Workers:   r.workers,
		Inputs:    []string{},
		Collected: len(r.inputs),
		Tools:     append([]dispatch.Tool{}, set.Tools...),
		Stages:    append([]dispatch.Row{}, set.Rows...),
	}
	for _, arg := range r.args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return err
		}
		m.Inputs = append(m.Inputs, abs)
	}
	run, err := dir.Begin(m)
	if err != nil {
		return err
	}
	rec := &recorder{
		dir: dir, run: run, inputs: map[string]input{}, first: map[string]time.Time{},
		term: &terminal{w: r.w, tally: map[tallyKey]int{}},
	}
	for _, in := range r.inputs {
		rec.inputs[in.abs] = in
	}
	stopped := r.dispatch(ctx, set, rec)
	ended := run.End(stopped)
	return errors.Join(stopped, ended, rec.term.summary(run.ID()))
}

// dispatch dispatches the inputs over set's stages, under its validated inspection options, on a
// dispatcher over the run directory rec writes, in a working directory of its own under it,
// removed once the pool returns. It returns the error that stopped the run, if any.
func (r *runner) dispatch(ctx context.Context, set stages.Set, rec *recorder) (err error) {
	work, err := os.MkdirTemp(r.out, ".work-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(work)) }()
	d, err := dispatch.New(dispatch.Config{
		Stages:  set.Stages,
		Index:   rec.dir.Index(),
		Content: rec.dir.Content(),
		Dir:     work,
		Limits:  r.cfg.Limits,
		Inspect: set.Inspect,
		Puts:    puts,
		Release: r.cfg.Release,
	})
	if err != nil {
		return err
	}
	return rec.loop(d.Pool(ctx, feed(r.inputs), r.workers))
}

// recorder turns a run's events into its record and its terminal lines. It runs on the pool's
// loop alone, so it holds no lock.
type recorder struct {
	dir    *rundir.Dir
	run    *rundir.Run
	inputs map[string]input     // each input by its absolute path, as the pool was given it
	first  map[string]time.Time // when each input's first event arrived, until its last
	term   *terminal
}

// feed yields the inputs as the pool takes them, each by its absolute path.
func feed(inputs []input) iter.Seq[dispatch.Input] {
	return func(yield func(dispatch.Input) bool) {
		for _, in := range inputs {
			if !yield(dispatch.Input{Path: in.abs}) {
				return
			}
		}
	}
}

// loop ranges over the pool's events, writing each one's record line, and returns the error that
// stops the run: the pool's own, yielded alone, a store's failure, or a line the run directory or
// the terminal refused. An input's other error is its error line, and the pool goes on.
func (rec *recorder) loop(events iter.Seq2[dispatch.PoolEvent, error]) error {
	for pe, err := range events {
		var store *dispatch.StoreError
		switch {
		case err != nil && (pe.Input.Path == "" || errors.As(err, &store)):
			return err
		case err != nil:
			err = rec.stopped(pe.Input.Path, err)
		default:
			err = rec.event(pe.Input.Path, pe.Event)
		}
		// The loop's return stops the pool.
		if err != nil {
			return err
		}
	}
	return nil
}

// event writes ev's line, an event of the input at path. A part line whose outcome is
// quarantined names the part's kept file, which it keeps first when the part has one; the
// input's own part line, its last, carries its file and its time, and is its terminal line.
func (rec *recorder) event(path string, ev dispatch.Event) error {
	now := rec.arrived(path)
	line := schema.Event{Kind: schema.PartEvent, Input: path, Event: ev}
	switch {
	case ev.Output != nil:
		line.Kind = schema.OutputEvent
	case ev.Warning != nil:
		line.Kind = schema.WarningEvent
	}
	if line.Kind != schema.PartEvent {
		return rec.run.Write(line)
	}
	if ev.Outcome == dispatch.Failed {
		rec.term.failed++
	}
	if ev.Outcome == dispatch.Quarantined {
		q, err := rec.kept(ev.Part)
		if err != nil {
			return err
		}
		line.Quarantine = q
	}
	if ev.Part.Depth > 0 {
		return rec.run.Write(line)
	}
	in := rec.inputs[path]
	span := rec.span(path, now)
	line.File, line.Time = rec.file(in), &span
	if err := rec.run.Write(line); err != nil {
		return err
	}
	return rec.term.input(in.path, ev, span)
}

// stopped writes the error line of the input at path, whose dispatch stop stopped before its own
// event, with its file, when it can still be read, and its time, and its terminal line.
func (rec *recorder) stopped(path string, stop error) error {
	now := rec.arrived(path)
	in := rec.inputs[path]
	span := rec.span(path, now)
	line := schema.Event{Kind: schema.ErrorEvent, Input: path, File: rec.file(in), Time: &span, Error: stop.Error()}
	if err := rec.run.Write(line); err != nil {
		return err
	}
	return rec.term.stopped(in.path, stop, span)
}

// arrived returns now, noting it as the time of the first event of the input at path when none
// has arrived yet.
func (rec *recorder) arrived(path string) time.Time {
	now := time.Now()
	if _, ok := rec.first[path]; !ok {
		rec.first[path] = now
	}
	return now
}

// span returns the time of the input at path, from its first event to end, its last, and
// forgets the first.
func (rec *recorder) span(path string, end time.Time) schema.Span {
	start := rec.first[path]
	delete(rec.first, path)
	return schema.Span{Start: schema.TimeOf(start), End: schema.TimeOf(end)}
}

// file returns in's file metadata, or nil when it can't be read.
func (rec *recorder) file(in input) *schema.File {
	f, err := fileOf(in)
	if err != nil {
		return nil
	}
	return f
}

// kept returns the name of p's kept file, quarantine/<sha256>, keeping p's file first when it has
// one and none is kept yet, or "" when p has no file and none is kept: a part replayed from the
// index has no file, and its kept file is there when an earlier run kept it. rundir.Quarantine
// returns a kept file's name without reading the path it is given, so a part with no file is
// asked for by the empty path, whose open fails matching fs.ErrNotExist when nothing is kept.
func (rec *recorder) kept(p dispatch.Part) (string, error) {
	name, err := rec.dir.Quarantine(p.SHA256, p.Path)
	if p.Path == "" && errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return name, err
}
