package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
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

// With --report, once the summary is printed, a run that ended renders its pages, as docproc
// report would, and prints its index's path last.
func TestRun_ReportAfterEndedRun(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n")
	out := filepath.Join(t.TempDir(), "out")

	r := runOK(t, apptest.NoTools, in, "--out", out, "--report")

	id := onlyRun(t, out).manifest.Run
	index := indexOf(t, out, id)
	if last := lastLine(r.stdout); last != index {
		t.Errorf("stdout's last line = %q, want the index's path %q", last, index)
	}
	wantLine(t, r.stdout, "run "+id)
	if page := readPage(t, index); !strings.Contains(page, id) || !strings.Contains(page, filepath.Join(in, "a.txt")) {
		t.Errorf("the index doesn't show run %s and its input", id)
	}
}

// With --report, a run a failure of the content store stopped still renders its pages, then
// exits 1 with the store's failure.
func TestRun_ReportAfterStoreFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory of mode 0555, so the store doesn't fail")
	}
	in := write(t, t.TempDir(), "hello.txt", helloText)
	out := filepath.Join(t.TempDir(), "out")
	if _, err := rundir.Create(out); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(out, "content")
	if err := os.Chmod(content, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(content, 0o755) })

	r := runCmd(t, context.Background(), func(a *app.App) { apptest.Tools(a, apptest.NoTools) }, in, "--out", out, "--report")

	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stdout = %q", r.code, process.ExitFailure, r.stdout)
	}
	if want := "docproc run: dispatch: content: "; !strings.HasPrefix(r.stderr, want) || strings.Contains(r.stderr, "docproc report") {
		t.Errorf("stderr = %q, want the store's failure alone", r.stderr)
	}
	index := indexOf(t, out, onlyRun(t, out).manifest.Run)
	if last := lastLine(r.stdout); last != index {
		t.Errorf("stdout's last line = %q, want the index's path %q", last, index)
	}
	if page := readPage(t, index); !strings.Contains(page, "dispatch: content: ") {
		t.Error("the index doesn't show the error that stopped the run")
	}
}

// With --report, a canceled run renders nothing.
func TestRun_ReportAfterCancellation(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n", "b.txt", "b\n")
	out := filepath.Join(t.TempDir(), "out")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := runCmd(t, ctx, func(a *app.App) {
		textStage(a, func(sctx context.Context, p dispatch.Part) error {
			cancel()
			<-sctx.Done()
			return sctx.Err()
		})
	}, in, "--out", out, "--workers", "1", "--report")

	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
	}
	run := onlyRun(t, out)
	if run.manifest.Error != context.Canceled.Error() {
		t.Errorf("manifest error %q, want %q", run.manifest.Error, context.Canceled)
	}
	if _, err := os.Stat(filepath.Dir(indexOf(t, out, run.manifest.Run))); !os.IsNotExist(err) {
		t.Errorf("the report directory: %v, want none", err)
	}
	if strings.Contains(r.stdout, "index.html") {
		t.Errorf("stdout = %q, want no index's path", r.stdout)
	}
}

// With --report, a render that fails after a run that ended exits 1, the error saying docproc
// report renders the pages again. The stand-in stage makes the run's report directory, during the
// run, a directory that can't be written, which a render can't move aside: on Linux, moving a
// directory to another parent needs write permission on it, for its "..".
func TestRun_ReportRenderFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root moves a directory of mode 0555, so the render doesn't fail")
	}
	in := write(t, t.TempDir(), "a.txt", "a\n")
	out := filepath.Join(t.TempDir(), "out")

	r := runCmd(t, context.Background(), func(a *app.App) {
		textStage(a, func(context.Context, dispatch.Part) error {
			dirs, err := filepath.Glob(filepath.Join(out, "runs", "*"))
			if err != nil || len(dirs) != 1 {
				return fmt.Errorf("the run's directory: %q, %v", dirs, err)
			}
			blocked := filepath.Join(dirs[0], "report")
			t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
			return os.Mkdir(blocked, 0o555)
		})
	}, in, "--out", out, "--report")

	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
	}
	run := onlyRun(t, out)
	if m := run.manifest; m.Error != "" || m.End.IsZero() {
		t.Errorf("manifest error %q, end %v; want a run that ended with no error", m.Error, m.End)
	}
	if e := run.line(t, schema.PartEvent, "a.txt"); e.Outcome != dispatch.Prepared {
		t.Errorf("a.txt's line = %+v, want prepared", e)
	}
	want := "docproc run: the run's report: "
	again := "docproc report " + out + " --run " + run.manifest.Run + " renders its pages again"
	if !strings.HasPrefix(r.stderr, want) || !strings.Contains(r.stderr, again) {
		t.Errorf("stderr = %q, want it to start with %q and say %q", r.stderr, want, again)
	}
	if strings.Contains(r.stdout, "index.html") {
		t.Errorf("stdout = %q, want no index's path", r.stdout)
	}
}

// The run's help states what --report does after a run that ended, a store's failure, and a
// cancellation, and that docproc report renders the pages again.
func TestRun_HelpStatesReport(t *testing.T) {
	a, stdout, _, _ := newApp(t)
	if code := a.Run(context.Background(), []string{"run", "--help"}); code != process.ExitUsage {
		t.Errorf("code = %d, want %d", code, process.ExitUsage)
	}
	help := strings.Join(strings.Fields(stdout.String()), " ")
	for _, want := range []string{
		"--report once the summary is printed",
		"with --report, its pages were rendered too",
		"a run stopped by a failure of the index or the content store still renders its pages",
		"a canceled run renders none",
		"pages couldn't be rendered exits 1 too, and docproc report renders them again",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}
