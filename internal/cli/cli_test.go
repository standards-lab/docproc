package cli_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

func TestOutcome(t *testing.T) {
	all := []error{cli.ErrNotWritten, cli.ErrUsage, cli.ErrRefused, cli.ErrFailed}
	for code := -1; code <= 9; code++ {
		var want error
		if code >= 1 && code <= 4 {
			want = all[code-1]
		}
		if got := cli.Outcome(code); got != want {
			t.Errorf("Outcome(%d) = %v, want %v", code, got, want)
		}
		err := error(&cli.ExitError{ExitError: &tool.ExitError{Program: "p", Code: code}})
		for _, other := range all {
			if errors.Is(err, other) != (other == want) {
				t.Errorf("exit code %d matches %v: %t", code, other, other != want)
			}
		}
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := &cli.LimitedBuffer{Limit: 5}
	for _, p := range []string{"abc", "defg", "hij"} {
		if n, err := b.Write([]byte(p)); n != len(p) || err != nil {
			t.Errorf("Write(%q) = %d, %v", p, n, err)
		}
	}
	if b.String() != "abcde" {
		t.Errorf("kept %q, want %q", b.String(), "abcde")
	}
	var zero cli.LimitedBuffer
	_, _ = zero.Write([]byte("x"))
	if zero.String() != "" {
		t.Errorf("zero limit kept %q", zero.String())
	}
}

func TestSpool(t *testing.T) {
	dir := t.TempDir()
	path, remove, err := cli.Spool(dir, strings.NewReader("doc"), ".pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), "spool-") || filepath.Ext(path) != ".pdf" {
		t.Errorf("path %q", path)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "doc" {
		t.Errorf("content %q, %v", b, err)
	}
	if err := remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after remove: %v", err)
	}

	if _, _, err := cli.Spool("", strings.NewReader("doc"), ""); err == nil || err.Error() != "spool: no working directory" {
		t.Errorf("empty dir: %v", err)
	}
	broken := errors.New("broken")
	_, _, err = cli.Spool(dir, io.MultiReader(strings.NewReader("part"), iotest.ErrReader(broken)), "")
	if !errors.Is(err, broken) || !strings.HasPrefix(err.Error(), "spool: ") {
		t.Errorf("failed copy: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("failed copy left %v", entries)
	}
}
