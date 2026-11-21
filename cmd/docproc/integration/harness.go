package integration

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
)

const envPrefix = "DOCPROC_"

var hostBounds = []string{
	"DOCPROC_BOMB_RATIO=100",
	"DOCPROC_BOMB_SIZE=67108864",
	"DOCPROC_ARCHIVE_ENTRIES=10000",
}

// Bound bounds every run the harness starts: one over every committed fixture takes seconds, so a
// run still going after it is a stall, killed, failing its test with its output.
const Bound = 10 * processtest.Failsafe

const fixtureList = "stages/testdata/fixtures.tsv"

// ErrNoFixtures is what [ReadFixtures] fails with when the fixture tree is absent.
var ErrNoFixtures = errors.New("integration: no fixture tree")

var (
	binary string
	root   string
)

// Main is the suite's TestMain: it unsets every DOCPROC_ variable the suite inherited, so each run
// reads only docproc's starting values and the harness's host bounds, then builds the command
// once, with the race detector so the program runs under it too, runs the tests, and removes the
// build. A build failure ends the run before any test starts.
func Main(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, envPrefix) {
			_ = os.Unsetenv(name)
		}
	}
	code, err := build(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration:", err)
		code = 1
	}
	os.Exit(code)
}

func build(m *testing.M) (int, error) {
	out, err := exec.Command("go", "list", "-f", "{{.Module.Dir}}", ".").Output()
	if err != nil {
		return 0, fmt.Errorf("locate module root: %w", err)
	}
	root = strings.TrimSpace(string(out))
	dir, err := os.MkdirTemp("", "docproc-integration-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	binary = filepath.Join(dir, "docproc")
	cmd := exec.Command("go", "build", "-race", "-o", binary, ".")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("build docproc: %w", err)
	}
	return m.Run(), nil
}

// Process is one running docproc: its captured streams and its exit.
type Process struct {
	cmd            *exec.Cmd
	args           []string
	stdout, stderr bytes.Buffer
	exited         chan struct{}
	err            error
}

// Start starts the binary Main built in the module root with args, the arguments without the
// program name, on empty standard input, with the parent's environment and the host bounds, and
// returns without waiting. At test cleanup, a process still running is killed. Its race runtime
// skips the one-second sleep at exit, since races report as they happen, keeping the parent's
// GORACE options, and Wait stops copying its output [processtest.Failsafe] after it exits, so a
// tool docproc leaves holding the output open can't keep the run going.
func Start(t testing.TB, args ...string) *Process {
	t.Helper()
	if binary == "" {
		t.Fatal("integration: no binary; the suite's TestMain must call Main")
	}
	p := &Process{args: args, exited: make(chan struct{})}
	p.cmd = exec.Command(binary, args...)
	p.cmd.Dir = root
	gorace := strings.TrimSpace(os.Getenv("GORACE") + " atexit_sleep_ms=0")
	p.cmd.Env = append(append(os.Environ(), hostBounds...), "GORACE="+gorace)
	p.cmd.Stdout, p.cmd.Stderr = &p.stdout, &p.stderr
	p.cmd.WaitDelay = processtest.Failsafe
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("start docproc: %v", err)
	}
	go func() {
		defer close(p.exited)
		p.err = p.cmd.Wait()
	}()
	t.Cleanup(func() {
		if !p.Exited() {
			_ = p.cmd.Process.Kill()
			<-p.exited
		}
	})
	return p
}

// Exited reports whether the process has ended.
func (p *Process) Exited() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

// Wait waits for the process to exit and returns its streams and its exit code, -1 when a signal
// ended it. A process still running after [Bound] is killed, and the test fails with its output.
// Wait logs the run through t.Log as a shell line, with its output after it.
func (p *Process) Wait(t testing.TB) processtest.Result {
	t.Helper()
	select {
	case <-p.exited:
	case <-time.After(Bound):
		_ = p.cmd.Process.Kill()
		<-p.exited
		t.Fatalf("%s did not exit within %s:\nstdout: %s\nstderr: %s", p.line(), Bound, p.stdout.String(), p.stderr.String())
	}
	res := processtest.Result{Stdout: p.stdout.String(), Stderr: p.stderr.String()}
	var exit *exec.ExitError
	switch {
	case p.err == nil:
	case errors.As(p.err, &exit):
		res.Code = exit.ExitCode()
	default:
		t.Fatalf("%s: %v", p.line(), p.err)
	}
	t.Logf("%s\nexit %d\nstdout: %s\nstderr: %s", p.line(), res.Code, res.Stdout, res.Stderr)
	return res
}

// Kill kills the process with SIGKILL, which it can't catch, so it stops wherever it is, and
// returns its result once it has exited, its code -1. A process that exited before the kill
// landed fails the test: the kill was to land mid-run.
func (p *Process) Kill(t testing.TB) processtest.Result {
	t.Helper()
	err := p.cmd.Process.Signal(syscall.SIGKILL)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("kill docproc: %v", err)
	}
	res := p.Wait(t)
	if ws, ok := p.cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("%s exited with %d before its kill", p.line(), res.Code)
	}
	return res
}

func (p *Process) line() string {
	words := append(slices.Clone(hostBounds), "docproc")
	return "$ " + strings.Join(append(words, p.args...), " ")
}

// Run runs the built program once with args, as [Start] does, and returns its result once it
// exits. A nonzero code is a result, not a failure.
func Run(t testing.TB, args ...string) processtest.Result {
	t.Helper()
	return Start(t, args...).Wait(t)
}

// Fixtures returns the absolute paths of copies of the repository's committed fixtures, as
// [ReadFixtures] reads them from the repository the module sits in, two directories up from its
// root, each at its path from the repository's root in a directory of the test's own, in the
// list's order. A run over a committed PDF in place would run gs's runs over a page of their own
// in its testdata directory, their working directory; a run over the copies keeps whatever every
// tool's runs write in their working directory out of the repository, a killed run's among them.
// An absent fixture tree skips the test, naming what is missing; any other failure, a listed
// fixture missing among them, fails it.
func Fixtures(t testing.TB) []string {
	t.Helper()
	if root == "" {
		t.Fatal("integration: no module root; the suite's TestMain must call Main")
	}
	repo, err := filepath.Abs(filepath.Join(root, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := ReadFixtures(repo)
	if errors.Is(err, ErrNoFixtures) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copies := make([]string, len(paths))
	for i, path := range paths {
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			t.Fatal(err)
		}
		copies[i] = filepath.Join(dir, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(copies[i]), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(copies[i], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return copies
}

// ReadFixtures returns the absolute paths of the committed fixtures of the repository at repo, in
// the order its list, stages/testdata/fixtures.tsv, names them. It fails matching [ErrNoFixtures],
// naming the list, when the list is absent, as it is outside a docproc checkout, and names every
// fixture the list names that is missing from the tree.
func ReadFixtures(repo string) ([]string, error) {
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	list := filepath.Join(repo, filepath.FromSlash(fixtureList))
	f, err := os.Open(list)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s is absent; the tier runs from a docproc checkout", ErrNoFixtures, list)
	}
	if err != nil {
		return nil, fmt.Errorf("integration: %w", err)
	}
	defer func() { _ = f.Close() }()
	var paths, missing []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		rel, _, _ := strings.Cut(s.Text(), "\t")
		if rel == "" {
			continue
		}
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, rel)
			continue
		}
		paths = append(paths, path)
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("integration: %s: %w", list, err)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("integration: %s names fixtures missing from %s: %s", list, repo, strings.Join(missing, ", "))
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("integration: %s names no fixture", list)
	}
	return paths, nil
}
