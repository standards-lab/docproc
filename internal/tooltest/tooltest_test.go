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

type echoSettings struct {
	Prefix string `json:"prefix"`
	Code   int    `json:"code"`
}

func echo(r tooltest.Run) int {
	var s echoSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println(strings.Join(append([]string{s.Prefix}, r.Args...), " "))
	return s.Code
}

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

func TestSet(t *testing.T) {
	t.Parallel()
	link := tooltest.Link(t, t.TempDir(), "echo", echoSettings{Prefix: "old"})
	tooltest.Set(t, link, echoSettings{Prefix: "new"})
	if out, err := exec.Command(link).Output(); err != nil || string(out) != "new\n" {
		t.Errorf("%q, %v", out, err)
	}
}

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

func TestUnknownProgram(t *testing.T) {
	t.Parallel()
	link := tooltest.Link(t, t.TempDir(), "nothing", nil)
	out, err := exec.Command(link).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "no stand-in program named nothing") {
		t.Errorf("%q, %v", out, err)
	}
}

func TestZone(t *testing.T) {
	t.Parallel()
	if time.Local != tooltest.Zone {
		t.Fatalf("local zone %v, want %v", time.Local, tooltest.Zone)
	}
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Error("the local zone's offset is 0")
	}
}

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

func TestToolsMissingFile(t *testing.T) {
	_, f := tools(t, "envelope/no-such-cases.json")
	if f.skipped != "" || !strings.Contains(f.failed, "envelope/no-such-cases.json") {
		t.Errorf("skipped %q, failed %q", f.skipped, f.failed)
	}
}

func TestToolsSkip(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/standards-lab/docproc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestPNG(t *testing.T) {
	b := tooltest.PNG("page 1")
	rest, ok := bytes.CutPrefix(b, []byte("\x89PNG\r\n\x1a\n"))
	if !ok {
		t.Fatalf("PNG(page 1) = %q, want PNG's signature first", b)
	}
	var kinds []string
	var text string
	for len(rest) > 0 {
		if len(rest) < 12 {
			t.Fatalf("PNG(page 1) ends in %q, short of a chunk", rest)
		}
		n := int(binary.BigEndian.Uint32(rest))
		if len(rest) < 12+n {
			t.Fatalf("PNG(page 1): a chunk of %d bytes in %d", n, len(rest))
		}
		chunk := rest[4 : 8+n]
		if crc := binary.BigEndian.Uint32(rest[8+n:]); crc != crc32.ChecksumIEEE(chunk) {
			t.Errorf("PNG(page 1): chunk %q's CRC doesn't check", chunk[:4])
		}
		kinds = append(kinds, string(chunk[:4]))
		if keyword, value, ok := strings.Cut(string(chunk[4:]), "\x00"); string(chunk[:4]) == "tEXt" && ok && keyword == "Comment" {
			text = value
		}
		rest = rest[12+n:]
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != "IEND" || text != "page 1" {
		t.Errorf("PNG(page 1): chunks %q, text %q; want IEND last and the text page 1", kinds, text)
	}
	if bytes.Equal(b, tooltest.PNG("page 2")) {
		t.Error("PNG(page 1) and PNG(page 2) are equal")
	}
}
