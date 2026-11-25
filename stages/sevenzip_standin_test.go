//go:build linux

package stages_test

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

type sevenZipSettings struct {
	Listing          string `json:"listing"`
	Content          []byte `json:"content"`
	EncryptedHeaders bool   `json:"encrypted_headers"`
}

func sevenZipStandin(r tooltest.Run) int {
	var s sevenZipSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	switch {
	case len(r.Args) > 0 && r.Args[0] == "l" && s.EncryptedHeaders:
		fmt.Fprintf(os.Stderr, "ERROR: %s : Cannot open encrypted archive. Wrong password?\n", r.Args[len(r.Args)-1])
		return 2
	case len(r.Args) > 0 && r.Args[0] == "l":
		fmt.Print(s.Listing)
		return 0
	case len(r.Args) > 0 && r.Args[0] == "e":
		if _, err := os.Stdout.Write(s.Content); err != nil {
			return 2
		}
		return 0
	}
	fmt.Printf("\n7-Zip (z) %s (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-11-06\n", sevenzip.SevenZipMinimum)
	return 0
}

func standinSevenZip(t *testing.T, settings sevenZipSettings) (*dispatch.Dispatcher, string, string) {
	t.Helper()
	spec := sevenzip.SevenZipSpec()
	link := tooltest.Link(t, t.TempDir(), "7zz", settings)
	spec.Program = link
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	d, _, work := archiveDispatcherWith(t, stages.ArchiveConfig{SevenZip: r, Version: stages.Versions(tl)})
	return d, link, work
}

func sevenZipRuns(t *testing.T, link string) []string {
	t.Helper()
	var runs []string
	for _, run := range tooltest.Runs(t, link) {
		if len(run.Args) == 0 {
			continue
		}
		named := slices.ContainsFunc(run.Args, func(a string) bool { return strings.HasPrefix(a, "-i!") })
		for _, a := range run.Args {
			if strings.HasPrefix(a, "-t") {
				runs = append(runs, fmt.Sprintf("%s %s named:%v", run.Args[0], a, named))
			}
		}
	}
	return runs
}

func rar4Head(flags byte) string {
	return "Rar!\x1a\x07\x00\xcf\x90\x73" + string([]byte{flags, 0}) + "\x0d\x00\x00\x00\x00\x00\x00\x00"
}

func rar5Head(header ...byte) string {
	return "Rar!\x1a\x07\x01\x00\x33\x92\xb5\xe5" + string(header)
}

const xzHead = "\xfd7zXZ\x00\x00\x04\xe6\xd6\xb4\x46"

func slt(items ...string) string {
	return "\n7-Zip (z) 26.06 (x64)\n\n--\nPath = t\nType = t\n\n----------\n" + strings.Join(items, "\n") + "\n"
}

func TestRARSplitVolume(t *testing.T) {
	for _, c := range []struct{ name, head string }{
		{"a.part1.rar", rar4Head(0x01)},
		{"b.part01.rar", rar5Head(0x03, 0x01, 0x00, 0x01)},
	} {
		d, link, work := standinSevenZip(t, sevenZipSettings{Listing: slt("Path = notes.txt\nSize = 5\n")})
		events := dispatchAll(t, d, input(t, c.name, c.head), nil)
		wantSteps(t, events, []step{{c.name, 0, dispatch.NoProcessor}})
		if !strings.Contains(events[0].Reason, "a split RAR volume") {
			t.Errorf("%s: reason %q", c.name, events[0].Reason)
		}
		if runs := sevenZipRuns(t, link); len(runs) != 0 {
			t.Errorf("%s: 7zz runs %q, want none", c.name, runs)
		}
		wantEmpty(t, work)
	}
}

func TestRARHandlers(t *testing.T) {
	const content = "a RAR's ordinary entry\n"
	listing := slt(fmt.Sprintf("Path = notes.txt\nSize = %d\nCRC = %08X\nAttributes =  -rw-r--r--\nEncrypted = -\n", len(content), crc32.ChecksumIEEE([]byte(content))))
	for _, c := range []struct{ name, head, flag string }{
		{"a.rar", rar4Head(0), "-trar"},
		{"b.rar", rar5Head(0x03, 0x01, 0x00, 0x00), "-trar5"},
	} {
		d, link, work := standinSevenZip(t, sevenZipSettings{Listing: listing, Content: []byte(content)})
		events := dispatchAll(t, d, input(t, c.name, c.head), nil)
		wantSteps(t, events, []step{{"notes.txt", 1, dispatch.NoProcessor}, {c.name, 0, dispatch.Prepared}})
		if ev := events[0]; ev.Part.Where != "RAR entry notes.txt" || ev.Part.SHA256 != sum(content) {
			t.Errorf("%s: notes.txt at %q, SHA-256 %s", c.name, ev.Part.Where, ev.Part.SHA256)
		}
		if got, want := sevenZipRuns(t, link), []string{"l " + c.flag + " named:false", "e " + c.flag + " named:false"}; !slices.Equal(got, want) {
			t.Errorf("%s: 7zz runs %q, want %q", c.name, got, want)
		}
		wantEmpty(t, work)
	}
}

func TestRAREncryptedHeaders(t *testing.T) {
	for _, c := range []struct{ name, head string }{
		{"a.rar", rar4Head(0)},
		{"b.rar", rar5Head(0x0c, 0x04, 0x00, 0x00, 0x00)},
	} {
		d, link, work := standinSevenZip(t, sevenZipSettings{EncryptedHeaders: true})
		events := dispatchAll(t, d, input(t, c.name, c.head), nil)
		wantSteps(t, events, []step{{c.name, 0, dispatch.Encrypted}})
		if !strings.Contains(events[0].Reason, "a RAR archive whose headers are encrypted") {
			t.Errorf("%s: reason %q", c.name, events[0].Reason)
		}
		if runs := sevenZipRuns(t, link); len(runs) != 1 {
			t.Errorf("%s: 7zz runs %q, want the listing alone", c.name, runs)
		}
		wantEmpty(t, work)
	}
}

func TestXZNames(t *testing.T) {
	cleanTar, err := os.ReadFile(filepath.Join("..", "inspect", "testdata", "clean.tar"))
	if err != nil {
		t.Fatal(err)
	}
	const notes = "an xz stream's content\n"
	for _, c := range []struct {
		name, content string
		want          []step
		where         string
	}{
		{"notes.txt.xz", notes, []step{{"notes.txt", 1, dispatch.NoProcessor}, {"notes.txt.xz", 0, dispatch.Prepared}}, "xz content notes.txt"},
		{"clean.TXZ", string(cleanTar), []step{
			{"docs/notes.txt", 2, dispatch.NoProcessor},
			{"docs/pixel.png", 2, dispatch.Prepared},
			{"clean.tar", 1, dispatch.Prepared},
			{"clean.TXZ", 0, dispatch.Prepared},
		}, "xz content clean.tar"},
		{"stream", notes, []step{{"stream", 1, dispatch.NoProcessor}, {"stream", 0, dispatch.Prepared}}, "xz content stream"},
	} {
		listing := slt(fmt.Sprintf("Size = %d\nMethod = LZMA2:12 CRC32\n", len(c.content)))
		d, link, work := standinSevenZip(t, sevenZipSettings{Listing: listing, Content: []byte(c.content)})
		events := dispatchAll(t, d, input(t, c.name, xzHead), nil)
		wantSteps(t, events, c.want)
		if got := wheres(events); !slices.Contains(got, c.where) {
			t.Errorf("%s: wheres %q, want %q among them", c.name, got, c.where)
		}
		if got, want := sevenZipRuns(t, link), []string{"l -txz named:false", "e -txz named:false"}; !slices.Equal(got, want) {
			t.Errorf("%s: 7zz runs %q, want %q", c.name, got, want)
		}
		wantEmpty(t, work)
	}
}

func TestXZDeclaredSize(t *testing.T) {
	listing := slt("Size = 100\nMethod = LZMA2:12 CRC32\n")
	d, _, work := standinSevenZip(t, sevenZipSettings{Listing: listing, Content: []byte("short\n")})
	events := dispatchAll(t, d, input(t, "short.xz", xzHead), nil)
	wantSteps(t, events, []step{{"short", 1, dispatch.Failed}, {"short.xz", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Reason != "6 bytes written, 100 declared" || ev.Part.Where != "xz content short" {
		t.Errorf("short: %q at %q", ev.Reason, ev.Part.Where)
	}
	wantEmpty(t, work)
}
