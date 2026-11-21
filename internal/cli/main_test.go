package cli_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"tool":            standin,
		"docproc-onenote": standin,
		"docproc-ooxml":   standin,
	})
}

type standinSettings struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	StderrFile string `json:"stderr_file"`
	Warnings   int    `json:"warnings"`
	Exit       int    `json:"exit"`
	Kill       bool   `json:"kill"`
	Sleep      bool   `json:"sleep"`
}

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
