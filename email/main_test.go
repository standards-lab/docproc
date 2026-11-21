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

const standinLines = `{"line":"store","schema":"2","format":"msg"}
{"line":"message","class":"IPM.Note","subject":"Hello"}
`

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"docproc-mail": standin,
		"docker":       dockerStandin,
	})
}

type mailSettings struct {
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

func dockerStandin(r tooltest.Run) int {
	switch args := r.Args; {
	case len(args) > 0 && args[0] == "version":
		fmt.Println("27.0.0")
	case len(args) > 0 && args[0] == "run":
		fmt.Printf("%s\nschema 2\n", email.MailMinimum)
	}
	return 0
}

type mailStandin struct {
	t        testing.TB
	link     string
	settings mailSettings
}

func installStandin(t testing.TB, version string) *mailStandin {
	t.Helper()
	m := &mailStandin{t: t, settings: mailSettings{Version: version}}
	m.link = tooltest.Link(t, t.TempDir(), "docproc-mail", m.settings)
	return m
}

func (m *mailStandin) spec() tool.Spec {
	spec := email.MailSpec()
	spec.Program = m.link
	return spec
}

func (m *mailStandin) set(f func(*mailSettings)) {
	m.t.Helper()
	f(&m.settings)
	tooltest.Set(m.t, m.link, m.settings)
}

func (m *mailStandin) stdout(s string) { m.set(func(ms *mailSettings) { ms.Stdout = &s }) }

func (m *mailStandin) stderr(s string) { m.set(func(ms *mailSettings) { ms.Stderr = s }) }

func (m *mailStandin) exit(code int) { m.set(func(ms *mailSettings) { ms.Exit = code }) }

func (m *mailStandin) sleep() { m.set(func(ms *mailSettings) { ms.Sleep = true }) }

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
