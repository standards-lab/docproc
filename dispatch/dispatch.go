package dispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// Outcome names what became of a part, as the package doc explains.
type Outcome string

const (
	// Prepared is a part its stage prepared.
	Prepared Outcome = "prepared"
	// NoProcessor is a part whose route has no stage registered, or whose stage has no
	// processor for its kind yet.
	NoProcessor Outcome = "no processor yet"
	// Seen is content the index already holds: its log is replayed, and its event carries its
	// recorded entry and flags.
	Seen Outcome = "seen"
	// Cycle is a part whose SHA-256 is one of its ancestors': it is neither prepared nor
	// recursed into.
	Cycle Outcome = "cycle"
	// Quarantined is a part its flags' verdict quarantines: no stage runs, or its stage stops,
	// and its flags hold the evidence.
	Quarantined Outcome = "quarantined"
	// Encrypted is a part its stage can't read because it is encrypted.
	Encrypted Outcome = "encrypted"
	// ReadElsewhere is a part whose content its stage reads as other parts, such as a OneNote
	// table of contents whose sections are read as their own: no failure.
	ReadElsewhere Outcome = "read elsewhere"
	// Failed is a part that couldn't be read, routed, inspected, or prepared: the event's Err
	// says why.
	Failed Outcome = "failed"
	// Limited is a part past a cap the caller set: it is neither inspected nor prepared, and
	// its siblings go on.
	Limited Outcome = "limited"
)

// Event is what the dispatcher reports of one part, and the JSON shape a host writes of it: an
// output its stage prepared, a warning, or, once its outcome is known, the part's own event. Each
// kind sets only its own fields, and a field left out of the JSON is zero.
//
// An output event has Output set: the part it belongs to, as far as it is known, the identity of
// its stage, the output, its SHA256 filled and its content already in the content store, and,
// for a store's message's last output, After, the message's cursor. A warning event has Warning
// set, as an output event has Output. The dispatcher yields each in the stage's order, among the
// events of the parts the stage finds, once it is appended to the part's log in the index. A
// [Seen] part's log is replayed from the index: its outputs and warnings, each with its Outcome
// [Seen] and the entry's in Recorded, and with no file, and its children, each dispatched as its
// recorded part, at its place in this input, with no file; a prepared output's event has no
// Outcome.
//
// A part's own event has neither set, and follows its outputs' and warnings' events and every
// part found in it: the part, its outcome and why, the error a [Failed] part came to, the identity
// of the stage its route has, if any, how many outputs it yielded, and how many parts it found,
// each of whose own events came before it. A [Seen] event carries the entry the index recorded,
// its outcome in Recorded. A part's entry is recorded before its own event is yielded.
//
// Err is local, beside its text in Reason. Every string of an event has passed the dispatcher's
// guard: valid UTF-8, no control but tab and line feed in a reason, and capped.
//
// The caller's loop owns the event until its body returns: its files are the caller's to read,
// link, or move until then, and the dispatcher removes what is left once the part's own event is
// done. Events are at least once: a part replayed from the index whose subtree the index no
// longer holds whole is prepared again, its events repeating, so a consumer counts by a part's ID
// and a store's cursor, never by events.
type Event struct {
	Part     Part     `json:"part,omitzero"`
	Stage    Identity `json:"stage,omitzero"`
	Output   *Output  `json:"output,omitempty"`
	Warning  *Warning `json:"warning,omitempty"`
	After    uint64   `json:"after,omitzero"`
	Outcome  Outcome  `json:"outcome,omitzero"`
	Recorded Outcome  `json:"recorded,omitzero"`
	Reason   string   `json:"reason,omitzero"`
	Err      error    `json:"-"`
	Outputs  int      `json:"outputs,omitzero"`
	Children int      `json:"children,omitzero"`
}

// Limits are the caps a caller may set on one dispatch, each off when zero: none applies by
// default. A part past one is [Limited], and its siblings go on. A part replayed from the index
// counts toward them as a prepared one does.
type Limits struct {
	// Parts is the most parts one dispatch admits, the input among them.
	Parts int
	// Bytes is the most bytes the parts one dispatch admits may come to, their sizes summed. It
	// bounds what a stage writes too, not only what the dispatcher admits: a stage that writes the
	// parts it finds holds them to what [Remaining] says is left, and a part it stops writing at
	// the cap is an unread child, [Limited], with no size or SHA-256.
	Bytes int64
	// Depth is the deepest part one dispatch admits, the input at depth 0.
	Depth int
}

// Config configures a [Dispatcher].
type Config struct {
	// Stages are the stages, by route. Two that share a name share their identity.
	Stages Stages
	// Index records what is prepared; it is required, and dispatchers that share one share
	// their work.
	Index Index
	// Content holds every output's content by its SHA-256; it is required.
	Content Content
	// Dir is the working directory the dispatcher makes each part's own directory in, which it
	// gives the part's stage and removes once the caller is done with the part, and spools an
	// input's Reader into; it must exist.
	Dir string
	// Limits are the caller's caps; none by default.
	Limits Limits
	// Inspect tunes every part's inspection and the verdict over its flags.
	Inspect inspect.Options
	// Puts is the most content puts in flight per dispatch, at least 1: a part's outputs are put
	// that many at once, then appended and yielded in order.
	Puts int
	// Release bounds a claim's release once the dispatch's context has ended, none when zero:
	// the release's failure is dropped, since a claim's lease ends it anyway.
	Release time.Duration
}

// Dispatcher dispatches inputs to their stages. It is safe for concurrent use, each Dispatch
// independent but for the index and the content store they share.
type Dispatcher struct {
	cfg        Config
	identities map[string]Identity
}

// New returns a Dispatcher over cfg. A nil Index or Content is the caller's mistake, and panics
// naming the field. It refuses, matching [ErrRequest], a Config with no working directory, a
// stage that is nil, two stages sharing a name with different identities, a negative cap or
// release bound, or Puts under 1; and inspection options inspect refuses, validating them once.
// It keeps its own copy of Stages, so a later change to the caller's map changes nothing.
func New(cfg Config) (*Dispatcher, error) {
	if cfg.Index == nil {
		panic("dispatch: Config.Index is nil")
	}
	if cfg.Content == nil {
		panic("dispatch: Config.Content is nil")
	}
	refuse := func(format string, args ...any) (*Dispatcher, error) {
		return nil, fmt.Errorf("dispatch: "+format+": %w", append(args, ErrRequest)...)
	}
	if cfg.Dir == "" {
		return refuse("no working directory")
	}
	if info, err := os.Stat(cfg.Dir); err != nil || !info.IsDir() {
		return refuse("working directory %q isn't a directory", cfg.Dir)
	}
	identities := map[string]Identity{}
	for _, r := range slices.Sorted(maps.Keys(cfg.Stages)) {
		s := cfg.Stages[r]
		if s == nil {
			return refuse("route %s: nil stage", r)
		}
		id := s.Identity()
		if other, ok := identities[id.Name]; ok && other != id {
			return refuse("two stages named %q: %s and %s", id.Name, other, id)
		}
		identities[id.Name] = id
	}
	if cfg.Limits.Parts < 0 || cfg.Limits.Bytes < 0 || cfg.Limits.Depth < 0 {
		return refuse("negative limit")
	}
	if cfg.Puts < 1 {
		return refuse("Config.Puts is %d, want at least 1", cfg.Puts)
	}
	if cfg.Release < 0 {
		return refuse("Config.Release is negative")
	}
	if err := cfg.Inspect.Validate(); err != nil {
		return nil, fmt.Errorf("dispatch: %w", err)
	}
	cfg.Stages = maps.Clone(cfg.Stages)
	return &Dispatcher{cfg: cfg, identities: identities}, nil
}

// Content returns the dispatcher's content store, Config.Content, for a host's own puts and
// reads.
func (d *Dispatcher) Content() Content { return d.cfg.Content }

// Index returns the dispatcher's index, Config.Index, for a host's drop of an entry.
func (d *Dispatcher) Index() Index { return d.cfg.Index }

// Input is the content a dispatch begins at: a file the caller owns, at Path, which the
// dispatcher never removes, or a stream, Reader, spooled into Config.Dir and hashed as it is
// copied, its spool removed once the dispatch is done; exactly one of them is set. SHA256, when
// the caller knows the content's SHA-256, lowercase hex, is checked against the hash the
// dispatcher takes anyway, as the input is spooled or read, never hashed twice: a mismatch stops
// the dispatch with an error matching [ErrSHA256]. Name is the name it was stored with, which
// routing reads, filepath.Base(Path) when empty and Path is set, and MIME its stored MIME type, if
// it has one.
//
// After, when not zero, resumes the input's read after the message whose store id it is, mail's
// --after: opaque, compared by equality alone. The input's [Part] carries it to its stage, and
// the stages package's email stage reads a .pst or an mbox from the message after it; every other
// stage, and every part found below the input, reads whole. A resumed input whose content the
// index holds replays its log past the item whose After equals it, an error ending the dispatch
// when no item's does. A resumed input's own entry is never recorded, its outputs being the read's
// tail alone, so a later dispatch of the same content prepares it whole; the parts found in it are
// recorded as any part is. The package doc's "Resuming a store" says how a caller resumes after a crash.
type Input struct {
	Path   string
	Reader io.Reader
	SHA256 string
	Name   string
	MIME   string
	After  uint64
}

// Dispatch dispatches in and every part its stages find in it, depth-first, yielding each output
// and warning a stage yields as its own event, as the stage yields it, and each part's event once
// its outcome is known: a part's event follows its outputs' and the events of every part found in
// it, so the input's is last. An error that stops the dispatch is yielded alone, last: the input
// unreadable or its SHA-256 not the one claimed, a [*StoreError] of the index or the content
// store, an error matching [ErrClaimLost], a file the dispatcher can't remove, or ctx ending; a
// part that fails is a [Failed] event, and the dispatch goes on. A loop that stops early stops the
// stages, waits for the puts in flight, releases every claim, and removes the files the dispatch
// made.
func (d *Dispatcher) Dispatch(ctx context.Context, in Input) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		ctx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		r := &run{d: d, ctx: ctx, yield: yield, after: in.After, released: map[string]released{}}
		r.renew = newRenewer(ctx, d.cfg.Index, cancel)
		defer r.renew.stop()
		f, h, err := r.open(in)
		if err != nil {
			r.fatal(err)
			return
		}
		if in.Reader != nil {
			defer func() {
				if err := os.Remove(f.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					r.fatal(fmt.Errorf("dispatch: remove the input's spool: %w", err))
				}
			}()
		}
		r.part(f, h, "", 0)
	}
}

type run struct {
	d         *Dispatcher
	ctx       context.Context
	yield     func(Event, error) bool
	renew     *renewer
	ancestors []string
	parts     int
	bytes     int64
	stopped   bool
	after     uint64
	released  map[string]released
}

type released struct {
	size        int64
	contentType ContentType
	outcome     Outcome
	flags       []inspect.Flag
}

type hashed struct {
	sum  string
	size int64
	head []byte
}

type result struct {
	child    Child
	complete bool
	stages   []Identity
	settled  bool
	miss     bool
}

func (r *run) open(in Input) (Found, *hashed, error) {
	if (in.Path == "") == (in.Reader == nil) {
		return Found{}, nil, fmt.Errorf("dispatch: an input sets one of Path and Reader: %w", ErrRequest)
	}
	if in.SHA256 != "" && !isSHA256(in.SHA256) {
		return Found{}, nil, fmt.Errorf("dispatch: the input's SHA-256 %q isn't lowercase hex: %w", in.SHA256, ErrRequest)
	}
	f := Found{Path: in.Path, Name: in.Name, MIME: in.MIME, SHA256: in.SHA256}
	if in.Reader == nil {
		if f.Name == "" {
			f.Name = filepath.Base(in.Path)
		}
		return f, nil, nil
	}
	spool, err := os.CreateTemp(r.d.cfg.Dir, "input-")
	if err != nil {
		return Found{}, nil, fmt.Errorf("dispatch: spool the input: %w", err)
	}
	f.Path = spool.Name()
	h := sha256.New()
	head := &headWriter{max: route.HeadLen}
	n, err := io.Copy(io.MultiWriter(spool, h, head), contextReader{r.ctx, in.Reader})
	if cerr := spool.Close(); err == nil {
		err = cerr
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err == nil && in.SHA256 != "" && sum != in.SHA256 {
		err = fmt.Errorf("SHA-256 %s, claimed %s: %w", sum, in.SHA256, ErrSHA256)
	}
	if err != nil {
		_ = os.Remove(f.Path)
		return Found{}, nil, fmt.Errorf("dispatch: spool the input %q: %w", f.Name, err)
	}
	return f, &hashed{sum: sum, size: n, head: head.b}, nil
}

func (r *run) fatal(err error) result {
	if !r.stopped {
		r.stopped = true
		if cause := context.Cause(r.ctx); errors.Is(cause, ErrClaimLost) {
			err = cause
		}
		r.yield(Event{}, err)
	}
	return result{}
}

func (r *run) send(ev Event) bool {
	if !r.yield(guardEvent(ev), nil) {
		r.stopped = true
	}
	return !r.stopped
}

func (r *run) emit(ev Event, dir string, complete, settled bool) result {
	ev = guardEvent(ev)
	if !r.yield(ev, nil) {
		r.stopped = true
	}
	child := Child{Part: ev.Part, Outcome: ev.Outcome}
	child.Part.Path, child.Part.After = "", 0
	res := result{child: child, complete: complete, settled: settled && ev.Outcome != Quarantined}
	if err := r.remove(ev.Part, dir); err != nil {
		return r.fatal(err)
	}
	return res
}

func (r *run) remove(p Part, dir string) error {
	if p.Depth > 0 && p.Path != "" {
		if err := os.Remove(p.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("dispatch: remove %s: %w", p.Path, err)
		}
	}
	if dir == "" {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("dispatch: remove %s: %w", dir, err)
	}
	return nil
}

func (r *run) part(f Found, h *hashed, parent string, depth int) result {
	p := Part{
		ID: "/", Path: f.Path, Name: f.Name, ContentType: ContentType{Stored: f.MIME},
		Parent: parent, Depth: depth, Where: f.Where,
	}
	if depth == 0 {
		p.After = r.after
	} else {
		p.ID = childID(parent, f.Where)
	}
	if f.Path == "" && f.Outcome != "" {
		return r.unread(p, f)
	}
	res := r.dispatchPart(&p, f.SHA256, h)
	if res.miss && !r.stopped {
		err := fmt.Errorf("dispatch: %s has no file, and the index no longer holds its subtree", p.Name)
		return r.emit(Event{Part: p, Outcome: Failed, Reason: err.Error(), Err: err}, "", false, false)
	}
	if res.settled && f.Release != nil && f.Path != "" && f.SHA256 == p.SHA256 && !r.stopped {
		r.released[p.SHA256] = released{
			size: p.Size, contentType: p.ContentType, outcome: res.child.Outcome, flags: res.child.Part.Flags,
		}
		f.Release()
	}
	return res
}

func (r *run) unread(p Part, f Found) result {
	refuse := func(err error) result {
		return r.emit(Event{Part: p, Outcome: Failed, Reason: err.Error(), Err: err}, "", false, false)
	}
	complete := true
	switch f.Outcome {
	case Quarantined, Encrypted, NoProcessor, Failed:
	case Limited:
		limit := r.d.cfg.Limits.Bytes
		if limit == 0 {
			return refuse(fmt.Errorf("dispatch: %s: an unread child limited with no bytes cap set", p.Name))
		}
		f.Reason, complete = pastBytes(limit), false
	default:
		return refuse(fmt.Errorf("dispatch: %s: an unread child's outcome is %q, want quarantined, encrypted, no processor yet, failed, or limited", p.Name, f.Outcome))
	}
	p.Flags = sortFlags(slices.Clone(f.Flags))
	return r.emit(Event{Part: p, Outcome: f.Outcome, Reason: f.Reason}, "", complete, false)
}

func childID(parent, where string) string {
	segment := strings.NewReplacer("%", "%25", "/", "%2F").Replace(where)
	if strings.HasSuffix(parent, "/") {
		return parent + segment
	}
	return parent + "/" + segment
}

func (r *run) dispatchPart(p *Part, sum string, h *hashed) result {
	if err := r.ctx.Err(); err != nil {
		return r.fatal(err)
	}
	failed := func(err error) result {
		return r.emit(Event{Part: *p, Outcome: Failed, Reason: err.Error(), Err: err}, "", false, false)
	}

	if p.Path == "" {
		c, ok := r.released[sum]
		if !ok {
			return failed(fmt.Errorf("dispatch: %s has no file, and its content is none this dispatch released", p.Name))
		}
		stored := p.ContentType.Stored
		p.SHA256, p.Size, p.ContentType = sum, c.size, c.contentType
		p.ContentType.Stored = stored
	} else {
		if h == nil {
			var err error
			if h, err = hashFile(p.Path); err != nil {
				if p.Depth == 0 {
					return r.fatal(err)
				}
				return failed(err)
			}
		}
		p.SHA256, p.Size, p.ContentType.Kind = h.sum, h.size, route.Identify(h.head)
		if sum != "" && sum != p.SHA256 {
			err := fmt.Errorf("dispatch: %s: SHA-256 %s, claimed %s: %w", p.Name, p.SHA256, sum, ErrSHA256)
			if p.Depth == 0 {
				return r.fatal(err)
			}
			return failed(err)
		}
		ct := &p.ContentType
		var err error
		if ct.Route, ct.RouteReason, err = route.File(p.Path, p.Name, ct.Stored); err != nil {
			return failed(fmt.Errorf("dispatch: route %s: %w", p.Name, err))
		}
	}

	if i := slices.Index(r.ancestors, p.SHA256); i >= 0 {
		why := fmt.Sprintf("same content as its ancestor at depth %d", i)
		return r.emit(Event{Part: *p, Outcome: Cycle, Reason: why}, "", true, false)
	}
	if why := r.limit(*p); why != "" {
		return r.emit(Event{Part: *p, Outcome: Limited, Reason: why}, "", false, false)
	}
	r.parts++
	r.bytes += p.Size
	p.Flags = inspect.Name(p.Name, p.ContentType.Kind)

	stage := r.d.cfg.Stages[p.ContentType.Route]
	switch {
	case p.Path == "":
		return r.releasedPart(*p, stage, failed)
	case stage == nil:
		return r.unstaged(*p, failed)
	}
	return r.staged(*p, stage, failed)
}

func (r *run) limit(p Part) string {
	l := r.d.cfg.Limits
	switch {
	case l.Depth > 0 && p.Depth > l.Depth:
		return fmt.Sprintf("past the depth cap, %d", l.Depth)
	case l.Parts > 0 && r.parts >= l.Parts:
		return fmt.Sprintf("past the parts cap, %d", l.Parts)
	case l.Bytes > 0 && r.bytes+p.Size > l.Bytes:
		return pastBytes(l.Bytes)
	}
	return ""
}

func pastBytes(limit int64) string { return fmt.Sprintf("past the bytes cap, %d", limit) }

type remainingKey struct{}

// Remaining returns the bytes the dispatch that called a stage's Prepare with ctx may still
// admit: its [Limits].Bytes less the sizes of the parts it had admitted when Prepare started,
// the part being prepared among them. ok is false when the dispatch has no bytes cap, or ctx is
// neither the context the dispatcher passed to Prepare nor derived from it.
//
// The dispatcher sets the value on the context it passes to Prepare, read as Prepare starts, and
// it never changes while the stage runs, though the parts the stage finds are admitted meanwhile.
// A stage that writes the parts it finds, such as an archive's entries, holds what it writes to
// it, less what it has written for the parts it found before in the same Prepare: it stops writing
// a part whose bytes would pass what remains, removes what it wrote, and finds the part as an
// unread child, [Limited], with no size or SHA-256, rather than writing what the dispatcher would
// refuse once it was written. The dispatcher gives that child the bytes cap's reason, as it gives
// a found part past the cap.
func Remaining(ctx context.Context) (int64, bool) {
	n, ok := ctx.Value(remainingKey{}).(int64)
	return n, ok
}

func (r *run) inspectContent(p Part) ([]inspect.Flag, error) {
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, fmt.Errorf("dispatch: inspect %s: %w", p.Name, err)
	}
	defer func() { _ = f.Close() }()
	rep, err := inspect.Content(r.ctx, f, p.Size, p.ContentType.Kind, r.d.cfg.Inspect)
	if err != nil {
		return nil, fmt.Errorf("dispatch: inspect %s: %w", p.Name, err)
	}
	return rep.Flags, nil
}

func (r *run) quarantines(flags []inspect.Flag) bool {
	return r.d.cfg.Inspect.Verdict(flags) == inspect.Quarantine
}

func quarantined(p Part) Event {
	var unsafe []string
	for _, f := range p.Flags {
		if f.Class == inspect.Unsafe {
			unsafe = append(unsafe, string(f.Check)+": "+f.Evidence)
		}
	}
	why := "quarantined by policy"
	if len(unsafe) > 0 {
		why = strings.Join(unsafe, "; ")
	}
	return Event{Part: p, Outcome: Quarantined, Reason: why}
}

func mergeFlags(a, b []inspect.Flag) []inspect.Flag {
	if len(a)+len(b) == 0 {
		return nil
	}
	return sortFlags(slices.Concat(a, b))
}

func sortFlags(flags []inspect.Flag) []inspect.Flag {
	slices.SortStableFunc(flags, func(a, b inspect.Flag) int { return strings.Compare(string(a.Check), string(b.Check)) })
	return flags
}

func (r *run) unstaged(p Part, failed func(error) result) result {
	content, err := r.inspectContent(p)
	if err != nil {
		if cerr := r.ctx.Err(); cerr != nil {
			return r.fatal(cerr)
		}
		return failed(err)
	}
	p.Flags = mergeFlags(content, p.Flags)
	if r.quarantines(p.Flags) {
		return r.emit(quarantined(p), "", true, false)
	}
	return r.emit(noStage(p), "", true, true)
}

func noStage(p Part) Event {
	return Event{Part: p, Outcome: NoProcessor, Reason: fmt.Sprintf("no stage for route %s", p.ContentType.Route)}
}

func (r *run) releasedPart(p Part, stage Stage, failed func(error) result) result {
	if stage == nil {
		c := r.released[p.SHA256]
		if c.outcome != NoProcessor {
			return failed(fmt.Errorf("dispatch: %s has no file, and its route %s no stage", p.Name, p.ContentType.Route))
		}
		p.Flags = mergeFlags(c.flags, nil)
		return r.emit(noStage(p), "", true, false)
	}
	id := stage.Identity()
	key := Key{SHA256: p.SHA256, Stage: id}
	e, ok, err := r.d.cfg.Index.Lookup(r.ctx, key)
	if err != nil {
		return r.fatal(storeErr("index", err))
	}
	if !ok || !r.fresh(e) {
		return failed(fmt.Errorf("dispatch: %s has no file, and the index no longer holds its content's entry", p.Name))
	}
	return r.seen(p, id, key, e)
}

func (r *run) fresh(e Entry) bool {
	for _, id := range e.Stages {
		if r.d.identities[id.Name] != id {
			return false
		}
	}
	return true
}

func (r *run) staged(p Part, stage Stage, failed func(error) result) result {
	idx := r.d.cfg.Index
	id := stage.Identity()
	key := Key{SHA256: p.SHA256, Stage: id}
	parts, bytes := r.parts, r.bytes
	missed := false
	for {
		e, ok, err := idx.Lookup(r.ctx, key)
		if err != nil {
			return r.fatal(storeErr("index", err))
		}
		if !ok {
			token, lease, claimed, err := idx.Claim(r.ctx, key)
			if err != nil {
				return r.fatal(storeErr("index", err))
			}
			if claimed {
				return r.prepare(p, stage, key, token, lease)
			}
			if e, ok, err = idx.Wait(r.ctx, key); err != nil {
				return r.fatal(storeErr("index", err))
			}
			if !ok {
				continue
			}
		}
		if r.fresh(e) {
			res := r.seen(p, id, key, e)
			if !res.miss || r.stopped {
				return res
			}
			if missed {
				return failed(fmt.Errorf("dispatch: %s: its replay missed again once it was prepared", p.Name))
			}
			missed = true
			r.parts, r.bytes = parts, bytes
		}
		if err := idx.Drop(r.ctx, key); err != nil {
			return r.fatal(storeErr("index", err))
		}
	}
}
