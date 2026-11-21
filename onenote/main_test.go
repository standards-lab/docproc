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

const standinLines = `{"line":"notebook","schema":"2","input":"section","name":"Notes"}
{"line":"section","id":"07e1868e-e7d0-c24e-85c6-7cbe04486764","path":["Notes"]}
{"line":"page","id":"3fdc5978-8647-8e4c-85de-356300ee58ab","section":"07e1868e-e7d0-c24e-85c6-7cbe04486764","level":1,"title":"Hello","modification_time":"2026-01-12T14:15:00Z","markdown":"# Hello"}
`

const errorPrefix = "docproc-onenote: error: "

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"docproc-onenote": standin,
		"docker":          dockerStandin,
	})
}

type onenoteSettings struct {
	Version string   `json:"version"`
	Schema  string   `json:"schema"`
	Stdout  *string  `json:"stdout"`
	Stderr  string   `json:"stderr"`
	Exit    int      `json:"exit"`
	Sleep   bool     `json:"sleep"`
	Media   []string `json:"media"`
	Replay  string   `json:"replay"`
}

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

func dockerStandin(r tooltest.Run) int {
	switch args := r.Args; {
	case len(args) > 0 && args[0] == "version":
		fmt.Println("27.0.0")
	case len(args) > 0 && args[0] == "run":
		fmt.Printf("%s\nschema 2\n", onenote.OneNoteMinimum)
	}
	return 0
}

type onenoteStandin struct {
	t        testing.TB
	link     string
	settings onenoteSettings
}

func installStandin(t testing.TB, version string) *onenoteStandin {
	t.Helper()
	m := &onenoteStandin{t: t, settings: onenoteSettings{Version: version}}
	m.link = tooltest.Link(t, t.TempDir(), "docproc-onenote", m.settings)
	return m
}

func (m *onenoteStandin) spec() tool.Spec {
	spec := onenote.OneNoteSpec()
	spec.Program = m.link
	return spec
}

func (m *onenoteStandin) set(f func(*onenoteSettings)) {
	m.t.Helper()
	f(&m.settings)
	tooltest.Set(m.t, m.link, m.settings)
}

func (m *onenoteStandin) stdout(s string) { m.set(func(ms *onenoteSettings) { ms.Stdout = &s }) }

func (m *onenoteStandin) stderr(s string) { m.set(func(ms *onenoteSettings) { ms.Stderr = s }) }

func (m *onenoteStandin) exit(code int) { m.set(func(ms *onenoteSettings) { ms.Exit = code }) }

func (m *onenoteStandin) sleep() { m.set(func(ms *onenoteSettings) { ms.Sleep = true }) }

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
