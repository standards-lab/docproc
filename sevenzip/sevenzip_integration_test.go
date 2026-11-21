//go:build integration && linux

package sevenzip_test

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/tool"
)

func TestNewReader(t *testing.T) {
	tl, err := tool.NewExec(t.Context(), sevenzip.SevenZipSpec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl}); err != nil {
		t.Errorf("7zz %s: %v", tl.Version(), err)
	}
	t.Logf("7zz %s", tl.Version())
}

func TestList(t *testing.T) {
	r := reader(t)
	list := func(name string, kind route.Kind) []sevenzip.Entry {
		t.Helper()
		l, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture(name), Kind: kind})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return l.Entries
	}
	paths := func(entries []sevenzip.Entry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Path)
		}
		return out
	}

	clean := list("generated/clean.7z", route.SevenZip)
	if len(clean) != 3 || !clean[0].Folder || clean[0].Path != "docs" {
		t.Fatalf("clean.7z: %+v", clean)
	}
	if e := clean[1]; e.Path != "docs/notes.txt" || e.Size != 22 || !e.HasCRC || e.CRC != crc32.ChecksumIEEE([]byte("a 7z's ordinary entry\n")) || e.Mode != 0o644 {
		t.Errorf("clean.7z's notes: %+v", e)
	}
	if e := list("generated/symlink.7z", route.SevenZip)[1]; e.Path != "passwd" || !e.IsSymlink() || e.Mode&fs.ModeSymlink == 0 {
		t.Errorf("symlink.7z's passwd: %+v", e)
	}
	if e := list("generated/device.7z", route.SevenZip)[0]; e.Path != "pipe" || !e.IsDevice() {
		t.Errorf("device.7z's pipe: %+v", e)
	}
	if got := paths(list("generated/traversal.7z", route.SevenZip)); strings.Join(got, ",") != "docs/../../evil.txt,notes.txt" {
		t.Errorf("traversal.7z: %q", got)
	}
	if got := paths(list("generated/absolute.7z", route.SevenZip)); strings.Join(got, ",") != `/tmp/evil.txt,C:\evil.txt` {
		t.Errorf("absolute.7z: %q", got)
	}
	enc := list("generated/encrypted.7z", route.SevenZip)
	if len(enc) != 2 || enc[0].Encrypted || !enc[1].Encrypted || enc[1].Path != "secret.txt" {
		t.Errorf("encrypted.7z: %+v", enc)
	}
	if got := paths(list("generated/traversal.cab", route.MSCF)); strings.Join(got, ",") != "notes.txt,docs/../../evil.txt" {
		t.Errorf("traversal.cab: %q", got)
	}
	if got := paths(list("generated/absolute.cab", route.MSCF)); strings.Join(got, ",") != "/tmp/evil.txt,C:/evil.txt" {
		t.Errorf("absolute.cab: %q", got)
	}
	if e := list("generated/declared-bomb.zst", route.Zstd); len(e) != 1 || !e[0].Sized || e[0].Size != 4<<30 {
		t.Errorf("declared-bomb.zst: %+v", e)
	}
	if e := list("generated/bomb.zst", route.Zstd); len(e) != 1 || e[0].Sized {
		t.Errorf("bomb.zst: %+v", e)
	}
}

func TestListRefused(t *testing.T) {
	r := reader(t)
	if _, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture("generated/encrypted-headers.7z"), Kind: route.SevenZip}); !errors.Is(err, sevenzip.ErrEncrypted) {
		t.Errorf("encrypted-headers.7z: %v, want ErrEncrypted", err)
	}
	if _, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture("generated/clean.tar.zst"), Kind: route.SevenZip}); !errors.Is(err, sevenzip.ErrNotArchive) {
		t.Errorf("a zstd stream as 7z: %v, want ErrNotArchive", err)
	}
	if _, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture("generated/multiblock.7z"), Kind: route.SevenZip, Entries: 5}); !errors.Is(err, sevenzip.ErrEntries) {
		t.Errorf("multiblock.7z past 5 entries: %v, want ErrEntries", err)
	}
	if l, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture("generated/multiblock.7z"), Kind: route.SevenZip, Entries: 13}); err != nil || len(l.Entries) != 13 {
		t.Errorf("multiblock.7z at 13 entries: %d, %v", len(l.Entries), err)
	}
	if _, err := r.List(t.Context(), sevenzip.ListRequest{Kind: route.SevenZip}); !errors.Is(err, sevenzip.ErrRequest) {
		t.Errorf("no path: %v, want ErrRequest", err)
	}
	if _, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture("generated/clean.7z"), Kind: "rar"}); !errors.Is(err, sevenzip.ErrRequest) {
		t.Errorf("rar: %v, want ErrRequest", err)
	}
}

func TestExtract(t *testing.T) {
	r := reader(t)
	extract := func(path string, kind route.Kind, name string) ([]byte, error) {
		var b bytes.Buffer
		err := r.Extract(t.Context(), sevenzip.ExtractRequest{Path: path, Kind: kind, Name: name}, &b)
		return b.Bytes(), err
	}
	for _, c := range []struct {
		path string
		kind route.Kind
		name string
		want string
	}{
		{fixture("generated/clean.7z"), route.SevenZip, "docs/notes.txt", "a 7z's ordinary entry\n"},
		{fixture("generated/traversal.7z"), route.SevenZip, "docs/../../evil.txt", "written outside the extraction directory\n"},
		{fixture("generated/traversal.cab"), route.MSCF, "docs/../../evil.txt", "written outside the extraction directory\n"},
		{fixture("generated/encrypted.7z"), route.SevenZip, "notes.txt", "an ordinary entry\n"},
	} {
		got, err := extract(c.path, c.kind, c.name)
		if err != nil || string(got) != c.want {
			t.Errorf("%s %s: %q, %v; want %q", filepath.Base(c.path), c.name, got, err, c.want)
		}
	}

	tar, err := os.ReadFile(filepath.Join("..", "inspect", "testdata", "clean.tar"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := extract(fixture("generated/clean.tar.zst"), route.Zstd, ""); err != nil || !bytes.Equal(got, tar) {
		t.Errorf("clean.tar.zst: %d bytes, %v; want inspect's clean.tar", len(got), err)
	}

	for _, c := range []struct{ path, name string }{
		{"commons-compress/lorem-ipsum-deflate64.zip", "lorem-ipsum.txt"},
		{"commons-compress/lorem-ipsum-implode.zip", "LOREM.TXT"},
		{"commons-compress/test-method-xz.zip", "LICENSE.txt"},
		{"commons-compress/bzip2-zip.zip", "lots-of-as"},
		{"libarchive/test_read_format_zip_lzma.zipx", "vimrc"},
		{"libarchive/test_read_format_zip_ppmd8.zipx", "vimrc"},
		{"libarchive/test_read_format_zip_zstd.zipx", "vimrc"},
	} {
		l, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture(c.path), Kind: route.ZIP})
		if err != nil || len(l.Entries) != 1 || l.Entries[0].Path != c.name {
			t.Fatalf("%s: %+v, %v", c.path, l.Entries, err)
		}
		got, err := extract(fixture(c.path), route.ZIP, c.name)
		if e := l.Entries[0]; err != nil || uint64(len(got)) != e.Size || crc32.ChecksumIEEE(got) != e.CRC {
			t.Errorf("%s: %d bytes, CRC-32 %08x, %v; want %d, %08x", c.path, len(got), crc32.ChecksumIEEE(got), err, e.Size, e.CRC)
		}
	}
	for _, name := range []string{"test1.xml", "test2.xml"} {
		got, err := extract(fixture("commons-compress/bla.deflate64.7z"), route.SevenZip, name)
		if err != nil || !bytes.Contains(got, []byte("<?xml")) {
			t.Errorf("bla.deflate64.7z %s: %q, %v", name, got, err)
		}
	}

	if got, err := extract(fixture("generated/clean.7z"), route.SevenZip, "docs/*.txt"); err != nil || len(got) != 0 {
		t.Errorf("a wildcard name: %q, %v; want nothing", got, err)
	}
	if _, err := extract(fixture("generated/encrypted.7z"), route.SevenZip, "secret.txt"); !errors.Is(err, sevenzip.ErrEncrypted) {
		t.Errorf("secret.txt: %v, want ErrEncrypted", err)
	}
	if _, err := extract(fixture("generated/clean.7z"), route.SevenZip, ""); !errors.Is(err, sevenzip.ErrRequest) {
		t.Errorf("a 7z with no name: %v, want ErrRequest", err)
	}
	if _, err := extract(fixture("generated/clean.tar.zst"), route.Zstd, "clean.tar"); !errors.Is(err, sevenzip.ErrRequest) {
		t.Errorf("a zstd stream with a name: %v, want ErrRequest", err)
	}
}

func TestExtractStops(t *testing.T) {
	r := reader(t)
	w := &failAfter{n: 64 << 10}
	err := r.Extract(t.Context(), sevenzip.ExtractRequest{Path: fixture("generated/bomb.7z"), Kind: route.SevenZip, Name: "zeros.bin"}, w)
	if !errors.Is(err, errFull) {
		t.Errorf("%v, want the writer's error", err)
	}
	if _, ok := errors.AsType[*tool.ExitError](err); ok {
		t.Errorf("%v, want the writer's error, not 7zz's exit", err)
	}
	if w.late != 0 {
		t.Errorf("%d writes after the writer failed, want none", w.late)
	}
}

func reader(t *testing.T) *sevenzip.Reader {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), sevenzip.SevenZipSpec())
	if err != nil {
		t.Fatalf("7zz, which mise pins, run the check through mise run check: %v", err)
	}
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type failAfter struct {
	n      int
	failed bool
	late   int
}

var errFull = errors.New("full")

func (w *failAfter) Write(p []byte) (int, error) {
	if w.failed {
		w.late++
		return 0, errFull
	}
	if len(p) > w.n {
		w.failed = true
		took := w.n
		w.n = 0
		return took, errFull
	}
	w.n -= len(p)
	return len(p), nil
}

func loggedReader(t *testing.T) (*sevenzip.Reader, func() []string) {
	t.Helper()
	real, err := exec.LookPath("7zz")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "7zz")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"${1:-version}\" >> '%s'\nexec '%s' \"$@\"\n", log, real)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := sevenzip.SevenZipSpec()
	spec.Program = script
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r, func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Fields(string(b))
	}
}

func TestReadMatches(t *testing.T) {
	for _, c := range []struct {
		path string
		kind route.Kind
		runs int
	}{
		{"generated/multiblock.7z", route.SevenZip, 1},
		{"generated/mixed.7z", route.SevenZip, 2},
		{"generated/clean.7z", route.SevenZip, 1},
		{"generated/encrypted.7z", route.SevenZip, 2},
		{"commons-compress/bla.deflate64.7z", route.SevenZip, 1},
		{"generated/notebook.cab", route.MSCF, 2},
		{"generated/linebreak.cab", route.MSCF, 4},
	} {
		r, runs := loggedReader(t)
		l, err := r.List(t.Context(), sevenzip.ListRequest{Path: fixture(c.path), Kind: c.kind})
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		type read struct {
			content []byte
			err     error
		}
		var whole []read
		for item, err := range r.Read(t.Context(), sevenzip.ReadRequest{Path: fixture(c.path), Kind: c.kind, Listing: l}) {
			if err != nil {
				t.Fatalf("%s: %v", c.path, err)
			}
			b, err := io.ReadAll(item.Content)
			whole = append(whole, read{b, err})
		}
		if got := extractions(runs()); got != c.runs {
			t.Errorf("%s: read in %d 7zz runs, want %d", c.path, got, c.runs)
		}
		if len(whole) != len(l.Entries) {
			t.Fatalf("%s: read %d items, listed %d", c.path, len(whole), len(l.Entries))
		}
		for i, e := range l.Entries {
			if e.Folder {
				continue
			}
			var alone bytes.Buffer
			err := r.Extract(t.Context(), sevenzip.ExtractRequest{Path: fixture(c.path), Kind: c.kind, Name: e.Path}, &alone)
			bad := err != nil || (e.Sized && uint64(alone.Len()) != e.Size) || (e.HasCRC && crc32.ChecksumIEEE(alone.Bytes()) != e.CRC)
			switch w := whole[i]; {
			case bad != (w.err != nil):
				t.Errorf("%s %q: read whole %v; alone %d bytes, %v", c.path, e.Path, w.err, alone.Len(), err)
			case !bad && !bytes.Equal(w.content, alone.Bytes()):
				t.Errorf("%s %q: read whole %d bytes, alone %d, not the same", c.path, e.Path, len(w.content), alone.Len())
			}
		}
	}
}

func extractions(logged []string) int {
	n := 0
	for _, cmd := range logged {
		if cmd == "e" {
			n++
		}
	}
	return n
}

func TestReadLineBreak(t *testing.T) {
	r := reader(t)
	path := fixture("generated/linebreak.cab")
	l, err := r.List(t.Context(), sevenzip.ListRequest{Path: path, Kind: route.MSCF})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"before.txt":     "the entry before\n",
		"line_break.txt": "",
		"return\r":       "a name ending in a carriage return\n",
		"after.txt":      "the entry after\n",
	}
	var names []string
	for item, err := range r.Read(t.Context(), sevenzip.ReadRequest{Path: path, Kind: route.MSCF, Listing: l}) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, item.Path)
		b, err := io.ReadAll(item.Content)
		if string(b) != want[item.Path] || (item.Path == "line_break.txt") != errors.Is(err, sevenzip.ErrContent) {
			t.Errorf("%q: %q, %v", item.Path, b, err)
		}
	}
	if strings.Join(names, ",") != "before.txt,line_break.txt,return\r,after.txt" {
		t.Errorf("listed %q", names)
	}
}
