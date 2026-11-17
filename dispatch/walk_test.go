package dispatch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// walk is a dispatcher over the fixture stage on route.Text, its index, its content store, and its
// working directory.
type walk struct {
	d       *dispatch.Dispatcher
	stage   *dispatchtest.Stage
	index   dispatch.Index
	content dispatch.Content
	dir     string
}

// newWalk returns a walk over world, its index and content store, new memory ones when nil, under
// opts, with puts in flight.
func newWalk(t *testing.T, world map[string]string, index dispatch.Index, content dispatch.Content, opts inspect.Options) *walk {
	t.Helper()
	if index == nil {
		index = dispatch.NewMemoryIndex(0)
	}
	if content == nil {
		content = dispatch.NewMemoryContent()
	}
	w := &walk{stage: dispatchtest.NewStage(world), index: index, content: content, dir: t.TempDir()}
	d, err := dispatch.New(dispatch.Config{
		Stages: dispatch.Stages{route.Text: w.stage}, Index: index, Content: content, Dir: w.dir,
		Inspect: opts, Puts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.d = d
	return w
}

// partSteps returns the steps of the parts' own events among events, which may repeat a part's.
func partSteps(t *testing.T, d *dispatch.Dispatcher, path string) []step {
	t.Helper()
	events, errs := all(t, d, dispatch.Input{Path: path})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var out []step
	for _, ev := range events {
		if ev.Output == nil && ev.Warning == nil {
			out = append(out, step{ev.Part.Name, ev.Part.Depth, ev.Outcome})
		}
	}
	return out
}

// wantPartSteps fails the test unless got is want.
func wantPartSteps(t *testing.T, got, want []step) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}
}

// fixtureKey returns the fixture stage's key of content.
func fixtureKey(content string) dispatch.Key {
	return dispatch.Key{SHA256: sum(content), Stage: dispatchtest.NewStage(nil).Identity()}
}

// all dispatches in through d, returning every event and the errors, in order.
func all(t *testing.T, d *dispatch.Dispatcher, in dispatch.Input) ([]dispatch.Event, []error) {
	t.Helper()
	var events []dispatch.Event
	var errs []error
	for ev, err := range d.Dispatch(t.Context(), in) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		events = append(events, ev)
	}
	return events, errs
}

// TestReplayMiss: a replayed part whose child's entry is gone is prepared again by the nearest
// part with a file. The input replays from the index, its first child's events going out; its
// second child, replayed with no file, finds its own child's entry gone before its first event and
// passes the miss up. The input, which has a file, drops its entry and is prepared again: its
// first child's events repeat, at least once, its second child, found again with its file,
// resolves the miss itself, and only the missing content and the parts above it are prepared.
func TestReplayMiss(t *testing.T) {
	world := map[string]string{"x.txt": "leaf x", "b.txt": "file c.txt", "c.txt": "leaf c"}
	w := newWalk(t, world, nil, nil, inspect.Options{})
	path := input(t, "a.txt", "file x.txt\nfile b.txt")
	wantPartSteps(t, partSteps(t, w.d, path), []step{
		{"x.txt", 1, dispatch.Prepared}, {"c.txt", 2, dispatch.Prepared}, {"b.txt", 1, dispatch.Prepared}, {"a.txt", 0, dispatch.Prepared},
	})
	if err := w.index.Drop(t.Context(), fixtureKey("leaf c")); err != nil {
		t.Fatal(err)
	}
	wantPartSteps(t, partSteps(t, w.d, path), []step{
		{"x.txt", 1, dispatch.Seen},
		{"x.txt", 1, dispatch.Seen},
		{"c.txt", 2, dispatch.Prepared},
		{"b.txt", 1, dispatch.Prepared},
		{"a.txt", 0, dispatch.Prepared},
	})
	for name, want := range map[string]int{"a.txt": 2, "b.txt": 2, "c.txt": 2, "x.txt": 1} {
		if got := w.stage.Calls(name); got != want {
			t.Errorf("%s prepared %d times, want %d", name, got, want)
		}
	}
	// Recorded again whole, the next dispatch replays every part.
	wantPartSteps(t, partSteps(t, w.d, path), []step{
		{"x.txt", 1, dispatch.Seen}, {"c.txt", 2, dispatch.Seen}, {"b.txt", 1, dispatch.Seen}, {"a.txt", 0, dispatch.Seen},
	})
	if got := w.stage.Calls("a.txt"); got != 2 {
		t.Errorf("a.txt prepared %d times, want 2", got)
	}
}

// TestReplayTree: a seen part replays its log from the index with no file: its outputs, warnings,
// and children, each child at its place under the replaying part, its ID and depth this input's;
// an unread child as recorded; and the stages of its subtree checked, so an entry recorded under a
// stage whose identity has changed since is dropped and prepared again.
func TestReplayTree(t *testing.T) {
	world := map[string]string{"b.txt": "warn a page read badly\nunread secret.bin\nrecord {\"b\":1}"}
	index, content := dispatch.NewMemoryIndex(0), dispatch.NewMemoryContent()
	w := newWalk(t, world, index, content, inspect.Options{})
	shape := func(events []dispatch.Event) []string {
		var out []string
		for _, ev := range events {
			switch {
			case ev.Output != nil:
				out = append(out, ev.Part.ID+" output "+ev.Output.Name+" "+string(ev.Outcome))
			case ev.Warning != nil:
				out = append(out, ev.Part.ID+" warning "+ev.Warning.What+" "+string(ev.Outcome))
			default:
				out = append(out, ev.Part.ID+" "+string(ev.Outcome))
			}
		}
		return out
	}
	first, errs := all(t, w.d, dispatch.Input{Path: input(t, "a.txt", "text\nfile b.txt")})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want := []string{
		"/ output lines ",
		"/line 2 output lines ", "/line 2 warning a page read badly ",
		"/line 2/line 2 encrypted", "/line 2 output record 3 ", "/line 2 prepared",
		"/ prepared",
	}
	if got := shape(first); !slices.Equal(got, want) {
		t.Fatalf("prepared:\n got %q\nwant %q", got, want)
	}
	// The same content at another place: b.txt's subtree replays under its new ID.
	second, errs := all(t, w.d, dispatch.Input{Path: input(t, "z.txt", "other\nmore\nfile b.txt")})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want = []string{
		"/ output lines ",
		"/line 3 output lines seen", "/line 3 warning a page read badly seen",
		"/line 3/line 2 encrypted", "/line 3 output record 3 seen", "/line 3 seen",
		"/ prepared",
	}
	if got := shape(second); !slices.Equal(got, want) {
		t.Fatalf("replayed:\n got %q\nwant %q", got, want)
	}
	if b := second[5]; b.Recorded != dispatch.Prepared || b.Outputs != 2 || b.Children != 1 || b.Part.Depth != 1 {
		t.Errorf("b.txt replayed: %+v", b)
	}
	if u := second[3]; u.Part.Name != "secret.bin" || u.Part.SHA256 != "" || u.Part.Depth != 2 || u.Part.Parent != "/line 3" {
		t.Errorf("the unread child replayed: %+v", u.Part)
	}

	// A stage of the same name with another identity: b.txt's entry, and the input's, which ran
	// it, are no longer fresh, so both are dropped and prepared again.
	changed := &renamed{Stage: dispatchtest.NewStage(world)}
	d, err := dispatch.New(dispatch.Config{
		Stages: dispatch.Stages{route.Text: changed}, Index: index, Content: content, Dir: t.TempDir(), Puts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	third, errs := all(t, d, dispatch.Input{Path: input(t, "z.txt", "other\nmore\nfile b.txt")})
	if len(errs) != 0 || third[len(third)-2].Outcome != dispatch.Prepared || changed.Calls("b.txt") != 1 {
		t.Fatalf("under a changed identity: %q, %v, b.txt prepared %d times", shape(third), errs, changed.Calls("b.txt"))
	}
}

// renamed is the fixture stage under another configuration of the same name.
type renamed struct{ *dispatchtest.Stage }

func (r *renamed) Identity() dispatch.Identity {
	id := r.Stage.Identity()
	id.Config = "changed"
	return id
}

// TestResumeCursor: a store's cursor is opaque, compared by equality alone. The store's messages
// come in an order whose ids don't ascend, as a .pst's node ids don't; resumed after message 10,
// a replay skips its log through message 10's output, the store's first outputs and message 30
// among it, and goes on with message 20 and the part found after it, as the stage itself resumes
// when the content is prepared. A cursor no item has ends the dispatch with an error, and a
// resumed input's entry is never recorded.
func TestResumeCursor(t *testing.T) {
	world := map[string]string{"att.txt": "leaf"}
	store := "message 30\nmessage 10\nmessage 20\nfile att.txt"
	names := func(events []dispatch.Event) []string {
		var out []string
		for _, ev := range events {
			switch {
			case ev.Part.Depth > 0:
				if ev.Output == nil {
					out = append(out, ev.Part.Name)
				}
			case ev.Output != nil:
				out = append(out, ev.Output.Name)
			}
		}
		return out
	}
	want := []string{"message 20", "att.txt"}

	// Prepared resumed, over an empty index: the stage resumes, and nothing of the store records.
	w := newWalk(t, world, nil, nil, inspect.Options{})
	path := input(t, "store.txt", store)
	events, errs := all(t, w.d, dispatch.Input{Path: path, After: 10})
	if got := names(events); len(errs) != 0 || !slices.Equal(got, want) {
		t.Fatalf("prepared resumed: %q, %v; want %q", got, errs, want)
	}
	if _, ok, err := w.index.Lookup(t.Context(), fixtureKey(store)); err != nil || ok {
		t.Fatalf("the resumed store recorded: %v, %v", ok, err)
	}

	// Prepared whole, then replayed resumed.
	events, errs = all(t, w.d, dispatch.Input{Path: path})
	if len(errs) != 0 || events[len(events)-1].Outcome != dispatch.Prepared {
		t.Fatalf("the whole store: %v", errs)
	}
	for _, ev := range events {
		if ev.Output != nil && strings.HasPrefix(ev.Output.Name, "message ") && ev.After == 0 {
			t.Errorf("%s: no cursor", ev.Output.Name)
		}
	}
	events, errs = all(t, w.d, dispatch.Input{Path: path, After: 10})
	if got := names(events); len(errs) != 0 || !slices.Equal(got, want) {
		t.Fatalf("replayed resumed: %q, %v; want %q", got, errs, want)
	}
	if last := events[len(events)-1]; last.Outcome != dispatch.Seen || events[0].After != 20 {
		t.Errorf("replayed resumed: the store %s, message 20's cursor %d", last.Outcome, events[0].After)
	}
	if w.stage.Calls("store.txt") != 2 {
		t.Errorf("the store prepared %d times, want 2: once resumed, once whole", w.stage.Calls("store.txt"))
	}

	events, errs = all(t, w.d, dispatch.Input{Path: path, After: 99})
	if len(events) != 0 || len(errs) != 1 || !errors.Is(errs[0], dispatch.ErrRequest) {
		t.Fatalf("a cursor no item has: %d events, %v", len(events), errs)
	}
}

// refusing is an index whose renewals are refused, the first signalled on renewed. Its Record
// waits for the dispatch to end, so the refused renewal, not a race between the renewer's cancel
// and the walk's record, decides the outcome: a dispatch that a refusal didn't end records after
// 10 s with an error that isn't ErrClaimLost.
type refusing struct {
	dispatch.Index
	renewed chan struct{}
	once    sync.Once
}

func (r *refusing) Renew(context.Context, dispatch.Key, string) error {
	r.once.Do(func() { close(r.renewed) })
	return errors.New("the index refused the renewal")
}

func (r *refusing) Record(ctx context.Context, _ dispatch.Key, _ string, _ dispatch.Entry) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return errors.New("the dispatch went on to record after its renewal was refused")
	}
}

// TestClaimLost: the dispatch's renewer renews a claim at a third of its lease while the caller's
// loop body holds the walk, and a renewal the index refuses ends the dispatch with an error
// matching ErrClaimLost, alone and last, nothing recorded and the working directory emptied.
func TestClaimLost(t *testing.T) {
	index := &refusing{Index: dispatch.NewMemoryIndex(30 * time.Millisecond), renewed: make(chan struct{})}
	w := newWalk(t, map[string]string{}, index, nil, inspect.Options{})
	var events []dispatch.Event
	var errs []error
	for ev, err := range w.d.Dispatch(t.Context(), dispatch.Input{Path: input(t, "a.txt", "text\nrecord {}")}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(events) == 0 {
			select {
			case <-index.renewed:
			case <-time.After(10 * time.Second):
				t.Fatal("no renewal while the loop body held the walk")
			}
		}
		events = append(events, ev)
	}
	if len(errs) != 1 || !errors.Is(errs[0], dispatch.ErrClaimLost) {
		t.Fatalf("errors %v, want ErrClaimLost alone", errs)
	}
	if slices.ContainsFunc(events, func(ev dispatch.Event) bool { return ev.Output == nil }) {
		t.Errorf("a part's event after the claim was lost: %+v", events)
	}
	if _, ok, err := index.Lookup(t.Context(), fixtureKey("text\nrecord {}")); err != nil || ok {
		t.Errorf("recorded: %v, %v", ok, err)
	}
	wantEmpty(t, w.dir)
}

// TestInputSHA256: an input's claimed SHA-256 is checked as it is read or spooled: a mismatch
// stops the dispatch with an error matching ErrSHA256, alone, its spool removed; a Reader whose
// SHA-256 matches is spooled once, dispatched as the file would be, and its spool removed; and an
// input with both or neither of Path and Reader is refused.
func TestInputSHA256(t *testing.T) {
	w := newWalk(t, map[string]string{}, nil, nil, inspect.Options{})
	const content = "text"
	path := input(t, "a.txt", content)
	for name, in := range map[string]dispatch.Input{
		"path":   {Path: path, SHA256: sum("other")},
		"reader": {Reader: strings.NewReader(content), Name: "a.txt", SHA256: sum("other")},
	} {
		events, errs := all(t, w.d, in)
		if len(events) != 0 || len(errs) != 1 || !errors.Is(errs[0], dispatch.ErrSHA256) {
			t.Errorf("%s: %d events, %v; want ErrSHA256 alone", name, len(events), errs)
		}
	}
	events, errs := all(t, w.d, dispatch.Input{Reader: strings.NewReader(content), Name: "a.txt", SHA256: sum(content)})
	if len(errs) != 0 || len(events) != 2 {
		t.Fatalf("a matching reader: %d events, %v", len(events), errs)
	}
	if p := events[1].Part; p.SHA256 != sum(content) || p.Name != "a.txt" || p.ContentType.Route != route.Text || events[1].Outcome != dispatch.Prepared {
		t.Errorf("the spooled input: %s, %+v", events[1].Outcome, p)
	}
	wantEmpty(t, w.dir)
	for name, in := range map[string]dispatch.Input{
		"both":    {Path: path, Reader: strings.NewReader(content)},
		"neither": {Name: "a.txt"},
		"bad sum": {Path: path, SHA256: "ABC"},
	} {
		if _, errs := all(t, w.d, in); len(errs) != 1 || !errors.Is(errs[0], dispatch.ErrRequest) {
			t.Errorf("%s: %v, want ErrRequest", name, errs)
		}
	}
}

// TestFoundSHA256: a stage's claim of a found part's SHA-256 is checked against the hash the
// dispatcher takes: a mismatch fails the part as the run's fault, matching ErrSHA256, so its
// parent isn't recorded; a claim that holds is no failure.
func TestFoundSHA256(t *testing.T) {
	world := map[string]string{"b.txt": "leaf"}
	w := newWalk(t, world, nil, nil, inspect.Options{})
	events := dispatchAll(t, w.d, input(t, "a.txt", "lie b.txt"), nil)
	wantSteps(t, events, []step{{"b.txt", 1, dispatch.Failed}, {"a.txt", 0, dispatch.Prepared}})
	if !errors.Is(events[0].Err, dispatch.ErrSHA256) {
		t.Errorf("b.txt: %v, want ErrSHA256", events[0].Err)
	}
	if _, ok, _ := w.index.Lookup(t.Context(), fixtureKey("lie b.txt")); ok {
		t.Error("a part whose child's claim failed was recorded")
	}
	wantSteps(t, dispatchAll(t, w.d, input(t, "a.txt", "sum b.txt"), nil), []step{
		{"b.txt", 1, dispatch.Prepared}, {"a.txt", 0, dispatch.Prepared},
	})
}

// TestStageFlags: a flag a stage raises before its first output is weighed under the dispatcher's
// options, and a quarantine stops the stage, the part quarantined and, its content's flags alone
// coming to it, recorded; one raised later is recorded and reported, and stops nothing. The
// verdict is taken over the recorded flags and the name's under the current options: a policy
// that quarantines a recorded flag quarantines the seen part, replaying nothing.
func TestStageFlags(t *testing.T) {
	index := dispatch.NewMemoryIndex(0)
	w := newWalk(t, map[string]string{}, index, nil, inspect.Options{})
	early := input(t, "early.txt", "flag unsafe archive-bomb\nrecord {}")
	events := dispatchAll(t, w.d, early, nil)
	wantSteps(t, events, []step{{"early.txt", 0, dispatch.Quarantined}})
	if ev := events[0]; len(ev.Outputs) != 0 || !hasFlag(ev.Part.Flags, inspect.ArchiveBomb) {
		t.Errorf("early.txt: %d outputs, flags %v", len(ev.Outputs), ev.Part.Flags)
	}
	if e, ok, _ := index.Lookup(t.Context(), fixtureKey("flag unsafe archive-bomb\nrecord {}")); !ok || e.Outcome != dispatch.Quarantined || !hasFlag(e.Flags, inspect.ArchiveBomb) {
		t.Errorf("early.txt's entry: %+v, %v", e, ok)
	}
	if ev := dispatchAll(t, w.d, early, nil)[0]; ev.Outcome != dispatch.Quarantined || ev.Recorded != dispatch.Quarantined {
		t.Errorf("early.txt again: %s, recorded %s", ev.Outcome, ev.Recorded)
	}

	late := input(t, "late.txt", "record {}\nflag unsafe archive-bomb\nflag active vba-project")
	events = dispatchAll(t, w.d, late, nil)
	wantSteps(t, events, []step{{"late.txt", 0, dispatch.Prepared}})
	if ev := events[0]; len(ev.Outputs) != 2 || !hasFlag(ev.Part.Flags, inspect.ArchiveBomb) || !hasFlag(ev.Part.Flags, inspect.VBAProject) {
		t.Errorf("late.txt: %d outputs, flags %v", len(ev.Outputs), ev.Part.Flags)
	}

	active := input(t, "macro.txt", "flag active vba-project\nrecord {}")
	wantSteps(t, dispatchAll(t, w.d, active, nil), []step{{"macro.txt", 0, dispatch.Prepared}})
	strict := newWalk(t, map[string]string{}, index, nil, inspect.Options{
		Policy: map[inspect.Check]inspect.Verdict{inspect.VBAProject: inspect.Quarantine},
	})
	events = dispatchAll(t, strict.d, active, nil)
	wantSteps(t, events, []step{{"macro.txt", 0, dispatch.Quarantined}})
	if ev := events[0]; ev.Recorded != dispatch.Prepared || len(ev.Outputs) != 0 || strict.stage.Calls("macro.txt") != 0 {
		t.Errorf("macro.txt under the new policy: recorded %s, %d outputs replayed, prepared %d times",
			ev.Recorded, len(ev.Outputs), strict.stage.Calls("macro.txt"))
	}
}

// failingContent is a content store whose puts fail with err.
type failingContent struct {
	dispatch.Content
	err error
}

func (f failingContent) Put(context.Context, string, io.Reader) error { return f.err }

// TestContentFailure: a put the content store refuses as too large fails the part, recorded, its
// outputs before it out; any other failure of the store stops the dispatch with a *StoreError of
// the content store, typed apart from an input's error, the part unrecorded.
func TestContentFailure(t *testing.T) {
	const content = "text\nrecord {}"
	tooLarge := failingContent{dispatch.NewMemoryContent(), dispatch.ErrTooLarge}
	w := newWalk(t, map[string]string{}, nil, tooLarge, inspect.Options{})
	events := dispatchAll(t, w.d, input(t, "a.txt", content), nil)
	wantSteps(t, events, []step{{"a.txt", 0, dispatch.Failed}})
	if !errors.Is(events[0].Err, dispatch.ErrTooLarge) || len(events[0].Outputs) != 0 {
		t.Errorf("a.txt: %v, %d outputs", events[0].Err, len(events[0].Outputs))
	}
	if e, ok, _ := w.index.Lookup(t.Context(), fixtureKey(content)); !ok || e.Outcome != dispatch.Failed {
		t.Errorf("the too large part's entry: %+v, %v", e, ok)
	}

	broken := errors.New("the disk is full")
	w = newWalk(t, map[string]string{}, nil, failingContent{dispatch.NewMemoryContent(), broken}, inspect.Options{})
	events2, errs := all(t, w.d, dispatch.Input{Path: input(t, "a.txt", content)})
	store, ok := errors.AsType[*dispatch.StoreError](errs[len(errs)-1])
	if len(errs) != 1 || len(events2) != 0 || !ok || store.Store != "content" || !errors.Is(errs[0], broken) {
		t.Fatalf("%d events, errors %v; want the content store's *StoreError alone", len(events2), errs)
	}
	if _, ok, _ := w.index.Lookup(t.Context(), fixtureKey(content)); ok {
		t.Error("recorded after the content store failed")
	}
	wantEmpty(t, w.dir)
}

// hostileBytes are what a stage may give the dispatcher from the content it reads: invalid UTF-8,
// NUL, C0 and C1 controls, an escape sequence, and, at the end, a string far past every cap.
var hostileBytes = []string{
	"bad\xffutf8", "nul\x00byte", "bell\a\x1b[31mred", "c1\u0085\u009b", "tab\tand\nline",
	strings.Repeat("long", 2000),
}

// hostile is a stage, untrusted in its names, that puts hostileBytes in every string it gives the
// dispatcher, and finds two parts at one Where.
type hostile struct{}

func (hostile) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "hostile", Version: "1", Config: "test"}
}

func (hostile) UntrustedWhere() {}

func (hostile) Prepare(_ context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		if p.Depth > 0 {
			return
		}
		for i, s := range hostileBytes {
			o := dispatch.Output{
				Kind: dispatch.TextOutput, Name: s, MediaType: s, Value: []byte(s), Meta: map[string]string{s: s},
			}
			flag := inspect.Flag{Check: inspect.Polyglot, Class: inspect.Active, Evidence: s}
			path := dir + "/" + string(rune('a'+i)) + ".txt"
			if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
				yield(dispatch.Item{}, err)
				return
			}
			for _, item := range []dispatch.Item{
				dispatch.OutputItem(o),
				dispatch.WarningItem(dispatch.Warning{Tool: s, Where: s, What: s}),
				dispatch.FlagItem(flag),
				dispatch.PartItem(dispatch.Found{Path: path, Name: s, MIME: s, Where: s}),
				dispatch.PartItem(dispatch.Found{Name: s, Where: s, Outcome: dispatch.Encrypted, Reason: s, Flags: []inspect.Flag{flag}}),
			} {
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

// TestHostileBytes: every string of every event and of the index's log passes the one guard:
// valid UTF-8, no control but tab and line feed in a reason, each refused byte percent-encoded,
// and each field within its cap, a long one cut with its hash's head; and siblings' Where are
// unique, a second " (2)", for a stage whose names are untrusted.
func TestHostileBytes(t *testing.T) {
	index := dispatch.NewMemoryIndex(0)
	d, err := dispatch.New(dispatch.Config{
		Stages: dispatch.Stages{route.Text: hostile{}}, Index: index, Content: dispatch.NewMemoryContent(),
		Dir: t.TempDir(), Puts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, errs := all(t, d, dispatch.Input{Path: input(t, "a.txt", "hostile")})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var wire bytes.Buffer
	for _, ev := range events {
		line(t, &wire, ev)
	}
	last := events[len(events)-1]
	if last.Outcome != dispatch.Prepared || last.Children != 2*len(hostileBytes) {
		t.Fatalf("the input: %s, %d children", last.Outcome, last.Children)
	}
	wheres := map[string]bool{}
	var reasons []string
	for _, ev := range events {
		if ev.Outcome == dispatch.Encrypted {
			reasons = append(reasons, ev.Reason)
		}
		if ev.Part.Depth == 1 && ev.Output == nil && ev.Warning == nil {
			if wheres[ev.Part.Where] {
				t.Errorf("two siblings at %q", ev.Part.Where)
			}
			wheres[ev.Part.Where] = true
		}
	}
	if !wheres["nul%00byte"] || !wheres["nul%00byte (2)"] || !wheres["tab%09and%0Aline"] {
		t.Errorf("wheres: %q", slices.Sorted(keys(wheres)))
	}
	if !slices.Contains(reasons, "tab\tand\nline") || !slices.Contains(reasons, "nul%00byte") {
		t.Errorf("a reason keeps tab and line feed alone: %q", reasons)
	}
	// The input's entry and its log, as the index holds them.
	key := dispatch.Key{SHA256: sum("hostile"), Stage: hostile{}.Identity()}
	e, ok, err := index.Lookup(t.Context(), key)
	if err != nil || !ok {
		t.Fatalf("the input's entry: %v, %v", ok, err)
	}
	line(t, &wire, e)
	for _, r := range logged(t, index, key) {
		line(t, &wire, r)
	}
	checkWire(t, wire.Bytes())
}

// keys returns m's keys.
func keys(m map[string]bool) iter.Seq[string] {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// checkWire fails the test unless every string in the JSON lines of wire is valid UTF-8 with no
// control but tab and line feed, those only in a reason, and within the largest cap, a reason's.
func checkWire(t *testing.T, wire []byte) {
	t.Helper()
	if !utf8.Valid(wire) {
		t.Fatal("the wire isn't valid UTF-8")
	}
	for l := range bytes.Lines(wire) {
		var v any
		if err := json.Unmarshal(l, &v); err != nil {
			t.Fatal(err)
		}
		walkStrings(v, "", func(key, s string) {
			for _, r := range s {
				lineBreak := key == "reason" && (r == '\t' || r == '\n')
				if (r < 0x20 || (r >= 0x80 && r <= 0x9f)) && !lineBreak {
					t.Errorf("%s holds control %U: %q", key, r, s)
					return
				}
			}
			limit := 255
			switch key {
			case "id", "parent":
				// A chain of capped Where, as deep as the input.
				return
			case "reason":
				limit = 4096
			case "what", "evidence", "meta", "where":
				// A warning's where is capped as its what; a part's, as a name.
				limit = 1024
			}
			if len(s) > limit {
				t.Errorf("%s past %d bytes: %d", key, limit, len(s))
			}
		})
	}
}

// walkStrings calls each with every string in v, a decoded JSON value, and its key, or "meta"
// for a Meta key or value.
func walkStrings(v any, key string, each func(key, s string)) {
	switch v := v.(type) {
	case string:
		each(key, v)
	case []any:
		for _, x := range v {
			walkStrings(x, key, each)
		}
	case map[string]any:
		for k, x := range v {
			if key == "meta" {
				each("meta", k)
				walkStrings(x, "meta", each)
				continue
			}
			walkStrings(x, k, each)
		}
	}
}
