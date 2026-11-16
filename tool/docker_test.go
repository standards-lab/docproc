//go:build linux

package tool_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
)

// dockerSettings are the stand-in docker's settings: its daemon's version, empty for a daemon the
// CLI can't reach.
type dockerSettings struct {
	Server string `json:"server"`
}

// dockerStandin is the stand-in docker CLI, its link's run log recording every command line it is
// given. Its state directory is its link's: images, the references present, one a line; and
// NAME.pid, the pid of the running container NAME. It answers:
//
//	version --format=F            the daemon's version, or exits 1 when it can't reach one
//	image inspect --format=F REF  exits 0 when REF is present, 1 when it isn't
//	pull REF                      makes REF present
//	run FLAGS... REF ARGS...      runs the stand-in tool on ARGS as the container, in a process
//	                              group of its own as a daemon's container would be, in
//	                              --workdir, with the CLI's stdin only with --interactive,
//	                              through the standin link beside it; exits
//	                              125 when REF isn't present and 127 when --entrypoint isn't
//	                              standin, else with the container's exit, 128 and the signal
//	                              when one ended it
//	stop --time=N NAME            SIGTERMs the container NAME, then SIGKILLs it after N seconds;
//	                              exits 1 when no container NAME runs
//	rm --force NAME               does nothing
//
// The CLI catches SIGTERM and does nothing with it: a runner that relied on the CLI's forwarding
// would never stop its container.
func dockerStandin(r tooltest.Run) int {
	var settings dockerSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dir, args := filepath.Dir(r.Link), r.Args
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "docker: no command")
		return 2
	}
	last := args[len(args)-1]
	switch args[0] {
	case "version":
		server := settings.Server
		if server == "" {
			fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
			return 1
		}
		fmt.Println(server)
		return 0
	case "image":
		if imagePresent(dir, last) {
			fmt.Println("sha256:" + strings.Repeat("0", 64))
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error response from daemon: No such image: %s\n", last)
		return 1
	case "pull":
		if err := appendLine(filepath.Join(dir, "images"), last); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	case "run":
		return dockerRun(dir, args[1:])
	case "stop":
		return dockerStop(dir, args[1:])
	case "rm":
		return 0
	}
	fmt.Fprintf(os.Stderr, "docker: unknown command %q\n", args[0])
	return 2
}

// dockerRun is the stand-in's docker run, its arguments after run.
func dockerRun(dir string, args []string) int {
	var name, entrypoint, workdir string
	var interactive bool
	i := 0
	for ; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		flag, value, _ := strings.Cut(args[i], "=")
		switch flag {
		case "--name":
			name = value
		case "--entrypoint":
			entrypoint = value
		case "--workdir":
			workdir = value
		case "--interactive":
			interactive = true
		}
	}
	if i == len(args) {
		fmt.Fprintln(os.Stderr, "docker: run: no image")
		return 125
	}
	ref, rest := args[i], args[i+1:]
	if !imagePresent(dir, ref) {
		fmt.Fprintf(os.Stderr, "docker: Error response from daemon: No such image: %s\n", ref)
		return 125
	}
	if entrypoint != "standin" {
		fmt.Fprintf(os.Stderr, "docker: Error response from daemon: exec: %q: executable file not found in $PATH\n", entrypoint)
		return 127
	}
	signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM)
	container := exec.Command(filepath.Join(dir, "standin"), rest...)
	container.Dir = workdir
	if interactive {
		container.Stdin = os.Stdin
	}
	container.Stdout, container.Stderr = os.Stdout, os.Stderr
	container.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := container.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	pidFile := filepath.Join(dir, name+".pid")
	if err := writeFileAtomic(pidFile, strconv.Itoa(container.Process.Pid)+"\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	err := container.Wait()
	_ = os.Remove(pidFile)
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitErr.ExitCode()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

// dockerStop is the stand-in's docker stop, its arguments after stop.
func dockerStop(dir string, args []string) int {
	var wait time.Duration
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--time="); ok {
			wait = time.Duration(atoi(value)) * time.Second
		}
	}
	name := args[len(args)-1]
	b, err := os.ReadFile(filepath.Join(dir, name+".pid"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error response from daemon: No such container: %s\n", name)
		return 1
	}
	pid := atoi(strings.TrimSpace(string(b)))
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if !awaitGone(pid, wait) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		awaitGone(pid, 5*time.Second)
	}
	fmt.Println(name)
	return 0
}

// awaitGone polls for pid to be gone, or a zombie, for at most d, reporting whether it is.
func awaitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) || zombie(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// appendLine appends line and a newline to file, in one write.
func appendLine(file, line string) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// imagePresent reports whether ref is among the state directory's images.
func imagePresent(dir, ref string) bool {
	f, err := os.Open(filepath.Join(dir, "images"))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	for s := bufio.NewScanner(f); s.Scan(); {
		if s.Text() == ref {
			return true
		}
	}
	return false
}

// writeFileAtomic writes content to file, whole, through a rename, so a reader never sees part of
// it.
func writeFileAtomic(file, content string) error {
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// installDocker installs the stand-ins, gives the stand-in docker a daemon at version server,
// empty for one it can't reach, and returns their directory, the stand-in docker's state
// directory.
func installDocker(t *testing.T, server string) string {
	t.Helper()
	dir := filepath.Dir(installStandin(t))
	tooltest.Set(t, dockerLink(dir), dockerSettings{Server: server})
	return dir
}

// dockerLink returns the stand-in docker's path in dir, the directory installDocker returned.
func dockerLink(dir string) string { return filepath.Join(dir, "docker") }

// addImage makes ref present to the stand-in docker whose state is in dir.
func addImage(t *testing.T, dir, ref string) {
	t.Helper()
	if err := appendLine(filepath.Join(dir, "images"), ref); err != nil {
		t.Fatal(err)
	}
}

// dockerCalls returns the stand-in docker's command lines, as its run log holds them, whose
// command is command.
func dockerCalls(t *testing.T, dir, command string) [][]string {
	t.Helper()
	var calls [][]string
	for _, r := range tooltest.Runs(t, dockerLink(dir)) {
		if len(r.Args) > 0 && r.Args[0] == command {
			calls = append(calls, r.Args)
		}
	}
	return calls
}

// runningContainers returns the names of the stand-in docker's containers still running.
func runningContainers(t *testing.T, dir string) []string {
	t.Helper()
	pids, err := filepath.Glob(filepath.Join(dir, "*.pid"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range pids {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".pid"))
	}
	return names
}
