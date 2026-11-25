//go:build linux

package sevenzip_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/tool"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"7zz": sevenZip})
}

type standinSettings struct {
	Version string        `json:"version"`
	Listing string        `json:"listing"`
	Items   []standinItem `json:"items"`
	Cut     int           `json:"cut"`
}

type standinItem struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	Encrypted bool   `json:"encrypted"`
}

func sevenZip(r tooltest.Run) int {
	var s standinSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	switch {
	case len(r.Args) > 0 && r.Args[0] == "l":
		fmt.Print(s.Listing)
		return 0
	case len(r.Args) > 0 && r.Args[0] == "e":
		return s.extract(r.Args)
	}
	fmt.Printf("\n7-Zip (z) %s (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-10-21\n", s.Version)
	return 0
}

func (s standinSettings) extract(args []string) int {
	include, exclude := map[string]bool{}, map[string]bool{}
	for _, a := range args {
		if name, ok := strings.CutPrefix(a, "-i!"); ok {
			include[name] = true
		}
		if name, ok := strings.CutPrefix(a, "-x!"); ok {
			exclude[name] = true
		}
	}
	var out []byte
	failed := ""
	for _, it := range s.Items {
		switch {
		case len(include) > 0 && !include[it.Name], len(include) == 0 && excluded(it.Name, exclude):
			continue
		case it.Encrypted:
			failed = it.Name
			continue
		}
		out = append(out, it.Content...)
	}
	if s.Cut > 0 && len(include) == 0 && s.Cut < len(out) {
		if _, err := os.Stdout.Write(out[:s.Cut]); err != nil {
			return 2
		}
		fmt.Fprintln(os.Stderr, "ERROR: Data Error : a damaged block")
		return 2
	}
	if _, err := os.Stdout.Write(out); err != nil {
		return 2
	}
	if failed != "" {
		fmt.Fprintf(os.Stderr, "ERROR: Wrong password : %s\n", failed)
		return 2
	}
	return 0
}

func excluded(name string, exclude map[string]bool) bool {
	for p := name; ; {
		if exclude[p] {
			return true
		}
		i := strings.LastIndexByte(p, '/')
		if i < 0 {
			return false
		}
		p = p[:i]
	}
}

func standin(t *testing.T, settings standinSettings) *tool.Tool {
	t.Helper()
	tl, _ := standinLinked(t, settings)
	return tl
}

func standinLinked(t *testing.T, settings standinSettings) (*tool.Tool, string) {
	t.Helper()
	spec := sevenzip.SevenZipSpec()
	link := tooltest.Link(t, t.TempDir(), "7zz", settings)
	spec.Program, spec.Minimum = link, "1"
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	return tl, link
}

func TestNewReaderNil(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != "sevenzip: Config.SevenZip is nil" {
			t.Errorf("recovered %v, want the nil field named", r)
		}
	}()
	_, _ = sevenzip.NewReader(sevenzip.Config{})
}

func TestNewReaderMinimum(t *testing.T) {
	t.Parallel()
	_, err := sevenzip.NewReader(sevenzip.Config{SevenZip: standin(t, standinSettings{Version: "26.05"})})
	if verr, ok := errors.AsType[*tool.VersionError](err); !ok || !errors.Is(err, tool.ErrTooOld) || verr.Found != "26.5" {
		t.Errorf("26.05: %v, want a VersionError wrapping ErrTooOld", err)
	}
	if _, err := sevenzip.NewReader(sevenzip.Config{SevenZip: standin(t, standinSettings{Version: "26.06"})}); err != nil {
		t.Errorf("26.06: %v", err)
	}
}

func TestExtractWriteFails(t *testing.T) {
	t.Parallel()
	tl := standin(t, standinSettings{Version: "26.06", Items: []standinItem{{Name: "big", Content: strings.Repeat("x", 1<<20)}}})
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	err = r.Extract(t.Context(), sevenzip.ExtractRequest{Path: "a.7z", Kind: route.SevenZip, Name: "big"}, failingWriter{})
	if !errors.Is(err, errWriterFull) {
		t.Errorf("%v, want the writer's error", err)
	}
	if _, ok := errors.AsType[*tool.ExitError](err); ok {
		t.Errorf("%v, want the writer's error, not 7zz's exit", err)
	}
}

func TestFormats(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		kind route.Kind
		name string
		flag string
	}{
		{route.RAR4, "notes.txt", "-trar"},
		{route.RAR5, "notes.txt", "-trar5"},
		{route.XZ, "", "-txz"},
		{route.Zstd, "", "-tzstd"},
	} {
		tl, link := standinLinked(t, standinSettings{
			Version: "26.06", Listing: "--\n\n----------\nSize = 8\n\n", Items: []standinItem{{Name: c.name, Content: "content\n"}},
		})
		r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.List(t.Context(), sevenzip.ListRequest{Path: "t", Kind: c.kind}); err != nil {
			t.Errorf("%s: list: %v", c.kind, err)
		}
		var b strings.Builder
		if err := r.Extract(t.Context(), sevenzip.ExtractRequest{Path: "t", Kind: c.kind, Name: c.name}, &b); err != nil || b.String() != "content\n" {
			t.Errorf("%s: extract: %q, %v", c.kind, b.String(), err)
		}
		other := "named"
		if c.name != "" {
			other = ""
		}
		if err := r.Extract(t.Context(), sevenzip.ExtractRequest{Path: "t", Kind: c.kind, Name: other}, &b); !errors.Is(err, sevenzip.ErrRequest) {
			t.Errorf("%s: extract named %q: %v, want ErrRequest", c.kind, other, err)
		}
		var flags []string
		for _, run := range tooltest.Runs(t, link) {
			for _, a := range run.Args {
				if strings.HasPrefix(a, "-t") {
					flags = append(flags, run.Args[0]+" "+a)
				}
			}
		}
		if want := []string{"l " + c.flag, "e " + c.flag}; !slices.Equal(flags, want) {
			t.Errorf("%s: runs %q, want %q", c.kind, flags, want)
		}
	}
}

var errWriterFull = errors.New("writer full")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriterFull }

func fixture(name string) string { return filepath.Join("testdata", name) }

func TestFixtureSums(t *testing.T) {
	readme, err := os.ReadFile(fixture("README.md"))
	if err != nil {
		t.Fatal(err)
	}
	sums := map[string]string{}
	for line := range strings.Lines(string(readme)) {
		cols := strings.Split(strings.TrimSpace(line), "|")
		if len(cols) < 5 || !strings.HasPrefix(strings.TrimSpace(cols[1]), "`") {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cols[1]), "`")
		sum := strings.Trim(strings.TrimSpace(cols[len(cols)-2]), "`")
		sums[name] = sum
	}
	var files []string
	for _, dir := range []string{"commons-compress", "libarchive", "generated"} {
		matches, err := filepath.Glob(fixture(filepath.Join(dir, "*")))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, path := range files {
		name := filepath.ToSlash(strings.TrimPrefix(path, fixture("")+string(filepath.Separator)))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if got, want := hex.EncodeToString(sum[:]), sums[name]; got != want {
			t.Errorf("%s: SHA-256 %s, README %q", name, got, want)
		}
	}
}
