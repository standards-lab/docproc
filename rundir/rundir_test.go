package rundir_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
)

// The committed schema "2" run directory: two ended runs, the second stopped by an error, and a
// third that never ended, its record's last line torn.
const fixture = "testdata/rundir"

const (
	ended    = "019a5f3c-1d2e-7a10-8b00-000000000001"
	stopped  = "019a5f3c-1d2e-7a10-8b00-000000000002"
	unended  = "019a5f3c-1d2e-7a10-8b00-000000000003"
	otherRun = "019a5f3c-1d2e-7a10-8b00-0000000000ff"
)

// open opens the run directory at root to read it.
func open(t *testing.T, root string) *rundir.Dir {
	t.Helper()
	d, err := rundir.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// create opens a new run directory to write it, under a fresh temporary directory, and returns it
// with its root.
func create(t *testing.T) (*rundir.Dir, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "out")
	d, err := rundir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	return d, root
}

// records collects the events d's record of id yields, and the errors, in order.
func records(d *rundir.Dir, id string) ([]schema.Event, []error) {
	var evs []schema.Event
	var errs []error
	for e, err := range d.Record(id) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		evs = append(evs, e)
	}
	return evs, errs
}

// readFile returns the file at path as text.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// writeFile writes text to path, making its directory.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// begin begins a run over d from a manifest with only its inputs.
func begin(t *testing.T, d *rundir.Dir) *rundir.Run {
	t.Helper()
	r, err := d.Begin(schema.Manifest{Inputs: []string{"/docs"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The fixture read through the reader, then written again through the writer into a new run
// directory, is the fixture again, file for file and byte for byte, but each manifest's end,
// which the writer stamps when the run ends. The unended run is read, not written again: End
// would end it.
func TestRoundTrip_Fixture(t *testing.T) {
	ctx := context.Background()
	src := open(t, fixture)
	dst, root := create(t)

	ids, err := src.Runs()
	if err != nil {
		t.Fatal(err)
	}
	ends := map[string][2]schema.Time{} // a run's end in the fixture, then as written
	for _, id := range ids {
		m, err := src.Manifest(id)
		if err != nil {
			t.Fatal(err)
		}
		if m.End.IsZero() {
			continue
		}
		r, err := dst.Begin(m)
		if err != nil {
			t.Fatal(err)
		}
		for e, err := range src.Record(id) {
			if err != nil {
				t.Fatal(err)
			}
			if e.Output != nil {
				copyContent(t, ctx, src, dst, e.Output.SHA256)
			}
			if e.Quarantine != "" {
				kept, err := dst.Quarantine(e.Part.SHA256, filepath.Join(fixture, e.Quarantine))
				if err != nil || kept != e.Quarantine {
					t.Fatalf("Quarantine = %q, %v; want %q", kept, err, e.Quarantine)
				}
			}
			if err := r.Write(e); err != nil {
				t.Fatal(err)
			}
		}
		var stop error
		if m.Error != "" {
			stop = errors.New(m.Error)
		}
		before := time.Now()
		if err := r.End(stop); err != nil {
			t.Fatal(err)
		}
		written, err := dst.Manifest(id)
		if err != nil {
			t.Fatal(err)
		}
		if end := written.End.Time(); end.Before(before) || end.After(time.Now()) {
			t.Errorf("run %s ended at %v, want the time End ran", id, end)
		}
		ends[id] = [2]schema.Time{m.End, written.End}
	}
	if len(ends) != 2 {
		t.Fatalf("wrote %d runs again, want the fixture's 2 ended runs", len(ends))
	}

	want := tree(t, fixture, unended)
	got := tree(t, root, "")
	if !slices.Equal(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want))) {
		t.Fatalf("written files\n %v\nwant\n %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	for name, text := range want {
		for id, end := range ends {
			if name == filepath.Join("runs", id, "manifest.json") {
				text = strings.Replace(text, `"end": "`+end[0].String()+`"`, `"end": "`+end[1].String()+`"`, 1)
			}
		}
		if got[name] != text {
			t.Errorf("%s =\n%s\nwant\n%s", name, got[name], text)
		}
	}
}

// copyContent puts the content sha256 names, read from src's store, into dst's.
func copyContent(t *testing.T, ctx context.Context, src, dst *rundir.Dir, sha256 string) {
	t.Helper()
	rc, err := src.Content().Open(ctx, sha256)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	if err := dst.Content().Put(ctx, sha256, rc); err != nil {
		t.Fatal(err)
	}
}

// tree returns every file under root, by its path relative to root, as text: the index, which is
// dispatch's, left out, and the run skip.
func tree(t *testing.T, root, skip string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case e.IsDir() && (rel == "index" || skip != "" && rel == filepath.Join("runs", skip)):
			return filepath.SkipDir
		case !e.IsDir():
			files[rel] = readFile(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestOpen_FixtureRuns(t *testing.T) {
	d := open(t, fixture)

	ids, err := d.Runs()
	if err != nil || !slices.Equal(ids, []string{ended, stopped, unended}) {
		t.Errorf("Runs = %v, %v; want the three runs in order", ids, err)
	}
	// The unended run is the last to begin; the latest run that ended is the one before it.
	if latest, err := d.Latest(); err != nil || latest != stopped {
		t.Errorf("Latest = %q, %v; want %q", latest, err, stopped)
	}
	m, err := d.Manifest(stopped)
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != schema.Version || m.Run != stopped || m.Error == "" || m.End.IsZero() {
		t.Errorf("Manifest = %+v, want the stopped run's", m)
	}
}

// The fixture's contents are at ContentPath, from its root, and its outputs name them.
func TestOpen_FixtureContent(t *testing.T) {
	ctx := context.Background()
	d := open(t, fixture)

	evs, errs := records(d, ended)
	if len(errs) != 0 || len(evs) != 6 {
		t.Fatalf("Record = %d events, %v; want 6, no error", len(evs), errs)
	}
	var outputs int
	for _, e := range evs {
		if e.Output == nil {
			continue
		}
		outputs++
		rc, err := d.Content().Open(ctx, e.Output.SHA256)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file := readFile(t, filepath.Join(fixture, rundir.ContentPath(e.Output.SHA256))); file != string(b) {
			t.Errorf("ContentPath(%s) holds %q, want the content %q", e.Output.SHA256, file, b)
		}
	}
	if outputs != 2 {
		t.Errorf("read %d outputs, want 2", outputs)
	}
}

func TestContentPath(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	// The layout is schema "2"'s, which a reader that isn't Go follows: content/<ab>/<sha256>.
	if got, want := rundir.ContentPath(sha), filepath.Join("content", "ab", sha); got != want {
		t.Errorf("ContentPath = %q, want %q", got, want)
	}
	for _, bad := range []string{"", "AB" + sha[2:], sha[:63], "../" + sha[3:]} {
		if got := rundir.ContentPath(bad); got != "" {
			t.Errorf("ContentPath(%q) = %q, want none", bad, got)
		}
	}
}

// ReportDir names a run's report directory under the run's own, on the writer for the run it is
// writing and on a reader, makes nothing, and refuses an id that isn't a run.
func TestReportDir(t *testing.T) {
	d, root := create(t)
	r := begin(t, d)
	want := "runs/" + r.ID() + "/report"
	if got, err := d.ReportDir(r.ID()); got != want || err != nil {
		t.Errorf("writer's ReportDir = %q, %v; want %q, nil", got, err, want)
	}
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}
	reader := open(t, root)
	if got, err := reader.ReportDir(r.ID()); got != want || err != nil {
		t.Errorf("reader's ReportDir = %q, %v; want %q, nil", got, err, want)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(want))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReportDir made %s: %v", want, err)
	}
	if got, err := open(t, fixture).ReportDir(unended); got != "runs/"+unended+"/report" || err != nil {
		t.Errorf("fixture's ReportDir(unended) = %q, %v; want runs/%s/report, nil", got, err, unended)
	}
	for _, id := range []string{"", "..", "notes", otherRun, r.ID() + "/../" + r.ID()} {
		if _, err := reader.ReportDir(id); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReportDir(%q) = %v, want an error matching fs.ErrNotExist", id, err)
		}
	}
}

// An unended run's record drops its torn last line, and says last that the run hasn't ended.
func TestRecord_Unended(t *testing.T) {
	d := open(t, fixture)

	evs, errs := records(d, unended)
	if len(evs) != 2 || evs[0].Kind != schema.OutputEvent || evs[1].Kind != schema.WarningEvent {
		t.Errorf("Record = %+v, want the two whole lines", evs)
	}
	if len(errs) != 1 || !errors.Is(errs[0], rundir.ErrNotEnded) {
		t.Errorf("Record errors = %v, want one matching ErrNotEnded", errs)
	}
}

// A record ends on the first line that doesn't decode, naming it.
func TestRecord_EndsOnUndecodable(t *testing.T) {
	d, root := create(t)
	r := begin(t, d)
	if err := r.Write(schema.Event{Kind: schema.ErrorEvent, Input: "/docs/a", Error: "gone"}); err != nil {
		t.Fatal(err)
	}
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(root, "runs", r.ID(), "record.jsonl")
	writeFile(t, record, readFile(t, record)+"{\"event\":\n"+`{"event":"error","input":"/docs/b"}`+"\n")

	evs, errs := records(open(t, root), r.ID())
	if len(evs) != 1 || evs[0].Input != "/docs/a" {
		t.Errorf("Record = %+v, want the first line alone", evs)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "line 2") {
		t.Errorf("Record errors = %v, want one naming line 2", errs)
	}
}

// An ended run's last line with no newline is a line, and is decoded.
func TestRecord_EndedLastLineWithoutNewline(t *testing.T) {
	d, root := create(t)
	r := begin(t, d)
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "runs", r.ID(), "record.jsonl"), `{"event":"error","input":"/docs/a"}`)

	evs, errs := records(open(t, root), r.ID())
	if len(errs) != 0 || len(evs) != 1 || evs[0].Input != "/docs/a" {
		t.Errorf("Record = %+v, %v; want the one line", evs, errs)
	}
}

// A caller that stops early is yielded nothing more.
func TestRecord_StopsWithCaller(t *testing.T) {
	d := open(t, fixture)

	var n int
	for _, err := range d.Record(unended) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		break
	}
	if n != 1 {
		t.Errorf("yielded %d after a break, want 1", n)
	}
}

// Every method taking an id refuses one that isn't a run of the directory, matching
// fs.ErrNotExist.
func TestManifestRecord_RefuseNonRun(t *testing.T) {
	d, root := create(t)
	r := begin(t, d)
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}
	noManifest := uuid.NewV7().String()
	if err := os.MkdirAll(filepath.Join(root, "runs", noManifest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "runs", "notes", "manifest.json"), "{}")
	d = open(t, root)

	for _, id := range []string{
		"", ".", "..", "../runs", "notes", noManifest, strings.ToUpper(r.ID()),
		uuid.NewV4().String(), otherRun, r.ID() + "/../" + r.ID(),
	} {
		if _, err := d.Manifest(id); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Manifest(%q) = %v, want an error matching fs.ErrNotExist", id, err)
		}
		evs, errs := records(d, id)
		if len(evs) != 0 || len(errs) != 1 || !errors.Is(errs[0], fs.ErrNotExist) {
			t.Errorf("Record(%q) = %v, %v; want one error matching fs.ErrNotExist", id, evs, errs)
		}
	}
}

// Runs lists the runs alone, in the order they began; Latest names the latest that ended.
func TestRuns_RunsAlone(t *testing.T) {
	d, root := create(t)
	if ids, err := d.Runs(); err != nil || len(ids) != 0 {
		t.Errorf("Runs of a new directory = %v, %v; want none", ids, err)
	}
	if _, err := d.Latest(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Latest of a new directory = %v, want an error matching fs.ErrNotExist", err)
	}

	first := begin(t, d)
	second := begin(t, d)
	if err := os.MkdirAll(filepath.Join(root, "runs", uuid.NewV7().String()), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "runs", "stray", "manifest.json"), "{}")
	writeFile(t, filepath.Join(root, "runs", "notes.txt"), "a note")

	ids, err := d.Runs()
	if err != nil || !slices.Equal(ids, []string{first.ID(), second.ID()}) {
		t.Errorf("Runs = %v, %v; want %v", ids, err, []string{first.ID(), second.ID()})
	}
	if _, err := d.Latest(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Latest with no run ended = %v, want an error matching fs.ErrNotExist", err)
	}
	if err := first.End(nil); err != nil {
		t.Fatal(err)
	}
	if latest, err := d.Latest(); err != nil || latest != first.ID() {
		t.Errorf("Latest = %q, %v; want the ended %q", latest, err, first.ID())
	}
	if err := second.End(errors.New("stopped")); err != nil {
		t.Fatal(err)
	}
	if latest, err := d.Latest(); err != nil || latest != second.ID() {
		t.Errorf("Latest = %q, %v; want %q", latest, err, second.ID())
	}
}

// Open fails on a root that is no run directory, matching fs.ErrNotExist, and makes nothing.
func TestOpen_NoRunDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	writeFile(t, file, "x")
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, root := range []string{filepath.Join(dir, "absent"), file, empty} {
		if _, err := rundir.Open(root); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%s) = %v, want an error matching fs.ErrNotExist", root, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open made the absent root: %v", err)
	}
	if entries, err := os.ReadDir(empty); err != nil || len(entries) != 0 {
		t.Errorf("Open wrote into an empty root: %v, %v", entries, err)
	}
}

// Open and Create refuse a run directory of another schema, or runs/ with no rundir.json, naming
// both versions, and Create makes nothing there.
func TestOpenCreate_RefuseOtherSchema(t *testing.T) {
	for name, setup := range map[string]func(root string){
		"other schema": func(root string) { writeFile(t, filepath.Join(root, "rundir.json"), `{"schema": "3"}`) },
		"schema 1":     func(root string) { writeFile(t, filepath.Join(root, "runs", "r", "manifest.json"), `{"schema": "1"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			setup(root)

			for call, opener := range map[string]func(string) (*rundir.Dir, error){"Open": rundir.Open, "Create": rundir.Create} {
				_, err := opener(root)
				if err == nil || errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), `"2"`) ||
					!strings.Contains(err.Error(), `"1"`) && !strings.Contains(err.Error(), `"3"`) {
					t.Errorf("%s = %v, want a refusal naming both versions", call, err)
				}
			}
			for _, made := range []string{"index", "content"} {
				if _, err := os.Stat(filepath.Join(root, made)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("a refused Create made %s: %v", made, err)
				}
			}
		})
	}
}

// Create makes the directory, rundir.json, the index, and the content store, and opens the run
// directory it made again.
func TestCreate_Makes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "out")
	d, err := rundir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, "rundir.json")); got != "{\n  \"schema\": \"2\"\n}\n" {
		t.Errorf("rundir.json = %q", got)
	}
	for _, made := range []string{"index", "content"} {
		if info, err := os.Stat(filepath.Join(root, made)); err != nil || !info.IsDir() {
			t.Errorf("Create made no %s: %v", made, err)
		}
	}
	if d.Index() == nil {
		t.Error("Index = nil, want the run directory's FileIndex")
	}
	r := begin(t, d)
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}

	again, err := rundir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := again.Runs(); err != nil || !slices.Equal(ids, []string{r.ID()}) {
		t.Errorf("Runs after Create again = %v, %v; want the earlier run", ids, err)
	}
}

// A Dir from Open writes nothing: its content refuses a put, it has no index, and Begin and
// Quarantine refuse; with its content directory gone, a content misses and nothing is made.
func TestOpen_ReadsAlone(t *testing.T) {
	ctx := context.Background()
	_, root := create(t)
	if err := os.Remove(filepath.Join(root, "content")); err != nil {
		t.Fatal(err)
	}
	d := open(t, root)
	sha := strings.Repeat("cd", 32)

	if d.Index() != nil {
		t.Error("Index of a reader isn't nil")
	}
	if err := d.Content().Put(ctx, sha, strings.NewReader("x")); err == nil {
		t.Error("Content().Put = nil, want a refusal")
	}
	if _, err := d.Content().Open(ctx, sha); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Content().Open = %v, want an error matching fs.ErrNotExist", err)
	}
	if _, err := d.Content().Open(ctx, "../x"); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("Content().Open(../x) = %v, want an error matching dispatch.ErrRequest", err)
	}
	if _, err := d.Begin(schema.Manifest{}); err == nil {
		t.Error("Begin on a reader = nil, want a refusal")
	}
	if _, err := d.Quarantine(sha, filepath.Join(root, "rundir.json")); err == nil {
		t.Error("Quarantine on a reader = nil, want a refusal")
	}
	for _, made := range []string{"content", "runs", "quarantine"} {
		if _, err := os.Stat(filepath.Join(root, made)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a reader made %s: %v", made, err)
		}
	}
}

// Begin removes the work directories a killed run left, and nothing else.
func TestBegin_RemovesWork(t *testing.T) {
	d, root := create(t)
	writeFile(t, filepath.Join(root, ".work-123", "part", "a.bin"), "left")
	writeFile(t, filepath.Join(root, ".work-456"), "left")
	writeFile(t, filepath.Join(root, "work-kept", "a"), "kept")

	begin(t, d)

	matches, err := filepath.Glob(filepath.Join(root, ".work-*"))
	if err != nil || len(matches) != 0 {
		t.Errorf("work left after Begin: %v, %v", matches, err)
	}
	if got := readFile(t, filepath.Join(root, "work-kept", "a")); got != "kept" {
		t.Errorf("Begin removed work-kept: %q", got)
	}
}

// Begin names the run with a new version 7 id when the manifest has none, starts it now, in UTC,
// and writes its manifest with no end; it refuses an id that isn't one, or a run's it holds.
func TestBegin_Manifest(t *testing.T) {
	d, _ := create(t)
	before := time.Now()

	r, err := d.Begin(schema.Manifest{Inputs: []string{"/docs"}, End: schema.TimeOf(before), Error: "old"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := d.Manifest(r.ID())
	if err != nil {
		t.Fatal(err)
	}
	if u, err := uuid.Parse(r.ID()); err != nil || u.String() != r.ID() || r.ID()[14] != '7' {
		t.Errorf("ID = %q, want a version 7 UUID", r.ID())
	}
	if m.Schema != schema.Version || m.Run != r.ID() || !m.End.IsZero() || m.Error != "" {
		t.Errorf("Manifest = %+v, want schema 2, its id, no end, no error", m)
	}
	if start := m.Start.Time(); start.Location() != time.UTC || start.Before(before) || start.After(time.Now()) {
		t.Errorf("Start = %v, want now, in UTC", start)
	}

	if _, err := d.Begin(schema.Manifest{Run: r.ID()}); err == nil {
		t.Error("Begin over a run's id = nil, want a refusal")
	}
	for _, id := range []string{"..", "run", uuid.NewV4().String(), strings.ToUpper(uuid.NewV7().String())} {
		if _, err := d.Begin(schema.Manifest{Run: id}); !errors.Is(err, dispatch.ErrRequest) {
			t.Errorf("Begin(%q) = %v, want an error matching dispatch.ErrRequest", id, err)
		}
	}
}

// A line written is in the record before the run ends: a reader of the running run has it, and
// is told the run has not ended.
func TestWrite_WrittenThrough(t *testing.T) {
	d, root := create(t)
	r := begin(t, d)
	e := schema.Event{Kind: schema.ErrorEvent, Input: "/docs/<a>&b", Error: "gone"}

	if err := r.Write(e); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(root, "runs", r.ID(), "record.jsonl")); got != `{"event":"error","input":"/docs/<a>&b","error":"gone"}`+"\n" {
		t.Errorf("record = %q, want the line, HTML unescaped", got)
	}
	evs, errs := records(open(t, root), r.ID())
	if len(evs) != 1 || evs[0].Input != e.Input || len(errs) != 1 || !errors.Is(errs[0], rundir.ErrNotEnded) {
		t.Errorf("Record of the running run = %+v, %v; want the line, then ErrNotEnded", evs, errs)
	}
}

// End records the run's end and the error that stopped it, if any.
func TestEnd(t *testing.T) {
	d, _ := create(t)
	for _, stop := range []error{nil, errors.New("store: disk full")} {
		r := begin(t, d)
		before := time.Now()

		if err := r.End(stop); err != nil {
			t.Fatal(err)
		}

		m, err := d.Manifest(r.ID())
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if stop != nil {
			want = stop.Error()
		}
		if end := m.End.Time(); end.Before(before) || end.After(time.Now()) || m.Error != want {
			t.Errorf("Manifest end %v, error %q; want now and %q", end, m.Error, want)
		}
		if err := r.Write(schema.Event{}); err == nil {
			t.Error("Write after End = nil, want an error")
		}
	}
}

// Quarantine keeps a part's file once, named by its SHA-256.
func TestQuarantine_Once(t *testing.T) {
	d, root := create(t)
	sha := strings.Repeat("ef", 32)
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	writeFile(t, first, "first")
	writeFile(t, second, "second")

	for _, src := range []string{first, second} {
		kept, err := d.Quarantine(sha, src)
		if err != nil || kept != "quarantine/"+sha {
			t.Errorf("Quarantine = %q, %v; want quarantine/%s", kept, err, sha)
		}
	}
	if got := readFile(t, filepath.Join(root, "quarantine", sha)); got != "first" {
		t.Errorf("kept %q, want the first file", got)
	}
	if _, err := d.Quarantine("../"+sha[3:], first); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("Quarantine of a bad SHA-256 = %v, want an error matching dispatch.ErrRequest", err)
	}
	if _, err := d.Quarantine(strings.Repeat("01", 32), filepath.Join(root, "absent")); err == nil {
		t.Error("Quarantine of an absent file = nil, want an error")
	}
}

// Kept finds a part's kept file once Quarantine has kept it, on the writer and on a reader of the
// same directory, and finds none, making nothing, before.
func TestKept(t *testing.T) {
	d, root := create(t)
	sha := strings.Repeat("ab", 32)
	src := filepath.Join(t.TempDir(), "part")
	writeFile(t, src, "part")

	if name, ok, err := d.Kept(sha); name != "" || ok || err != nil {
		t.Errorf("Kept before Quarantine = %q, %t, %v; want \"\", false, nil", name, ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, "quarantine")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Kept made quarantine/: %v", err)
	}
	if _, err := d.Quarantine(sha, src); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]*rundir.Dir{"writer": d, "reader": open(t, root)} {
		if kept, ok, err := dir.Kept(sha); kept != "quarantine/"+sha || !ok || err != nil {
			t.Errorf("%s's Kept = %q, %t, %v; want quarantine/%s, true, nil", name, kept, ok, err, sha)
		}
		if _, _, err := dir.Kept("../" + sha[3:]); !errors.Is(err, dispatch.ErrRequest) {
			t.Errorf("%s's Kept of a bad SHA-256 = %v, want an error matching dispatch.ErrRequest", name, err)
		}
	}
}
