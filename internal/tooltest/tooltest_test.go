package tooltest_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"echo": echo})
}

// echoSettings are the echo stand-in's settings.
type echoSettings struct {
	Prefix string `json:"prefix"`
	Code   int    `json:"code"`
}

// echo writes its settings' prefix and its arguments, space-separated, to stdout, and exits with
// its settings' code.
func echo(r tooltest.Run) int {
	var s echoSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println(strings.Join(append([]string{s.Prefix}, r.Args...), " "))
	return s.Code
}

// TestLink: a link runs the stand-in as its program with its own settings, logs each run, and
// two links in one test, with settings of their own, run apart from each other and from the
// links of other tests running in parallel.
func TestLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := tooltest.Link(t, dir, "echo", echoSettings{Prefix: "a"})
	b := tooltest.Link(t, t.TempDir(), "echo", echoSettings{Prefix: "b", Code: 3})
	out, err := exec.Command(a, "one", "two").Output()
	if err != nil || string(out) != "a one two\n" {
		t.Errorf("a: %q, %v", out, err)
	}
	out, err = exec.Command(b, "three").Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || string(out) != "b three\n" {
		t.Errorf("b: %q, %v, want exit 3", out, err)
	}
	want := []tooltest.Logged{{Program: "echo", Args: []string{"one", "two"}}}
	if got := tooltest.Runs(t, a); !slices.EqualFunc(got, want, equalLogged) {
		t.Errorf("a's runs %+v, want %+v", got, want)
	}
	if got := tooltest.Runs(t, filepath.Join(dir, "unrun")); got != nil {
		t.Errorf("an unrun link's runs %+v", got)
	}
}

// TestSet: a run after Set reads the new settings.
func TestSet(t *testing.T) {
	t.Parallel()
	link := tooltest.Link(t, t.TempDir(), "echo", echoSettings{Prefix: "old"})
	tooltest.Set(t, link, echoSettings{Prefix: "new"})
	if out, err := exec.Command(link).Output(); err != nil || string(out) != "new\n" {
		t.Errorf("%q, %v", out, err)
	}
}

// TestRunsConcurrent: runs of one link started at once each land whole in its log.
func TestRunsConcurrent(t *testing.T) {
	t.Parallel()
	link := tooltest.Link(t, t.TempDir(), "echo", nil)
	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			if err := exec.Command(link, fmt.Sprint(i)).Run(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var got []string
	for _, r := range tooltest.Runs(t, link) {
		got = append(got, r.Args...)
	}
	slices.Sort(got)
	if want := []string{"0", "1", "2", "3", "4", "5", "6", "7"}; !slices.Equal(got, want) {
		t.Errorf("logged %q, want %q", got, want)
	}
}

// TestUnknownProgram: a link whose name is no program of the package's exits 2, naming it.
func TestUnknownProgram(t *testing.T) {
	t.Parallel()
	link := tooltest.Link(t, t.TempDir(), "nothing", nil)
	out, err := exec.Command(link).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "no stand-in program named nothing") {
		t.Errorf("%q, %v", out, err)
	}
}

// TestZone: the tests run in a local zone that isn't UTC.
func TestZone(t *testing.T) {
	t.Parallel()
	if time.Local != tooltest.Zone {
		t.Fatalf("local zone %v, want %v", time.Local, tooltest.Zone)
	}
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Error("the local zone's offset is 0")
	}
}

// fakeTB records a test helper's Skip and Fatal; each ends the goroutine it is called in, as
// testing's do.
type fakeTB struct {
	testing.TB
	skipped, failed string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Skip(args ...any) {
	f.skipped = fmt.Sprint(args...)
	runtime.Goexit()
}

func (f *fakeTB) Fatal(args ...any) {
	f.failed = fmt.Sprint(args...)
	runtime.Goexit()
}

// tools runs Tools for rel in a goroutine of its own over a fakeTB, and returns its path and the
// fakeTB.
func tools(t *testing.T, rel string) (string, *fakeTB) {
	f := &fakeTB{TB: t}
	var path string
	done := make(chan struct{})
	go func() {
		defer close(done)
		path = tooltest.Tools(f, rel)
	}()
	<-done
	return path, f
}

// TestTools: in the repository, the tools tree is beside the base module, and Tools returns the
// envelope's shared cases, which parse.
func TestTools(t *testing.T) {
	path, f := tools(t, "envelope/cases.json")
	if f.skipped != "" || f.failed != "" {
		t.Fatalf("skipped %q, failed %q", f.skipped, f.failed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Errorf("%s isn't JSON", path)
	}
}

// TestToolsMissingFile: with the tools tree present, a path it doesn't hold fails the test,
// naming the path.
func TestToolsMissingFile(t *testing.T) {
	_, f := tools(t, "envelope/no-such-cases.json")
	if f.skipped != "" || !strings.Contains(f.failed, "envelope/no-such-cases.json") {
		t.Errorf("skipped %q, failed %q", f.skipped, f.failed)
	}
}

// TestToolsSkip: in a module with no tools module beside it, as the base module's zip unpacks,
// Tools skips the test, naming the tree it looked for and the path it didn't read.
func TestToolsSkip(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/standards-lab/docproc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A tools directory without its go.mod is no tools module, as a zip holding stray files
	// under tools/ would have.
	if err := os.Mkdir(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(pkg)
	_, f := tools(t, "envelope/cases.json")
	tree := filepath.Join(root, "tools")
	if f.failed != "" || !strings.Contains(f.skipped, "no tools tree at "+tree) || !strings.Contains(f.skipped, "envelope/cases.json") {
		t.Errorf("skipped %q, failed %q", f.skipped, f.failed)
	}
}

func equalLogged(a, b tooltest.Logged) bool {
	return a.Program == b.Program && slices.Equal(a.Args, b.Args)
}

// TestPNG: the stand-in's PNG is PNG's signature, a tEXt chunk holding its text, and IEND, each
// chunk its length, its type and data, and their CRC; and two texts give two PNGs.
func TestPNG(t *testing.T) {
	b := tooltest.PNG("page 1")
	text := []byte("tEXtComment\x00page 1")
	want := []byte("\x89PNG\r\n\x1a\n")
	want = binary.BigEndian.AppendUint32(want, uint32(len(text)-4))
	want = binary.BigEndian.AppendUint32(append(want, text...), crc32.ChecksumIEEE(text))
	// IEND's CRC is fixed: every PNG ends with these twelve bytes.
	want = append(want, "\x00\x00\x00\x00IEND\xaeB`\x82"...)
	if !bytes.Equal(b, want) {
		t.Errorf("PNG(page 1) = %q, want %q", b, want)
	}
	if bytes.Equal(b, tooltest.PNG("page 2")) {
		t.Error("PNG(page 1) and PNG(page 2) are equal")
	}
}
