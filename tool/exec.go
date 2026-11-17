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
// under ctx, through prlimit under the bound as every run is, and refuses a program it can't
// find, whose version command fails, or whose version is unmatched or older than the minimum. A
// spec with a MemoryLimit and no prlimit on PATH is refused.
//
// Every run starts the program as the leader of a process group of its own, with:
//
//   - a fixed environment, never the host's: PATH as it was at construction, LANG=C.UTF-8, and
//     HOME and TMPDIR the invocation's Dir, made absolute, when it has one; then spec's Env, a key
//     there replacing the fixed one's;
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
// started outlives the run, whether the run succeeded, failed, or was ended.
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
	run := execRun(cfg)
	version, line, err := checkVersion(ctx, name, spec, minimum, run)
	if err != nil {
		return nil, err
	}
	return &Tool{name: name, version: version, line: line, timeout: spec.Timeout, idle: spec.Idle, run: run}, nil
}

// execConfig is what the exec runner runs, and how.
type execConfig struct {
	// name is the program's name, for errors.
	name string
	// path is what is executed: the program, or prlimit leading it.
	path string
	// lead are the arguments before the invocation's: prlimit's, ending with the program's path.
	lead []string
	// killAfter is the time between SIGTERM and SIGKILL for a run that is ended.
	killAfter time.Duration
	// lockdown gives the program the fixed environment and env, and an oom_score_adj of 1000. The
	// container runner's docker CLI runs without it, with the host's environment, which holds
	// its daemon's address and its configuration, and the host's oom_score_adj.
	lockdown bool
	// fixed is the fixed environment, without HOME and TMPDIR, which each run's Dir gives.
	fixed []string
	// env is the spec's Env, which follows the fixed environment.
	env []string
}

// oomScoreAdj is the oom_score_adj a locked-down program takes: the most, so the OOM killer
// chooses it first.
const oomScoreAdj = "1000"

// execRun returns the exec runner's runFunc for cfg, which runs cfg.path with cfg.lead and then
// the invocation's arguments, and gives a run that ctx ends cfg.killAfter between SIGTERM and
// SIGKILL.
//
// The program leads a process group of its own, so an end reaches everything it started: SIGTERM
// to the group when ctx ends; SIGKILL to the program once killAfter passes (WaitDelay), or with
// the SIGTERM when killAfter is zero; and, once the program has exited but before it is reaped,
// SIGKILL to the group, so no child outlives it.
//
// An *os.File stdin passes straight to the program; any other reader goes through a stdinFeed,
// so a run never waits on a Read of the caller's.
func execRun(cfg execConfig) runFunc {
	name, killAfter := cfg.name, cfg.killAfter
	return func(ctx context.Context, inv Invocation) error {
		cmd := exec.CommandContext(ctx, cfg.path, slices.Concat(cfg.lead, inv.Args)...)
		cmd.Dir = inv.Dir
		if cfg.lockdown {
			env, err := environ(cfg.fixed, cfg.env, inv.Dir)
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
		// WaitDelay's timer starts when ctx ends, in Start's own goroutine, so it SIGKILLs a
		// program that outlasts killAfter while awaitExit blocks. Zero leaves the pipes to be read
		// to their end, which the group's SIGKILL brings once the program has exited.
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
		// The program's exit, when it was reaped: os/exec reports an exit 0 after ctx ended as
		// ctx's error, so the facts come from the process state, not err.
		var exit *ExitError
		if state := cmd.ProcessState; state != nil && err != nil {
			exit = &ExitError{Program: name, Code: state.ExitCode(), Signal: waitSignal(state), err: err}
			if tail != nil {
				exit.Message = string(tail.buf)
			}
		}
		if err != nil && ctx.Err() != nil {
			// ctx ended the run: its error is the cause, whatever the program's exit.
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

// startAndWait starts cmd and waits for it, on an OS thread locked to the run from the start to
// the reaping, since the parent-death signal is sent when the thread that started the program
// exits, not the process. A stdin feed, when the run has one, starts its copy once the program
// has started, and, with lockdown, the program's oom_score_adj is raised then. Once the program
// has exited, and before it is reaped, its process group is sent SIGKILL.
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
		// Best effort: a program that has already exited has no oom_score_adj left to set.
		_ = os.WriteFile("/proc/"+strconv.Itoa(cmd.Process.Pid)+"/oom_score_adj", []byte(oomScoreAdj), 0)
	}
	// The program is exited but unreaped once awaitExit returns: a zombie keeps its pid, and so
	// the group's ID, from reuse, so the group's SIGKILL reaches only what the program started.
	// Should waitid fail, which Linux has offered since 2.6.9, the group is left alone rather
	// than risk a reused ID.
	if awaitExit(cmd.Process.Pid) == nil {
		_ = signalGroup(cmd.Process, syscall.SIGKILL)
	}
	return cmd.Wait()
}

// environ returns a run's environment: fixed; then HOME and TMPDIR, dir made absolute, when dir
// isn't empty; then env, the spec's Env, last, so os/exec, which keeps a key's last value, gives
// its keys precedence.
func environ(fixed, env []string, dir string) ([]string, error) {
	var home []string
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("working directory %s: %w", dir, err)
		}
		home = []string{"HOME=" + abs, "TMPDIR=" + abs}
	}
	return slices.Concat(fixed, home, env), nil
}

// waitSignal returns the name of the signal that ended the program, from its wait status; empty
// for an exit.
func waitSignal(state *os.ProcessState) string {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return signalName(int(status.Signal()))
}

// usage returns the CPU time of a reaped program, from its rusage; zero when state is nil, a
// program that never started. It leaves out ru_maxrss, which holds the Go parent's resident
// memory from the vfork-style clone that started the program (see Usage).
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

// stdinFeed is the pipe a run's stdin passes through when the caller's reader isn't an *os.File.
// os/exec would copy such a reader itself, and, once the program has exited, wait for that copy
// with no bound, even past WaitDelay: a copy blocked in the caller's Read, on a pipe the caller
// holds open, would keep Run from ever returning. The feed's copy is execRun's own, so Run decides
// how long to wait for it.
type stdinFeed struct {
	src  io.Reader
	r, w *os.File
	// done carries the copy's outcome; nil until start begins the copy.
	done chan error
}

// newStdinFeed makes the pipe src is copied into, its read end the program's stdin.
func newStdinFeed(src io.Reader) (*stdinFeed, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &stdinFeed{src: src, r: r, w: w}, nil
}

// start runs once the command's Start has returned, started reporting whether it succeeded. The
// program holds the read end of its own, so the parent's copy is closed either way: the program,
// and whatever it starts, are then the pipe's only readers, and a write once they have all exited
// fails with EPIPE rather than blocking. A program that started gets the copy, which closes the
// write end when it ends, so the program reads EOF at the end of src; one that didn't, nothing.
func (f *stdinFeed) start(started bool) {
	_ = f.r.Close()
	if !started {
		_ = f.w.Close()
		return
	}
	f.done = make(chan error, 1)
	go func() {
		_, err := io.Copy(f.w, f.src)
		// EPIPE is a program that exited, or closed its stdin, without reading all of src, and
		// ErrClosed is a write end that finish closed: neither is the copy's failure.
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed) {
			err = nil
		}
		// The outcome is in done before the program can read EOF, so a program that reads to
		// the end, exits, and is reaped has its copy's outcome waiting for finish.
		f.done <- err
		_ = f.w.Close()
	}()
}

// finish runs once the program has exited and been reaped, and returns the copy's error, a read
// of src that failed while the program ran, when the copy has ended. A run that ctx ended doesn't
// wait for the copy at all, nor does a zero killAfter. Otherwise finish waits for the copy at most killAfter, as WaitDelay bounds the wait for
// the program's output, and stops early should ctx end: a program that read all of src has its
// copy already ended, and one that left src unread ends its copy with EPIPE at the next write,
// but a copy blocked in a Read of src, which the caller holds open, ends only when that Read
// returns, which no bound here can hasten. When finish stops waiting, it closes the write end, so
// the copy's next write fails and it ends, and reports nothing: an exited program can read no
// more of src, and leaving src unread is no error.
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

// pPID is waitid's idtype for a single pid, P_PID, which syscall doesn't define.
const pPID = 1

// awaitExit blocks until the child pid has exited, without reaping it: waitid with WNOWAIT
// leaves the child waitable, so the os.Process's own wait, through its pidfd, still reaps it and
// collects its status. The os package's wait does the same where it has no pidfd.
func awaitExit(pid int) error {
	// The kernel writes a siginfo_t, 128 bytes on Linux, which nothing here reads.
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

// signalGroup sends sig to the process group p leads, reporting a group already gone as
// os.ErrProcessDone.
func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
