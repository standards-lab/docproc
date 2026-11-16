package tooltest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zone is the zone Main makes the test binary's local zone: five hours and 45 minutes east of UTC,
// an offset no whole-hour rounding hides, so a time left in the local zone shows in a test.
var Zone = time.FixedZone("tooltest", 5*3600+45*60)

// settingsSuffix and logSuffix end the names of a link's settings file and run log, which sit
// beside the link.
const (
	settingsSuffix = ".tooltest"
	logSuffix      = ".runs"
)

// Program is what a stand-in does as one program: it acts on the run's arguments as the program
// would, writing to os.Stdout and os.Stderr, and returns the exit code.
type Program func(r Run) int

// Run is one run of a stand-in.
type Run struct {
	// Program is the name of the program the stand-in acts as, the link's name.
	Program string
	// Link is the absolute path of the link the run was started through, which a stand-in that
	// starts another of itself runs again.
	Link string
	// Args are the run's arguments, the program's name not among them.
	Args []string
	// settings is the link's settings, as Link or Set wrote them.
	settings json.RawMessage
}

// Settings decodes the link's settings into v. A link with no settings decodes as JSON null,
// leaving v as it is.
func (r Run) Settings(v any) error {
	if len(r.settings) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.settings, v); err != nil {
		return fmt.Errorf("tooltest: %s's settings: %w", r.Program, err)
	}
	return nil
}

// Logged is one run as a link's log holds it.
type Logged struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

// Main runs a package's tests, m, and exits with their code, or, in a process started through a
// link Link made, runs the program the link names and exits with its code, never running m.
//
// Before the tests it makes Zone the local zone, and sets GORACE's atexit_sleep_ms to 0 for the
// processes the tests start, so a stand-in built under -race doesn't sleep a second as it exits.
// A stand-in's run is appended to the link's log before its program runs. A link whose program
// isn't among programs, or whose settings or log can't be read or written, exits 2 with the
// reason on stderr.
func Main(m *testing.M, programs map[string]Program) {
	if link, ok := standinLink(); ok {
		os.Exit(standin(link, programs))
	}
	time.Local = Zone
	if err := os.Setenv("GORACE", "atexit_sleep_ms=0"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

// standinLink returns the absolute path of the link the process was started through, and whether
// it was started through one: whether its argument zero, found on PATH when it has no separator,
// has a settings file beside it.
func standinLink() (string, bool) {
	if len(os.Args) == 0 {
		return "", false
	}
	path := os.Args[0]
	if !strings.ContainsRune(path, filepath.Separator) {
		found, err := exec.LookPath(path)
		if err != nil {
			return "", false
		}
		path = found
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(abs + settingsSuffix); err != nil {
		return "", false
	}
	return abs, true
}

// standin runs the stand-in started through link: it reads the link's settings, logs the run,
// and runs the link's program.
func standin(link string, programs map[string]Program) int {
	name := filepath.Base(link)
	data, err := os.ReadFile(link + settingsSuffix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tooltest:", err)
		return 2
	}
	program, ok := programs[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "tooltest: no stand-in program named %s\n", name)
		return 2
	}
	r := Run{Program: name, Link: link, Args: os.Args[1:], settings: data}
	if err := logRun(link, Logged{Program: name, Args: r.Args}); err != nil {
		fmt.Fprintln(os.Stderr, "tooltest:", err)
		return 2
	}
	return program(r)
}

// logRun appends one run to link's log, a JSON object a line, in one write, so the runs of a
// link started at once each land whole.
func logRun(link string, run Logged) error {
	line, err := json.Marshal(run)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(link+logSuffix, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Link links the running test binary as name in dir, writes settings, as JSON, to the file the
// link names, and returns the link's absolute path. A Tool built over that path, through the exec
// runner, runs the stand-in as name, which Main runs as the package's program of that name. A
// nil settings writes JSON null. dir is the caller's, typically t.TempDir(), and should hold
// nothing else of the test's named like name.
func Link(t testing.TB, dir, name string, settings any) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	Set(t, link, settings)
	return link
}

// Set replaces the settings of link, which Link made, with settings, as JSON, through a rename,
// so a run of the link reads the old settings or the new, whole.
func Set(t testing.TB, link string, settings any) {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	tmp := link + settingsSuffix + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, link+settingsSuffix); err != nil {
		t.Fatal(err)
	}
}

// Runs returns the runs of link that its log holds, in the order they began, a version command's
// included; none when it was never run.
func Runs(t testing.TB, link string) []Logged {
	t.Helper()
	data, err := os.ReadFile(link + logSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var runs []Logged
	for line := range strings.Lines(string(data)) {
		var r Logged
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("%s's log: %v", link, err)
		}
		runs = append(runs, r)
	}
	return runs
}
