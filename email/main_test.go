//go:build linux

package email_test

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

// standinLines is the stand-in docproc-mail's output when its settings give no Stdout: a .msg's
// store line and its message.
const standinLines = `{"line":"store","schema":"2","format":"msg"}
{"line":"message","class":"IPM.Note","subject":"Hello"}
`

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"docproc-mail": standin,
		"docker":       dockerStandin,
	})
}

// mailSettings are the stand-in docproc-mail's settings, which its link names.
type mailSettings struct {
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
	// Replay, when set, is a golden of docproc-mail's, a .jsonl file: a run writes it to stdout
	// and the .stderr beside it to stderr, and exits with the code the .exit beside it holds, 0
	// without one, as docproc-mail did over the fixture.
	Replay string `json:"replay"`
}

// standin is the stand-in docproc-mail. With --version alone it prints its settings' version, then
// `schema 2`, or their Schema, as docproc-mail does. Otherwise it writes their Media into the
// directory after --media, if any; then replays their Replay when it is set; or else writes their
// Stdout, or standinLines, to stdout, sleeps when they say to, writes their Stderr to stderr, and
// exits with their Exit. It reads no input.
func standin(r tooltest.Run) int {
	var s mailSettings
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
		fmt.Fprintln(os.Stderr, "docproc-mail: error: no input")
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

// replayRun writes the golden jsonl to stdout and the .stderr beside it to stderr, and returns
// the exit code the .exit beside it holds, 0 when there is none.
func replayRun(jsonl string) int {
	stem := strings.TrimSuffix(jsonl, ".jsonl")
	stdout, err := os.ReadFile(jsonl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	stderr, err := os.ReadFile(stem + ".stderr")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	code := 0
	if exit, err := os.ReadFile(stem + ".exit"); err == nil {
		if code, err = strconv.Atoi(strings.TrimSpace(string(exit))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := os.Stdout.Write(stdout); err != nil {
		return 1
	}
	_, _ = os.Stderr.Write(stderr)
	return code
}

// dockerStandin is a stand-in docker CLI enough to build a container Tool: its daemon's version,
// every image present, and a run that prints a docproc-mail version.
func dockerStandin(r tooltest.Run) int {
	switch args := r.Args; {
	case len(args) > 0 && args[0] == "version":
		fmt.Println("27.0.0")
	case len(args) > 0 && args[0] == "run":
		fmt.Printf("%s\nschema 2\n", email.MailMinimum)
	}
	return 0
}

// mailStandin is a test's stand-in docproc-mail: its link and the settings last written to it.
type mailStandin struct {
	t        testing.TB
	link     string
	settings mailSettings
}

// installStandin links the stand-in as docproc-mail in a directory of its own, reporting version.
func installStandin(t testing.TB, version string) *mailStandin {
	t.Helper()
	m := &mailStandin{t: t, settings: mailSettings{Version: version}}
	m.link = tooltest.Link(t, t.TempDir(), "docproc-mail", m.settings)
	return m
}

// spec returns MailSpec with the stand-in's link as its program.
func (m *mailStandin) spec() tool.Spec {
	spec := email.MailSpec()
	spec.Program = m.link
	return spec
}

// set changes the stand-in's settings through f.
func (m *mailStandin) set(f func(*mailSettings)) {
	m.t.Helper()
	f(&m.settings)
	tooltest.Set(m.t, m.link, m.settings)
}

// stdout sets what a run writes to stdout.
func (m *mailStandin) stdout(s string) { m.set(func(ms *mailSettings) { ms.Stdout = &s }) }

// stderr sets what a run writes to stderr.
func (m *mailStandin) stderr(s string) { m.set(func(ms *mailSettings) { ms.Stderr = s }) }

// exit sets a run's exit code.
func (m *mailStandin) exit(code int) { m.set(func(ms *mailSettings) { ms.Exit = code }) }

// sleep makes a run sleep a minute after its stdout.
func (m *mailStandin) sleep() { m.set(func(ms *mailSettings) { ms.Sleep = true }) }

// runs returns the arguments of every run of the stand-in, in order, the version commands left
// out.
func (m *mailStandin) runs() [][]string {
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

// standinReader builds a Reader over the stand-in docproc-mail at MailMinimum, built from
// MailSpec through the exec runner, and returns it with the stand-in.
func standinReader(t testing.TB) (*email.Reader, *mailStandin) {
	t.Helper()
	m := installStandin(t, email.MailMinimum)
	tl, err := tool.NewExec(t.Context(), m.spec())
	if err != nil {
		t.Fatal(err)
	}
	r, err := email.NewReader(email.Config{Mail: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r, m
}

// collect ranges over seq, returning the records yielded and the error, if any, that ended
// them.
func collect(seq iter.Seq2[email.Record, error]) ([]email.Record, error) {
	var recs []email.Record
	for rec, err := range seq {
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}
