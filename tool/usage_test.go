//go:build linux

package tool_test

import (
	"context"
	"errors"
	"testing"

	"github.com/standards-lab/docproc/tool"
)

func assertUsage(t *testing.T, u tool.Usage) {
	t.Helper()
	if u.UserCPU < spinCPU {
		t.Errorf("UserCPU = %v, want at least the spin's %v", u.UserCPU, spinCPU)
	}
}

func TestRunUsage(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	var u tool.Usage
	err := tl.Run(t.Context(), tool.Invocation{Args: []string{"spin", "print", "done"}, Usage: &u})
	if err != nil {
		t.Fatal(err)
	}
	assertUsage(t, u)
}

func TestRunUsageOnExitError(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	var u tool.Usage
	err := tl.Run(t.Context(), tool.Invocation{Args: []string{"spin", "exit", "3"}, Usage: &u})
	if _, ok := errors.AsType[*tool.ExitError](err); !ok {
		t.Fatalf("Run error = %v, want a *tool.ExitError", err)
	}
	assertUsage(t, u)
}

func TestRunUsageOnCancel(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var u tool.Usage
	ready := newReadyWriter()
	done := runAsync(ctx, tl, tool.Invocation{Args: []string{"spin", "sleep", "1m"}, Stdout: ready, Usage: &u})
	ready.wait(t)
	cancel()
	assertContextError(t, <-done, context.Canceled)
	assertUsage(t, u)
}

func TestRunUsageCountsReapedChildren(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	var u tool.Usage
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"reap", "spin", "print", "done"}, Usage: &u}); err != nil {
		t.Fatal(err)
	}
	assertUsage(t, u)
}

func TestRunNilUsage(t *testing.T) {
	t.Parallel()
	tl := newStandin(t)
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"print", "done"}}); err != nil {
		t.Fatal(err)
	}
}

func TestContainerUsageZero(t *testing.T) {
	t.Parallel()
	dir := installDocker(t, "29.0.1")
	addImage(t, dir, testRef)
	tl := newContainer(t, dir, tool.Image{Ref: testRef})
	u := tool.Usage{UserCPU: 1, SystemCPU: 1}
	if err := tl.Run(t.Context(), tool.Invocation{Args: []string{"spin", "print", "done"}, Usage: &u}); err != nil {
		t.Fatal(err)
	}
	if u != (tool.Usage{}) {
		t.Errorf("Usage = %+v, want zero", u)
	}
}
