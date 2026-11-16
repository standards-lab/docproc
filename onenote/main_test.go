//go:build linux

package onenote_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/tool"
)

// standinLines is the stand-in onenote's output when its settings give no Stdout: a section
// input's notebook line, its section, and one page.
const standinLines = `{"line":"notebook","schema":"1","input":"section","name":"Notes"}
{"line":"section","id":"07e1868e-e7d0-c24e-85c6-7cbe04486764","path":["Notes"],"display_name":"Notes"}
{"line":"page","id":"3fdc5978-8647-8e4c-85de-356300ee58ab","section":"07e1868e-e7d0-c24e-85c6-7cbe04486764","level":1,"title":"Hello","markdown":"# Hello"}
`

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"spike-ai-pipeline-onenote": standin,
	})
}

// onenoteSettings are the stand-in onenote's settings, which its link names.
type onenoteSettings struct {
	// Version is what its --version prints.
	Version string `json:"version"`
	// Stdout, when not nil, is what a run writes to stdout in place of standinLines.
	Stdout *string `json:"stdout"`
	// Stderr is what a run writes to stderr, after its stdout, each {path} in it the input's
	// path, its last argument, as the run was given it.
	Stderr string `json:"stderr"`
	// Exit is a run's exit code.
	Exit int `json:"exit"`
	// Sleep makes a run sleep a minute after writing its stdout, for a test to cancel it or stop
	// reading.
	Sleep bool `json:"sleep"`
}

// standin is the stand-in onenote. With --version alone it prints its settings' version alone, as
// onenote does. Otherwise it writes its settings' Stdout, or standinLines, to stdout, sleeps when
// they say to, writes their Stderr to stderr, and exits with their Exit. It reads no input.
func standin(r tooltest.Run) int {
	var s onenoteSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(s.Version)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "stand-in: no input")
		return 2
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
	fmt.Fprint(os.Stderr, strings.ReplaceAll(s.Stderr, "{path}", args[len(args)-1]))
	return s.Exit
}

// onenoteStandin is a test's stand-in onenote: its link and the settings last written to it.
type onenoteStandin struct {
	t        testing.TB
	link     string
	settings onenoteSettings
}

// installStandin links the stand-in onenote as spike-ai-pipeline-onenote in a directory of its own,
// reporting version.
func installStandin(t *testing.T, version string) *onenoteStandin {
	t.Helper()
	m := &onenoteStandin{t: t, settings: onenoteSettings{Version: version}}
	m.link = tooltest.Link(t, t.TempDir(), "spike-ai-pipeline-onenote", m.settings)
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

// standinReader builds a Reader over the stand-in onenote at 0.1.0, built from OneNoteSpec through the
// exec runner, and returns it with the stand-in.
func standinReader(t *testing.T) (*onenote.Reader, *onenoteStandin) {
	t.Helper()
	m := installStandin(t, "0.1.0")
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

// mustRead returns a function that returns Read's Stream, given Read's results, failing the test
// if Read refused the request: mustRead(t)(r.Read(ctx, req)).
func mustRead(t *testing.T) func(*onenote.Stream, error) *onenote.Stream {
	return func(s *onenote.Stream, err error) *onenote.Stream {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}

// collect ranges over s's records, returning those yielded and the error, if any, that ended
// them. It takes Read's results as they are: err, Read's refusal, is returned at once.
func collect(s *onenote.Stream, err error) ([]onenote.Record, error) {
	if err != nil {
		return nil, err
	}
	var recs []onenote.Record
	for rec, err := range s.Records() {
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}
