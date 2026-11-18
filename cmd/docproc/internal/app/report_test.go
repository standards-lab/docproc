package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docproc/cmd/docproc/internal/apptest"
)

// reportCmd runs docproc report over args with an unreadable lifecycle configuration and every
// bound unset, so a run that read any configuration or built any node would fail, and fails the
// test when the run built a node.
func reportCmd(t *testing.T, args ...string) result {
	t.Helper()
	clearBounds(t)
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, stdout, stderr, built := newApp(t)
	code := a.Run(context.Background(), append([]string{"report"}, args...))
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
	return result{code, stdout.String(), stderr.String()}
}

// reportOK runs docproc report as reportCmd does and fails the test unless it exits 0.
func reportOK(t *testing.T, args ...string) result {
	t.Helper()
	r := reportCmd(t, args...)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitOK, r.stderr)
	}
	return r
}

// indexOf returns the absolute path of the index page of the run id in the run directory out.
func indexOf(t *testing.T, out, id string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(out, "runs", id, "report", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// readPage returns the page at path, failing the test when it can't be read.
func readPage(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// unendedRun begins a run in the run directory out whose dispatch of in stopped with an error
// line, and leaves it unended, its record torn after that line, as a killed run leaves it. It
// returns the run's id.
func unendedRun(t *testing.T, out, in string) string {
	t.Helper()
	d, err := rundir.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Begin(schema.Manifest{Workers: 1, Inputs: []string{in}, Collected: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Write(schema.Event{Kind: schema.ErrorEvent, Input: in, Error: "killed"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(out, "runs", run.ID(), "record.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(`{"kind":"part","input":"/torn`); err != nil {
		t.Fatal(err)
	}
	return run.ID()
}

// By default, docproc report renders the latest run that ended, passing over a later one that
// never ended, prints its index's path, and builds nothing: it reads no configuration and looks
// up no tool.
func TestReport_LatestEnded(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n")
	out := filepath.Join(t.TempDir(), "out")
	runOK(t, apptest.NoTools, in, "--out", out)
	runOK(t, apptest.NoTools, in, "--out", out)
	unendedRun(t, out, filepath.Join(in, "a.txt"))
	ids := runIDs(t, out)

	r := reportOK(t, out)

	index := indexOf(t, out, ids[1])
	if r.stdout != index+"\n" {
		t.Errorf("stdout = %q, want the index's path %q", r.stdout, index)
	}
	if page := readPage(t, index); !strings.Contains(page, ids[1]) || strings.Contains(page, "not ended") {
		t.Errorf("the index doesn't show the latest ended run, %s", ids[1])
	}
	for _, id := range []string{ids[0], ids[2]} {
		if _, err := os.Stat(filepath.Dir(indexOf(t, out, id))); !os.IsNotExist(err) {
			t.Errorf("run %s's report directory: %v, want none", id, err)
		}
	}
}

// runIDs returns the ids of the runs of the run directory out, in the order they began.
func runIDs(t *testing.T, out string) []string {
	t.Helper()
	d, err := rundir.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := d.Runs()
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// --run renders the run it names, whichever run is the latest.
func TestReport_NamedRun(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n")
	out := filepath.Join(t.TempDir(), "out")
	runOK(t, apptest.NoTools, in, "--out", out)
	runOK(t, apptest.NoTools, in, "--out", out)
	first := runIDs(t, out)[0]

	r := reportOK(t, out, "--run", first)

	index := indexOf(t, out, first)
	if r.stdout != index+"\n" {
		t.Errorf("stdout = %q, want the index's path %q", r.stdout, index)
	}
	if page := readPage(t, index); !strings.Contains(page, first) {
		t.Errorf("the index doesn't show the named run, %s", first)
	}
}

// A named run that never ended renders its whole lines, its torn last line dropped, marked not
// ended, and exits 0.
func TestReport_UnendedRun(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	id := unendedRun(t, out, "/docs/killed.txt")

	r := reportOK(t, out, "--run", id)

	page := readPage(t, strings.TrimSuffix(r.stdout, "\n"))
	if !strings.Contains(page, "not ended") {
		t.Error("the unended run's index doesn't say it has not ended")
	}
	if !strings.Contains(page, "/docs/killed.txt") || strings.Contains(page, "/torn") {
		t.Error("the unended run's index doesn't list its whole line's input alone")
	}
}

// A report replaces the run's earlier pages whole.
func TestReport_ReplacesPages(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n")
	out := filepath.Join(t.TempDir(), "out")
	runOK(t, apptest.NoTools, in, "--out", out)
	reportOK(t, out)
	index := indexOf(t, out, runIDs(t, out)[0])
	stray := filepath.Join(filepath.Dir(index), "stray.html")
	if err := os.WriteFile(stray, []byte("stray"), 0o644); err != nil {
		t.Fatal(err)
	}

	reportOK(t, out)

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("an earlier render's file: %v, want it gone", err)
	}
	if _, err := os.Stat(index); err != nil {
		t.Errorf("the index: %v", err)
	}
}

// A run directory that doesn't exist, a --run that isn't one of its runs, or no --run over a
// run directory with no run that ended, exits 1, the error naming it, and writes nothing.
func TestReport_Refusals(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	unended := filepath.Join(root, "unended")
	unendedRun(t, unended, "/docs/a.txt")
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"no directory", []string{missing}, []string{missing}},
		{"no run directory", []string{root}, []string{root, "no run directory"}},
		{"no such run", []string{unended, "--run", "019a5f3c-1d2e-7a10-8b00-0000000000ff"}, []string{`"019a5f3c-1d2e-7a10-8b00-0000000000ff" is no run of ` + unended}},
		{"no run id", []string{unended, "--run", "nope"}, []string{`"nope" is no run of ` + unended}},
		{"no ended run", []string{unended}, []string{unended, "no run that ended", "--run"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := reportCmd(t, tc.args...)
			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if !strings.HasPrefix(r.stderr, "docproc report: ") {
				t.Errorf("stderr = %q, want docproc report's error", r.stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr = %q, want it to name %q", r.stderr, want)
				}
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
		})
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("the missing run directory: %v, want it not made", err)
	}
	if reports, _ := filepath.Glob(filepath.Join(unended, "runs", "*", "report")); len(reports) != 0 {
		t.Errorf("report directories %q, want none", reports)
	}
}

// A usage error exits 2 with docproc report's usage, and so does its help, which states what
// each exit status means.
func TestReportCommand_Usage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no run directory", nil, "accepts 1 argument, got 0"},
		{"two run directories", []string{out, out}, "accepts 1 argument, got 2"},
		{"empty run", []string{out, "--run", ""}, "--run names no run"},
		{"unknown flag", []string{out, "--out", out}, "out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := reportCmd(t, tc.args...)
			if r.code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitUsage, r.stderr)
			}
			if !strings.HasPrefix(r.stderr, "docproc report: ") || !strings.Contains(r.stderr, tc.want) {
				t.Errorf("stderr = %q, want docproc report's usage error naming %q", r.stderr, tc.want)
			}
		})
	}

	r := reportCmd(t, "--help")
	if r.code != process.ExitUsage {
		t.Errorf("help: code = %d, want %d", r.code, process.ExitUsage)
	}
	for _, want := range []string{"docproc report [flags] <run-dir>", "--run id", "Exit status:", "  0  the report was rendered", "never ended", "  1  nothing was rendered", "  2  a usage error"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, r.stdout)
		}
	}
}
