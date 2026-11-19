package dispatch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// prep is the preparation of one part under its claim: a pipeline of the part's outputs and
// warnings, in the stage's order, each output hashed on the walk's goroutine, its content put on
// a goroutine of its own, at most Config.Puts in flight, then appended to the part's log and
// yielded, in order, from the walk's goroutine. The queue drains before a found part is
// dispatched and before the part's own event.
type prep struct {
	r     *run
	p     *Part
	id    Identity
	key   Key
	token string
	// record is whether the part's log is appended to and its entry recorded: not for a resumed
	// read, whose outputs are the read's tail alone.
	record bool
	// content are the part's content flags, its inspection's and its stage's, which its entry
	// records; p.Flags holds them with its name's.
	content []inspect.Flag
	// siblings are the Where of the part's found parts, for a stage whose names are untrusted.
	siblings map[string]bool

	queue []*pending
	// started is set once the stage yielded an output or a part, after which a flag is recorded
	// but never stops the stage.
	started     bool
	quarantined bool
	// tooLarge is the content store's refusal of an output past its size, which fails the part,
	// recorded; fault is the run's own failure, such as an output's file that can't be read,
	// which fails it unrecorded.
	tooLarge error
	fault    error

	outputs, children int
	complete          bool
	stages            []Identity
}

// pending is an output or a warning on its way to the part's log and the caller: an output's put,
// whose result done delivers once its content is in the content store, err once it is taken.
type pending struct {
	output   *Output
	warning  *Warning
	after    uint64
	done     chan error
	resolved bool
	err      error
}

// wait waits for the put's result, if it has one and it isn't taken yet.
func (h *pending) wait() error {
	if h.done != nil && !h.resolved {
		h.err, h.resolved = <-h.done, true
	}
	return h.err
}

// ready reports whether h may go out without waiting: a warning, or an output whose put is done.
func (h *pending) ready() bool { return h.done == nil || h.resolved }

// stageItem is one of a stage's items, as its goroutine hands it to the walk's: the item and its
// error, or the value the stage panicked with.
type stageItem struct {
	item     Item
	err      error
	panicked bool
	value    any
}

// prepare prepares p under its claim of key, token's, whose lease the dispatch's renewer renews
// until the claim is recorded or released: p's content is inspected, and, unless its verdict
// quarantines it, its stage runs.
func (r *run) prepare(p Part, stage Stage, key Key, token string, lease time.Duration) result {
	r.renew.add(key, token, lease)
	recorded := false
	defer func() {
		if !recorded {
			r.release(key, token)
		}
	}()

	content, err := r.inspectContent(p)
	if err != nil {
		if cerr := r.ctx.Err(); cerr != nil {
			return r.fatal(cerr)
		}
		return r.emit(Event{Part: p, Stage: key.Stage, Outcome: Failed, Reason: err.Error(), Err: err}, "", false, false)
	}
	pr := &prep{
		r: r, p: &p, id: key.Stage, key: key, token: token, record: p.After == 0,
		content: content, complete: true, stages: []Identity{key.Stage},
	}
	p.Flags = mergeFlags(content, p.Flags)
	if r.quarantines(p.Flags) {
		pr.quarantined = true
		return pr.finish(nil, "", &recorded)
	}

	dir, err := os.MkdirTemp(r.d.cfg.Dir, "part-")
	if err != nil {
		return r.fatal(fmt.Errorf("dispatch: %w", err))
	}
	if _, ok := stage.(UntrustedWhere); ok {
		pr.siblings = map[string]bool{}
	}
	stageErr := pr.run(stage, dir)
	if r.stopped {
		_ = os.RemoveAll(dir)
		return result{}
	}
	if err := r.ctx.Err(); err != nil {
		// The stage failed, if it did, because the dispatch's context ended: that is no failure
		// of the part's.
		_ = os.RemoveAll(dir)
		return r.fatal(err)
	}
	return pr.finish(stageErr, dir, &recorded)
}

// release ends key's claim, token's, unrecorded: under the dispatch's context, or, once that has
// ended, under Config.Release's bound. Its failure is dropped, since the claim's lease ends it
// anyway.
func (r *run) release(key Key, token string) {
	r.renew.drop(key)
	ctx := r.ctx
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
		if d := r.d.cfg.Release; d > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}
	_ = r.d.cfg.Index.Release(ctx, key, token)
}

// run runs the stage over the part, in dir, through the pipeline, returning the stage's error.
// The stage runs on a goroutine of its own, which hands each item to the walk's goroutine and
// waits for it to be taken, as a yield does: while the stage works toward its next item, the walk
// yields the outputs whose puts are done. Once run returns, the stage has returned and every put
// it started is done; a stage's panic is the walk's.
func (pr *prep) run(stage Stage, dir string) error {
	r := pr.r
	r.ancestors = append(r.ancestors, pr.p.SHA256)
	defer func() { r.ancestors = r.ancestors[:len(r.ancestors)-1] }()
	defer pr.drain()
	// What the dispatch may still admit, read here, on the walk's goroutine, which alone admits
	// parts: the stage's goroutine reads the copy on its context, never r.bytes.
	ctx := r.ctx
	if limit := r.d.cfg.Limits.Bytes; limit > 0 {
		ctx = context.WithValue(ctx, remainingKey{}, limit-r.bytes)
	}
	items, ack := make(chan stageItem), make(chan bool)
	go func() {
		defer close(items)
		defer func() {
			if v := recover(); v != nil {
				items <- stageItem{panicked: true, value: v}
			}
		}()
		for item, err := range stage.Prepare(ctx, *pr.p, dir) {
			items <- stageItem{item: item, err: err}
			if !<-ack {
				return
			}
		}
	}()
	finished := false
	defer func() {
		if !finished {
			// The walk is unwinding from a panic, the caller's loop body's or the stage's: the
			// stage is told to stop, and its goroutine ends once it returns.
			go func() {
				for {
					select {
					case ack <- false:
					case _, ok := <-items:
						if !ok {
							return
						}
					}
				}
			}()
		}
	}()
	var stageErr error
	goOn := true
	for {
		it, ok := pr.next(items)
		if !ok {
			finished = true
			break
		}
		if it.panicked {
			finished = true
			panic(it.value)
		}
		if goOn {
			stageErr = pr.take(it)
			goOn = stageErr == nil && !r.stopped && !pr.quarantined && pr.tooLarge == nil && pr.fault == nil
		}
		ack <- goOn
	}
	if !r.stopped && pr.tooLarge == nil && pr.fault == nil {
		pr.flush(0)
	}
	return stageErr
}

// next returns the stage's next item, false once the stage has returned, yielding, while it waits,
// each output at the queue's head whose put is done.
func (pr *prep) next(items <-chan stageItem) (stageItem, bool) {
	for {
		pr.flushReady()
		var done <-chan error
		if len(pr.queue) > 0 && !pr.r.stopped && pr.tooLarge == nil {
			done = pr.queue[0].done
		}
		select {
		case it, ok := <-items:
			return it, ok
		case err := <-done:
			pr.queue[0].err, pr.queue[0].resolved = err, true
		}
	}
}

// take takes one of the stage's items, returning the stage's error, if the item is one.
func (pr *prep) take(it stageItem) error {
	if it.err != nil {
		return it.err
	}
	switch item := it.item; {
	case item.Output != nil:
		pr.output(*item.Output, item.After)
	case item.Warning != nil:
		pr.queue = append(pr.queue, &pending{warning: item.Warning})
		pr.flush(pr.r.d.cfg.Puts)
	case item.Flag != nil:
		pr.flag(*item.Flag)
	case item.Part != nil:
		pr.found(*item.Part)
	default:
		return fmt.Errorf("dispatch: %s stage: an item that sets none of its fields", pr.id.Name)
	}
	return nil
}

// output hashes o, the part's own SHA-256 when its file is the part's, and queues its put.
func (pr *prep) output(o Output, after uint64) {
	pr.started = true
	var r io.Reader
	switch {
	case o.Path != "":
		f, err := os.Open(o.Path)
		if err != nil {
			pr.fault = fmt.Errorf("dispatch: output %q: %w", o.Name, err)
			return
		}
		if o.Path == pr.p.Path {
			o.SHA256 = pr.p.SHA256
		} else {
			h := sha256.New()
			_, err = io.Copy(h, f)
			if err == nil {
				_, err = f.Seek(0, io.SeekStart)
			}
			if err != nil {
				_ = f.Close()
				pr.fault = fmt.Errorf("dispatch: hash output %q: %w", o.Name, err)
				return
			}
			o.SHA256 = hex.EncodeToString(h.Sum(nil))
		}
		r = f
	default:
		sum := sha256.Sum256(o.Value)
		o.SHA256 = hex.EncodeToString(sum[:])
		r = bytes.NewReader(o.Value)
	}
	// The queue holds at most Config.Puts: the oldest goes out before another put starts.
	if pr.flush(pr.r.d.cfg.Puts - 1); pr.r.stopped || pr.tooLarge != nil {
		if c, ok := r.(io.Closer); ok {
			_ = c.Close()
		}
		return
	}
	done := make(chan error, 1)
	ctx, content, sum := pr.r.ctx, pr.r.d.cfg.Content, o.SHA256
	go func() {
		err := content.Put(ctx, sum, r)
		if c, ok := r.(io.Closer); ok {
			_ = c.Close() // read only: a close error loses nothing
		}
		done <- err
	}()
	pr.queue = append(pr.queue, &pending{output: &o, after: after, done: done})
}

// flush takes the queue's oldest items out, each appended and yielded, until it holds at most
// keep, or the dispatch stops, or a put fails.
func (pr *prep) flush(keep int) {
	for len(pr.queue) > keep {
		if !pr.out() {
			return
		}
	}
}

// flushReady takes the queue's oldest items out while each is ready, as flush does.
func (pr *prep) flushReady() {
	for len(pr.queue) > 0 && pr.queue[0].ready() {
		if !pr.out() {
			return
		}
	}
}

// out takes the queue's oldest item out, once its put is done: appended to the part's log, then
// yielded. It reports whether the pipeline goes on: not once the dispatch stops or a put fails.
func (pr *prep) out() bool {
	r := pr.r
	if r.stopped || pr.tooLarge != nil {
		return false
	}
	h := pr.queue[0]
	pr.queue = pr.queue[1:]
	if err := h.wait(); err != nil {
		if errors.Is(err, ErrTooLarge) {
			pr.tooLarge = fmt.Errorf("dispatch: output %q: %w", h.output.Name, err)
			return false
		}
		r.fatal(storeErr("content", fmt.Errorf("put output %q of %s: %w", h.output.Name, pr.p.Name, err)))
		return false
	}
	rec := guardRecorded(Recorded{Output: h.output, Warning: h.warning, After: h.after})
	if pr.record {
		if err := r.d.cfg.Index.Append(r.ctx, pr.key, pr.token, rec); err != nil {
			r.fatal(storeErr("index", err))
			return false
		}
	}
	if !r.send(Event{Part: *pr.p, Stage: pr.id, Output: h.output, Warning: h.warning, After: h.after}) {
		return false
	}
	if h.output != nil {
		pr.outputs++
	}
	return true
}

// drain waits for every put left in the queue, discarding it: the dispatch stopped, or a put
// failed.
func (pr *prep) drain() {
	for _, h := range pr.queue {
		_ = h.wait()
	}
	pr.queue = nil
}

// flag adds f to the part's content flags; before the stage's first output or part, a verdict to
// quarantine stops the stage.
func (pr *prep) flag(f inspect.Flag) {
	pr.content = mergeFlags(pr.content, []inspect.Flag{f})
	pr.p.Flags = mergeFlags(pr.p.Flags, []inspect.Flag{f})
	if !pr.started && pr.r.quarantines(pr.p.Flags) {
		pr.quarantined = true
	}
}

// found dispatches f, a part the stage found, once the queue has drained, its Where normalised,
// and unique among its siblings for a stage whose names are untrusted, then appends it to the
// part's log as a child.
func (pr *prep) found(f Found) {
	pr.started = true
	r := pr.r
	if pr.flush(0); r.stopped || pr.tooLarge != nil {
		return
	}
	f.Where = guard(f.Where, capName, false)
	if pr.siblings != nil {
		f.Where = pr.unique(f.Where)
	}
	res := r.part(f, nil, pr.p.ID, pr.p.Depth+1)
	if r.stopped {
		return
	}
	pr.children++
	pr.complete = pr.complete && res.complete
	pr.stages = addStages(pr.stages, res.stages)
	if pr.record {
		if err := r.d.cfg.Index.Append(r.ctx, pr.key, pr.token, guardRecorded(Recorded{Child: &res.child})); err != nil {
			r.fatal(storeErr("index", err))
		}
	}
}

// unique returns where, or, when a sibling already has it, where with " (n)", n the smallest
// from 2 that no sibling's takes, and holds it among the siblings.
func (pr *prep) unique(where string) string {
	w := where
	for n := 2; pr.siblings[w]; n++ {
		// Past the cap, the guard cuts it with the hash of the whole, the suffix within it.
		w = guard(fmt.Sprintf("%s (%d)", where, n), capName, false)
	}
	pr.siblings[w] = true
	return w
}

// finish classifies the part's outcome from its stage's error, records its entry when its whole
// subtree is complete and the read whole, before its event, then emits the event, removing dir.
func (pr *prep) finish(stageErr error, dir string, recorded *bool) result {
	r := pr.r
	p := *pr.p
	ev := Event{Part: p, Stage: pr.id, Outputs: pr.outputs, Children: pr.children}
	complete := pr.complete
	if stageErr == nil {
		stageErr = pr.fault
	}
	switch {
	case pr.quarantined:
		q := quarantined(p)
		ev.Outcome, ev.Reason = q.Outcome, q.Reason
	case pr.tooLarge != nil:
		ev.Outcome, ev.Reason, ev.Err = Failed, pr.tooLarge.Error(), pr.tooLarge
	case stageErr == nil:
		ev.Outcome = Prepared
	case errors.Is(stageErr, ErrNoProcessor):
		ev.Outcome, ev.Reason = NoProcessor, stageErr.Error()
	case errors.Is(stageErr, ErrEncrypted):
		ev.Outcome, ev.Reason, ev.Err = Encrypted, stageErr.Error(), stageErr
	case errors.Is(stageErr, ErrReadElsewhere):
		ev.Outcome, ev.Reason = ReadElsewhere, stageErr.Error()
	case errors.Is(stageErr, ErrRefused):
		ev.Outcome, ev.Reason, ev.Err = Failed, stageErr.Error(), stageErr
	default:
		ev.Outcome, ev.Reason, ev.Err, complete = Failed, stageErr.Error(), stageErr, false
	}
	// A quarantine is recorded only when the content's flags alone come to it: the name's
	// differ at another occurrence of the same content.
	record := complete && pr.record && (!pr.quarantined || r.quarantines(pr.content))
	if record {
		r.renew.drop(pr.key)
		e := Entry{
			Outcome: ev.Outcome, Reason: ev.Reason, Outputs: ev.Outputs, Children: ev.Children,
			Stages: pr.stages, Flags: pr.content,
		}
		if err := r.d.cfg.Index.Record(r.ctx, pr.key, pr.token, guardEntry(e)); err != nil {
			if dir != "" {
				_ = os.RemoveAll(dir)
			}
			return r.fatal(storeErr("index", err))
		}
		*recorded = true
	}
	res := r.emit(ev, dir, complete, record)
	if complete {
		res.stages = pr.stages
	}
	return res
}

// addStages adds to stages each of more it doesn't hold, in more's order.
func addStages(stages, more []Identity) []Identity {
	for _, id := range more {
		if !slices.Contains(stages, id) {
			stages = append(stages, id)
		}
	}
	return stages
}

// hashFile returns what the dispatcher takes of the file at path, read whole and streamed: its
// SHA-256, its size, and its head.
func hashFile(path string) (*hashed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("dispatch: %w", err)
	}
	defer func() { _ = f.Close() }() // read only: a close error loses nothing
	h := sha256.New()
	head := &headWriter{max: route.HeadLen}
	n, err := io.Copy(io.MultiWriter(h, head), f)
	if err != nil {
		return nil, fmt.Errorf("dispatch: hash %s: %w", path, err)
	}
	return &hashed{sum: hex.EncodeToString(h.Sum(nil)), size: n, head: head.b}, nil
}

// headWriter keeps the first max bytes written to it.
type headWriter struct {
	b   []byte
	max int
}

func (w *headWriter) Write(p []byte) (int, error) {
	if room := w.max - len(w.b); room > 0 {
		w.b = append(w.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}
