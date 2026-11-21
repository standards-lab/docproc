package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Spec declares a tool: its program, how to check the program's version at construction, and
// the bounds every run of it takes.
type Spec struct {
	// Program is a name looked up on PATH, or a path, which is taken as given when it contains a
	// separator.
	Program string
	// VersionArgs are the arguments that make the program print its version.
	VersionArgs []string
	// VersionPattern matches the version command's output, stdout and stderr together; its first
	// capture group is the version, dotted numbers only.
	VersionPattern *regexp.Regexp
	// Minimum is the oldest version the tool accepts, dotted numbers.
	Minimum string
	// VersionExit is the exit code the version command exits with when it prints the version: 0
	// unless set. poppler's pdfdetach prints its version for -v and exits 99, as it does for its
	// usage text.
	VersionExit int
	// KillAfter is how long a run that is ended, by its context or its own Timeout or Idle, has
	// to exit after SIGTERM before SIGKILL ends it. Zero, or less, is no grace: SIGKILL follows
	// SIGTERM at once. It has no default; the caller states it.
	KillAfter time.Duration
	// MemoryLimit is each run's memory bound in bytes, zero for none, and below zero refused.
	// The exec runner sets it as the program's address-space limit, RLIMIT_AS, through
	// util-linux's prlimit before the program runs, the version command's run included; the
	// container runner passes it as docker run's --memory. It has no default.
	MemoryLimit int64
	// Env is added, as KEY=value entries, to the exec runner's fixed environment, a key here
	// replacing the fixed one's. The container runner doesn't pass it.
	Env []string
	// Timeout and Idle are the bounds of every run of the tool whose Invocation sets neither of
	// its own, as Invocation's Timeout and Idle say, so a caller bounds the runs of an adapter
	// that sets none; zero is none. The version command's run takes neither. Neither has a
	// default.
	Timeout, Idle time.Duration
}

func (s Spec) validate(name string) (Version, error) {
	if s.VersionPattern == nil || s.VersionPattern.NumSubexp() < 1 {
		return Version{}, fmt.Errorf("tool: %s: version pattern must have a capture group", name)
	}
	minimum, err := ParseVersion(s.Minimum)
	if err != nil {
		return Version{}, fmt.Errorf("tool: %s: minimum: %w", name, err)
	}
	if s.MemoryLimit < 0 {
		return Version{}, fmt.Errorf("tool: %s: memory limit %d is below zero", name, s.MemoryLimit)
	}
	if s.Timeout < 0 || s.Idle < 0 {
		return Version{}, fmt.Errorf("tool: %s: a time bound is below zero", name)
	}
	return minimum, nil
}

// Invocation is one run of a tool. It names nothing about the runner, so an adapter builds the
// same Invocation whichever runner built its Tool.
type Invocation struct {
	// Args are the program's arguments, the program's name not among them.
	Args []string
	// Dir is the working directory, which the caller owns; empty is the process's own. Through
	// the exec runner it is the program's HOME too, but not its TMPDIR, a new empty directory of
	// the run's own under the process's temporary directory, removed once the program is reaped,
	// so a program ended early leaves no temporary file in Dir.
	Dir string
	// Stdin is the program's standard input; nil is none. An *os.File passes straight to the
	// program. Any other reader is copied into a pipe by a goroutine of Run's own, which closes
	// the pipe at the reader's EOF. Once the program has exited, Run waits for that copy at most
	// a short bound, the Spec's KillAfter period through the exec runner and 2 s through the
	// container runner, then returns: a program that exits without reading all of Stdin is no
	// error, and a failed Read of Stdin is Run's error only when the program otherwise succeeds.
	// A run that ctx ended doesn't wait for the copy. The cost: after Run returns, the goroutine
	// may make one more Read of Stdin, and it exits when that Read returns.
	Stdin io.Reader
	// Stdout receives the program's standard output; nil discards it.
	Stdout io.Writer
	// Stderr receives all of the program's standard error; nil keeps its last 4 KiB for the
	// ExitError's Message instead.
	Stderr io.Writer
	// Usage, when not nil, receives the run's resource usage. Run sets it whatever the run's
	// outcome: once the program has been reaped, whether it exited zero, exited nonzero, or was
	// cancelled, the exec runner fills it from the program's rusage; a program that never
	// started leaves it zero. The container runner always leaves it zero: the only process it
	// reaps is the docker CLI, whose usage isn't the container's. nil asks for nothing.
	Usage *Usage
	// Timeout is a unit run's own bound, zero for none: past it the run is ended as a cancelled
	// one is, and Run returns an *ExitError with TimedOut set.
	Timeout time.Duration
	// Idle is a stream run's own bound, zero for none: the run is ended as a cancelled one is
	// once the program has written nothing to stdout or stderr for this long, and Run returns
	// an *ExitError with TimedOut set. A write the caller's writer holds up isn't idleness.
	Idle time.Duration
}

// Usage is a run's CPU time, as the kernel accounts it for the program once it is reaped. On
// Linux that accounting covers the children the program itself waited for, such as a
// converter's delegate, but not a child it left unreaped.
//
// It has no peak memory. Go starts a program with a vfork-style clone, so Linux records the Go
// parent's resident memory in the program's peak at exec, and the rusage figure reads the
// caller's size rather than the program's whenever the caller is the larger.
type Usage struct {
	// UserCPU is the CPU time spent in user mode, the program's and its reaped children's.
	UserCPU time.Duration
	// SystemCPU is the CPU time spent in the kernel on their behalf.
	SystemCPU time.Duration
}

type runFunc func(ctx context.Context, inv Invocation) error

// Tool is one program, found and version-checked by the runner's constructor that built it.
type Tool struct {
	name          string
	version       Version
	line          string
	timeout, idle time.Duration
	run           runFunc
	image         *Image
}

// Name returns the program's name.
func (t *Tool) Name() string { return t.name }

// Version returns the program's version, found at construction.
func (t *Tool) Version() Version { return t.version }

// VersionLine returns the whole line of the version command's output the version was found on,
// trimmed, such as "GPL Ghostscript 10.05.1 (2025-04-29)": it tells two builds of one version
// apart, a development build from a release, where Version can't.
func (t *Tool) VersionLine() string { return t.line }

// Timeout returns the Spec's Timeout, the bound of every run whose Invocation sets neither bound of
// its own; zero is none. An adapter that runs as one stream what the Spec bounds as units, such as
// many pages in one process, reads it to bound the stream's idleness by the unit's bound instead.
func (t *Tool) Timeout() time.Duration { return t.timeout }

// AtLeast refuses the tool when its version is older than minimum, dotted numbers, whatever
// minimum its own Spec declared, with a *VersionError wrapping ErrTooOld. A minimum that isn't
// dotted numbers is an error of its own.
func (t *Tool) AtLeast(minimum string) error {
	m, err := ParseVersion(minimum)
	if err != nil {
		return fmt.Errorf("tool: %s: minimum: %w", t.name, err)
	}
	if t.version.Compare(m) < 0 {
		return &VersionError{Program: t.name, Found: t.version.String(), Minimum: minimum, Err: ErrTooOld}
	}
	return nil
}

// Image returns the image a tool the container runner built runs in, its Network and CapAdd
// included, and true; a tool the exec runner built returns false. An adapter whose runs need more
// than the lockdown allows, such as a network, reads it to refuse a run the tool can't make.
func (t *Tool) Image() (Image, bool) {
	if t.image == nil {
		return Image{}, false
	}
	image := *t.image
	image.CapAdd = slices.Clone(image.CapAdd)
	return image, true
}

// Run runs the program once, under ctx. A program that exits nonzero, or dies by a signal, gives
// an *ExitError. When ctx ends the run, SIGTERM goes to the program's process group, then SIGKILL
// once the Spec's KillAfter period passes, and the error matches ctx's (errors.Is
// context.Canceled or context.DeadlineExceeded), and is never an *ExitError; it names the
// program, ctx's error, and the program's own outcome once each, as in "tool: ffprobe: context
// canceled (exit status 143)". A run that passes the Invocation's own Timeout or Idle is ended
// the same way, and gives an *ExitError with TimedOut set, whatever the program's exit. An
// Invocation that sets neither bound takes the Spec's.
func (t *Tool) Run(ctx context.Context, inv Invocation) error {
	if inv.Timeout <= 0 && inv.Idle <= 0 {
		inv.Timeout, inv.Idle = t.timeout, t.idle
	}
	if inv.Timeout <= 0 && inv.Idle <= 0 {
		return t.run(ctx, inv)
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	b := newBounds(inv.Timeout, inv.Idle, cancel)
	var tail *tailWriter
	if inv.Idle > 0 {
		if inv.Stderr == nil {
			tail = &tailWriter{n: stderrTail}
			inv.Stderr = tail
		}
		stdout := inv.Stdout
		if stdout == nil {
			stdout = io.Discard
		}
		shared := sameWriter(inv.Stdout, inv.Stderr)
		inv.Stdout = &idleWriter{w: stdout, b: b}
		if shared {
			inv.Stderr = inv.Stdout
		} else {
			inv.Stderr = &idleWriter{w: inv.Stderr, b: b}
		}
	}
	b.start()
	err := t.run(runCtx, inv)
	timedOut := b.end()
	if timedOut && ctx.Err() == nil {
		e := &ExitError{Program: t.name, Code: -1, err: err}
		if ce, ok := errors.AsType[*cancelError](err); ok && ce.exit != nil {
			*e = *ce.exit
		} else if ee, ok := errors.AsType[*ExitError](err); ok {
			*e = *ee
		} else if err == nil {
			e.Code = 0
		}
		e.TimedOut = true
		err = e
	}
	if ee, ok := errors.AsType[*ExitError](err); ok && tail != nil && ee.Message == "" {
		ee.Message = string(tail.buf)
	}
	return err
}

var errTimedOut = errors.New("the run passed its own time bound")

type bounds struct {
	timeout, idle time.Duration
	cancel        context.CancelCauseFunc

	mu               sync.Mutex
	timer, idleTimer *time.Timer
	writing          int
	fired, ended     bool
}

func newBounds(timeout, idle time.Duration, cancel context.CancelCauseFunc) *bounds {
	return &bounds{timeout: timeout, idle: idle, cancel: cancel}
}

func (b *bounds) start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timeout > 0 {
		b.timer = time.AfterFunc(b.timeout, b.expire)
	}
	if b.idle > 0 {
		b.idleTimer = time.AfterFunc(b.idle, b.expire)
	}
}

func (b *bounds) expire() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ended || b.fired {
		return
	}
	b.fired = true
	b.cancel(errTimedOut)
}

func (b *bounds) pause() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writing++
	b.idleTimer.Stop()
}

func (b *bounds) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.writing--; b.writing == 0 && !b.ended && !b.fired {
		b.idleTimer.Reset(b.idle)
	}
}

func (b *bounds) end() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ended = true
	for _, t := range []*time.Timer{b.timer, b.idleTimer} {
		if t != nil {
			t.Stop()
		}
	}
	return b.fired
}

type idleWriter struct {
	w io.Writer
	b *bounds
}

func (w *idleWriter) Write(p []byte) (int, error) {
	w.b.pause()
	defer w.b.resume()
	return w.w.Write(p)
}

func sameWriter(a, b io.Writer) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a != nil && a == b
}

const stderrTail = 4 << 10

// ExitError reports a run's own failure: a program that exited nonzero or died by a signal, or a
// run that passed its own Timeout or Idle.
type ExitError struct {
	// Program is the program's name.
	Program string
	// Code is the exit code. It is -1 when the exec runner's program died by a signal, and
	// 128 and the signal's number when the container runner's did.
	Code int
	// Signal is the signal's name, such as SIGKILL, when the program died by one; empty for an
	// exit. The exec runner reads it from the wait status; the container runner, which sees only
	// the container's exit code, from codes 129 to 159, 128 and a signal's number, as docker's
	// init reports a death by signal.
	Signal string
	// TimedOut reports a run that passed its own Timeout or Idle, not the caller's context; Code
	// and Signal are then the program's outcome once it was ended.
	TimedOut bool
	// Message is the last 4 KiB of the program's standard error, empty when the caller passed a
	// writer for it.
	Message string
	err     error
}

func (e *ExitError) Error() string {
	outcome := fmt.Sprintf("exit code %d", e.Code)
	if e.Signal != "" {
		outcome = "killed by " + e.Signal
	}
	if e.TimedOut {
		outcome = "timed out, " + outcome
	}
	msg := fmt.Sprintf("tool: %s: %s", e.Program, outcome)
	if s := strings.TrimSpace(e.Message); s != "" {
		msg += ": " + s
	}
	return msg
}

// Unwrap returns the runner's own error, an *exec.ExitError for the exec runner's program or the
// container runner's docker CLI.
func (e *ExitError) Unwrap() error { return e.err }

var signalNames = [...]string{
	1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT", 7: "SIGBUS",
	8: "SIGFPE", 9: "SIGKILL", 10: "SIGUSR1", 11: "SIGSEGV", 12: "SIGUSR2", 13: "SIGPIPE",
	14: "SIGALRM", 15: "SIGTERM", 16: "SIGSTKFLT", 17: "SIGCHLD", 18: "SIGCONT", 19: "SIGSTOP",
	20: "SIGTSTP", 21: "SIGTTIN", 22: "SIGTTOU", 23: "SIGURG", 24: "SIGXCPU", 25: "SIGXFSZ",
	26: "SIGVTALRM", 27: "SIGPROF", 28: "SIGWINCH", 29: "SIGIO", 30: "SIGPWR", 31: "SIGSYS",
}

func signalName(n int) string {
	switch {
	case n > 0 && n < len(signalNames):
		return signalNames[n]
	case n >= 34 && n <= 64:
		return fmt.Sprintf("SIGRTMIN+%d", n-34)
	}
	return fmt.Sprintf("signal %d", n)
}

type cancelError struct {
	program string
	ctxErr  error
	outcome error
	exit    *ExitError
}

func (e *cancelError) Error() string {
	if e.outcome == nil {
		return fmt.Sprintf("tool: %s: %v", e.program, e.ctxErr)
	}
	return fmt.Sprintf("tool: %s: %v (%v)", e.program, e.ctxErr, e.outcome)
}

func (e *cancelError) Unwrap() error { return e.ctxErr }

func runOutcome(err error) error {
	if ce, ok := errors.AsType[*cancelError](err); ok {
		return ce.outcome
	}
	if ee, ok := errors.AsType[*ExitError](err); ok {
		return ee.err
	}
	return err
}

func lookPath(program string) (string, error) {
	path, err := exec.LookPath(program)
	if err != nil {
		return "", fmt.Errorf("tool: %s: not found: %w", program, err)
	}
	return path, nil
}

// VersionError reports a tool its constructor refused over its version: the version command
// failed, its output didn't match the pattern, or the version is older than the minimum.
type VersionError struct {
	// Program is the program's name.
	Program string
	// Found is the version found, empty when none was.
	Found string
	// Minimum is the oldest version the tool accepts.
	Minimum string
	// Err is the cause: ErrTooOld, ErrNoVersion, or the version command's error.
	Err error
}

func (e *VersionError) Error() string {
	found := e.Found
	if found == "" {
		found = "none"
	}
	return fmt.Sprintf("tool: %s: version %s, minimum %s: %v", e.Program, found, e.Minimum, e.Err)
}

func (e *VersionError) Unwrap() error { return e.Err }

var (
	// ErrTooOld is a VersionError's cause when the version is older than the minimum.
	ErrTooOld = errors.New("older than the minimum")
	// ErrNoVersion is a VersionError's cause when the version output doesn't match the pattern,
	// or its capture isn't dotted numbers.
	ErrNoVersion = errors.New("no version in the output")
)

func checkVersion(ctx context.Context, name string, spec Spec, minimum Version, run runFunc) (Version, string, error) {
	var out bytes.Buffer
	verr := &VersionError{Program: name, Minimum: spec.Minimum}
	err := run(ctx, Invocation{Args: spec.VersionArgs, Stdout: &out, Stderr: &out})
	if exit, ok := errors.AsType[*ExitError](err); ok && spec.VersionExit != 0 && exit.Code == spec.VersionExit {
		err = nil
	}
	if err != nil {
		verr.Err = fmt.Errorf("version command: %w", err)
		if line := firstLine(out.Bytes()); line != "" {
			verr.Err = fmt.Errorf("%w: %s", verr.Err, line)
		}
		return Version{}, "", verr
	}
	m := spec.VersionPattern.FindSubmatchIndex(out.Bytes())
	if m == nil {
		verr.Err = fmt.Errorf("%w: %s matched none of %q", ErrNoVersion, spec.VersionPattern, firstLine(out.Bytes()))
		return Version{}, "", verr
	}
	verr.Found = string(out.Bytes()[m[2]:m[3]])
	found, err := ParseVersion(verr.Found)
	if err != nil {
		verr.Err = fmt.Errorf("%w: %w", ErrNoVersion, err)
		return Version{}, "", verr
	}
	if found.Compare(minimum) < 0 {
		verr.Err = ErrTooOld
		return Version{}, "", verr
	}
	return found, matchLine(out.Bytes(), m[0]), nil
}

func matchLine(out []byte, at int) string {
	start := bytes.LastIndexByte(out[:at], '\n') + 1
	end := bytes.IndexByte(out[at:], '\n')
	if end < 0 {
		end = len(out) - at
	}
	return string(bytes.TrimSpace(out[start : at+end]))
}

func firstLine(out []byte) string {
	line, _, _ := bytes.Cut(bytes.TrimSpace(out), []byte("\n"))
	if len(line) > 200 {
		line = line[:200]
	}
	return string(line)
}

type tailWriter struct {
	n   int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if over := len(w.buf) - w.n; over > 0 {
		w.buf = append(w.buf[:0], w.buf[over:]...)
	}
	return len(p), nil
}
