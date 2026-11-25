//go:build linux

package sevenzip_test

import (
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
)

func file(name, content string) sevenzip.Entry {
	return sevenzip.Entry{Path: name, Size: uint64(len(content)), Sized: true, CRC: crc32.ChecksumIEEE([]byte(content)), HasCRC: true, Mode: 0o644}
}

type archive struct {
	entries []sevenzip.Entry
	items   []standinItem
}

func (a *archive) add(e sevenzip.Entry, content string) {
	a.entries = append(a.entries, e)
	if !e.Folder {
		a.items = append(a.items, standinItem{Name: e.Path, Content: content, Encrypted: e.Encrypted})
	}
}

type readResult struct {
	name    string
	content string
	err     error
}

func readAll(t *testing.T, a archive, cut int, partial string, limit int64) ([]readResult, [][]string) {
	t.Helper()
	tl, link := standinLinked(t, standinSettings{Version: "26.06", Items: a.items, Cut: cut})
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	var got []readResult
	for item, err := range r.Read(t.Context(), sevenzip.ReadRequest{Path: "t.7z", Kind: route.SevenZip, Listing: sevenzip.Listing{Entries: a.entries}}) {
		if err != nil {
			t.Fatal(err)
		}
		src := item.Content
		if item.Path == partial {
			src = io.LimitReader(src, limit)
		}
		b, err := io.ReadAll(src)
		got = append(got, readResult{item.Path, string(b), err})
	}
	var runs [][]string
	for _, run := range tooltest.Runs(t, link) {
		if len(run.Args) > 0 && run.Args[0] == "e" {
			runs = append(runs, run.Args)
		}
	}
	return got, runs
}

func named(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-i!") || strings.HasPrefix(a, "-x!") {
			out = append(out, a)
		}
	}
	return out
}

func wantRuns(t *testing.T, runs [][]string, want ...[]string) {
	t.Helper()
	if len(runs) != len(want) {
		t.Fatalf("%d e runs %q, want %d", len(runs), runs, len(want))
	}
	for i, run := range runs {
		if got := named(run); !slices.Equal(got, want[i]) {
			t.Errorf("run %d names %q, want %q", i, got, want[i])
		}
	}
}

func TestReadWhole(t *testing.T) {
	t.Parallel()
	var a archive
	a.add(sevenzip.Entry{Path: "docs", Folder: true, Sized: true, Mode: fs.ModeDir | 0o755}, "")
	a.add(file("docs/a.txt", "the first file\n"), "the first file\n")
	link := file("passwd", "/etc/passwd")
	link.Mode = fs.ModeSymlink | 0o777
	a.add(link, "/etc/passwd")
	a.add(file("docs/b.txt", "the second, after a link\n"), "the second, after a link\n")
	secret := file("secret.txt", "encrypted\n")
	secret.Encrypted = true
	a.add(secret, "encrypted\n")
	a.add(sevenzip.Entry{Path: "empty.txt", Sized: true}, "")
	unsized := sevenzip.Entry{Path: "unsized.txt"}
	a.add(unsized, "no size listed\n")
	a.add(file("c.txt", "the last\n"), "the last\n")

	got, runs := readAll(t, a, 0, "", 0)
	want := []readResult{
		{"docs", "", nil},
		{"docs/a.txt", "the first file\n", nil},
		{"passwd", "/etc/passwd", nil},
		{"docs/b.txt", "the second, after a link\n", nil},
		{"secret.txt", "", sevenzip.ErrEncrypted},
		{"empty.txt", "", nil},
		{"unsized.txt", "no size listed\n", nil},
		{"c.txt", "the last\n", nil},
	}
	wantRead(t, got, want)
	wantRuns(t, runs,
		[]string{"-x!passwd", "-x!secret.txt", "-x!unsized.txt"},
		[]string{"-i!passwd"}, []string{"-i!secret.txt"}, []string{"-i!unsized.txt"})
}

func wantRead(t *testing.T, got, want []readResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("read %d items, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.name != w.name || g.content != w.content || (w.err == nil) != (g.err == nil) || !errors.Is(g.err, w.err) {
			t.Errorf("item %d: %q %q %v; want %q %q %v", i, g.name, g.content, g.err, w.name, w.content, w.err)
		}
	}
}

func threeFiles() archive {
	var a archive
	for _, c := range []struct{ name, content string }{{"a", "aaaaaaaaa\n"}, {"b", "bbbbbbbbbb\n"}, {"c", "ccccccccccc\n"}} {
		a.add(file(c.name, c.content), c.content)
	}
	return a
}

func TestReadMismatch(t *testing.T) {
	t.Parallel()
	a := threeFiles()
	a.entries[1].CRC ^= 1
	got, runs := readAll(t, a, 0, "", 0)
	wantRead(t, got, []readResult{{"a", "aaaaaaaaa\n", nil}, {"b", "bbbbbbbbbb\n", sevenzip.ErrContent}, {"c", "ccccccccccc\n", nil}})
	wantRuns(t, runs, nil, []string{"-i!c"})
}

func TestReadCut(t *testing.T) {
	t.Parallel()
	got, runs := readAll(t, threeFiles(), 15, "", 0)
	wantRead(t, got, []readResult{{"a", "aaaaaaaaa\n", nil}, {"b", "bbbbb", sevenzip.ErrContent}, {"c", "ccccccccccc\n", nil}})
	if !strings.Contains(got[1].err.Error(), "Data Error") {
		t.Errorf("a cut item's error %q names no run message", got[1].err)
	}
	wantRuns(t, runs, nil, []string{"-i!c"})

	got, runs = readAll(t, threeFiles(), 10, "", 0)
	wantRead(t, got, []readResult{{"a", "aaaaaaaaa\n", nil}, {"b", "bbbbbbbbbb\n", nil}, {"c", "ccccccccccc\n", nil}})
	wantRuns(t, runs, nil, []string{"-i!b"}, []string{"-i!c"})
}

func TestReadUnfinished(t *testing.T) {
	t.Parallel()
	got, runs := readAll(t, threeFiles(), 0, "a", 4)
	wantRead(t, got, []readResult{{"a", "aaaa", nil}, {"b", "bbbbbbbbbb\n", nil}, {"c", "ccccccccccc\n", nil}})
	wantRuns(t, runs, nil, []string{"-i!b"}, []string{"-i!c"})

	got, runs = readAll(t, threeFiles(), 0, "b", 0)
	wantRead(t, got, []readResult{{"a", "aaaaaaaaa\n", nil}, {"b", "", nil}, {"c", "ccccccccccc\n", nil}})
	wantRuns(t, runs, nil, []string{"-i!c"})
}

func TestReadAlone(t *testing.T) {
	t.Parallel()
	var a archive
	a.add(file("plain", "plain\n"), "plain\n")
	x := file("x", "x's target")
	x.Mode = fs.ModeSymlink | 0o777
	a.add(x, "x's target")
	a.add(file("x/under", "under x\n"), "under x\n")
	a.add(file("xy", "not under x\n"), "not under x\n")
	got, runs := readAll(t, a, 0, "", 0)
	wantRead(t, got, []readResult{{"plain", "plain\n", nil}, {"x", "x's target", nil}, {"x/under", "under x\n", nil}, {"xy", "not under x\n", nil}})
	wantRuns(t, runs, []string{"-x!x"}, []string{"-i!x"}, []string{"-i!x/under"})

	var b archive
	b.add(file("first", "first\n"), "first\n")
	odd := sevenzip.Entry{Path: "line_break"}
	b.add(odd, "unsized\n")
	b.add(file("last", "last\n"), "last\n")
	got, runs = readAll(t, b, 0, "", 0)
	wantRead(t, got, []readResult{{"first", "first\n", nil}, {"line_break", "unsized\n", nil}, {"last", "last\n", nil}})
	wantRuns(t, runs, []string{"-i!first"}, []string{"-i!line_break"}, []string{"-i!last"})
}

func TestReadRefused(t *testing.T) {
	t.Parallel()
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: standin(t, standinSettings{Version: "26.06"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []sevenzip.ReadRequest{{Kind: route.SevenZip}, {Path: "t.arj", Kind: "arj"}} {
		for _, err := range r.Read(t.Context(), req) {
			if !errors.Is(err, sevenzip.ErrRequest) {
				t.Errorf("%+v: %v, want ErrRequest", req, err)
			}
		}
	}
}
