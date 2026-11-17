package stages_test

import (
	"context"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/stages"
)

// sevenZipSignature begins a 7z archive, so a test input routes to the archive stage as a 7z.
const sevenZipSignature = "7z\xbc\xaf\x27\x1c\x00\x04"

// fakeSevenZip stands in for sevenzip's Reader: List returns its listing, or its listErr; Read
// yields its listing's items, each one's content the content its contents hold for its name; and
// Extract writes that content for the name asked. A name is recorded as extracted when its
// content is first read, or Extract writes it.
type fakeSevenZip struct {
	listing   sevenzip.Listing
	listErr   error
	contents  map[string]string
	mu        sync.Mutex
	extracted []string
}

func (f *fakeSevenZip) List(context.Context, sevenzip.ListRequest) (sevenzip.Listing, error) {
	return f.listing, f.listErr
}

func (f *fakeSevenZip) Read(_ context.Context, req sevenzip.ReadRequest) iter.Seq2[sevenzip.Item, error] {
	return func(yield func(sevenzip.Item, error) bool) {
		for _, e := range req.Listing.Entries {
			if !yield(sevenzip.Item{Entry: e, Content: &fakeContent{f: f, name: e.Path}}, nil) {
				return
			}
		}
	}
}

func (f *fakeSevenZip) Extract(_ context.Context, req sevenzip.ExtractRequest, w io.Writer) error {
	f.note(req.Name)
	_, err := io.WriteString(w, f.contents[req.Name])
	return err
}

// note records name as extracted.
func (f *fakeSevenZip) note(name string) {
	f.mu.Lock()
	f.extracted = append(f.extracted, name)
	f.mu.Unlock()
}

// fakeContent is an item's content as fakeSevenZip's Read yields it, noted at its first read.
type fakeContent struct {
	f    *fakeSevenZip
	name string
	r    *strings.Reader
}

func (c *fakeContent) Read(p []byte) (int, error) {
	if c.r == nil {
		c.f.note(c.name)
		c.r = strings.NewReader(c.f.contents[c.name])
	}
	return c.r.Read(p)
}

// fakeEntries returns a listing's file entries of contents' names, in names' order, each sized.
func fakeEntries(contents map[string]string, names ...string) []sevenzip.Entry {
	var entries []sevenzip.Entry
	for _, n := range names {
		entries = append(entries, sevenzip.Entry{Path: n, Size: uint64(len(contents[n])), Sized: true, Mode: 0o644})
	}
	return entries
}

// TestListingQuarantine: under a policy that quarantines archive-traversal, a 7z whose listing
// names a traversing entry is quarantined by the stage before anything is extracted: its outcome
// is quarantined, its flags hold the listing's flag beside its inspection's and its reason says a
// policy quarantined it, and 7-Zip extracts nothing. By default, the flag active, the ordinary
// entry is extracted and found, "7z entry <name>", and the traversing one is an unread child,
// quarantined with its flag, never extracted.
func TestListingQuarantine(t *testing.T) {
	contents := map[string]string{"notes.txt": "an ordinary entry\n", "docs/../../evil.txt": "evil\n"}
	fake := func() *fakeSevenZip {
		return &fakeSevenZip{
			listing:  sevenzip.Listing{Entries: fakeEntries(contents, "notes.txt", "docs/../../evil.txt")},
			contents: contents,
		}
	}
	strict := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{inspect.ArchiveTraversal: inspect.Quarantine}}
	sz := fake()
	d, zs, work := archiveDispatcherWith(t, stages.ArchiveConfig{Inspect: strict, SevenZip: sz, Version: "7zz 26.5"})
	events := dispatchAll(t, d, input(t, "a.7z", sevenZipSignature), nil)
	wantSteps(t, events, []step{{"a.7z", 0, dispatch.Quarantined}})
	ev := events[0]
	if !slices.ContainsFunc(ev.Part.Flags, func(f inspect.Flag) bool {
		return f.Check == inspect.ArchiveTraversal && f.Evidence == `entry "docs/../../evil.txt"`
	}) || ev.Reason != "quarantined by policy" {
		t.Errorf("a.7z: flags %v, reason %q", ev.Part.Flags, ev.Reason)
	}
	if ev.Stage.Name != "archive" || zs.calls.Load() != 1 || len(sz.extracted) != 0 || len(ev.Outputs) != 0 {
		t.Errorf("a.7z: stage %v, called %d times, extracted %q, %d outputs", ev.Stage, zs.calls.Load(), sz.extracted, len(ev.Outputs))
	}
	wantEmpty(t, work)

	sz = fake()
	d, _, work = archiveDispatcherWith(t, stages.ArchiveConfig{SevenZip: sz, Version: "7zz 26.5"})
	events = dispatchAll(t, d, input(t, "a.7z", sevenZipSignature), nil)
	wantSteps(t, events, []step{
		{"notes.txt", 1, dispatch.NoProcessor},
		{"docs/../../evil.txt", 1, dispatch.Quarantined},
		{"a.7z", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"7z entry notes.txt", "7z entry docs/../../evil.txt"}; !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if !hasFlag(events[1].Part.Flags, inspect.ArchiveTraversal) || events[1].Part.SHA256 != "" {
		t.Errorf("the traversing entry: flags %v, SHA-256 %q", events[1].Part.Flags, events[1].Part.SHA256)
	}
	if !slices.Equal(sz.extracted, []string{"notes.txt"}) {
		t.Errorf("extracted %q", sz.extracted)
	}
	wantEmpty(t, work)
}

// TestListedMismatch: an entry whose bytes from 7-Zip aren't the size or CRC-32 its listing
// declares is an unread child, failed, its reason naming the mismatch, its partial file removed,
// and the archive is prepared.
func TestListedMismatch(t *testing.T) {
	contents := map[string]string{"short.txt": "short\n", "crc.txt": "crc\n"}
	entries := fakeEntries(contents, "short.txt", "crc.txt")
	entries[0].Size = 100
	entries[1].CRC, entries[1].HasCRC = 0xdeadbeef, true
	sz := &fakeSevenZip{listing: sevenzip.Listing{Entries: entries}, contents: contents}
	d, _, work := archiveDispatcherWith(t, stages.ArchiveConfig{SevenZip: sz, Version: "7zz 26.5"})
	events := dispatchAll(t, d, input(t, "a.7z", sevenZipSignature), nil)
	wantSteps(t, events, []step{{"short.txt", 1, dispatch.Failed}, {"crc.txt", 1, dispatch.Failed}, {"a.7z", 0, dispatch.Prepared}})
	for i, want := range []string{"6 bytes written, 100 listed", "CRC-32 "} {
		if ev := events[i]; !strings.HasPrefix(ev.Reason, want) || ev.Part.Where != "7z entry "+ev.Part.Name {
			t.Errorf("%s: reason %q, where %q; want reason %q", ev.Part.Name, ev.Reason, ev.Part.Where, want)
		}
	}
	wantEmpty(t, work)
}

// TestListedFailures: a 7z whose headers are encrypted ends encrypted, nothing extracted, and a
// listing 7-Zip fails fails the part.
func TestListedFailures(t *testing.T) {
	for _, c := range []struct {
		err     error
		outcome dispatch.Outcome
	}{
		{fmt.Errorf("sevenzip: list: %w: Cannot open encrypted archive", sevenzip.ErrEncrypted), dispatch.Encrypted},
		{fmt.Errorf("sevenzip: list: %w: Cannot open the file as [7z] archive", sevenzip.ErrNotArchive), dispatch.Failed},
	} {
		sz := &fakeSevenZip{listErr: c.err}
		d, _, work := archiveDispatcherWith(t, stages.ArchiveConfig{SevenZip: sz, Version: "7zz 26.5"})
		events := dispatchAll(t, d, input(t, "a.7z", sevenZipSignature), nil)
		wantSteps(t, events, []step{{"a.7z", 0, c.outcome}})
		if len(sz.extracted) != 0 || !strings.Contains(events[0].Reason, "a 7z archive") && c.outcome == dispatch.Encrypted {
			t.Errorf("%v: extracted %q, reason %q", c.err, sz.extracted, events[0].Reason)
		}
		wantEmpty(t, work)
	}
}
