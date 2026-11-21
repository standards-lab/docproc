//go:build linux

package tool_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/tool"
)

func TestCancelTerminates(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := newReadyWriter()
	done := runAsync(ctx, tl, tool.Invocation{Args: []string{"sleep", "1m"}, Stdout: ready})
	ready.wait(t)
	start := time.Now()
	cancel()
	err := <-done
	assertContextError(t, err, context.Canceled)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Run took %v after cancel, want well under the 10 s KillAfter period", elapsed)
	}
}

func TestCancelKillsAfterKillAfter(t *testing.T) {
	t.Parallel()
	link := installStandin(t)
	const killAfter = 200 * time.Millisecond
	spec := standinSpec(link, "1.0", "1.0")
	spec.KillAfter = killAfter
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := newReadyWriter()
	done := runAsync(ctx, tl, tool.Invocation{Args: []string{"stubborn", "1m"}, Stdout: ready})
	ready.wait(t)
	start := time.Now()
	cancel()
	err = <-done
	elapsed := time.Since(start)
	assertContextError(t, err, context.Canceled)
	if elapsed < killAfter {
		t.Errorf("Run took %v after cancel, want at least the %v KillAfter period", elapsed, killAfter)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Run took %v after cancel, want about the %v KillAfter period", elapsed, killAfter)
	}
}

func TestCancelEndsChild(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	done := runAsync(ctx, tl, tool.Invocation{Args: []string{"spawn", pidFile}})
	pid := readPid(t, pidFile)
	start := time.Now()
	cancel()
	assertContextError(t, <-done, context.Canceled)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Run took %v after cancel, want well under the 10 s KillAfter period", elapsed)
	}
	assertGone(t, pid)
}

func TestCancelEndsChildHoldingOutput(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	done := runAsync(ctx, tl, tool.Invocation{Args: []string{"spawn", pidFile, "held"}, Stdout: newReadyWriter()})
	pid := readPid(t, pidFile)
	start := time.Now()
	cancel()
	assertContextError(t, <-done, context.Canceled)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Run took %v after cancel, want well under the 10 s KillAfter period", elapsed)
	}
	assertGone(t, pid)
}

func TestDeadlineExceeded(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err := tl.Run(ctx, tool.Invocation{Args: []string{"sleep", "1m"}})
	assertContextError(t, err, context.DeadlineExceeded)
}

func TestRunContextAlreadyDone(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := tl.Run(ctx, tool.Invocation{Args: []string{"sleep", "1m"}})
	assertContextError(t, err, context.Canceled)
}

func TestCancelErrorNamesContextOnce(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"exec":      "tool: standin: context canceled (signal: killed)",
		"container": "tool: standin: context canceled (exit status 137)",
	}
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			tl, leftover := build(t, time.Second)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := newReadyWriter()
			done := runAsync(ctx, tl, tool.Invocation{Args: []string{"stubborn", "1m"}, Stdout: ready})
			ready.wait(t)
			cancel()
			err := <-done
			assertContextError(t, err, context.Canceled)
			if err.Error() != want[label] {
				t.Errorf("Run error = %q, want %q", err, want[label])
			}
			leftover(t)
		})
	}
}

func runAsync(ctx context.Context, tl *tool.Tool, inv tool.Invocation) <-chan error {
	done := make(chan error, 1)
	go func() { done <- tl.Run(ctx, inv) }()
	return done
}

func assertContextError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want one matching %v", err, want)
	}
	if _, ok := errors.AsType[*tool.ExitError](err); ok {
		t.Errorf("Run error %q is an ExitError, want only the context's", err)
	}
}

type readyWriter struct {
	once  sync.Once
	ready chan struct{}
}

func newReadyWriter() *readyWriter { return &readyWriter{ready: make(chan struct{})} }

func (w *readyWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return len(p), nil
}

func (w *readyWriter) wait(t *testing.T) {
	t.Helper()
	select {
	case <-w.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the stand-in never wrote ready")
	}
}

func readPid(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(file); err == nil && bytes.HasSuffix(b, []byte("\n")) {
			pid, err := strconv.Atoi(string(bytes.TrimSpace(b)))
			if err != nil {
				t.Fatalf("pid file %q: %v", b, err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no pid in %s", file)
	return 0
}

func assertGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) || zombie(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("child %d outlived the cancelled run", pid)
}

func zombie(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(stat, ')')
	return i >= 0 && i+2 < len(stat) && stat[i+2] == 'Z'
}
