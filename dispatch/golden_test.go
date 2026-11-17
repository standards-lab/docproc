package dispatch_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/internal/goldentest"
	"github.com/standards-lab/docproc/route"
)

// TestGoldens: dispatch's shapes as JSON on encoding/json, one line each, compared as text with
// the committed goldens: testdata/dispatch.jsonl, every event of a dispatch over the fixture stage
// whose found parts are routed and flagged, and testdata/log.jsonl, the input's entry and its log
// as the index reads them back. With goldentest.RecordEnv set to 1, the goldens are written.
func TestGoldens(t *testing.T) {
	world := map[string]string{
		// A PDF's signature under a picture's extension: routed by its signature and flagged.
		"scan.png": "%PDF-1.7\n%%EOF\n",
		// An executable's name over text of no signature: unrouted, its name flagged.
		"notes.txt.exe": "x",
		"b.txt":         "record {\"a\":1}\nfile c.txt",
		"c.txt":         "text",
	}
	index := dispatch.NewMemoryIndex(0)
	stage := dispatchtest.NewStage(world)
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: dispatch.Stages{route.Text: stage}, Index: index, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	path := input(t, "a.txt", "file scan.png\nfile notes.txt.exe\nfile b.txt")
	var events bytes.Buffer
	var last dispatch.Event
	for ev, err := range d.Dispatch(t.Context(), dispatch.Input{Path: path, MIME: "text/plain"}) {
		if err != nil {
			t.Fatal(err)
		}
		line(t, &events, ev)
		last = ev
	}
	golden(t, "dispatch.jsonl", events.Bytes())

	key := dispatch.Key{SHA256: last.Part.SHA256, Stage: stage.Identity()}
	e, ok, err := index.Lookup(t.Context(), key)
	if err != nil || !ok {
		t.Fatalf("the input's entry: %v, %v", ok, err)
	}
	var log bytes.Buffer
	line(t, &log, e)
	for _, r := range logged(t, index, key) {
		line(t, &log, r)
	}
	golden(t, "log.jsonl", log.Bytes())
}

// line appends v's JSON to b as one line.
func line(t *testing.T, b *bytes.Buffer, v any) {
	t.Helper()
	j, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	b.Write(append(j, '\n'))
}

// golden compares got with testdata/name as text, or writes it there under goldentest.RecordEnv.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv(goldentest.RecordEnv) == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from its golden:\n got %s\nwant %s", name, got, want)
	}
}
