//go:build linux

package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/tool"
)

func TestRunTmpdir(t *testing.T) {
	link := installStandin(t)
	dir, reports, host := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", host)
	spec := standinSpec(link, "1.0", "1.0")
	spec.KillAfter = 0
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, inv tool.Invocation) error { return tl.Run(t.Context(), inv) }
	tests := []struct {
		label string
		cmd   []string
		run   func(t *testing.T, inv tool.Invocation) error
		want  func(err error) bool
	}{
		{"exit", []string{"exit", "0"}, run, func(err error) bool { return err == nil }},
		{"failure", []string{"exit", "3"}, run, func(err error) bool {
			exitErr, ok := errors.AsType[*tool.ExitError](err)
			return ok && exitErr.Code == 3
		}},
		{"cancel", []string{"sleep", "1m"}, func(t *testing.T, inv tool.Invocation) error {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := newReadyWriter()
			inv.Stdout = ready
			done := runAsync(ctx, tl, inv)
			ready.wait(t)
			cancel()
			return <-done
		}, func(err error) bool { return errors.Is(err, context.Canceled) }},
		{"bound", []string{"stubborn", "1m"}, func(t *testing.T, inv tool.Invocation) error {
			inv.Timeout = 2 * time.Second
			return tl.Run(t.Context(), inv)
		}, func(err error) bool {
			exitErr, ok := errors.AsType[*tool.ExitError](err)
			return ok && exitErr.TimedOut && exitErr.Signal == "SIGKILL"
		}},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		report := filepath.Join(reports, tt.label+".json")
		err := tt.run(t, tool.Invocation{Args: append([]string{"tmp", report}, tt.cmd...), Dir: dir})
		if !tt.want(err) {
			t.Errorf("%s: Run error = %v", tt.label, err)
		}
		b, err := os.ReadFile(report)
		if err != nil {
			t.Fatalf("%s: the stand-in's report: %v", tt.label, err)
		}
		var got tmpdir
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: decoding %q: %v", tt.label, b, err)
		}
		if filepath.Dir(got.TMPDIR) != host || !strings.HasPrefix(filepath.Base(got.TMPDIR), "docproc-run-") {
			t.Errorf("%s: TMPDIR = %s, want docproc-run-* under the test's TMPDIR %s", tt.label, got.TMPDIR, host)
		}
		if len(got.Entries) != 0 {
			t.Errorf("%s: TMPDIR held %q as the program started, want it empty", tt.label, got.Entries)
		}
		if prev, ok := seen[got.TMPDIR]; ok {
			t.Errorf("%s: TMPDIR %s is the %s run's too, want one of its own", tt.label, got.TMPDIR, prev)
		}
		seen[got.TMPDIR] = tt.label
		if got.HOME != dir {
			t.Errorf("%s: HOME = %s, want the run's Dir %s", tt.label, got.HOME, dir)
		}
		if _, err := os.Stat(got.TMPDIR); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: TMPDIR %s is left once Run returned: %v", tt.label, got.TMPDIR, err)
		}
		for name, d := range map[string]string{"the run's Dir": dir, "the test's TMPDIR": host} {
			if entries, err := os.ReadDir(d); err != nil || len(entries) != 0 {
				t.Errorf("%s: %s holds %v, %v; want it empty", tt.label, name, entries, err)
			}
		}
	}

	t.Setenv("TMPDIR", filepath.Join(host, "missing"))
	report := filepath.Join(reports, "missing.json")
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"tmp", report, "exit", "0"}, Dir: dir})
	if _, ok := errors.AsType[*tool.ExitError](err); ok || err == nil || !strings.HasPrefix(err.Error(), "tool: standin: temporary directory: ") {
		t.Errorf("a run with no TMPDIR to make: %v, want the run's failure over its temporary directory", err)
	}
	if _, err := os.Stat(report); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a run with no TMPDIR to make started the program: %v", err)
	}
}
