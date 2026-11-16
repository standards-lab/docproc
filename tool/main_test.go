//go:build linux

package tool_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"standin": func(r tooltest.Run) int { return standin(r.Link, r.Args) },
		"docker":  dockerStandin,
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
//	reap CMD...       runs the stand-in on CMD as a child, waits for it, and exits with its code
//	orphan FILE [held]
//	                  starts a stubborn child, which writes its pid to FILE, waits for the
//	                  pid, then exits 0; the child has no streams, or with held, holds stdout
//	                  and stderr
//	die SIGNAL        sends itself SIGNAL, TERM or KILL, by name
//	alloc N           maps N bytes of anonymous memory, untouched, then writes allocated; exits
//	                  3, naming mmap's error, when the mapping is refused
//	as-print N TEXT   writes TEXT to stdout when its RLIMIT_AS is N bytes; else exits 1, naming
//	                  the limit it has
//	env               writes its environment to stdout as a JSON array, sorted
//	oom               writes its oom_score_adj to stdout once it is other than 0, or after 5 s
//	tick N D          writes a line N times, D apart, then exits 0
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
	case "reap":
		child := exec.Command(link, args[1:]...)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	case "orphan":
		return orphan(link, args[1], len(args) > 2 && args[2] == "held")
	case "die":
		sig := map[string]syscall.Signal{"TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL}[args[1]]
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		time.Sleep(time.Minute)
		return 1
	case "alloc":
		mem, err := syscall.Mmap(-1, 0, atoi(args[1]), syscall.PROT_READ|syscall.PROT_WRITE,
			syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE)
		if err != nil {
			fmt.Fprintln(os.Stderr, "alloc:", err)
			return 3
		}
		_ = syscall.Munmap(mem)
		fmt.Println("allocated")
		return 0
	case "as-print":
		var lim syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_AS, &lim); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if strconv.FormatUint(lim.Cur, 10) != args[1] {
			fmt.Fprintf(os.Stderr, "RLIMIT_AS is %d\n", lim.Cur)
			return 1
		}
		fmt.Println(args[2])
		return 0
	case "env":
		env := os.Environ()
		slices.Sort(env)
		if err := json.NewEncoder(os.Stdout).Encode(env); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	case "oom":
		var adj []byte
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			var err error
			if adj, err = os.ReadFile("/proc/self/oom_score_adj"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			if strings.TrimSpace(string(adj)) != "0" {
				break
			}
		}
		_, _ = os.Stdout.Write(adj)
		return 0
	case "tick":
		for range atoi(args[1]) {
			fmt.Println("tick")
			time.Sleep(duration(args[2]))
		}
		return 0
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

// orphan starts a stubborn stand-in as a child, through link, in the stand-in's process group,
// which writes its pid to file once it ignores SIGTERM; it waits for that pid, then exits 0,
// leaving the child running. The child holds none of the stand-in's streams, or, when held, its
// stdout and stderr.
func orphan(link, file string, held bool) int {
	child := exec.Command(link, "stubborn", "1m", file)
	if held {
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
	}
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(file); err == nil && strings.HasSuffix(string(b), "\n") {
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "orphan: the child wrote no pid")
	return 2
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

// installStandin links the stand-in tool as standin and the stand-in docker CLI as docker, in a
// directory of their own, its daemon one docker can't reach, and returns standin's path. The
// directory is the stand-in docker's state directory too.
func installStandin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	link := tooltest.Link(t, dir, "standin", nil)
	tooltest.Link(t, dir, "docker", dockerSettings{})
	return link
}
