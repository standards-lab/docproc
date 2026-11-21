package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/docproc/stages"

	"github.com/standards-lab/docproc/cmd/docproc/internal/report"
)

const puts = 1

type runner struct {
	cfg     Config
	w       io.Writer
	out     string
	args    []string
	inputs  []input
	workers int
	report  bool
}

func (r *runner) run(ctx context.Context) error {
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
	summary := rec.term.summary(run.ID())
	err = errors.Join(stopped, ended, summary)
	if !r.report || ended != nil || summary != nil || !rendersReport(ctx, stopped) {
		return err
	}
	if rerr := report.Make(ctx, r.w, dir, r.out, run.ID()); rerr != nil {
		err = errors.Join(err, fmt.Errorf("the run's report: %w; docproc report %s --run %s renders its pages again",
			rerr, r.out, run.ID()))
	}
	return err
}

func rendersReport(ctx context.Context, stopped error) bool {
	if ctx.Err() != nil {
		return false
	}
	var store *dispatch.StoreError
	return stopped == nil || errors.As(stopped, &store)
}

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

type recorder struct {
	dir    *rundir.Dir
	run    *rundir.Run
	inputs map[string]input
	first  map[string]time.Time
	term   *terminal
}

func feed(inputs []input) iter.Seq[dispatch.Input] {
	return func(yield func(dispatch.Input) bool) {
		for _, in := range inputs {
			if !yield(dispatch.Input{Path: in.abs}) {
				return
			}
		}
	}
}

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
		if err != nil {
			return err
		}
	}
	return nil
}

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
	if ev.Outcome == dispatch.Failed || ev.Outcome == dispatch.Seen && ev.Recorded == dispatch.Failed {
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

func (rec *recorder) arrived(path string) time.Time {
	now := time.Now()
	if _, ok := rec.first[path]; !ok {
		rec.first[path] = now
	}
	return now
}

func (rec *recorder) span(path string, end time.Time) schema.Span {
	start := rec.first[path]
	delete(rec.first, path)
	return schema.Span{Start: schema.TimeOf(start), End: schema.TimeOf(end)}
}

func (rec *recorder) file(in input) *schema.File {
	f, err := fileOf(in)
	if err != nil {
		return nil
	}
	return f
}

func (rec *recorder) kept(p dispatch.Part) (string, error) {
	switch {
	case p.SHA256 == "":
		return "", nil
	case p.Path != "":
		return rec.dir.Quarantine(p.SHA256, p.Path)
	}
	name, _, err := rec.dir.Kept(p.SHA256)
	return name, err
}
