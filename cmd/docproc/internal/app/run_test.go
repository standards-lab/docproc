package app_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/docproc/stages"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
	"github.com/standards-lab/docproc/cmd/docproc/internal/apptest"
)

// The inputs' contents, written by the tests into a fresh directory.
const (
	helloText = "Hello from docproc.\n"
	notesText = "Notes kept in an archive.\n"
)

// zipOf returns a ZIP holding each of files, name then content, in order.
func zipOf(t *testing.T, files ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < len(files); i += 2 {
		w, err := zw.Create(files[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, files[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// brokenZip is a ZIP whose central directory can't be read: inspection finds it unsafe, so its
// part is quarantined before any stage runs.
const brokenZip = "PK\x03\x04 a local header and no central directory"

// write writes each of files, a path under dir then its content, making its directories, each
// file mode 0644, and returns dir.
func write(t *testing.T, dir string, files ...string) string {
	t.Helper()
	for i := 0; i < len(files); i += 2 {
		path := filepath.Join(dir, filepath.FromSlash(files[i]))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[i+1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// sha returns s's SHA-256, lowercase hex.
func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// sevenZip writes a stand-in for 7-Zip's 7zz, which prints 7zz's version line whatever its
// arguments, and returns a lookup that finds it and no other tool. The archive stage it wires
// reads a ZIP through archive/zip, so the stand-in is never asked to extract.
func sevenZip(t *testing.T) stages.Lookup {
	t.Helper()
	path := filepath.Join(t.TempDir(), "7zz")
	script := "#!/bin/sh\necho '7-Zip (z) 26.06 (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-06-01'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return func(program string) (string, error) {
		if program == "7zz" {
			return path, nil
		}
		return apptest.NoTools(program)
	}
}

// probes wraps lookup, recording each program it is asked for.
func probes(lookup stages.Lookup) (stages.Lookup, *apptest.Recorder) {
	r := &apptest.Recorder{}
	return func(program string) (string, error) {
		r.Record(program)
		return lookup(program)
	}, r
}

// result is one run of the App: its exit code and its streams.
type result struct {
	code           int
	stdout, stderr string
}

// runCmd runs docproc run over args under ctx, with every bound at its starting value but the
// tools' memory bound, which would need prlimit, after setup has replaced what the test
// replaces.
func runCmd(t *testing.T, ctx context.Context, setup func(*app.App), args ...string) result {
	t.Helper()
	clearBounds(t)
	t.Setenv("DOCPROC_TOOL_MEMORY", "0")
	t.Setenv(shutdownTimeout, "")
	a, stdout, stderr, _ := newApp(t)
	setup(a)
	code := a.Run(ctx, append([]string{"run"}, args...))
	return result{code, stdout.String(), stderr.String()}
}

// runOK runs docproc run as runCmd does, finding tools through lookup, and fails the test unless
// it exits 0.
func runOK(t *testing.T, lookup stages.Lookup, args ...string) result {
	t.Helper()
	r := runCmd(t, context.Background(), func(a *app.App) { apptest.Tools(a, lookup) }, args...)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitOK, r.stderr)
	}
	return r
}

// readRun is one run read back through rundir: its manifest and its record's lines.
type readRun struct {
	manifest schema.Manifest
	lines    []schema.Event
}

// readRuns reads every run of the run directory out through rundir's reader, in the order they
// began, failing the test on any error, a record's included.
func readRuns(t *testing.T, out string) []readRun {
	t.Helper()
	dir, err := rundir.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := dir.Runs()
	if err != nil {
		t.Fatal(err)
	}
	var runs []readRun
	for _, id := range ids {
		m, err := dir.Manifest(id)
		if err != nil {
			t.Fatal(err)
		}
		r := readRun{manifest: m}
		for e, err := range dir.Record(id) {
			if err != nil {
				t.Fatalf("run %s: %v", id, err)
			}
			r.lines = append(r.lines, e)
		}
		runs = append(runs, r)
	}
	return runs
}

// onlyRun reads the run directory out's one run.
func onlyRun(t *testing.T, out string) readRun {
	t.Helper()
	runs := readRuns(t, out)
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}
	return runs[0]
}

// inputLines returns the input's own part line of each input, in the record's order.
func (r readRun) inputLines() []schema.Event {
	var lines []schema.Event
	for _, e := range r.lines {
		if e.Kind == schema.PartEvent && e.Part.Depth == 0 {
			lines = append(lines, e)
		}
	}
	return lines
}

// line returns the first line of kind for the part named name, failing the test when there is
// none.
func (r readRun) line(t *testing.T, kind, name string) schema.Event {
	t.Helper()
	for _, e := range r.lines {
		if e.Kind == kind && e.Part.Name == name {
			return e
		}
	}
	t.Fatalf("no %s line for %s in %+v", kind, name, r.lines)
	return schema.Event{}
}

// lastLine returns s's last non-empty line.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// wantLine fails the test unless one of out's lines matches the regular expression re whole.
func wantLine(t *testing.T, out, re string) {
	t.Helper()
	if !regexp.MustCompile(`(?m)^` + re + `$`).MatchString(out) {
		t.Errorf("no line matches %q in\n%s", re, out)
	}
}

// A file argument is one input and a directory yields each regular file under it in lexical
// order; a file reached twice is one input, from the argument that reached it first.
func TestRun_CollectsInputs(t *testing.T) {
	root := t.TempDir()
	in := write(t, filepath.Join(root, "in"), "b.txt", "b\n", "a/c.txt", "c\n", "a/b.txt", "ab\n")
	x := filepath.Join(write(t, root, "x.txt", "x\n"), "x.txt")
	out := filepath.Join(root, "out")

	runOK(t, apptest.NoTools, in, filepath.Join(in, "b.txt"), x, "--out", out, "--workers", "1")

	r := onlyRun(t, out)
	var got []string
	for _, e := range r.inputLines() {
		got = append(got, e.Input)
		if e.File == nil {
			t.Fatalf("%s's line has no file", e.Input)
		}
		if want := filepath.Join(e.File.Arg, filepath.FromSlash(e.File.Path)); want != e.Input {
			t.Errorf("file arg %q joined to path %q = %s, want %s", e.File.Arg, e.File.Path, want, e.Input)
		}
	}
	want := []string{
		filepath.Join(in, "a", "b.txt"), filepath.Join(in, "a", "c.txt"), filepath.Join(in, "b.txt"), x,
	}
	if !slices.Equal(got, want) {
		t.Errorf("inputs = %q, want %q", got, want)
	}
	if r.manifest.Collected != 4 {
		t.Errorf("collected = %d, want 4", r.manifest.Collected)
	}
	if want := []string{in, filepath.Join(in, "b.txt"), x}; !slices.Equal(r.manifest.Inputs, want) {
		t.Errorf("manifest inputs = %q, want %q", r.manifest.Inputs, want)
	}
}

// Every path is recorded absolute and cleaned, however it was given, and a file argument's path
// under itself is ".".
func TestRun_PathsAbsolute(t *testing.T) {
	root := t.TempDir()
	write(t, root, "in/hello.txt", helloText)
	t.Chdir(root)

	runOK(t, apptest.NoTools, "./in/", "in//hello.txt", "--out", "out", "--workers", "1")

	r := onlyRun(t, filepath.Join(root, "out"))
	in := filepath.Join(root, "in")
	if want := []string{in, filepath.Join(in, "hello.txt")}; !slices.Equal(r.manifest.Inputs, want) {
		t.Errorf("manifest inputs = %q, want %q", r.manifest.Inputs, want)
	}
	lines := r.inputLines()
	if len(lines) != 1 {
		t.Fatalf("%d inputs, want 1", len(lines))
	}
	if f := lines[0].File; lines[0].Input != filepath.Join(in, "hello.txt") || f.Arg != in || f.Path != "hello.txt" {
		t.Errorf("input %q, file arg %q, path %q; want %s, %s, hello.txt", lines[0].Input, f.Arg, f.Path, filepath.Join(in, "hello.txt"), in)
	}

	// The same file named alone is its own argument.
	runOK(t, apptest.NoTools, "in/hello.txt", "--out", "out2")
	if f := onlyRun(t, filepath.Join(root, "out2")).inputLines()[0].File; f.Arg != filepath.Join(in, "hello.txt") || f.Path != "." {
		t.Errorf("file arg %q, path %q; want %s, .", f.Arg, f.Path, filepath.Join(in, "hello.txt"))
	}
}

// A named argument that is a symbolic link is followed, to a file or a directory, and a link
// under a directory is not, and is no input.
func TestRun_FollowsNamedLinks(t *testing.T) {
	root := t.TempDir()
	write(t, root, "target.txt", helloText, "real/f.txt", "f\n", "in/a.txt", "a\n")
	for _, l := range [][2]string{
		{"target.txt", "link.txt"},
		{"real", "dirlink"},
		{"../target.txt", "in/linked.txt"},
		{"../real", "in/linkeddir"},
	} {
		if err := os.Symlink(l[0], filepath.Join(root, l[1])); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(root, "out")

	runOK(t, apptest.NoTools, filepath.Join(root, "link.txt"), filepath.Join(root, "dirlink"), filepath.Join(root, "in"),
		"--out", out, "--workers", "1")

	lines := onlyRun(t, out).inputLines()
	var got []string
	for _, e := range lines {
		got = append(got, e.Input)
	}
	want := []string{filepath.Join(root, "link.txt"), filepath.Join(root, "dirlink", "f.txt"), filepath.Join(root, "in", "a.txt")}
	if !slices.Equal(got, want) {
		t.Fatalf("inputs = %q, want %q", got, want)
	}
	// A linked file's line describes the file it leads to, under the link's name.
	if f := lines[0].File; f.Name != "link.txt" || f.Size != int64(len(helloText)) || !strings.HasPrefix(f.Mode, "-") {
		t.Errorf("link.txt's file = %+v, want the target's regular file of %d bytes", f, len(helloText))
	}
	if f := lines[1].File; f.Arg != filepath.Join(root, "dirlink") || f.Path != "f.txt" {
		t.Errorf("dirlink's file arg %q, path %q; want %s, f.txt", f.Arg, f.Path, filepath.Join(root, "dirlink"))
	}
}

// An argument that is neither a file nor a directory, one that doesn't exist, and arguments that
// yield no input fail the run, exit 1, before any tool is probed or the run directory is made.
func TestRun_RefusesInputsBeforeProbing(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	onlyLinks := filepath.Join(root, "links")
	for _, dir := range []string{empty, onlyLinks} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, root, "a.txt", "a\n")
	if err := os.Symlink("../a.txt", filepath.Join(onlyLinks, "a.txt")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"device", []string{filepath.Join(root, "a.txt"), os.DevNull}, os.DevNull + " is neither a file nor a directory"},
		{"missing", []string{filepath.Join(root, "missing.txt")}, "no such file or directory"},
		{"empty directory", []string{empty}, "the arguments yield no input"},
		{"links alone", []string{onlyLinks}, "the arguments yield no input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			lookup, probed := probes(apptest.NoTools)
			r := runCmd(t, context.Background(), func(a *app.App) { apptest.Tools(a, lookup) }, append(tc.args, "--out", out)...)
			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if !strings.HasPrefix(r.stderr, "docproc run: ") || !strings.Contains(r.stderr, tc.want) {
				t.Errorf("stderr = %q, want docproc run's error naming %q", r.stderr, tc.want)
			}
			if log := probed.Log(); len(log) != 0 {
				t.Errorf("tools probed = %q, want none", log)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want no stage table", r.stdout)
			}
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the run directory: %v, want it not made", err)
			}
		})
	}
}

// A usage error exits 2 before anything is built: an unknown route to --exclude, --workers under
// 1, no --out, or no path.
func TestRunCommand_UsageErrors(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n")
	out := filepath.Join(t.TempDir(), "out")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown route", []string{in, "--out", out, "--exclude", "text", "--exclude", "pictures"}, `--exclude: stages: unknown route "pictures"`},
		{"no workers", []string{in, "--out", out, "--workers", "0"}, "--workers 0 is under 1"},
		{"negative workers", []string{in, "--out", out, "--workers", "-2"}, "--workers -2 is under 1"},
		{"no out", []string{in}, "out"},
		{"no path", []string{"--out", out}, "requires at least one path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearBounds(t)
			t.Setenv(shutdownTimeout, "not-a-duration")
			a, stdout, stderr, built := newApp(t)
			code := a.Run(context.Background(), append([]string{"run"}, tc.args...))
			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.HasPrefix(stderr.String(), "docproc run: ") || !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want docproc run's usage error naming %q", stderr, tc.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the run directory: %v, want it not made", err)
			}
		})
	}
}

// The run's help states what each exit status means, failed parts and error inputs exiting 0.
func TestRun_HelpStatesExitStatus(t *testing.T) {
	a, stdout, _, _ := newApp(t)
	if code := a.Run(context.Background(), []string{"run", "--help"}); code != process.ExitUsage {
		t.Errorf("code = %d, want %d", code, process.ExitUsage)
	}
	for _, want := range []string{"Exit status:", "  0  the run ended", "some of its parts failed", "stopped with an error", "  1  the run stopped", "  2  a usage error"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}

// --workers defaults to the host's CPUs and has no ceiling.
func TestRun_Workers(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n", "b.txt", "b\n")
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"default", nil, runtime.NumCPU()},
		{"above the CPUs", []string{"--workers", "1000"}, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			runOK(t, apptest.NoTools, append([]string{in, "--out", out}, tc.args...)...)
			r := onlyRun(t, out)
			if r.manifest.Workers != tc.want {
				t.Errorf("workers = %d, want %d", r.manifest.Workers, tc.want)
			}
			if n := len(r.inputLines()); n != 2 {
				t.Errorf("%d inputs recorded, want 2", n)
			}
		})
	}
}

// --exclude repeats, each route's stage left out: its row is excluded, and its parts have no
// processor yet.
func TestRun_Exclude(t *testing.T) {
	in := write(t, t.TempDir(), "hello.txt", helloText)
	out := filepath.Join(t.TempDir(), "out")

	r := runOK(t, apptest.NoTools, in, "--out", out, "--exclude", "text", "--exclude", "vision")

	run := onlyRun(t, out)
	for _, row := range run.manifest.Stages {
		excluded := row.Route == route.Text || row.Route == route.Vision
		if excluded != (row.State == dispatch.Excluded) {
			t.Errorf("row %+v, want excluded exactly for text and vision", row)
		}
	}
	if e := run.inputLines()[0]; e.Outcome != dispatch.NoProcessor {
		t.Errorf("hello.txt's outcome = %q, want %q", e.Outcome, dispatch.NoProcessor)
	}
	wantLine(t, r.stdout, `text\s+-\s+excluded\s+`)
}

// The run prints the stage table, and its manifest names its tools, its stage table, its docproc
// build, and how many inputs it collected, and ends without an error.
func TestRun_StageTableAndManifest(t *testing.T) {
	in := write(t, t.TempDir(), "hello.txt", helloText, "notes.zip", string(zipOf(t, "notes.txt", notesText)))
	out := filepath.Join(t.TempDir(), "out")

	r := runOK(t, sevenZip(t), in, "--out", out, "--workers", "2")

	wantLine(t, r.stdout, `TOOL\s+VERSION\s+MINIMUM\s+FROM`)
	wantLine(t, r.stdout, `7zz\s+7-Zip \(z\) 26\.06 .*\s+/\S+/7zz`)
	wantLine(t, r.stdout, `gs\s+-\s+\S+\s+gs: not found`)
	wantLine(t, r.stdout, `ROUTE\s+STAGE\s+STATE\s+TOOLS`)
	wantLine(t, r.stdout, `archive\s+archive\s+wired\s+7-Zip.*`)
	wantLine(t, r.stdout, `text\s+text\s+wired\s+none`)
	wantLine(t, r.stdout, `document\s+-\s+missing\s+.*gs: not found.*`)
	if i, j := strings.Index(r.stdout, "TOOL "), strings.Index(r.stdout, "outputs"); i != 0 || j < i {
		t.Errorf("the stage table doesn't come before the inputs' lines:\n%s", r.stdout)
	}

	m := onlyRun(t, out).manifest
	// The stages as the run built them: under the starting bounds, the memory bound as the test
	// set it.
	opts := startingBounds().Stages
	opts.Memory = 0
	want, err := stages.Build(context.Background(), sevenZip(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tools) != len(want.Tools) || m.Tools[0].Program != "gs" || m.Tools[0].Error != "gs: not found" {
		t.Errorf("manifest tools = %+v, want %d, gs first, not found", m.Tools, len(want.Tools))
	}
	if i := slices.IndexFunc(m.Tools, func(tl dispatch.Tool) bool { return tl.Program == "7zz" }); i < 0 || !strings.HasPrefix(m.Tools[i].Version, "7-Zip (z) 26.06") {
		t.Errorf("manifest tools = %+v, want 7zz with its version line", m.Tools)
	}
	if len(m.Stages) != len(stages.Routes()) {
		t.Errorf("manifest has %d rows, want %d", len(m.Stages), len(stages.Routes()))
	}
	for i, row := range m.Stages {
		if row.Route != want.Rows[i].Route || row.State != want.Rows[i].State || row.Stage != want.Rows[i].Stage {
			t.Errorf("row %d = %+v, want %+v", i, row, want.Rows[i])
		}
	}
	// A test binary is built from this checkout, the library through the module's replace.
	if m.Docproc != (schema.Build{Command: "(devel)", Library: "(devel)"}) {
		t.Errorf("docproc = %+v, want (devel) for both", m.Docproc)
	}
	if m.Schema != schema.Version || m.Collected != 2 || m.Workers != 2 || !slices.Equal(m.Inputs, []string{in}) {
		t.Errorf("manifest schema %q, collected %d, workers %d, inputs %q; want %q, 2, 2, [%s]", m.Schema, m.Collected, m.Workers, m.Inputs, schema.Version, in)
	}
	if m.End.IsZero() || m.End.Time().Before(m.Start.Time()) || m.Error != "" {
		t.Errorf("manifest start %v, end %v, error %q; want an end after the start, no error", m.Start, m.End, m.Error)
	}
}

// One record line per event, in dispatch's order: each output's content is in the run
// directory's content store, a found part's line follows its output's, and the input's own line,
// its last, carries its file, with its detected type, and its time.
func TestRun_Record(t *testing.T) {
	in := write(t, t.TempDir(), "hello.txt", helloText, "notes.zip", string(zipOf(t, "notes.txt", notesText)))
	out := filepath.Join(t.TempDir(), "out")

	r := runOK(t, sevenZip(t), in, "--out", out, "--workers", "1")

	run := onlyRun(t, out)
	var kinds []string
	for _, e := range run.lines {
		kinds = append(kinds, e.Kind+" "+e.Part.Name)
	}
	want := []string{"output hello.txt", "part hello.txt", "output notes.txt", "part notes.txt", "part notes.zip"}
	if !slices.Equal(kinds, want) {
		t.Fatalf("record = %q, want %q", kinds, want)
	}

	dir, err := rundir.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, text string }{{"hello.txt", helloText}, {"notes.txt", notesText}} {
		o := run.line(t, schema.OutputEvent, tc.name).Output
		rc, err := dir.Content().Open(context.Background(), o.SHA256)
		if err != nil {
			t.Fatalf("%s's output %s: %v", tc.name, o.SHA256, err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil || !strings.Contains(string(b), strings.TrimSpace(tc.text)) {
			t.Errorf("%s's stored output = %q, %v; want its text", tc.name, b, err)
		}
	}

	hello := run.line(t, schema.PartEvent, "hello.txt")
	f := hello.File
	if f == nil || f.Name != "hello.txt" || f.Arg != in || f.Path != "hello.txt" || f.Size != int64(len(helloText)) ||
		f.Mode != "-rw-r--r--" || f.Detected != "text/plain; charset=utf-8" || f.Modified.IsZero() {
		t.Errorf("hello.txt's file = %+v, want its metadata and detected type", f)
	}
	if hello.Outcome != dispatch.Prepared || hello.Outputs != 1 || hello.Part.SHA256 != sha(helloText) {
		t.Errorf("hello.txt's line = %+v, want prepared, one output", hello)
	}
	if tm := hello.Time; tm == nil || tm.Start.IsZero() || tm.End.Time().Before(tm.Start.Time()) {
		t.Errorf("hello.txt's time = %+v, want its first event to its last", tm)
	}
	if zipped := run.line(t, schema.PartEvent, "notes.zip"); zipped.File.Detected != "application/zip" || zipped.Children != 1 {
		t.Errorf("notes.zip's line = %+v, want detected application/zip, one child", zipped)
	}
	// A found part's lines carry no file and no time: they are the input's.
	for _, e := range run.lines {
		if (e.Kind != schema.PartEvent || e.Part.Depth > 0) && (e.File != nil || e.Time != nil) {
			t.Errorf("line %+v carries a file or a time, want neither", e)
		}
	}

	wantLine(t, r.stdout, regexp.QuoteMeta(filepath.Join(in, "hello.txt"))+`  text  prepared  outputs 1  children 0  \S+`)
	wantLine(t, r.stdout, regexp.QuoteMeta(filepath.Join(in, "notes.zip"))+`  archive  prepared  outputs 0  children 1  \S+`)
	wantLine(t, r.stdout, `archive\s+prepared\s+1`)
	wantLine(t, r.stdout, `text\s+prepared\s+1`)
	wantLine(t, r.stdout, `total\s+2`)
	wantLine(t, r.stdout, `run `+regexp.QuoteMeta(run.manifest.Run))
	if last := lastLine(r.stdout); last != "failed parts 0  error inputs 0" {
		t.Errorf("summary's last line = %q, want the counts of failed parts and error inputs", last)
	}
}

// A re-run over the same run directory prepares nothing: its every line is seen, the dispatcher
// replaying a seen part's log, its children's lines and outputs included, none copied from the
// first run's record.
func TestRun_ReRunReplaysSeenParts(t *testing.T) {
	in := write(t, t.TempDir(), "hello.txt", helloText, "notes.zip", string(zipOf(t, "notes.txt", notesText)))
	out := filepath.Join(t.TempDir(), "out")
	lookup := sevenZip(t)

	runOK(t, lookup, in, "--out", out, "--workers", "1")
	// With the first run's record gone, the second's lines can only be the dispatcher's.
	first := readRuns(t, out)[0]
	if err := os.RemoveAll(filepath.Join(out, "runs", first.manifest.Run)); err != nil {
		t.Fatal(err)
	}
	r := runOK(t, lookup, in, "--out", out, "--workers", "1")

	second := onlyRun(t, out)
	if len(second.lines) != len(first.lines) {
		t.Fatalf("re-run wrote %d lines, want the first run's %d", len(second.lines), len(first.lines))
	}
	for i, e := range second.lines {
		f := first.lines[i]
		if e.Kind != f.Kind || e.Part.Name != f.Part.Name || e.Part.Depth != f.Part.Depth {
			t.Errorf("re-run line %d = %s %s at %d, want %s %s at %d", i, e.Kind, e.Part.Name, e.Part.Depth, f.Kind, f.Part.Name, f.Part.Depth)
		}
		if e.Outcome != dispatch.Seen {
			t.Errorf("re-run line %d %s %s is %q, want seen", i, e.Kind, e.Part.Name, e.Outcome)
		}
		if e.Output != nil && e.Output.SHA256 != f.Output.SHA256 {
			t.Errorf("re-run output %s, want the first run's %s", e.Output.SHA256, f.Output.SHA256)
		}
	}
	if zipped := second.line(t, schema.PartEvent, "notes.zip"); zipped.Children != 1 || zipped.Recorded != dispatch.Prepared {
		t.Errorf("notes.zip's re-run line = %+v, want seen, recorded prepared, one child", zipped)
	}
	wantLine(t, r.stdout, `archive\s+seen\s+1`)
	wantLine(t, r.stdout, `text\s+seen\s+1`)
	if matches, err := filepath.Glob(filepath.Join(out, ".work-*")); err != nil || len(matches) != 0 {
		t.Errorf("working directories left: %q, %v", matches, err)
	}
}

// A quarantined part's file is kept in the run directory, and its line names the kept file
// whenever it exists: in the run that kept it, and in a later one, the input's and a replayed
// child's alike.
func TestRun_QuarantineKept(t *testing.T) {
	in := write(t, t.TempDir(), "broken.zip", brokenZip, "outer.zip", string(zipOf(t, "inner.zip", brokenZip)))
	out := filepath.Join(t.TempDir(), "out")
	lookup := sevenZip(t)
	kept := "quarantine/" + sha(brokenZip)

	runOK(t, lookup, in, "--out", out, "--workers", "1")
	runOK(t, lookup, in, "--out", out, "--workers", "1")

	for i, run := range readRuns(t, out) {
		for _, name := range []string{"broken.zip", "inner.zip"} {
			e := run.line(t, schema.PartEvent, name)
			if e.Outcome != dispatch.Quarantined || e.Quarantine != kept || len(e.Part.Flags) == 0 {
				t.Errorf("run %d's %s line = %+v, want quarantined, naming %s, with its flags", i+1, name, e, kept)
			}
		}
		for _, e := range run.lines {
			if e.Outcome != dispatch.Quarantined && e.Quarantine != "" {
				t.Errorf("run %d's line %+v names a kept file, want it unnamed", i+1, e)
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(kept)))
	if err != nil || string(b) != brokenZip {
		t.Errorf("kept file = %q, %v; want the quarantined part's content", b, err)
	}

	// Once the kept file is gone, a replayed child, which has no file, names none, and an input,
	// which has, is kept again, its line naming it, as is every line after it of the same content.
	if err := os.RemoveAll(filepath.Join(out, "quarantine")); err != nil {
		t.Fatal(err)
	}
	runOK(t, lookup, filepath.Join(in, "outer.zip"), "--out", out)
	if e := readRuns(t, out)[2].line(t, schema.PartEvent, "inner.zip"); e.Outcome != dispatch.Quarantined || e.Quarantine != "" {
		t.Errorf("third run's inner.zip line = %+v, want quarantined, naming no kept file", e)
	}
	runOK(t, lookup, in, "--out", out, "--workers", "1")
	fourth := readRuns(t, out)[3]
	for _, name := range []string{"broken.zip", "inner.zip"} {
		if e := fourth.line(t, schema.PartEvent, name); e.Quarantine != kept {
			t.Errorf("fourth run's %s line = %+v, want it naming %s", name, e, kept)
		}
	}
	if b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(kept))); err != nil || string(b) != brokenZip {
		t.Errorf("kept file = %q, %v; want the quarantined part's content, kept again", b, err)
	}
}

// An input that can't be read is an error line, its file and its time on it and no part, and the
// run goes on and exits 0, the summary's last line counting it.
func TestRun_ErrorInputExits0(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 0000, so no input fails")
	}
	in := write(t, t.TempDir(), "hello.txt", helloText, "locked.txt", "locked\n")
	locked := filepath.Join(in, "locked.txt")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")

	r := runOK(t, apptest.NoTools, in, "--out", out, "--workers", "1")

	run := onlyRun(t, out)
	var errs []schema.Event
	for _, e := range run.lines {
		if e.Kind == schema.ErrorEvent {
			errs = append(errs, e)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("error lines = %+v, want one", errs)
	}
	e := errs[0]
	if e.Input != locked || !strings.Contains(e.Error, "permission denied") || e.Part.ID != "" || e.Part.Name != "" {
		t.Errorf("error line = %+v, want locked.txt's, its error, no part", e)
	}
	if e.File == nil || e.File.Name != "locked.txt" || e.File.Mode != "----------" || e.File.Detected != "" {
		t.Errorf("error line's file = %+v, want locked.txt's metadata, no detected type", e.File)
	}
	if e.Time == nil || e.Time.End.Time().Before(e.Time.Start.Time()) {
		t.Errorf("error line's time = %+v, want its span", e.Time)
	}
	if run.line(t, schema.PartEvent, "hello.txt").Outcome != dispatch.Prepared {
		t.Error("hello.txt wasn't prepared beside the error")
	}
	wantLine(t, r.stdout, regexp.QuoteMeta(locked)+`  -  error: .*permission denied  \S+`)
	wantLine(t, r.stdout, `-\s+error\s+1`)
	if last := lastLine(r.stdout); last != "failed parts 0  error inputs 1" {
		t.Errorf("summary's last line = %q, want one error input", last)
	}
}

// stage is a stand-in stage: prepare is its Prepare's body.
type stage struct {
	id      dispatch.Identity
	prepare func(ctx context.Context, p dispatch.Part) error
}

func (s stage) Identity() dispatch.Identity { return s.id }

func (s stage) Prepare(ctx context.Context, p dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		if err := s.prepare(ctx, p); err != nil {
			yield(dispatch.Item{}, err)
		}
	}
}

// textStage makes a's runs build docproc's stages over no tool, the text route's stage replaced
// by a stand-in that runs prepare.
func textStage(a *app.App, prepare func(ctx context.Context, p dispatch.Part) error) {
	apptest.Stages(a, func(ctx context.Context, opts stages.Options) (stages.Set, error) {
		set, err := stages.Build(ctx, apptest.NoTools, opts)
		if err != nil {
			return set, err
		}
		set.Stages[route.Text] = stage{id: dispatch.Identity{Name: "stand-in", Version: "1", Config: "c"}, prepare: prepare}
		return set, nil
	})
}

// A part that fails is its line's outcome, its error on the terminal, and the run goes on and
// exits 0, the summary's last line counting it.
func TestRun_FailedPartExits0(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n", "b.txt", "b\n")
	out := filepath.Join(t.TempDir(), "out")

	r := runCmd(t, context.Background(), func(a *app.App) {
		textStage(a, func(_ context.Context, p dispatch.Part) error {
			if p.Name == "a.txt" {
				return errors.New("the stand-in can't read a.txt\nits second line")
			}
			return nil
		})
	}, in, "--out", out, "--workers", "1")

	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitOK, r.stderr)
	}
	run := onlyRun(t, out)
	if e := run.line(t, schema.PartEvent, "a.txt"); e.Outcome != dispatch.Failed || !strings.Contains(e.Reason, "its second line") {
		t.Errorf("a.txt's line = %+v, want failed, its whole error", e)
	}
	if e := run.line(t, schema.PartEvent, "b.txt"); e.Outcome != dispatch.Prepared {
		t.Errorf("b.txt's line = %+v, want prepared", e)
	}
	wantLine(t, r.stdout, regexp.QuoteMeta(filepath.Join(in, "a.txt"))+`  text  failed: .*the stand-in can't read a\.txt…  outputs 0  children 0  \S+`)
	if last := lastLine(r.stdout); last != "failed parts 1  error inputs 0" {
		t.Errorf("summary's last line = %q, want one failed part", last)
	}
	if m := run.manifest; m.Error != "" || m.End.IsZero() {
		t.Errorf("manifest error %q, end %v; want an end, no error", m.Error, m.End)
	}
}

// A failure of the content store stops the run: it ends with the error, which the manifest
// names, and exits 1, its record still readable.
func TestRun_StoreFailureExits1(t *testing.T) {
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

	r := runCmd(t, context.Background(), func(a *app.App) { apptest.Tools(a, apptest.NoTools) }, in, "--out", out)

	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stdout = %q", r.code, process.ExitFailure, r.stdout)
	}
	if want := "docproc run: dispatch: content: "; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	run := onlyRun(t, out)
	if m := run.manifest; !strings.HasPrefix(m.Error, "dispatch: content: ") || m.End.IsZero() {
		t.Errorf("manifest error %q, end %v; want the content store's failure and an end", m.Error, m.End)
	}
	for _, e := range run.lines {
		if e.Kind == schema.ErrorEvent {
			t.Errorf("error line %+v, want the store's failure no input's", e)
		}
	}
}

// A canceled run stops: its manifest ends with the cancellation, it exits 1, and its record
// stays readable, every line written before the cancellation in it.
func TestRun_CanceledRun(t *testing.T) {
	in := write(t, t.TempDir(), "a.txt", "a\n", "b.txt", "b\n", "c.txt", "c\n")
	out := filepath.Join(t.TempDir(), "out")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once

	r := runCmd(t, ctx, func(a *app.App) {
		textStage(a, func(sctx context.Context, p dispatch.Part) error {
			if p.Name != "b.txt" {
				return nil
			}
			once.Do(cancel)
			<-sctx.Done()
			return sctx.Err()
		})
	}, in, "--out", out, "--workers", "1")

	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
	}
	run := onlyRun(t, out)
	if m := run.manifest; m.Error != context.Canceled.Error() || m.End.IsZero() {
		t.Errorf("manifest error %q, end %v; want %q and an end", m.Error, m.End, context.Canceled)
	}
	if e := run.line(t, schema.PartEvent, "a.txt"); e.Outcome != dispatch.Prepared {
		t.Errorf("a.txt's line = %+v, want prepared before the cancellation", e)
	}
	for _, e := range run.lines {
		if e.Part.Name == "c.txt" || e.Input == filepath.Join(in, "c.txt") {
			t.Errorf("line %+v, want none for c.txt, never dispatched", e)
		}
	}
	dir, err := rundir.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := dir.Latest(); err != nil || id != run.manifest.Run {
		t.Errorf("Latest() = %q, %v; want the canceled run, which ended", id, err)
	}
}
