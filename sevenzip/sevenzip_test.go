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

	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/tool"
)

// standinEnv marks a process as the stand-in: the test binary, re-executed through a link named
// 7zz, printing the banner of the version standinVersionEnv names instead of running the tests,
// or, for a listing, its l command, the listing standinListingEnv holds.
const (
	standinEnv        = "SEVENZIP_TEST_STANDIN"
	standinVersionEnv = "SEVENZIP_TEST_STANDIN_VERSION"
	standinListingEnv = "SEVENZIP_TEST_STANDIN_LISTING"
)

func TestMain(m *testing.M) {
	if os.Getenv(standinEnv) == "1" && filepath.Base(os.Args[0]) == "7zz" {
		if len(os.Args) > 1 && os.Args[1] == "l" {
			fmt.Print(os.Getenv(standinListingEnv))
			os.Exit(0)
		}
		fmt.Printf("\n7-Zip (z) %s (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-10-21\n", os.Getenv(standinVersionEnv))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// standin builds a 7zz Tool over the test binary, linked as 7zz, printing version's banner, its
// Spec's minimum lowered so the Tool is built whatever version it prints.
func standin(t *testing.T, version string) *tool.Tool {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "7zz")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(standinEnv, "1")
	t.Setenv(standinVersionEnv, version)
	spec := sevenzip.SevenZipSpec()
	spec.Program, spec.Minimum = link, "1"
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	return tl
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
