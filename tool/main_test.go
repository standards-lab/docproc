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
			if strings.TrimSpace(string(adj)) == args[1] {
				break
			}
		}
		_, _ = os.Stdout.Write(adj)
		return 0
	case "tmp":
		if err := tmp(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return standin(link, args[2:])
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

const spinCPU = 50 * time.Millisecond

var spinSink uint64

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

type tmpdir struct {
	TMPDIR  string   `json:"tmpdir"`
	HOME    string   `json:"home"`
	Entries []string `json:"entries"`
}

func tmp(file string) error {
	dir := os.Getenv("TMPDIR")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	got := tmpdir{TMPDIR: dir, HOME: os.Getenv("HOME"), Entries: []string{}}
	for _, e := range entries {
		got.Entries = append(got.Entries, e.Name())
	}
	b, err := json.Marshal(got)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file+".tmp", b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(file+".tmp", file); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "gs_")
	if err != nil {
		return err
	}
	return f.Close()
}

func writePid(file string) error {
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

type echo struct {
	Args  []string `json:"args"`
	Dir   string   `json:"dir"`
	Stdin string   `json:"stdin"`
}

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

func installStandin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	link := tooltest.Link(t, dir, "standin", nil)
	tooltest.Link(t, dir, "docker", dockerSettings{})
	return link
}
