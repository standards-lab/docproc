//go:build linux

package tool_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/standards-lab/docproc/tool"
)

const largeStdin = 1 << 20

type runnerCase func(t *testing.T, killAfter time.Duration) (*tool.Tool, func(t *testing.T))

var runnerCases = map[string]runnerCase{
	"exec": func(t *testing.T, killAfter time.Duration) (*tool.Tool, func(t *testing.T)) {
		link := installStandin(t)
		spec := standinSpec(link, "1.0", "1.0")
		spec.KillAfter = killAfter
		tl, err := tool.NewExec(t.Context(), spec)
		if err != nil {
			t.Fatal(err)
		}
		return tl, func(*testing.T) {}
	},
	"container": func(t *testing.T, killAfter time.Duration) (*tool.Tool, func(t *testing.T)) {
		dir := installDocker(t, "29.0.1")
		addImage(t, dir, testRef)
		spec := standinSpec("standin", "1.2.3", "1.0")
		spec.KillAfter = killAfter
		tl, err := tool.NewContainer(t.Context(), dockerLink(dir), tool.Image{Ref: testRef}, spec)
		if err != nil {
			t.Fatal(err)
		}
		return tl, func(t *testing.T) {
			t.Helper()
			if running := runningContainers(t, dir); len(running) != 0 {
				t.Errorf("containers %q outlived the run", running)
			}
		}
	},
}

func awaitRun(t *testing.T, done <-chan error, start time.Time, within time.Duration) (time.Duration, error) {
	t.Helper()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(within):
		t.Fatalf("Run hadn't returned after %v", within)
		return 0, nil
	}
}

func TestCancelHeldReaderStdin(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			tl, assertNothingRunning := build(t, standinKillAfter)
			stdin, held := io.Pipe()
			defer func() { _ = held.Close() }()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := newReadyWriter()
			args := []string{"sleep", "1m"}
			var pidFile string
			pid := 0
			if label == "exec" {
				pidFile = filepath.Join(t.TempDir(), "child.pid")
				args = []string{"spawn", pidFile}
			}
			done := runAsync(ctx, tl, tool.Invocation{Args: args, Stdin: stdin, Stdout: ready})
			ready.wait(t)
			if pidFile != "" {
				pid = readPid(t, pidFile)
			}
			start := time.Now()
			cancel()
			elapsed, err := awaitRun(t, done, start, 15*time.Second)
			assertContextError(t, err, context.Canceled)
			if elapsed > 5*time.Second {
				t.Errorf("Run took %v after cancel, want well under the 10 s KillAfter period", elapsed)
			}
			if pid != 0 {
				assertGone(t, pid)
			}
			assertNothingRunning(t)
		})
	}
}

func TestRunUnreadHeldReaderStdin(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			tl, assertNothingRunning := build(t, 200*time.Millisecond)
			stdin, held := io.Pipe()
			defer func() { _ = held.Close() }()
			var stdout bytes.Buffer
			start := time.Now()
			done := runAsync(t.Context(), tl, tool.Invocation{Args: []string{"print", "x"}, Stdin: stdin, Stdout: &stdout})
			elapsed, err := awaitRun(t, done, start, 15*time.Second)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if elapsed > 5*time.Second {
				t.Errorf("Run took %v, want the bound on the copy, a few seconds at most", elapsed)
			}
			if stdout.String() != "x\n" {
				t.Errorf("stdout = %q, want %q", stdout.String(), "x\n")
			}
			assertNothingRunning(t)
		})
	}
}

func TestRunUnreadLargeStdin(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			tl, assertNothingRunning := build(t, standinKillAfter)
			start := time.Now()
			done := runAsync(t.Context(), tl, tool.Invocation{Args: []string{"print", "x"}, Stdin: bytes.NewReader(letters(largeStdin))})
			elapsed, err := awaitRun(t, done, start, 15*time.Second)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if elapsed > 5*time.Second {
				t.Errorf("Run took %v, want well under the 10 s KillAfter period", elapsed)
			}
			assertNothingRunning(t)
		})
	}
}

func TestRunLargeStdin(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			tl, _ := build(t, 0)
			want := letters(largeStdin)
			var stdout bytes.Buffer
			err := tl.Run(t.Context(), tool.Invocation{Args: []string{"echo"}, Stdin: bytes.NewReader(want), Stdout: &stdout})
			if err != nil {
				t.Fatal(err)
			}
			var got echo
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("decoding %d bytes of stdout: %v", stdout.Len(), err)
			}
			if got.Stdin != string(want) {
				t.Errorf("the program read %d bytes of stdin, want all %d intact", len(got.Stdin), len(want))
			}
		})
	}
}

var errStdinRead = errors.New("stdin read failed")

type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, errStdinRead
	}
	return n, err
}

func TestRunStdinReadError(t *testing.T) {
	t.Parallel()
	for label, build := range runnerCases {
		t.Run(label, func(t *testing.T) {
			tl, _ := build(t, 0)
			var stdout bytes.Buffer
			err := tl.Run(t.Context(), tool.Invocation{
				Args:   []string{"echo"},
				Stdin:  &failingReader{r: bytes.NewReader([]byte("partial"))},
				Stdout: &stdout,
			})
			if !errors.Is(err, errStdinRead) {
				t.Fatalf("Run error = %v, want one matching %v", err, errStdinRead)
			}
			var got echo
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("decoding %q: %v", stdout.String(), err)
			}
			if got.Stdin != "partial" {
				t.Errorf("the program read stdin %q, want %q", got.Stdin, "partial")
			}
		})
	}
}
