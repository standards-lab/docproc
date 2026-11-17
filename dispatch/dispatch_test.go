package dispatch_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// sum returns content's lowercase hex SHA-256.
func sum(content string) string {
	s := sha256.Sum256([]byte(content))
	return hex.EncodeToString(s[:])
}

// TestDepthFirst: every part a stage finds is dispatched, with every part in it, before the
// stage's next; a part's event follows its descendants', the input's last. Each part names its
// ID, size, SHA-256, kind, route and reason, parent's ID, depth, and where it was found, and its
// event its stage's identity, its outputs, and its children, which its entry's log records.
func TestDepthFirst(t *testing.T) {
	world := map[string]string{
		"b.txt": "file d.txt",
		"c.txt": "leaf c",
		"d.txt": "leaf d",
	}
	f := newFixture(t, world, nil, dispatch.Limits{})
	top := "file b.txt\nfile c.txt"
	events := dispatchAll(t, f.d, input(t, "a.txt", top), nil)
	wantSteps(t, events, []step{
		{"d.txt", 2, dispatch.Prepared},
		{"b.txt", 1, dispatch.Prepared},
		{"c.txt", 1, dispatch.Prepared},
		{"a.txt", 0, dispatch.Prepared},
	})
	d, b, a := byName(t, events, "d.txt"), byName(t, events, "b.txt"), byName(t, events, "a.txt")
	if d.Part.Parent != "/line 1" || b.Part.Parent != "/" || a.Part.Parent != "" {
		t.Errorf("parents: d %s, b %s, a %q", d.Part.Parent, b.Part.Parent, a.Part.Parent)
	}
	if d.Part.ID != "/line 1/line 1" || b.Part.ID != "/line 1" || a.Part.ID != "/" {
		t.Errorf("IDs: d %s, b %s, a %s", d.Part.ID, b.Part.ID, a.Part.ID)
	}
	if d.Part.SHA256 != sum("leaf d") || d.Part.Size != int64(len("leaf d")) || d.Part.Where != "line 1" {
		t.Errorf("d: %+v", d.Part)
	}
	if a.Part.ContentType.Route != route.Text || a.Part.ContentType.RouteReason != "text by its .txt extension" || a.Part.ContentType.Kind != route.Unknown {
		t.Errorf("a's route: %s, %q, kind %q", a.Part.ContentType.Route, a.Part.ContentType.RouteReason, a.Part.ContentType.Kind)
	}
	if a.Stage != f.text.Identity() || len(a.Outputs) != 1 || a.Outputs[0].Kind != dispatch.TextOutput {
		t.Errorf("a's stage and outputs: %v, %+v", a.Stage, a.Outputs)
	}
	want := []dispatch.Part{b.Part, byName(t, events, "c.txt").Part}
	for i := range want {
		want[i].Path = ""
	}
	key := dispatch.Key{SHA256: a.Part.SHA256, Stage: f.text.Identity()}
	got := children(t, f.index, key)
	if a.Children != len(want) || len(got) != len(want) {
		t.Fatalf("a's %d children, read back: %+v", a.Children, got)
	}
	for i, c := range got {
		if c.Outcome != dispatch.Prepared || !reflect.DeepEqual(c.Part, want[i]) {
			t.Errorf("a's child %d, read back:\n got %+v\nwant %+v", i, c, want[i])
		}
	}
}

// TestOutcomes: one input reaches every outcome but limited: a route with no stage and a stage
// with no processor yet are no processor yet; a stage's ErrEncrypted is encrypted; a stage's
// failure is failed, content flagged encrypted among it, since a flag decides no outcome; a ZIP
// whose central directory can't be read is quarantined, no stage run and its file removed once its
// event is done; content equal to an ancestor's is a cycle; and content seen earlier in the input
// is seen, carrying its first event's outputs and flags.
func TestOutcomes(t *testing.T) {
	top := "file x.bin\nfile n.txt\nfile e.txt\nfile f.txt\nfile broken.zip\ncopy encrypted.zip\nfile loop.txt\ncopy javascript.pdf\nfile s1.txt\nfile s2.txt"
	world := map[string]string{
		"x.bin":      "\x00\x01no signature",
		"n.txt":      "noproc",
		"e.txt":      "encrypted",
		"f.txt":      "leaf\nfail",
		"loop.txt":   "file again.txt",
		"broken.zip": "PK\x03\x04 and no central directory",
		"again.txt": "file x.bin\nfile n.txt\nfile e.txt\nfile f.txt\nfile broken.zip\ncopy encrypted.zip\nfile loop.txt\n" +
			"copy javascript.pdf\nfile s1.txt\nfile s2.txt",
		"s1.txt": "same",
		"s2.txt": "same",
	}
	if world["again.txt"] != top {
		t.Fatal("again.txt must be the input's content")
	}
	f := newFixture(t, world, nil, dispatch.Limits{})
	var quarantinedPath string
	events := dispatchAll(t, f.d, input(t, "top.txt", top), func(ev *dispatch.Event) {
		if ev.Outcome == dispatch.Quarantined {
			quarantinedPath = ev.Part.Path
			if _, err := os.Stat(quarantinedPath); err != nil {
				t.Errorf("the quarantined file is gone in its event's body: %v", err)
			}
		}
	})
	wantSteps(t, events, []step{
		{"x.bin", 1, dispatch.NoProcessor},
		{"n.txt", 1, dispatch.NoProcessor},
		{"e.txt", 1, dispatch.Encrypted},
		{"f.txt", 1, dispatch.Failed},
		{"broken.zip", 1, dispatch.Quarantined},
		{"encrypted.zip", 1, dispatch.Failed},
		{"again.txt", 2, dispatch.Cycle},
		{"loop.txt", 1, dispatch.Prepared},
		{"javascript.pdf", 1, dispatch.Prepared},
		{"s1.txt", 1, dispatch.Prepared},
		{"s2.txt", 1, dispatch.Seen},
		{"top.txt", 0, dispatch.Prepared},
	})
	if ev := byName(t, events, "x.bin"); ev.Reason != "no stage for route unrouted" || ev.Stage != (dispatch.Identity{}) {
		t.Errorf("x.bin: %q, %v", ev.Reason, ev.Stage)
	}
	if ev := byName(t, events, "n.txt"); ev.Reason != "container: no processor yet for this" {
		t.Errorf("n.txt: %q", ev.Reason)
	}
	if ev := byName(t, events, "e.txt"); !errors.Is(ev.Err, dispatch.ErrEncrypted) {
		t.Errorf("e.txt: %v", ev.Err)
	}
	if ev := byName(t, events, "f.txt"); ev.Err == nil || len(ev.Outputs) != 1 {
		t.Errorf("f.txt: %v, its outputs before the failure %+v", ev.Err, ev.Outputs)
	}
	q := byName(t, events, "broken.zip")
	if !hasFlag(q.Part.Flags, inspect.ZIPUnreadable) || q.Reason == "" {
		t.Errorf("broken.zip: %+v, %q", q.Part.Flags, q.Reason)
	}
	if _, err := os.Stat(quarantinedPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the quarantined file is left once its event is done: %v", err)
	}
	if enc := byName(t, events, "encrypted.zip"); !hasFlag(enc.Part.Flags, inspect.ArchiveEncrypted) || enc.Err == nil {
		t.Errorf("encrypted.zip: %+v, %v", enc.Part.Flags, enc.Err)
	}
	if f.archive.called() != 1 {
		t.Errorf("archive stage ran %d times, want once: never for the quarantined ZIP", f.archive.called())
	}
	if ev := byName(t, events, "again.txt"); ev.Reason != "same content as its ancestor at depth 0" {
		t.Errorf("again.txt: %q", ev.Reason)
	}
	s1, s2 := byName(t, events, "s1.txt"), byName(t, events, "s2.txt")
	if s2.Recorded != dispatch.Prepared || s2.Stage != f.text.Identity() || len(s2.Outputs) != 1 || s2.Outputs[0].Path != "" {
		t.Errorf("s2.txt: recorded %s, stage %v, outputs %+v", s2.Recorded, s2.Stage, s2.Outputs)
	}
	if s1.Part.SHA256 != s2.Part.SHA256 || f.text.called("s2.txt") != 0 {
		t.Errorf("s2.txt prepared %d times", f.text.called("s2.txt"))
	}
}

// TestRetry: a part with a failed descendant is not recorded, so a second dispatch over the same
// index prepares it again, and the failure again, while its complete parts are seen.
func TestRetry(t *testing.T) {
	world := map[string]string{"ok.txt": "leaf", "f.txt": "fail"}
	f := newFixture(t, world, nil, dispatch.Limits{})
	path := input(t, "top.txt", "file ok.txt\nfile f.txt")
	dispatchAll(t, f.d, path, nil)
	events := dispatchAll(t, f.d, path, nil)
	wantSteps(t, events, []step{
		{"ok.txt", 1, dispatch.Seen},
		{"f.txt", 1, dispatch.Failed},
		{"top.txt", 0, dispatch.Prepared},
	})
	if n := f.text.called("f.txt"); n != 2 {
		t.Errorf("f.txt prepared %d times, want 2", n)
	}
}

// TestLimits: no cap applies by default; each cap the caller sets limits the parts past it,
// neither inspected nor prepared, and their siblings go on.
func TestLimits(t *testing.T) {
	world := map[string]string{
		"b.txt":   "file d.txt",
		"d.txt":   "leaf d",
		"c.txt":   "leaf c",
		"big.txt": "a line longer than the bytes cap leaves room for",
	}
	top := "file b.txt\nfile big.txt\nfile c.txt"
	for _, tc := range []struct {
		name   string
		limits dispatch.Limits
		want   []step
	}{
		{"none", dispatch.Limits{}, []step{
			{"d.txt", 2, dispatch.Prepared}, {"b.txt", 1, dispatch.Prepared},
			{"big.txt", 1, dispatch.Prepared}, {"c.txt", 1, dispatch.Prepared}, {"top.txt", 0, dispatch.Prepared},
		}},
		{"depth", dispatch.Limits{Depth: 1}, []step{
			{"d.txt", 2, dispatch.Limited}, {"b.txt", 1, dispatch.Prepared},
			{"big.txt", 1, dispatch.Prepared}, {"c.txt", 1, dispatch.Prepared}, {"top.txt", 0, dispatch.Prepared},
		}},
		{"parts", dispatch.Limits{Parts: 3}, []step{
			{"d.txt", 2, dispatch.Prepared}, {"b.txt", 1, dispatch.Prepared},
			{"big.txt", 1, dispatch.Limited}, {"c.txt", 1, dispatch.Limited}, {"top.txt", 0, dispatch.Prepared},
		}},
		{"bytes", dispatch.Limits{Bytes: int64(len(top) + len("file d.txt") + len("leaf d") + len("leaf c"))}, []step{
			{"d.txt", 2, dispatch.Prepared}, {"b.txt", 1, dispatch.Prepared},
			{"big.txt", 1, dispatch.Limited}, {"c.txt", 1, dispatch.Prepared}, {"top.txt", 0, dispatch.Prepared},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, world, nil, tc.limits)
			events := dispatchAll(t, f.d, input(t, "top.txt", top), nil)
			wantSteps(t, events, tc.want)
			for _, ev := range events {
				if ev.Outcome == dispatch.Limited && (ev.Reason == "" || ev.Part.Flags != nil || f.text.called(ev.Part.Name) != 0) {
					t.Errorf("%s: limited, yet %q, flags %v, prepared %d times", ev.Part.Name, ev.Reason, ev.Part.Flags, f.text.called(ev.Part.Name))
				}
			}
		})
	}
	t.Run("limited is not recorded", func(t *testing.T) {
		index := dispatch.NewMemoryIndex(0)
		path := input(t, "top.txt", top)
		dispatchAll(t, newFixture(t, world, index, dispatch.Limits{Depth: 1}).d, path, nil)
		events := dispatchAll(t, newFixture(t, world, index, dispatch.Limits{}).d, path, nil)
		wantSteps(t, events, []step{
			{"d.txt", 2, dispatch.Prepared}, {"b.txt", 1, dispatch.Prepared},
			{"big.txt", 1, dispatch.Seen}, {"c.txt", 1, dispatch.Seen}, {"top.txt", 0, dispatch.Prepared},
		})
	})
}

// TestSeenAcrossInputs: two inputs sharing an index prepare their shared content once; the
// second's seen events, the shared part's replayed child's among them, carry the outputs'
// SHA-256s, not their removed paths, and the flags the entry recorded.
func TestSeenAcrossInputs(t *testing.T) {
	world := map[string]string{"shared.txt": "file inner.txt", "inner.txt": "leaf"}
	for name, newIndex := range indexes() {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, world, newIndex(t), dispatch.Limits{})
			dispatchAll(t, f.d, input(t, "one.txt", "file shared.txt\ncopy javascript.pdf"), nil)
			events := dispatchAll(t, f.d, input(t, "two.txt", "copy javascript.pdf\nfile shared.txt"), nil)
			wantSteps(t, events, []step{
				{"javascript.pdf", 1, dispatch.Seen},
				{"inner.txt", 2, dispatch.Seen},
				{"shared.txt", 1, dispatch.Seen},
				{"two.txt", 0, dispatch.Prepared},
			})
			shared := byName(t, events, "shared.txt")
			if len(shared.Outputs) != 1 || shared.Outputs[0].SHA256 != sum("1") || shared.Outputs[0].Path != "" {
				t.Errorf("shared.txt's outputs: %+v", shared.Outputs)
			}
			got := children(t, f.index, dispatch.Key{SHA256: shared.Part.SHA256, Stage: shared.Stage})
			if shared.Children != 1 || len(got) != 1 || got[0].Part.Name != "inner.txt" || got[0].Part.SHA256 != sum("leaf") {
				t.Errorf("shared.txt's %d children, read back: %+v", shared.Children, got)
			}
			pdf := byName(t, events, "javascript.pdf")
			if !hasFlag(pdf.Part.Flags, inspect.PDFJavaScript) || pdf.Outputs[0].SHA256 != sum(`{"name":"javascript.pdf"}`) {
				t.Errorf("javascript.pdf: flags %+v, outputs %+v", pdf.Part.Flags, pdf.Outputs)
			}
			if f.text.called("shared.txt") != 1 || f.text.called("inner.txt") != 1 || f.pdf.called() != 1 {
				t.Errorf("prepared shared %d, inner %d, pdf %d times, want each once",
					f.text.called("shared.txt"), f.text.called("inner.txt"), f.pdf.called())
			}
		})
	}
}

// TestRemoval: each part's file, and its working directory with its outputs' files, is there
// while the caller's loop body runs for each of its events, its outputs' and its own, and gone
// once its own event's body returns; a file the caller moved away is no error; the input is never
// removed.
func TestRemoval(t *testing.T) {
	world := map[string]string{"move.txt": "leaf move", "drop.txt": "leaf drop"}
	f := newFixture(t, world, nil, dispatch.Limits{})
	path := input(t, "top.txt", "file move.txt\nfile drop.txt")
	moved := filepath.Join(t.TempDir(), "moved.txt")
	paths := map[string][]string{}
	dispatchAll(t, f.d, path, func(ev *dispatch.Event) {
		files := []string{ev.Part.Path}
		if ev.Output != nil {
			files = append(files, ev.Output.Path)
		}
		for _, p := range files {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s: %s missing in the loop's body: %v", ev.Part.Name, p, err)
			}
			if !slices.Contains(paths[ev.Part.Name], p) {
				paths[ev.Part.Name] = append(paths[ev.Part.Name], p)
			}
		}
		if ev.Part.Name == "move.txt" && ev.Output != nil {
			if err := os.Rename(ev.Output.Path, moved); err != nil {
				t.Fatal(err)
			}
		}
	})
	for name, files := range paths {
		for _, p := range files {
			_, err := os.Stat(p)
			if exists := err == nil; exists != (p == path) {
				t.Errorf("%s: %s exists %v, want %v", name, p, exists, p == path)
			}
		}
	}
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("the moved output: %v", err)
	}
	if entries, err := os.ReadDir(f.dir); err != nil || len(entries) != 0 {
		t.Errorf("left in the working directory: %v, %v", entries, err)
	}
}

// TestOutputContent: each output's content is in the content store, by its SHA-256, before its
// event is yielded, and the index records the output without its path, so a seen part's output
// carries its SHA-256 alone, its content found in the store.
func TestOutputContent(t *testing.T) {
	f := newFixture(t, map[string]string{}, nil, dispatch.Limits{})
	path := input(t, "top.txt", "leaf")
	read := func(sum string) string {
		t.Helper()
		rc, err := f.d.Content().Open(t.Context(), sum)
		if err != nil {
			t.Fatalf("open %s: %v", sum, err)
		}
		defer func() { _ = rc.Close() }()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	dispatchAll(t, f.d, path, func(ev *dispatch.Event) {
		if ev.Output != nil {
			if got := read(ev.Output.SHA256); got != "1" || ev.Output.SHA256 != sum("1") {
				t.Errorf("output %s: content %q", ev.Output.SHA256, got)
			}
		}
	})
	events := dispatchAll(t, f.d, path, nil)
	if o := events[0].Outputs[0]; events[0].Outcome != dispatch.Seen || o.Path != "" || o.SHA256 != sum("1") || read(o.SHA256) != "1" {
		t.Fatalf("seen %s, output %+v", events[0].Outcome, o)
	}
}

// TestStopEarly: a loop that stops early stops the stage, releases every claim, and leaves the
// working directory empty, so a second dispatch prepares everything.
func TestStopEarly(t *testing.T) {
	world := map[string]string{"b.txt": "file d.txt", "d.txt": "leaf", "c.txt": "leaf c"}
	f := newFixture(t, world, nil, dispatch.Limits{})
	path := input(t, "top.txt", "file b.txt\nfile c.txt")
	// The loop stops at the first part's own event, d.txt's, the outputs of top.txt, b.txt, and
	// d.txt before it.
	for ev, err := range f.d.Dispatch(t.Context(), dispatch.Input{Path: path}) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Output != nil {
			continue
		}
		if ev.Part.Name != "d.txt" {
			t.Fatalf("first part's event %s", ev.Part.Name)
		}
		break
	}
	if entries, err := os.ReadDir(f.dir); err != nil || len(entries) != 0 {
		t.Fatalf("working directory: %v, %v", entries, err)
	}
	events := dispatchAll(t, f.d, path, nil)
	wantSteps(t, events, []step{
		{"d.txt", 2, dispatch.Seen},
		{"b.txt", 1, dispatch.Prepared},
		{"c.txt", 1, dispatch.Prepared},
		{"top.txt", 0, dispatch.Prepared},
	})
}

// failingRecord is an index whose Record fails, once the entry's claim has been released.
type failingRecord struct{ dispatch.Index }

var errRecord = errors.New("told to fail recording")

func (f failingRecord) Record(ctx context.Context, key dispatch.Key, token string, _ dispatch.Entry) error {
	_ = f.Release(ctx, key, token)
	return errRecord
}

// TestRecordFails: an index that fails to record stops the dispatch, before the part's own event,
// with its error, alone and last, a *StoreError of the index, typed apart from an input's; the
// files of the part it failed on are removed all the same.
func TestRecordFails(t *testing.T) {
	f := newFixture(t, map[string]string{"b.txt": "leaf"}, failingRecord{dispatch.NewMemoryIndex(0)}, dispatch.Limits{})
	var events []dispatch.Event
	var errs []error
	for ev, err := range f.d.Dispatch(t.Context(), dispatch.Input{Path: input(t, "top.txt", "file b.txt")}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ev.Output == nil {
			events = append(events, ev)
		}
	}
	if len(events) != 0 {
		t.Fatalf("events %+v, want none: an entry is recorded before its part's event", events)
	}
	store, ok := errors.AsType[*dispatch.StoreError](errs[0])
	if len(errs) != 1 || !errors.Is(errs[0], errRecord) || !ok || store.Store != "index" {
		t.Fatalf("errors: %v, want the index's *StoreError", errs)
	}
	if entries, err := os.ReadDir(f.dir); err != nil || len(entries) != 0 {
		t.Fatalf("working directory: %v, %v", entries, err)
	}
}

// TestInputErrors: an input that can't be read is an error alone, which stops the dispatch; a
// cancelled context stops it too.
func TestInputErrors(t *testing.T) {
	f := newFixture(t, map[string]string{}, nil, dispatch.Limits{})
	var errs []error
	for ev, err := range f.d.Dispatch(t.Context(), dispatch.Input{Path: filepath.Join(t.TempDir(), "missing.txt")}) {
		if err == nil {
			t.Fatalf("event %v", ev)
		}
		errs = append(errs, err)
	}
	if len(errs) != 1 || !errors.Is(errs[0], fs.ErrNotExist) {
		t.Fatalf("errors: %v", errs)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	errs = nil
	for _, err := range f.d.Dispatch(ctx, dispatch.Input{Path: input(t, "a.txt", "leaf")}) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("a cancelled dispatch yielded %v, want the context's error alone", errs)
	}
}

// TestNew: a config with no working directory, a missing one, a nil stage, two stages sharing a
// name with different identities, a negative cap or release bound, or puts under 1 is refused,
// matching ErrRequest; one with no index or no content store panics, naming the field.
func TestNew(t *testing.T) {
	t.Parallel()
	index, content := dispatch.NewMemoryIndex(0), dispatch.NewMemoryContent()
	dir := t.TempDir()
	wantPanic(t, "dispatch: Config.Index is nil", func() { _, _ = dispatch.New(dispatch.Config{Content: content, Puts: 1, Dir: dir}) })
	wantPanic(t, "dispatch: Config.Content is nil", func() { _, _ = dispatch.New(dispatch.Config{Index: index, Puts: 1, Dir: dir}) })
	ok := dispatch.Config{Index: index, Content: content, Dir: dir, Puts: 1}
	for name, change := range map[string]func(*dispatch.Config){
		"no dir":           func(c *dispatch.Config) { c.Dir = "" },
		"missing dir":      func(c *dispatch.Config) { c.Dir = filepath.Join(dir, "missing") },
		"nil stage":        func(c *dispatch.Config) { c.Stages = dispatch.Stages{route.Text: nil} },
		"negative cap":     func(c *dispatch.Config) { c.Limits = dispatch.Limits{Depth: -1} },
		"no puts":          func(c *dispatch.Config) { c.Puts = 0 },
		"negative release": func(c *dispatch.Config) { c.Release = -1 },
		"one name, two identities": func(c *dispatch.Config) {
			c.Stages = dispatch.Stages{route.Document: &leaf{name: "same"}, route.Archive: &other{leaf{name: "same"}}}
		},
	} {
		cfg := ok
		change(&cfg)
		if _, err := dispatch.New(cfg); !errors.Is(err, dispatch.ErrRequest) {
			t.Errorf("%s: %v, want ErrRequest", name, err)
		}
	}
	if _, err := dispatch.New(ok); err != nil {
		t.Errorf("a whole config: %v", err)
	}
}

// other is a leaf whose identity's configuration differs from the leaf's of its name.
type other struct{ leaf }

func (o *other) Identity() dispatch.Identity {
	id := o.leaf.Identity()
	id.Config = "other"
	return id
}

// TestHashConfig: the same configuration hashes the same, whatever its map's order, and another
// differently.
func TestHashConfig(t *testing.T) {
	a, err := dispatch.HashConfig(map[string]int{"dpi": 150, "scale": 2})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := dispatch.HashConfig(map[string]int{"scale": 2, "dpi": 150})
	c, _ := dispatch.HashConfig(map[string]int{"dpi": 300, "scale": 2})
	if a != b || a == c || len(a) != 64 {
		t.Fatalf("hashes %s, %s, %s", a, b, c)
	}
}
