package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/cli"
)

func TestLineWriter(t *testing.T) {
	var got []string
	w := &cli.LineWriter{Line: func(s string) error { got = append(got, s); return nil }}
	for _, p := range []string{"one\r", "\ntw", "o \n\n  three", "\n" + strings.Repeat("x", cli.MaxLine+10) + "\nlast"} {
		if n, err := w.Write([]byte(p)); n != len(p) || err != nil {
			t.Errorf("Write(%q) = %d, %v", p, n, err)
		}
	}
	want := []string{"one", "two", "three", strings.Repeat("x", cli.MaxLine)}
	if !slices.Equal(got, want) {
		t.Errorf("lines before Flush %q, want %q", got, want)
	}
	w.Flush()
	if want = append(want, "last"); !slices.Equal(got, want) || w.Last() != "last" || w.Err() != nil {
		t.Errorf("lines %q, last %q, err %v", got, w.Last(), w.Err())
	}

	broken := errors.New("broken")
	got = nil
	w = &cli.LineWriter{Line: func(s string) error { got = append(got, s); return broken }}
	_, _ = w.Write([]byte("a\nb\nc\n"))
	if !slices.Equal(got, []string{"a"}) || !errors.Is(w.Err(), broken) || w.Last() != "c" {
		t.Errorf("after an error: lines %q, err %v, last %q", got, w.Err(), w.Last())
	}

	var silent cli.LineWriter
	_, _ = silent.Write([]byte("only the last\n"))
	if silent.Last() != "only the last" || silent.Err() != nil {
		t.Errorf("nil Line: last %q, err %v", silent.Last(), silent.Err())
	}
}

func TestWriteTemp(t *testing.T) {
	dir := t.TempDir()
	path, err := cli.WriteTemp(dir, ".window-*.wav", []byte("RIFF"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), ".window-") || filepath.Ext(path) != ".wav" {
		t.Errorf("path %q", path)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "RIFF" {
		t.Errorf("content %q, %v", b, err)
	}
	if _, err := cli.WriteTemp(filepath.Join(dir, "missing"), ".x-*", nil); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing dir: %v", err)
	}
}
