//go:build linux

package tool_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"standin":       func(r tooltest.Run) int { return standin(r.Link, r.Args) },
		interpreterName: func(r tooltest.Run) int { return interpreter(r.Link, r.Args) },
		"docker":        dockerStandin,
	})
}

// standin is the stand-in tool, run through link. Its first argument picks what it does:
//
//	print TEXT        writes TEXT and a newline to stdout
//	exit CODE [TEXT]  writes TEXT to stderr, then exits with CODE
//	stderr N CODE     writes N bytes of stderr, letters a to z repeating, then exits with CODE
//	echo ARGS...      writes its arguments, working directory, and stdin to stdout as JSON
//	sleep D           exits 0 on SIGTERM, writing ready to stdout once it handles it; else
//	                  sleeps D and exits 1
//	stubborn D [FILE] ignores SIGTERM, then writes ready to stdout and its pid to FILE, then
//	                  sleeps D and exits 1
//	spawn FILE [held] starts a stubborn child, which writes its pid to FILE, then acts as
//	                  sleep 1m; the child has no streams, or with held, holds stdout and stderr
//	spin CMD...       spins until it has spent spinCPU of user CPU time, then does CMD as the
//	                  stand-in would
func standin(link string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "standin: no command")
		return 2
	}
	switch args[0] {
	case "print":
		fmt.Println(args[1])
		return 0
	case "exit":
		if len(args) > 2 {
			fmt.Fprint(os.Stderr, args[2])
		}
		return atoi(args[1])
	case "stderr":
		_, _ = os.Stderr.Write(letters(atoi(args[1])))
		return atoi(args[2])
	case "echo":
		dir, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if err := json.NewEncoder(os.Stdout).Encode(echo{Args: args[1:], Dir: dir, Stdin: string(stdin)}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	case "sleep":
		return sleep(args[1])
	case "stubborn":
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("ready")
		if len(args) > 2 {
			if err := writePid(args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		}
		time.Sleep(duration(args[1]))
		return 1
	case "spawn":
		return spawn(link, args[1], len(args) > 2 && args[2] == "held")
	case "spin":
		if err := spin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return standin(link, args[1:])
	}
	fmt.Fprintf(os.Stderr, "standin: unknown command %q\n", args[0])
	return 2
}

// spinCPU is the user CPU time the spin command spends; a usage test's program has spent at
// least this much.
const spinCPU = 50 * time.Millisecond

// spinSink keeps the spin's arithmetic from being optimized away.
var spinSink uint64

// spin busies the CPU until the process's own user CPU time, as its rusage counts it, reaches
// spinCPU. It measures CPU time rather than wall time, so a loaded machine can't cut it short.
func spin() error {
	var ru syscall.Rusage
	for {
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
			return err
		}
		if time.Duration(ru.Utime.Nano()) >= spinCPU {
			return nil
		}
		for i := range 1 << 20 {
			spinSink += uint64(i)
		}
	}
}

// sleep waits for SIGTERM and exits 0 on it, or exits 1 after d.
func sleep(d string) int {
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	fmt.Println("ready")
	select {
	case <-term:
		return 0
	case <-time.After(duration(d)):
		return 1
	}
}

// spawn starts a stubborn stand-in as a child, through link, in the stand-in's process group,
// which writes its pid to file once it ignores SIGTERM; then it sleeps as sleep does. The child
// holds none of the stand-in's streams, or, when held, its stdout and stderr.
func spawn(link, file string, held bool) int {
	child := exec.Command(link, "stubborn", "1m", file)
	if held {
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
	}
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return sleep("1m")
}

// writePid writes the process's pid and a newline to file, whole, through a rename, so a reader
// never sees part of it.
func writePid(file string) error {
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// interpreterName is the stand-in interpreter's name: the test binary, linked under this name,
// acts as a sh with a version command.
const interpreterName = "standin-sh"

// interpreter is the stand-in interpreter, run through link. With --version V it writes
// "standin-sh version V" to stdout; otherwise it replaces itself with /bin/sh on the same
// arguments, so a script runs in a real shell with the stand-in's pid and process group, its
// PATH led by link's directory, so a script runs the stand-in as standin.
func interpreter(link string, args []string) int {
	if len(args) == 2 && args[0] == "--version" {
		fmt.Println(interpreterName + " version " + args[1])
		return 0
	}
	env := append(os.Environ(), "PATH="+filepath.Dir(link)+string(filepath.ListSeparator)+os.Getenv("PATH"))
	err := syscall.Exec("/bin/sh", append([]string{"sh"}, args...), env)
	fmt.Fprintln(os.Stderr, err)
	return 2
}

// echo is what the stand-in's echo command writes.
type echo struct {
	Args  []string `json:"args"`
	Dir   string   `json:"dir"`
	Stdin string   `json:"stdin"`
}

// letters returns n bytes, a to z repeating.
func letters(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func duration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return d
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return n
}

// installStandin links the stand-in tool as standin, the stand-in interpreter as standin-sh, and
// the stand-in docker CLI as docker, in a directory of their own, its daemon one docker can't
// reach, and returns standin's path. The directory is the stand-in docker's state directory too.
func installStandin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	link := tooltest.Link(t, dir, "standin", nil)
	tooltest.Link(t, dir, interpreterName, nil)
	tooltest.Link(t, dir, "docker", dockerSettings{})
	return link
}
