package cli_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
)

// TestMain runs the package's tests in a local zone that isn't UTC, its stand-in acting as the
// envelope's tool under three names: the cases' "tool", and the two tools whose goldens it reads.
func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"tool":            standin,
		"docproc-onenote": standin,
		"docproc-ooxml":   standin,
	})
}

// standinSettings are the stand-in tool's settings, which its link names.
type standinSettings struct {
	// Stdout is what a run writes to stdout.
	Stdout string `json:"stdout"`
	// Stderr is what a run writes to stderr, after its stdout and its Warnings.
	Stderr string `json:"stderr"`
	// StderrFile, when set, is a file whose content a run writes to stderr after Stderr.
	StderrFile string `json:"stderr_file"`
	// Warnings is how many warning lines a run writes to stderr, numbered, before Stderr.
	Warnings int `json:"warnings"`
	// Exit is a run's exit code.
	Exit int `json:"exit"`
	// Kill makes a run kill itself with SIGKILL once it has written its output.
	Kill bool `json:"kill"`
	// Sleep makes a run sleep a minute after writing its stdout, for a test to stop reading.
	Sleep bool `json:"sleep"`
}

// standin is the stand-in tool. With --version alone it prints a schema "2" version, as the
// tools do; otherwise it writes its settings' output and exits as they say. It reads no input.
func standin(r tooltest.Run) int {
	var s standinSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(r.Args) == 1 && r.Args[0] == "--version" {
		fmt.Print("0.1.0\nschema 2\n")
		return 0
	}
	if _, err := os.Stdout.WriteString(s.Stdout); err != nil {
		return 1
	}
	if s.Sleep {
		time.Sleep(time.Minute)
	}
	for i := range s.Warnings {
		fmt.Fprintf(os.Stderr, "%s: warning: line %d: %s\n", r.Program, i+1, strings.Repeat("x", 100))
	}
	fmt.Fprint(os.Stderr, s.Stderr)
	if s.StderrFile != "" {
		b, err := os.ReadFile(s.StderrFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		_, _ = os.Stderr.Write(b)
	}
	if s.Kill {
		p, err := os.FindProcess(os.Getpid())
		if err == nil {
			err = p.Kill()
		}
		fmt.Fprintln(os.Stderr, err)
		time.Sleep(time.Minute)
		return 2
	}
	return s.Exit
}
