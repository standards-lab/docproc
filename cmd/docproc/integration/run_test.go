//go:build integration

package integration_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
	"github.com/standards-lab/go-core/process/processtest"

	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docproc/cmd/docproc/integration"
)

// docproc run as a built binary, over the docproc tools built from the checkout and the host's
// other system tools, writing a run directory that rundir reads back.

// version prints the module's version, one line, to stdout.
func TestCLI_Version(t *testing.T) {
	res := integration.Run(t, "version")

	if res.Code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", res.Code, process.ExitOK, res.Stderr)
	}
	line, rest, found := strings.Cut(res.Stdout, "\n")
	if line == "" || !found || rest != "" {
		t.Errorf("stdout = %q, want one non-empty line", res.Stdout)
	}
	if res.Stderr != "" {
		t.Errorf("stderr = %q, want empty", res.Stderr)
	}
}

// runArgs returns docproc run's arguments over inputs into the run directory out, with more
// flags after them.
func runArgs(inputs []string, out string, flags ...string) []string {
	args := append([]string{"run"}, inputs...)
	return append(append(args, "--out", out), flags...)
}

// onlyRun opens the run directory out through rundir's reader and returns it and the id of its
// one run, failing the test unless it holds exactly one.
func onlyRun(t *testing.T, out string) (*rundir.Dir, string) {
	t.Helper()
	d, err := rundir.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := d.Runs()
	if err != nil || len(ids) != 1 {
		t.Fatalf("Runs = %v, %v; want one run", ids, err)
	}
	return d, ids[0]
}

// A run over every committed fixture writes a schema "2" run directory that rundir reads: its one
// run ended, with no error, is the latest, its manifest names every input, and its record decodes
// whole and accounts for every input, each with its own part line, depth 0, or its error line,
// exactly once.
func TestRun_Fixtures(t *testing.T) {
	fixtures := integration.Fixtures(t)
	out := filepath.Join(t.TempDir(), "out")

	res := integration.Run(t, runArgs(fixtures, out)...)

	if res.Code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", res.Code, process.ExitOK, res.Stderr)
	}
	d, id := onlyRun(t, out)
	if latest, err := d.Latest(); err != nil || latest != id {
		t.Errorf("Latest = %q, %v; want %q", latest, err, id)
	}
	m, err := d.Manifest(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != schema.Version || m.End.IsZero() || m.Error != "" {
		t.Errorf("manifest schema %q, end %v, error %q; want schema %q, an end, no error", m.Schema, m.End, m.Error, schema.Version)
	}
	if m.Collected != len(fixtures) || len(m.Inputs) != len(fixtures) {
		t.Errorf("manifest collected %d, %d inputs; want %d of each", m.Collected, len(m.Inputs), len(fixtures))
	}
	seen := map[string]int{}
	for e, err := range d.Record(id) {
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if e.Kind == schema.ErrorEvent || e.Kind == schema.PartEvent && e.Part.Depth == 0 {
			seen[e.Input]++
		}
	}
	for _, path := range fixtures {
		if n := seen[path]; n != 1 {
			t.Errorf("%s has %d input or error lines, want 1", path, n)
		}
		delete(seen, path)
	}
	for path, n := range seen {
		t.Errorf("%s, no input of the run, has %d input or error lines", path, n)
	}
}

// recordLines returns how many whole lines, each ending in a newline, the record file at path
// holds, 0 when it doesn't exist yet.
func recordLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(b, []byte("\n"))
}

// A run killed by SIGKILL partway through its record, which it can't catch, still reads through
// Record: every whole line it wrote, a torn last line alone dropped, then ErrNotEnded, since its
// manifest has no end; Latest passes it over. The kill lands mid-run: the run dispatches every
// fixture one at a time, which takes seconds, and is killed once its record holds a line. docproc
// report then renders it by --run, marked not ended, and exits 0.
func TestRun_Killed(t *testing.T) {
	fixtures := integration.Fixtures(t)
	out := filepath.Join(t.TempDir(), "out")

	p := integration.Start(t, runArgs(fixtures, out, "--workers", "1")...)
	var record string
	processtest.WaitFor(t, "the run's first record line", func() bool {
		if p.Exited() {
			return true // Kill fails the test: the run ended before its kill
		}
		paths, err := filepath.Glob(filepath.Join(out, "runs", "*", "record.jsonl"))
		if err != nil || len(paths) != 1 {
			return false
		}
		record = paths[0]
		return recordLines(t, record) > 0
	})
	if res := p.Kill(t); res.Code != -1 {
		t.Errorf("code = %d, want -1, killed by a signal", res.Code)
	}

	whole := recordLines(t, record)
	d, id := onlyRun(t, out)
	if latest, err := d.Latest(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Latest = %q, %v; want an error matching fs.ErrNotExist, the killed run passed over", latest, err)
	}
	if m, err := d.Manifest(id); err != nil || !m.End.IsZero() || len(m.Inputs) != len(fixtures) {
		t.Errorf("manifest = %+v, %v; want its inputs and no end", m, err)
	}
	var lines int
	var errs []error
	for _, err := range d.Record(id) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lines++
	}
	if len(errs) != 1 || !errors.Is(errs[0], rundir.ErrNotEnded) {
		t.Errorf("Record's errors = %v, want ErrNotEnded alone, last", errs)
	}
	if lines != whole || lines == 0 {
		t.Errorf("Record yielded %d lines, want the record's %d whole lines, at least one", lines, whole)
	}

	// docproc report renders the killed run, by --run, its index marked not ended, and exits 0,
	// printing the index's path last.
	rep := integration.Run(t, "report", out, "--run", id)
	if rep.Code != process.ExitOK {
		t.Fatalf("report code = %d, want %d; stderr = %q", rep.Code, process.ExitOK, rep.Stderr)
	}
	index := filepath.Join(reportDir(t, d, out, id), indexPage)
	if got := lastLine(rep.Stdout); got != index {
		t.Errorf("report's last stdout line = %q, want the index's path %q", got, index)
	}
	page, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "not ended") {
		t.Errorf("%s doesn't mark the run not ended", index)
	}
}
