//go:build linux

package sevenzip_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/tool"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"7zz": sevenZip})
}

// standinSettings are the stand-in 7zz's settings: the version its banner names, and the listing
// its l command prints.
type standinSettings struct {
	Version string `json:"version"`
	Listing string `json:"listing"`
}

// sevenZip is the stand-in 7zz: its l command prints its settings' listing, and anything else
// prints the banner of its settings' version.
func sevenZip(r tooltest.Run) int {
	var s standinSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(r.Args) > 0 && r.Args[0] == "l" {
		fmt.Print(s.Listing)
		return 0
	}
	fmt.Printf("\n7-Zip (z) %s (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-10-21\n", s.Version)
	return 0
}

// standin builds a 7zz Tool over the test binary, linked as 7zz with settings, printing its
// version's banner, its Spec's minimum lowered so the Tool is built whatever version it prints.
func standin(t *testing.T, settings standinSettings) *tool.Tool {
	t.Helper()
	spec := sevenzip.SevenZipSpec()
	spec.Program, spec.Minimum = tooltest.Link(t, t.TempDir(), "7zz", settings), "1"
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// TestNewReaderNil: a Config with no 7zz is the caller's mistake, and panics naming the field.
func TestNewReaderNil(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != "sevenzip: Config.SevenZip is nil" {
			t.Errorf("recovered %v, want the nil field named", r)
		}
	}()
	_, _ = sevenzip.NewReader(sevenzip.Config{})
}

// fixture returns the path of one of the package's fixtures, under testdata.
func fixture(name string) string { return filepath.Join("testdata", name) }

// TestFixtureSums holds every committed fixture to the SHA-256 the testdata README gives it.
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
