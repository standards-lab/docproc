//go:build linux

package tool_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

func TestRunSignal(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		signal string
		code   map[string]int
	}{
		"TERM": {"SIGTERM", map[string]int{"exec": -1, "container": 143}},
		"KILL": {"SIGKILL", map[string]int{"exec": -1, "container": 137}},
	}
	for label, build := range runnerCases {
		for sig, tt := range tests {
			t.Run(label+"/"+sig, func(t *testing.T) {
				t.Parallel()
				tl, leftover := build(t, standinKillAfter)
				err := tl.Run(t.Context(), tool.Invocation{Args: []string{"die", sig}})
				exitErr, ok := errors.AsType[*tool.ExitError](err)
				if !ok {
					t.Fatalf("Run error = %v, want a *tool.ExitError", err)
				}
				if exitErr.Signal != tt.signal || exitErr.Code != tt.code[label] || exitErr.TimedOut {
					t.Errorf("ExitError = %+v, want signal %s, code %d, not timed out", exitErr, tt.signal, tt.code[label])
				}
				if want := "tool: standin: killed by " + tt.signal; err.Error() != want {
					t.Errorf("Run error = %q, want %q", err, want)
				}
				leftover(t)
			})
		}
	}
}

func TestRunExitHasNoSignal(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			tl, _ := build(t, standinKillAfter)
			err := tl.Run(t.Context(), tool.Invocation{Args: []string{"exit", "128"}})
			if exitErr, ok := errors.AsType[*tool.ExitError](err); !ok || exitErr.Code != 128 || exitErr.Signal != "" {
				t.Errorf("Run error = %v, want exit code 128 and no signal", err)
			}
		})
	}
}

const memoryLimit = tooltest.Memory

func TestMemoryLimit(t *testing.T) {
	t.Parallel()
	spec := standinSpec(installStandin(t), "", "1.0")
	limit := strconv.Itoa(memoryLimit)
	spec.VersionArgs = []string{"as-print", limit, "standin version 1.0"}
	spec.MemoryLimit = memoryLimit
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatalf("NewExec: %v", err)
	}
	if tl.Name() != "standin" {
		t.Errorf("Name() = %q, want standin, not prlimit", tl.Name())
	}
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"as-print", limit, "bounded"}}); err != nil {
		t.Errorf("a run's RLIMIT_AS: %v, want the bound, %s", err, limit)
	}
	var out bytes.Buffer
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"alloc", strconv.Itoa(1 << 20)}, Stdout: &out}); err != nil || out.String() != "allocated\n" {
		t.Errorf("1 MiB under the bound: %v, %q; want allocated", err, out.String())
	}
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"alloc", strconv.Itoa(4 * memoryLimit)}})
	exitErr, ok := errors.AsType[*tool.ExitError](err)
	if !ok || exitErr.Program != "standin" || exitErr.Code != 3 || !strings.Contains(exitErr.Message, "cannot allocate memory") {
		t.Errorf("four times the bound: %v, want the mapping refused", err)
	}
	if err := newStandin(t).Run(t.Context(), tool.Invocation{Args: []string{"alloc", strconv.Itoa(4 * memoryLimit)}}); err != nil {
		t.Errorf("four times the bound, unbounded: %v, want it mapped", err)
	}
}

func TestMemoryLimitNeedsPrlimit(t *testing.T) {
	link := installStandin(t)
	t.Setenv("PATH", t.TempDir())
	spec := standinSpec(link, "1.0", "1.0")
	spec.MemoryLimit = memoryLimit
	if _, err := tool.NewExec(t.Context(), spec); err == nil || !strings.Contains(err.Error(), "prlimit") {
		t.Errorf("NewExec without prlimit on PATH: %v, want an error naming prlimit", err)
	}
	spec.MemoryLimit = 0
	if _, err := tool.NewExec(t.Context(), spec); err != nil {
		t.Errorf("NewExec unbounded without prlimit on PATH: %v", err)
	}
}

func TestMemoryLimitBelowZero(t *testing.T) {
	t.Parallel()
	spec := standinSpec("missing-tool", "1.0", "1.0")
	spec.MemoryLimit = -1
	if _, err := tool.NewExec(t.Context(), spec); err == nil || !strings.Contains(err.Error(), "memory limit -1") {
		t.Errorf("NewExec: %v, want the negative bound refused", err)
	}
}

func TestRunFixedEnvironment(t *testing.T) {
	t.Parallel()
	spec := standinSpec(installStandin(t), "1.0", "1.0")
	spec.Env = []string{"DOCPROC_TEST=1", "LANG=en_US.UTF-8"}
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := "PATH=" + os.Getenv("PATH")
	tests := map[string]struct {
		dir  string
		want []string
	}{
		"no dir": {"", []string{"DOCPROC_TEST=1", "LANG=en_US.UTF-8", path, "TMPDIR="}},
		"dir":    {dir, []string{"DOCPROC_TEST=1", "HOME=" + dir, "LANG=en_US.UTF-8", path, "TMPDIR="}},
	}
	for label, tt := range tests {
		t.Run(label, func(t *testing.T) {
			if got := runEnv(t, tl, tt.dir); !slices.Equal(got, tt.want) {
				t.Errorf("environment =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestRunFixedEnvironmentLang(t *testing.T) {
	t.Parallel()
	got := runEnv(t, newStandin(t), "")
	if want := []string{"LANG=C.UTF-8", "PATH=" + os.Getenv("PATH"), "TMPDIR="}; !slices.Equal(got, want) {
		t.Errorf("environment = %q, want %q", got, want)
	}
}

func runEnv(t *testing.T, tl *tool.Tool, dir string) []string {
	t.Helper()
	var out bytes.Buffer
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"env"}, Dir: dir, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decoding %q: %v", out.String(), err)
	}
	for i, kv := range got {
		if tmp, ok := strings.CutPrefix(kv, "TMPDIR="); ok {
			if filepath.Dir(tmp) != os.TempDir() || !strings.HasPrefix(filepath.Base(tmp), "docproc-run-") {
				t.Errorf("TMPDIR = %s, want docproc-run-* under %s", tmp, os.TempDir())
			}
			got[i] = "TMPDIR="
		}
	}
	return got
}

func TestRunOOMScoreAdj(t *testing.T) {
	t.Parallel()
	own, err := os.ReadFile("/proc/self/oom_score_adj")
	if err != nil {
		t.Skipf("no oom_score_adj to read: %v", err)
	}
	if strings.TrimSpace(string(own)) == "1000" {
		t.Skip("the test's own oom_score_adj is already 1000, which the program would inherit")
	}
	tl := newStandin(t)
	var out bytes.Buffer
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"oom", "1000"}, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "1000" {
		t.Errorf("oom_score_adj = %q, want 1000", got)
	}
}

func TestRunKillsChildLeftRunning(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"orphan", pidFile}}); err != nil {
		t.Fatal(err)
	}
	assertGone(t, readPid(t, pidFile))
}

func TestRunChildHoldingStdout(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	var out bytes.Buffer
	done := runAsync(t.Context(), tl, tool.Invocation{Args: []string{"orphan", pidFile, "held"}, Stdout: &out})
	elapsed, err := awaitRun(t, done, time.Now(), 2*standinKillAfter)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > standinKillAfter/2 {
		t.Errorf("Run took %v, want well under the %v KillAfter period", elapsed, standinKillAfter)
	}
	if out.String() != "ready\n" {
		t.Errorf("stdout = %q, want the child's ready", out.String())
	}
	assertGone(t, readPid(t, pidFile))
}

func TestRunTimeout(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			tl, leftover := build(t, time.Second)
			err := tl.Run(t.Context(), tool.Invocation{Args: []string{"stubborn", "1m"}, Timeout: 2 * time.Second})
			exitErr, ok := errors.AsType[*tool.ExitError](err)
			if !ok || !exitErr.TimedOut || exitErr.Signal != "SIGKILL" {
				t.Fatalf("Run error = %v, want a timed-out ExitError killed by SIGKILL", err)
			}
			if !strings.HasPrefix(err.Error(), "tool: standin: timed out, killed by SIGKILL") {
				t.Errorf("Run error = %q, want it to say it timed out", err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Run error %q matches the context's deadline, which didn't pass", err)
			}
			leftover(t)
		})
	}
}

func TestRunTimeoutExitZero(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	err := tl.Run(t.Context(), tool.Invocation{Args: []string{"sleep", "1m"}, Timeout: 2 * time.Second})
	exitErr, ok := errors.AsType[*tool.ExitError](err)
	if !ok || !exitErr.TimedOut || exitErr.Code != 0 || exitErr.Signal != "" {
		t.Errorf("Run error = %v, want a timed-out ExitError with exit code 0", err)
	}
}

func TestRunIdle(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	err := tl.Run(t.Context(), tool.Invocation{Args: []string{"sleep", "1m"}, Idle: 300 * time.Millisecond})
	if exitErr, ok := errors.AsType[*tool.ExitError](err); !ok || !exitErr.TimedOut {
		t.Errorf("an idle run: %v, want a timed-out ExitError", err)
	}
	var out bytes.Buffer
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"tick", "10", "50ms"}, Stdout: &out, Idle: time.Second})
	if err != nil || strings.Count(out.String(), "tick\n") != 10 {
		t.Errorf("a run writing every 50 ms under a 1 s Idle: %v, %q", err, out.String())
	}
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"exit", "3", "it broke"}, Idle: time.Second})
	if exitErr, ok := errors.AsType[*tool.ExitError](err); !ok || exitErr.TimedOut || exitErr.Message != "it broke" {
		t.Errorf("a failing run under an Idle: %v, want its exit and stderr", err)
	}
}

func TestSpecBounds(t *testing.T) {
	t.Parallel()
	spec := standinSpec(installStandin(t), "1.0", "1.0")
	spec.Timeout = 200 * time.Millisecond
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := tl.Timeout(); got != spec.Timeout {
		t.Errorf("Timeout() = %v, want the Spec's %v", got, spec.Timeout)
	}
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"sleep", "1m"}})
	if exitErr, ok := errors.AsType[*tool.ExitError](err); !ok || !exitErr.TimedOut {
		t.Errorf("a run under the Spec's Timeout: %v, want a timed-out ExitError", err)
	}
	var out bytes.Buffer
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"tick", "5", "100ms"}, Stdout: &out, Idle: time.Second})
	if err != nil || strings.Count(out.String(), "tick\n") != 5 {
		t.Errorf("a run with its own Idle, past the Spec's Timeout: %v, %q", err, out.String())
	}

	spec.Timeout, spec.Idle = 0, 300*time.Millisecond
	if tl, err = tool.NewExec(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	err = tl.Run(t.Context(), tool.Invocation{Args: []string{"sleep", "1m"}})
	if exitErr, ok := errors.AsType[*tool.ExitError](err); !ok || !exitErr.TimedOut {
		t.Errorf("a run under the Spec's Idle: %v, want a timed-out ExitError", err)
	}

	spec.Idle = -time.Second
	if _, err := tool.NewExec(t.Context(), spec); err == nil || !strings.Contains(err.Error(), "below zero") {
		t.Errorf("a negative Idle: %v, want it refused", err)
	}
}

func TestVersionLine(t *testing.T) {
	t.Parallel()
	spec := standinSpec(installStandin(t), "1.0", "1.0")
	spec.VersionArgs = []string{"print", "  build 7\nstandin version 1.2.3-dev+abc (2026-11-16)  \nlast"}
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tl.VersionLine(), "standin version 1.2.3-dev+abc (2026-11-16)"; got != want || tl.Version().String() != "1.2.3" {
		t.Errorf("VersionLine %q, Version %s; want %q, 1.2.3", got, tl.Version(), want)
	}
}

func TestRunTimeoutContextFirst(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err := tl.Run(ctx, tool.Invocation{Args: []string{"sleep", "1m"}, Timeout: time.Minute})
	assertContextError(t, err, context.DeadlineExceeded)
}

func TestCancelNoGrace(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			tl, leftover := build(t, 0)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := newReadyWriter()
			done := runAsync(ctx, tl, tool.Invocation{Args: []string{"stubborn", "1m"}, Stdout: ready})
			ready.wait(t)
			start := time.Now()
			cancel()
			elapsed, err := awaitRun(t, done, start, 10*time.Second)
			assertContextError(t, err, context.Canceled)
			if elapsed > 3*time.Second {
				t.Errorf("Run took %v after cancel, want SIGKILL at once", elapsed)
			}
			leftover(t)
		})
	}
}

func TestAtLeast(t *testing.T) {
	t.Parallel()
	tl, err := tool.NewExec(t.Context(), standinSpec(installStandin(t), "1.2.3", "1.0"))
	if err != nil {
		t.Fatal(err)
	}
	for _, minimum := range []string{"1.0", "1.2.3", "1.2"} {
		if err := tl.AtLeast(minimum); err != nil {
			t.Errorf("AtLeast(%s) = %v, want nil", minimum, err)
		}
	}
	err = tl.AtLeast("1.10")
	verr, ok := errors.AsType[*tool.VersionError](err)
	if !ok || !errors.Is(err, tool.ErrTooOld) || verr.Found != "1.2.3" || verr.Minimum != "1.10" {
		t.Errorf("AtLeast(1.10) = %v, want a VersionError wrapping ErrTooOld", err)
	}
	if err := tl.AtLeast("v2"); err == nil || errors.Is(err, tool.ErrTooOld) {
		t.Errorf("AtLeast(v2) = %v, want the minimum refused", err)
	}
}

func TestContainerMemoryLimit(t *testing.T) {
	t.Parallel()
	dir := installDocker(t, "29.0.1")
	addImage(t, dir, testRef)
	spec := standinSpec("standin", "1.2.3", "1.0")
	spec.MemoryLimit = memoryLimit
	tl, err := tool.NewContainer(t.Context(), dockerLink(dir), tool.Image{Ref: testRef}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"print", "x"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=4096",
		"--memory=" + strconv.Itoa(memoryLimit), "--pull=never",
	}
	runs := dockerCalls(t, dir, "run")
	if len(runs) != 2 {
		t.Fatalf("run calls = %q, want the version command's and the run's", runs)
	}
	for _, run := range runs {
		if i := slices.Index(run, want[0]); i < 0 || !slices.Equal(run[i:i+len(want)], want) {
			t.Errorf("run args = %q, want them to hold %q", run, want)
		}
	}
}
