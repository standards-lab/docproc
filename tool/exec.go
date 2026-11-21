//go:build linux

package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

// NewExec builds a Tool that runs spec's program directly, the exec runner. It validates the
// spec, then finds the program on PATH, or takes a path containing a separator as given, and,
// for a spec with a MemoryLimit, util-linux's prlimit on PATH; then it runs the version command
// under ctx, through prlimit under the bound and in the fixed environment, as every run is, with
// HOME a new empty directory of its own, and refuses a program it can't find, whose
// version command fails, or whose version is unmatched or older than the minimum. A spec with a
// MemoryLimit and no prlimit on PATH is refused.
//
// Every run starts the program as the leader of a process group of its own, with:
//
//   - a fixed environment, never the host's: PATH as it was at construction, LANG=C.UTF-8, HOME
//     the invocation's Dir, made absolute, when it has one, and TMPDIR a new empty directory of
//     the run's own, docproc-run-*, under the process's temporary directory, os.TempDir, which the
//     host's TMPDIR steers; then spec's Env, a key there replacing the fixed one's;
//   - the memory bound, when spec has one, as RLIMIT_AS, set by `prlimit --as=<bytes> --` before
//     the program is executed, so it holds from the program's first allocation;
//   - SIGKILL as its parent-death signal, from an OS thread locked to the run, so a host that
//     dies, even by SIGKILL, takes the program with it;
//   - an oom_score_adj of 1000, so the kernel's OOM killer takes the program before its host.
//     It is written to /proc once the program has started, so the program runs its first
//     instructions, prlimit's, before it holds, and a host without /proc, or a program that has
//     already exited, leaves it unset.
//
// Whenever the leader exits, its group is sent SIGKILL before the leader is reaped, so no child it
// started outlives the run, whether the run succeeded, failed, or was ended. The run's TMPDIR is
// removed once the leader is reaped, with whatever the program left in it, so a program ended
// early leaves its temporary files neither beside its input nor under the host's. A run whose
// TMPDIR can't be made fails before the program starts.
func NewExec(ctx context.Context, spec Spec) (*Tool, error) {
	name := filepath.Base(spec.Program)
	minimum, err := spec.validate(name)
	if err != nil {
		return nil, err
	}
	path, err := lookPath(spec.Program)
	if err != nil {
		return nil, err
	}
	cfg := execConfig{
		name: name, path: path, killAfter: spec.KillAfter, lockdown: true,
		fixed: []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}, env: slices.Clone(spec.Env),
	}
	if spec.MemoryLimit > 0 {
		prlimit, err := exec.LookPath("prlimit")
		if err != nil {
			return nil, fmt.Errorf("tool: %s: a memory limit needs util-linux's prlimit on PATH: %w", name, err)
		}
		cfg.path = prlimit
		cfg.lead = []string{"--as=" + strconv.FormatInt(spec.MemoryLimit, 10), "--", path}
	}
	version, line, err := execVersion(ctx, name, spec, minimum, cfg)
	if err != nil {
		return nil, err
	}
	return &Tool{name: name, version: version, line: line, timeout: spec.Timeout, idle: spec.Idle, run: execRun(cfg)}, nil
}

func execVersion(ctx context.Context, name string, spec Spec, minimum Version, cfg execConfig) (Version, string, error) {
	home, err := os.MkdirTemp("", "docproc-version-")
	if err != nil {
		return Version{}, "", fmt.Errorf("tool: %s: the version command's home: %w", name, err)
	}
	defer func() { _ = os.RemoveAll(home) }()
	cfg.fixed = slices.Concat(cfg.fixed, []string{"HOME=" + home})
	return checkVersion(ctx, name, spec, minimum, execRun(cfg))
}

type execConfig struct {
	name      string
	path      string
	lead      []string
	killAfter time.Duration
	lockdown  bool
	fixed     []string
	env       []string
}

const oomScoreAdj = "1000"

// See docs/api.md, "Running the tools".
func execRun(cfg execConfig) runFunc {
	name, killAfter := cfg.name, cfg.killAfter
	return func(ctx context.Context, inv Invocation) error {
		cmd := exec.CommandContext(ctx, cfg.path, slices.Concat(cfg.lead, inv.Args)...)
		cmd.Dir = inv.Dir
		if cfg.lockdown {
			tmp, err := os.MkdirTemp("", "docproc-run-")
			if err != nil {
				return fmt.Errorf("tool: %s: temporary directory: %w", name, err)
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			env, err := environ(cfg.fixed, cfg.env, inv.Dir, tmp)
			if err != nil {
				return fmt.Errorf("tool: %s: %w", name, err)
			}
			cmd.Env = env
		}
		cmd.Stdin = inv.Stdin
		var feed *stdinFeed
		if _, ok := inv.Stdin.(*os.File); inv.Stdin != nil && !ok {
			var err error
			if feed, err = newStdinFeed(inv.Stdin); err != nil {
				return fmt.Errorf("tool: %s: stdin: %w", name, err)
			}
			cmd.Stdin = feed.r
		}
		cmd.Stdout = inv.Stdout
		var tail *tailWriter
		if inv.Stderr != nil {
			cmd.Stderr = inv.Stderr
		} else {
			tail = &tailWriter{n: stderrTail}
			cmd.Stderr = tail
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
		cmd.Cancel = func() error {
			err := signalGroup(cmd.Process, syscall.SIGTERM)
			if killAfter <= 0 {
				err = signalGroup(cmd.Process, syscall.SIGKILL)
			}
			return err
		}
		if killAfter > 0 {
			cmd.WaitDelay = killAfter
		}
		err := startAndWait(cmd, feed, cfg.lockdown)
		if inv.Usage != nil {
			*inv.Usage = usage(cmd.ProcessState)
		}
		if feed != nil {
			if ferr := feed.finish(ctx, killAfter); ferr != nil && err == nil {
				err = fmt.Errorf("stdin: %w", ferr)
			}
		}
		var exit *ExitError
		if state := cmd.ProcessState; state != nil && err != nil {
			exit = &ExitError{Program: name, Code: state.ExitCode(), Signal: waitSignal(state), err: err}
			if tail != nil {
				exit.Message = string(tail.buf)
			}
		}
		if err != nil && ctx.Err() != nil {
			return &cancelError{program: name, ctxErr: ctx.Err(), outcome: err, exit: exit}
		}
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return exit
		}
		if err != nil {
			return fmt.Errorf("tool: %s: %w", name, err)
		}
		return nil
	}
}

func startAndWait(cmd *exec.Cmd, feed *stdinFeed, lockdown bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := cmd.Start()
	if feed != nil {
		feed.start(err == nil)
	}
	if err != nil {
		return err
	}
	if lockdown {
		_ = os.WriteFile("/proc/"+strconv.Itoa(cmd.Process.Pid)+"/oom_score_adj", []byte(oomScoreAdj), 0)
	}
	if awaitExit(cmd.Process.Pid) == nil {
		_ = signalGroup(cmd.Process, syscall.SIGKILL)
	}
	return cmd.Wait()
}

func environ(fixed, env []string, dir, tmp string) ([]string, error) {
	var home []string
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("working directory %s: %w", dir, err)
		}
		home = []string{"HOME=" + abs}
	}
	return slices.Concat(fixed, home, []string{"TMPDIR=" + tmp}, env), nil
}

func waitSignal(state *os.ProcessState) string {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return signalName(int(status.Signal()))
}

func usage(state *os.ProcessState) Usage {
	if state == nil {
		return Usage{}
	}
	ru, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil {
		return Usage{}
	}
	return Usage{
		UserCPU:   time.Duration(ru.Utime.Nano()),
		SystemCPU: time.Duration(ru.Stime.Nano()),
	}
}

type stdinFeed struct {
	src  io.Reader
	r, w *os.File
	done chan error
}

func newStdinFeed(src io.Reader) (*stdinFeed, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &stdinFeed{src: src, r: r, w: w}, nil
}

func (f *stdinFeed) start(started bool) {
	_ = f.r.Close()
	if !started {
		_ = f.w.Close()
		return
	}
	f.done = make(chan error, 1)
	go func() {
		_, err := io.Copy(f.w, f.src)
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed) {
			err = nil
		}
		f.done <- err
		_ = f.w.Close()
	}()
}

func (f *stdinFeed) finish(ctx context.Context, killAfter time.Duration) error {
	if f.done == nil {
		return nil
	}
	select {
	case err := <-f.done:
		return err
	default:
	}
	if ctx.Err() == nil && killAfter > 0 {
		timer := time.NewTimer(killAfter)
		defer timer.Stop()
		select {
		case err := <-f.done:
			return err
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	_ = f.w.Close()
	return nil
}

const pPID = 1

func awaitExit(pid int) error {
	var info [128]byte
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(pid),
			uintptr(unsafe.Pointer(&info)), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno != syscall.EINTR {
			if errno != 0 {
				return errno
			}
			return nil
		}
	}
}

func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
