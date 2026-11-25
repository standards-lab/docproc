package stages_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

func bzip2Fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(sevenZipFixture("generated/" + name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBzip2Stage(t *testing.T) {
	d, zs, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, sevenZipFixture("generated/notes.txt.bz2"), nil)
	wantSteps(t, events, []step{{"notes.txt", 1, dispatch.NoProcessor}, {"notes.txt.bz2", 0, dispatch.Prepared}})
	if content, bz := events[0], events[1]; content.Part.Where != "bzip2 content notes.txt" || content.Part.Size != 18 ||
		content.Part.ContentType.Route != route.Text || bz.Part.ContentType.Kind != route.Bzip2 || bz.Children != 1 || len(bz.Outputs) != 0 {
		t.Errorf("notes.txt.bz2: %q, %d bytes, routed %s; %s, %d children, %d outputs", content.Part.Where, content.Part.Size,
			content.Part.ContentType.Route, bz.Part.ContentType.Kind, bz.Children, len(bz.Outputs))
	}
	if n := zs.calls.Load(); n != 1 {
		t.Errorf("the archive stage prepared %d parts, want 1", n)
	}
	wantEmpty(t, work)

	d, _, work = archiveDispatcher(t, inspect.Options{})
	events = dispatchAll(t, d, sevenZipFixture("generated/concatenated.txt.bz2"), nil)
	wantSteps(t, events, []step{{"concatenated.txt", 1, dispatch.NoProcessor}, {"concatenated.txt.bz2", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Part.Size != 59 {
		t.Errorf("concatenated.txt: %d bytes, want both streams' 59", ev.Part.Size)
	}
	wantEmpty(t, work)
}

func TestBzip2Tar(t *testing.T) {
	d, zs, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, sevenZipFixture("generated/clean.tbz2"), nil)
	wantSteps(t, events, []step{
		{"docs/notes.txt", 2, dispatch.NoProcessor},
		{"docs/pixel.png", 2, dispatch.Prepared},
		{"clean.tar", 1, dispatch.Prepared},
		{"clean.tbz2", 0, dispatch.Prepared},
	})
	want := []string{"tar entry docs/notes.txt", "tar entry docs/pixel.png", "bzip2 content clean.tar"}
	if got := wheres(events); !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if tar, bz := events[2], events[3]; tar.Part.ContentType.Kind != route.Tar || bz.Part.ContentType.Kind != route.Bzip2 || bz.Children != 1 {
		t.Errorf("clean.tbz2: %s holding %s, %d children", bz.Part.ContentType.Kind, tar.Part.ContentType.Kind, bz.Children)
	}
	if n := zs.calls.Load(); n != 2 {
		t.Errorf("the archive stage prepared %d parts, want 2", n)
	}
	wantEmpty(t, work)
}

func TestBzip2Stem(t *testing.T) {
	notes := string(bzip2Fixture(t, "notes.txt.bz2"))
	for name, want := range map[string]string{
		"notes.txt.bz2": "notes.txt", "NOTES.TXT.BZ2": "NOTES.TXT", "bundle.tar.bz2": "bundle.tar",
		"bundle.TBZ2": "bundle.tar", "bundle.tbz": "bundle.tar", "notes": "notes", ".bz2": ".bz2",
	} {
		d, _, work := archiveDispatcher(t, inspect.Options{})
		events := dispatchAll(t, d, input(t, name, notes), nil)
		if len(events) != 2 || events[0].Part.Name != want || events[0].Part.Where != "bzip2 content "+want {
			t.Errorf("%s: %v, want its content named %q", name, steps(events), want)
		}
		wantEmpty(t, work)
	}
}

func TestBzip2BombAsWritten(t *testing.T) {
	d, _, work := archiveDispatcher(t, inspect.Options{BombSize: 1 << 20, BombRatio: 100})
	events := dispatchAll(t, d, sevenZipFixture("generated/bomb.bz2"), func(ev *dispatch.Event) {
		if left := files(t, work); len(left) > 0 {
			t.Errorf("files at the stream's event: %q, want the partial file removed", left)
		}
	})
	wantSteps(t, events, []step{{"bomb", 1, dispatch.Quarantined}, {"bomb.bz2", 0, dispatch.Prepared}})
	wantBomb(t, events[0], "bzip2 content bomb", `content "bomb"`, "from 50")
	wantEmpty(t, work)

	d, _, work = archiveDispatcher(t, inspect.Options{})
	events = dispatchAll(t, d, sevenZipFixture("generated/bomb.bz2"), nil)
	wantSteps(t, events, []step{{"bomb", 1, dispatch.NoProcessor}, {"bomb.bz2", 0, dispatch.Prepared}})
	if events[0].Part.Size != 8<<20 {
		t.Errorf("bomb: %d bytes, want %d", events[0].Part.Size, 8<<20)
	}
	wantEmpty(t, work)
}

func TestBzip2Damaged(t *testing.T) {
	tbz := bzip2Fixture(t, "clean.tbz2")
	flipped := slices.Clone(tbz)
	flipped[len(flipped)/2] ^= 0xff
	for name, body := range map[string][]byte{"cut.tbz2": tbz[:len(tbz)-20], "flipped.tbz2": flipped} {
		d, _, work := archiveDispatcher(t, inspect.Options{})
		events := dispatchAll(t, d, input(t, name, string(body)), nil)
		stem := strings.TrimSuffix(name, ".tbz2") + ".tar"
		wantSteps(t, events, []step{{stem, 1, dispatch.Failed}, {name, 0, dispatch.Prepared}})
		if ev := events[0]; ev.Part.Where != "bzip2 content "+stem || !strings.HasPrefix(ev.Reason, "doesn't decompress") ||
			errors.Is(ev.Err, dispatch.ErrRefused) {
			t.Errorf("%s's content: %q, %q, %v", name, ev.Part.Where, ev.Reason, ev.Err)
		}
		wantEmpty(t, work)
	}
}
