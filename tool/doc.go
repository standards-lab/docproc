// Package tool runs mature system tools behind one contract: a Tool bound to one program, built by
// a runner's constructor, which finds the program and checks its version so a missing or outdated
// tool fails at startup. Tool.Run runs one Invocation, arguments, a working directory the caller
// owns, and optional standard streams, under the caller's context. An Invocation names nothing
// about the runner, so an adapter written against Tool runs the same code whichever runner built
// it. The package reports a run's facts, its exit code, the signal that killed it, and whether it
// passed its own time bound, and decides no outcome; its caller classifies them. It builds on
// every platform, on the standard library alone; the exec runner, and so the container runner,
// which runs the docker CLI through it, runs on Linux only, and NewExec elsewhere returns an error.
//
// The exec runner, NewExec, runs a program directly, a name looked up on PATH or a path with a
// separator taken as given. Every program it starts leads a process group of its own, in a fixed
// environment, never the host's: PATH as it was at construction, LANG=C.UTF-8, HOME and TMPDIR
// the invocation's working directory when it has one, then the Spec's Env. Its parent-death
// signal is SIGKILL, from an OS thread locked to the run, so the host's death ends it; its
// oom_score_adj is 1000, written to /proc once it has started, so the OOM killer chooses it over
// its host; and whenever it exits, its group is sent SIGKILL before it is reaped, so no child it
// started outlives the run. A Spec's MemoryLimit, zero for none, is the program's RLIMIT_AS, set
// by util-linux's `prlimit --as=<bytes> --` before the program is executed, for every run of the
// Spec, the version command's included; NewExec refuses a bounded Spec when prlimit isn't on PATH.
// An allocation the bound refuses fails as the program handles it, typically by a signal such as
// SIGABRT or SIGSEGV.
//
// The container runner, NewContainer, runs the program inside an Image through the docker CLI, the
// only runtime, which it runs through the exec runner, in the host's environment, and checks
// against the server's version, minimum 20.10. The image's reference is pinned by digest;
// construction pulls it when it isn't present and runs the version command inside it. Every run
// is locked down, --rm, --init, --network=none, the caller's uid:gid, --cap-drop=ALL,
// --security-opt=no-new-privileges, a fixed --pids-limit, the MemoryLimit as --memory, and
// --pull=never, and the Image's Network and CapAdd are its only relaxations. A run mounts its
// working directory at the same absolute path and works in it, so the same arguments name the same
// files through either runner.
//
// A Spec declares the version check: arguments that print the version, a pattern whose first
// capture group is the version, and a minimum; and, for a program whose version command exits
// nonzero, as pdfdetach's -v exits 99, the exit code that means it printed the version. A
// Version is dotted numbers compared as numbers, a missing component counting as zero, and the
// check is a minimum, never an exact version. Tool.AtLeast is the one later check, against a
// minimum of the caller's.
//
// No bound has a default: a Spec's KillAfter, MemoryLimit, Timeout, and Idle, and an Invocation's
// Timeout and Idle, are the caller's, and zero is none, or, for KillAfter, no grace. A Spec's
// Timeout and Idle bound every run whose Invocation sets neither, so a caller bounds the runs of an
// adapter that sets none.
//
// Cancelling a run's context stops its tool. Through the exec runner the program's process group
// gets SIGTERM, then SIGKILL once the Spec's KillAfter period passes, at once when it is zero.
// Through the container runner, docker stop on the run's container stops it, given the KillAfter
// period; the CLI's own signal forwarding is never relied on. Either way, a run that fails once
// its context has ended returns an error matching the context's, errors.Is context.Canceled or
// context.DeadlineExceeded, never an *ExitError; it names the program's own outcome once, as in
// "tool: ffprobe: context canceled (exit status 143)". If the process running the package is
// itself SIGKILLed, a running container runs on until its tool exits, and --rm removes it then. A
// run that passes its own Timeout, or writes nothing for its Idle, is stopped the same way and
// returns an *ExitError with TimedOut set.
//
// A program that exits nonzero or dies by a signal is an *ExitError: its code, the signal's name,
// as in "tool: gs: killed by SIGSEGV", and the last 4 KiB of its standard error when the caller
// passed no writer for it. The container runner, which sees only the container's exit code, names
// the signal from codes 129 to 159. A constructor's refusal over the version is a *VersionError.
// An Invocation's Stdin, when it isn't an *os.File, is copied through a pipe Run owns, so a reader
// the caller holds open never keeps Run from returning; the cost is that the copy may make one
// more Read of it after Run returns.
//
// An Invocation's Usage, when not nil, asks for the run's resource usage, which Run sets whatever
// the run's outcome. The exec runner fills it once the program is reaped, from the program's
// rusage: user and system CPU time, the children the program itself reaped included. It reports
// no peak memory, since the program's rusage peak starts at its Go parent's resident memory. The
// container runner leaves it zero, since the docker CLI's usage isn't the container's.
//
// Its exported identifiers:
//
//   - [Spec] declares a tool: its program, its version command, exit code, and pattern, its
//     minimum version, a run's KillAfter period, its memory bound, its added environment, and the
//     Timeout and Idle bounds of a run that sets none of its own.
//   - [Invocation] is one run of a tool: arguments, working directory, standard streams, where to
//     report its usage, and its own Timeout and Idle bounds.
//   - [Usage] is a run's resource usage: user and system CPU time.
//   - [Tool] is one program, found and version-checked by the runner's constructor that built it.
//   - [NewExec] builds a Tool that runs a program directly, the exec runner.
//   - [NewContainer] builds a Tool that runs a program inside a pinned image through the docker
//     CLI, the container runner.
//   - [Image] is a container tool's image, pinned by digest, and its relaxations of the lockdown.
//   - [Tool.Name] returns the program's name.
//   - [Tool.Version] returns the version found at construction, and [Tool.VersionLine] the whole
//     line it was found on, which tells two builds of one version apart.
//   - [Tool.AtLeast] refuses a tool older than a minimum of the caller's.
//   - [Tool.Image] returns a container tool's Image, its Network included.
//   - [Tool.Run] runs the program once under the caller's context.
//   - [ExitError] reports a run's own failure: its program, exit code, signal, whether it timed
//     out, and its stderr tail.
//   - [VersionError] reports a tool its constructor, or AtLeast, refused over its version.
//   - [ErrTooOld] and [ErrNoVersion] are a VersionError's causes: a version older than the
//     minimum, and output with no version in it.
//   - [Version] is a tool's version, dotted numbers compared as numbers.
//   - [ParseVersion] parses dotted numbers into a Version.
//   - [Version.Compare] orders two versions.
//   - [Version.String] returns a version's dotted form.
package tool
