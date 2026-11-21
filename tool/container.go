package tool

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Image is the image a container tool runs in, and the relaxations of a run's lockdown that the
// image needs. A run is otherwise locked down: no network, no capabilities, the caller's user.
type Image struct {
	// Ref is the image's reference, pinned by digest with its tag kept for readers:
	// repo:tag@sha256: and 64 hex digits.
	Ref string
	// Network is the network a run joins, a docker network name such as bridge; empty is none.
	Network string
	// CapAdd are the capabilities a run gets back after it drops them all, such as NET_RAW.
	CapAdd []string
}

var imageRef = regexp.MustCompile(`^[a-z0-9][^\s@]*:[\w][\w.-]{0,127}@sha256:[0-9a-f]{64}$`)

func dockerSpec(program string) Spec {
	if program == "" {
		program = "docker"
	}
	return Spec{
		Program:        program,
		VersionArgs:    []string{"version", "--format={{.Server.Version}}"},
		VersionPattern: regexp.MustCompile(`^(\d+(?:\.\d+)*)`),
		Minimum:        "20.10",
		KillAfter:      cliKillAfter,
	}
}

const pidsLimit = 4096

const containerPrefix = "tool-"

const stopMargin = 15 * time.Second

const cliKillAfter = 2 * time.Second

// NewContainer builds a Tool that runs spec's program inside image, the container runner. docker
// is the docker CLI, a name looked up on PATH or a path, "docker" when empty; it runs through the
// exec runner, found and checked as NewExec checks a program, against the server's version,
// minimum 20.10, so a daemon it can't reach is an error here. It validates spec and image first,
// refusing a reference without a digest; then it checks docker, pulls the image by its reference
// when it isn't present, and runs spec's version command inside the image, spec's program as the
// entrypoint, checking it as NewExec does. All of it runs under ctx.
//
// The Tool is named for spec's program, which is the image's, never looked up on the host. Each
// run passes --rm, --init, --network=none, the caller's uid:gid as --user, --cap-drop=ALL,
// --security-opt=no-new-privileges, --pids-limit=4096, spec's MemoryLimit as --memory when it has
// one, --pull=never, a name of its own, and spec's program as --entrypoint; it mounts the
// invocation's working directory at the same absolute path and works in it, so the same arguments
// name the same files through either runner, and files the tool writes there belong to the
// caller. An empty Dir is the process's working directory, and a relative one is made absolute.
// It passes --interactive only when the invocation has stdin. The image's Network and CapAdd relax
// the network and add capabilities; nothing else of the lockdown changes. spec's Env isn't passed:
// the image's environment is the container's.
//
// A cancelled run's container is stopped by docker stop on its name, under a context of its own,
// given spec's KillAfter period rounded up to whole seconds; the CLI's own signal forwarding is
// never relied on. Run then waits for the CLI, and returns an error matching ctx's, which names
// the CLI's outcome once, as the exec runner's names the program's. A tool that exits nonzero
// gives an *ExitError naming spec's program; docker's own failures exit 125, and an entrypoint it
// can't run, 126 or 127. A container exit of 129 to 159 is a death by signal, as docker's init
// reports one, its ExitError's Signal named from the code less 128; an exit of the tool's own in
// that range reads the same.
//
// A run leaves an invocation's Usage zero: the docker CLI's rusage, the only one the runner can
// read, isn't the container's, whose processes the daemon starts and reaps.
func NewContainer(ctx context.Context, docker string, image Image, spec Spec) (*Tool, error) {
	name := filepath.Base(spec.Program)
	if spec.Program == "" {
		return nil, errors.New("tool: container: no program")
	}
	minimum, err := spec.validate(name)
	if err != nil {
		return nil, err
	}
	if !imageRef.MatchString(image.Ref) {
		return nil, fmt.Errorf("tool: %s: image %q: want repo:tag@sha256: and 64 hex digits", name, image.Ref)
	}
	dspec := dockerSpec(docker)
	dname := filepath.Base(dspec.Program)
	dminimum, err := dspec.validate(dname)
	if err != nil {
		return nil, err
	}
	dpath, err := lookPath(dspec.Program)
	if err != nil {
		return nil, err
	}
	drun := execRun(execConfig{name: dname, path: dpath, killAfter: dspec.KillAfter})
	if _, _, err := checkVersion(ctx, dname, dspec, dminimum, drun); err != nil {
		return nil, err
	}
	if err := ensureImage(ctx, name, image.Ref, drun); err != nil {
		return nil, err
	}
	run := containerRun(name, dpath, drun, runArgs(spec, image), image.Ref, spec.KillAfter)
	version, line, err := checkVersion(ctx, name, spec, minimum, run)
	if err != nil {
		return nil, err
	}
	image.CapAdd = slices.Clone(image.CapAdd)
	return &Tool{name: name, version: version, line: line, timeout: spec.Timeout, idle: spec.Idle, run: run, image: &image}, nil
}

func ensureImage(ctx context.Context, name, ref string, drun runFunc) error {
	if drun(ctx, Invocation{Args: []string{"image", "inspect", "--format={{.Id}}", ref}}) == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tool: %s: image %s: %w", name, ref, err)
	}
	if err := drun(ctx, Invocation{Args: []string{"pull", ref}}); err != nil {
		return fmt.Errorf("tool: %s: pull %s: %w", name, ref, err)
	}
	return nil
}

func runArgs(spec Spec, image Image) []string {
	network := image.Network
	if network == "" {
		network = "none"
	}
	args := []string{
		"run", "--rm", "--init",
		"--network=" + network,
		"--user=" + strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"--cap-drop=ALL",
	}
	for _, c := range image.CapAdd {
		args = append(args, "--cap-add="+c)
	}
	args = append(args, "--security-opt=no-new-privileges", "--pids-limit="+strconv.Itoa(pidsLimit))
	if spec.MemoryLimit > 0 {
		args = append(args, "--memory="+strconv.FormatInt(spec.MemoryLimit, 10))
	}
	return append(args, "--pull=never", "--entrypoint="+spec.Program)
}

// See docs/api.md, "Running the tools".
func containerRun(name, dpath string, drun runFunc, base []string, ref string, killAfter time.Duration) runFunc {
	cli := execRun(execConfig{name: name, path: dpath, killAfter: cliKillAfter})
	return func(ctx context.Context, inv Invocation) error {
		if inv.Usage != nil {
			*inv.Usage = Usage{}
		}
		if err := ctx.Err(); err != nil {
			return &cancelError{program: name, ctxErr: err}
		}
		dir, err := workDir(inv.Dir)
		if err != nil {
			return fmt.Errorf("tool: %s: %w", name, err)
		}
		cname := containerName()
		args := slices.Concat(base, []string{"--name=" + cname, "--volume=" + dir + ":" + dir, "--workdir=" + dir})
		if inv.Stdin != nil {
			args = append(args, "--interactive")
		}
		args = append(append(args, ref), inv.Args...)

		cliCtx, endCLI := context.WithCancel(context.WithoutCancel(ctx))
		defer endCLI()
		exited := make(chan struct{})
		stopped := make(chan bool, 1)
		go func() {
			select {
			case <-exited:
				stopped <- false
			case <-ctx.Done():
				stopContainer(ctx, cname, drun, killAfter, exited, endCLI)
				stopped <- true
			}
		}()
		err = cli(cliCtx, Invocation{Args: args, Stdin: inv.Stdin, Stdout: inv.Stdout, Stderr: inv.Stderr})
		close(exited)
		if <-stopped {
			var outcome error
			if err != nil {
				outcome = runOutcome(err)
			}
			return &cancelError{program: name, ctxErr: ctx.Err(), outcome: outcome, exit: containerExit(err)}
		}
		if err != nil && ctx.Err() != nil {
			return &cancelError{program: name, ctxErr: ctx.Err(), outcome: runOutcome(err), exit: containerExit(err)}
		}
		if exit := containerExit(err); exit != nil {
			return exit
		}
		return err
	}
}

func containerExit(err error) *ExitError {
	exit, ok := errors.AsType[*ExitError](err)
	if !ok {
		ce, isCancel := errors.AsType[*cancelError](err)
		if !isCancel || ce.exit == nil {
			return nil
		}
		exit = ce.exit
	}
	e := *exit
	if e.Signal == "" && e.Code > 128 && e.Code < 160 {
		e.Signal = signalName(e.Code - 128)
	}
	return &e
}

func stopContainer(ctx context.Context, cname string, drun runFunc, killAfter time.Duration, exited <-chan struct{}, endCLI context.CancelFunc) {
	secs := strconv.FormatInt(int64((killAfter+time.Second-1)/time.Second), 10)
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), killAfter+stopMargin)
	defer cancel()
	for drun(stopCtx, Invocation{Args: []string{"stop", "--time=" + secs, cname}, Stdout: io.Discard, Stderr: io.Discard}) != nil {
		select {
		case <-exited:
			return
		case <-stopCtx.Done():
		case <-time.After(time.Second):
		}
		if stopCtx.Err() != nil {
			break
		}
	}
	endCLI()
	<-exited
	rmCtx, cancelRm := context.WithTimeout(context.WithoutCancel(ctx), stopMargin)
	defer cancelRm()
	_ = drun(rmCtx, Invocation{Args: []string{"rm", "--force", cname}, Stdout: io.Discard, Stderr: io.Discard})
}

func workDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("working directory %s: %w", dir, err)
	}
	if strings.Contains(abs, ":") {
		return "", fmt.Errorf("working directory %s: a colon can't be mounted", abs)
	}
	return abs, nil
}

func containerName() string {
	return containerPrefix + strings.ToLower(rand.Text())
}
