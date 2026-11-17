package dispatch

import (
	"fmt"
	"slices"

	"github.com/standards-lab/docproc/inspect"
)

// seen replays p, content the index holds under key, e its fresh entry under id, with no file: its
// verdict taken over the entry's flags and p's name's under the current options, a quarantine
// replaying nothing; its children resolved one level down, before its first event; then its log,
// in the order it was prepared, each output and warning an event, each child dispatched as its
// recorded part at its place under p; then p's own event. A resumed input skips its log through
// the item whose After is its cursor. A child the index no longer holds is a miss, which seen
// returns for the nearest part with a file to resolve, before or after events are out.
func (r *run) seen(p Part, id Identity, key Key, e Entry) result {
	p.Flags = mergeFlags(e.Flags, p.Flags)
	if e.Outcome == Quarantined || r.quarantines(p.Flags) {
		ev := quarantined(p)
		ev.Stage, ev.Recorded = id, e.Outcome
		return r.emit(ev, "", true, false)
	}
	if p.After != 0 || e.Children > 0 {
		miss, err := r.resolve(p, key)
		switch {
		case err != nil:
			return r.fatal(err)
		case miss:
			return result{miss: true}
		}
	}

	r.ancestors = append(r.ancestors, p.SHA256)
	defer func() { r.ancestors = r.ancestors[:len(r.ancestors)-1] }()
	skipping := p.After != 0
	var outputs, children, logged, found int
	for rec, err := range r.d.cfg.Index.Items(r.ctx, key) {
		if err != nil {
			return r.fatal(storeErr("index", err))
		}
		switch {
		case rec.Output != nil:
			logged++
		case rec.Child != nil:
			found++
		}
		if skipping {
			skipping = rec.After != p.After
			continue
		}
		switch {
		case rec.Output != nil:
			if !r.send(Event{Part: p, Stage: id, Output: rec.Output, After: rec.After, Outcome: Seen, Recorded: e.Outcome}) {
				return result{}
			}
			outputs++
		case rec.Warning != nil:
			if !r.send(Event{Part: p, Stage: id, Warning: rec.Warning, Outcome: Seen, Recorded: e.Outcome}) {
				return result{}
			}
		case rec.Child != nil:
			res := r.replayChild(*rec.Child, p)
			if r.stopped {
				return result{}
			}
			if res.miss {
				return res
			}
			children++
		}
	}
	if logged != e.Outputs || found != e.Children {
		return r.fatal(&StoreError{Store: "index", Err: fmt.Errorf(
			"the log of %s under %s holds %d outputs and %d children, its entry %d and %d",
			key.SHA256, id, logged, found, e.Outputs, e.Children)})
	}
	why := e.Reason
	if why == "" {
		why = "recorded by " + id.String()
	}
	ev := Event{Part: p, Stage: id, Outcome: Seen, Recorded: e.Outcome, Reason: why, Outputs: outputs, Children: children}
	res := r.emit(ev, "", true, true)
	res.stages = e.Stages
	return res
}

// resolve reads p's log once, before any of p's events, and reports whether a child it replays
// misses: one looked up, whose entry under its route's current stage is gone or not fresh. A
// resumed input's log is read for its cursor too, an error when no item's After is it.
func (r *run) resolve(p Part, key Key) (bool, error) {
	cursor := p.After == 0
	for rec, err := range r.d.cfg.Index.Items(r.ctx, key) {
		if err != nil {
			return false, storeErr("index", err)
		}
		if !cursor {
			cursor = rec.After == p.After
			continue
		}
		if rec.Child == nil {
			continue
		}
		miss, err := r.missing(*rec.Child, p)
		if err != nil || miss {
			return miss, err
		}
	}
	if !cursor {
		return false, fmt.Errorf("dispatch: %s: no item of its log is the cursor %d: %w", p.Name, p.After, ErrRequest)
	}
	return false, nil
}

// lookedUp reports whether a child recorded with outcome o replays its own entry's log, looked up
// under its route's current stage, rather than replaying as recorded.
func lookedUp(o Outcome) bool {
	switch o {
	case Prepared, Encrypted, NoProcessor, Seen, ReadElsewhere:
		return true
	}
	return false
}

// missing reports whether c, a child of p's log, misses: looked up under its route's stage, now
// wired, whose entry is gone or no longer fresh.
func (r *run) missing(c Child, p Part) (bool, error) {
	if c.Part.SHA256 == "" || !lookedUp(c.Outcome) || c.Part.SHA256 == p.SHA256 || slices.Contains(r.ancestors, c.Part.SHA256) {
		return false, nil
	}
	stage := r.d.cfg.Stages[c.Part.ContentType.Route]
	if stage == nil {
		return false, nil
	}
	e, ok, err := r.d.cfg.Index.Lookup(r.ctx, Key{SHA256: c.Part.SHA256, Stage: stage.Identity()})
	if err != nil {
		return false, storeErr("index", err)
	}
	return !ok || !r.fresh(e), nil
}

// replayChild dispatches c, a child of parent's log, as its recorded part, its ID, parent, and
// depth the replaying walk's: a cycle or past a cap as a found part is; looked up under its
// route's current stage and seen, or a miss; with no stage, no processor yet, or quarantined by
// its recorded flags; and any other outcome as recorded.
func (r *run) replayChild(c Child, parent Part) result {
	if err := r.ctx.Err(); err != nil {
		return r.fatal(err)
	}
	p := c.Part
	p.Path, p.After = "", 0
	p.Parent, p.Depth, p.ID = parent.ID, parent.Depth+1, childID(parent.ID, p.Where)
	if p.SHA256 == "" {
		// An unread child.
		return r.emit(Event{Part: p, Outcome: c.Outcome}, "", true, false)
	}
	if i := slices.Index(r.ancestors, p.SHA256); i >= 0 {
		why := fmt.Sprintf("same content as its ancestor at depth %d", i)
		return r.emit(Event{Part: p, Outcome: Cycle, Reason: why}, "", true, false)
	}
	if why := r.limit(p); why != "" {
		p.Flags = nil
		return r.emit(Event{Part: p, Outcome: Limited, Reason: why}, "", false, false)
	}
	r.parts++
	r.bytes += p.Size
	if !lookedUp(c.Outcome) {
		return r.emit(Event{Part: p, Outcome: c.Outcome}, "", true, false)
	}
	stage := r.d.cfg.Stages[p.ContentType.Route]
	if stage == nil {
		// Its recorded flags are its content's and its name's when it was found.
		if r.quarantines(p.Flags) {
			return r.emit(quarantined(p), "", true, false)
		}
		return r.emit(noStage(p), "", true, true)
	}
	p.Flags = inspect.Name(p.Name, p.ContentType.Kind)
	id := stage.Identity()
	key := Key{SHA256: p.SHA256, Stage: id}
	e, ok, err := r.d.cfg.Index.Lookup(r.ctx, key)
	if err != nil {
		return r.fatal(storeErr("index", err))
	}
	if !ok || !r.fresh(e) {
		return result{miss: true}
	}
	return r.seen(p, id, key, e)
}
