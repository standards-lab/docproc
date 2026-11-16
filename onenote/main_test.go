//go:build linux

package onenote_test

import (
	"bytes"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/tool"
)

// standinLines is the stand-in docproc-onenote's output when its settings give no Stdout: a
// section input's notebook line, its section, and one page.
const standinLines = `{"line":"notebook","schema":"2","input":"section","name":"Notes"}
{"line":"section","id":"07e1868e-e7d0-c24e-85c6-7cbe04486764","path":["Notes"]}
{"line":"page","id":"3fdc5978-8647-8e4c-85de-356300ee58ab","section":"07e1868e-e7d0-c24e-85c6-7cbe04486764","level":1,"title":"Hello","modification_time":"2026-01-12T14:15:00Z","markdown":"# Hello"}
`

// errorPrefix begins docproc-onenote's error line on stderr.
const errorPrefix = "docproc-onenote: error: "

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"docproc-onenote": standin,
		"docker":          dockerStandin,
	})
}

// onenoteSettings are the stand-in docproc-onenote's settings, which its link names.
type onenoteSettings struct {
	// Version is what its --version prints on its first line.
	Version string `json:"version"`
	// Schema, when set, is the schema its --version prints in place of 2.
	Schema string `json:"schema"`
	// Stdout, when not nil, is what a run writes to stdout in place of standinLines.
	Stdout *string `json:"stdout"`
	// Stderr is what a run writes to stderr, after its stdout.
	Stderr string `json:"stderr"`
	// Exit is a run's exit code.
	Exit int `json:"exit"`
	// Sleep makes a run sleep a minute after writing its stdout, for a test to cancel it or stop
	// reading.
	Sleep bool `json:"sleep"`
	// Media names files a run writes into its --media directory, each holding its own name.
	Media []string `json:"media"`
	// Replay, when set, is a golden of docproc-onenote's, a .jsonl file: a run writes it to stdout
	// and the .stderr beside it to stderr, and exits 3 when that holds an error line, else 0, as
	// docproc-onenote did over the fixture.
	Replay string `json:"replay"`
}

// standin is the stand-in docproc-onenote. With --version alone it prints its settings' version,
// then `schema 2`, or their Schema, as docproc-onenote does. Otherwise it writes their Media into
// the directory after --media, if any; then replays their Replay when it is set; or else writes
// their Stdout, or standinLines, to stdout, sleeps when they say to, writes their Stderr to
// stderr, and exits with their Exit. It reads no input.
func standin(r tooltest.Run) int {
	var s onenoteSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 1 && args[0] == "--version" {
		schema := s.Schema
		if schema == "" {
			schema = "2"
		}
		fmt.Printf("%s\nschema %s\n", s.Version, schema)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, errorPrefix+"no input")
		return 2
	}
	for i, arg := range args[:len(args)-1] {
		if arg != "--media" {
			continue
		}
		dir := args[i+1]
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, name := range s.Media {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}
	if s.Replay != "" {
		return replayRun(s.Replay)
	}
	out := standinLines
	if s.Stdout != nil {
		out = *s.Stdout
	}
	if _, err := os.Stdout.WriteString(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if s.Sleep {
		time.Sleep(time.Minute)
	}
	fmt.Fprint(os.Stderr, s.Stderr)
	return s.Exit
}

// replayRun writes the golden jsonl to stdout and the .stderr beside it to stderr, and returns 3
// when the .stderr holds an error line, else 0. docproc-onenote's goldens record no exit code: its
// golden test lists the two inputs it refuses, each exit 3, an encrypted section and a table of
// contents under --file-only, and every other golden is a read that exits 0 with warnings alone.
func replayRun(jsonl string) int {
	stdout, err := os.ReadFile(jsonl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	stderr, err := os.ReadFile(strings.TrimSuffix(jsonl, ".jsonl") + ".stderr")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := os.Stdout.Write(stdout); err != nil {
		return 1
	}
	_, _ = os.Stderr.Write(stderr)
	if bytes.Contains(stderr, []byte(errorPrefix)) {
		return 3
	}
	return 0
}

// dockerStandin is a stand-in docker CLI enough to build a container Tool: its daemon's version,
// every image present, and a run that prints a docproc-onenote version.
func dockerStandin(r tooltest.Run) int {
	switch args := r.Args; {
	case len(args) > 0 && args[0] == "version":
		fmt.Println("27.0.0")
	case len(args) > 0 && args[0] == "run":
		fmt.Printf("%s\nschema 2\n", onenote.OneNoteMinimum)
	}
	return 0
}

// onenoteStandin is a test's stand-in docproc-onenote: its link and the settings last written to
// it.
type onenoteStandin struct {
	t        testing.TB
	link     string
	settings onenoteSettings
}

// installStandin links the stand-in as docproc-onenote in a directory of its own, reporting
// version.
func installStandin(t testing.TB, version string) *onenoteStandin {
	t.Helper()
	m := &onenoteStandin{t: t, settings: onenoteSettings{Version: version}}
	m.link = tooltest.Link(t, t.TempDir(), "docproc-onenote", m.settings)
	return m
}

// spec returns OneNoteSpec with the stand-in's link as its program.
func (m *onenoteStandin) spec() tool.Spec {
	spec := onenote.OneNoteSpec()
	spec.Program = m.link
	return spec
}

// set changes the stand-in's settings through f.
func (m *onenoteStandin) set(f func(*onenoteSettings)) {
	m.t.Helper()
	f(&m.settings)
	tooltest.Set(m.t, m.link, m.settings)
}

// stdout sets what a run writes to stdout.
func (m *onenoteStandin) stdout(s string) { m.set(func(ms *onenoteSettings) { ms.Stdout = &s }) }

// stderr sets what a run writes to stderr.
func (m *onenoteStandin) stderr(s string) { m.set(func(ms *onenoteSettings) { ms.Stderr = s }) }

// exit sets a run's exit code.
func (m *onenoteStandin) exit(code int) { m.set(func(ms *onenoteSettings) { ms.Exit = code }) }

// sleep makes a run sleep a minute after its stdout.
func (m *onenoteStandin) sleep() { m.set(func(ms *onenoteSettings) { ms.Sleep = true }) }

// runs returns the arguments of every run of the stand-in, in order, the version commands left
// out.
func (m *onenoteStandin) runs() [][]string {
	m.t.Helper()
	var runs [][]string
	for _, r := range tooltest.Runs(m.t, m.link) {
		if len(r.Args) == 1 && r.Args[0] == "--version" {
			continue
		}
		runs = append(runs, r.Args)
	}
	return runs
}

// standinReader builds a Reader over the stand-in docproc-onenote at OneNoteMinimum, built from
// OneNoteSpec through the exec runner, and returns it with the stand-in.
func standinReader(t testing.TB) (*onenote.Reader, *onenoteStandin) {
	t.Helper()
	m := installStandin(t, onenote.OneNoteMinimum)
	tl, err := tool.NewExec(t.Context(), m.spec())
	if err != nil {
		t.Fatal(err)
	}
	r, err := onenote.NewReader(onenote.Config{OneNote: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r, m
}

// collect ranges over seq, returning the records yielded and the error, if any, that ended
// them.
func collect(seq iter.Seq2[onenote.Record, error]) ([]onenote.Record, error) {
	var recs []onenote.Record
	for rec, err := range seq {
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}
