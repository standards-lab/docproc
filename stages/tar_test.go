package stages_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

func inspectFixture(name string) string { return filepath.Join(inspectFixtures, name) }

func TestTarStage(t *testing.T) {
	d, zs, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, inspectFixture("clean.tar"), func(ev *dispatch.Event) {
		if ev.Part.Depth == 0 {
			return
		}
		if filepath.Base(ev.Part.Path) != ev.Part.SHA256 || !inside(work, ev.Part.Path) {
			t.Errorf("%s at %s: want its SHA-256 inside the working directory", ev.Part.Name, ev.Part.Path)
		}
	})
	wantSteps(t, events, []step{
		{"docs/notes.txt", 1, dispatch.NoProcessor},
		{"docs/pixel.png", 1, dispatch.Prepared},
		{"clean.tar", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"tar entry docs/notes.txt", "tar entry docs/pixel.png"}; !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if ev := events[2]; ev.Part.ContentType.Kind != route.Tar || ev.Stage.Name != "archive" || ev.Children != 2 || len(ev.Outputs) != 0 {
		t.Errorf("clean.tar: %s by %v, %d children, %d outputs", ev.Part.ContentType.Kind, ev.Stage, ev.Children, len(ev.Outputs))
	}
	if ev := events[0]; ev.Part.ContentType.Route != route.Text {
		t.Errorf("docs/notes.txt routed %s, want text", ev.Part.ContentType.Route)
	}
	if n := zs.calls.Load(); n != 1 {
		t.Errorf("the archive stage prepared %d parts, want 1", n)
	}
	wantEmpty(t, work)
}

func TestGzipStage(t *testing.T) {
	for _, c := range []struct{ fixture, content string }{
		{"clean.tar.gz", "clean.tar"},
		{"unnamed.tgz", "unnamed.tar"},
	} {
		d, zs, work := archiveDispatcher(t, inspect.Options{})
		events := dispatchAll(t, d, inspectFixture(c.fixture), nil)
		wantSteps(t, events, []step{
			{"docs/notes.txt", 2, dispatch.NoProcessor},
			{"docs/pixel.png", 2, dispatch.Prepared},
			{c.content, 1, dispatch.Prepared},
			{c.fixture, 0, dispatch.Prepared},
		})
		want := []string{"tar entry docs/notes.txt", "tar entry docs/pixel.png", "gzip content " + c.content}
		if got := wheres(events); !slices.Equal(got, want) {
			t.Errorf("%s: wheres:\n got %q\nwant %q", c.fixture, got, want)
		}
		if tar, gz := events[2], events[3]; tar.Part.ContentType.Kind != route.Tar || gz.Part.ContentType.Kind != route.Gzip || gz.Children != 1 || len(gz.Outputs) != 0 {
			t.Errorf("%s: %s holding %s, %d children, %d outputs", c.fixture, gz.Part.ContentType.Kind, tar.Part.ContentType.Kind, gz.Children, len(gz.Outputs))
		}
		if n := zs.calls.Load(); n != 2 {
			t.Errorf("%s: the archive stage prepared %d parts, want 2", c.fixture, n)
		}
		wantEmpty(t, work)
	}
}

func TestGzipStem(t *testing.T) {
	gz := func(body string) string {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	d, _, work := archiveDispatcher(t, inspect.Options{})
	for name, want := range map[string]string{
		"notes.txt.gz": "notes.txt", "NOTES.TXT.GZ": "NOTES.TXT", "bundle.TGZ": "bundle.tar", "notes": "notes",
	} {
		events := dispatchAll(t, d, input(t, name, gz("hello "+name+"\n")), nil)
		if len(events) != 2 || events[0].Part.Name != want || events[0].Part.Where != "gzip content "+want {
			t.Errorf("%s: %v, want its content named %q", name, steps(events), want)
		}
	}
	wantEmpty(t, work)
}

func TestTarUnsafe(t *testing.T) {
	strict := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{
		inspect.ArchiveTraversal: inspect.Quarantine, inspect.ArchiveAbsolutePath: inspect.Quarantine, inspect.ArchiveSymlink: inspect.Quarantine,
		inspect.ArchiveHardlink: inspect.Quarantine, inspect.ArchiveDevice: inspect.Quarantine,
	}}
	for _, c := range []struct {
		fixture  string
		parts    []string
		children map[string]inspect.Check
	}{
		{"traversal.tar", []string{"notes.txt"}, map[string]inspect.Check{"docs/../../evil.txt": inspect.ArchiveTraversal}},
		{"absolute.tar", nil, map[string]inspect.Check{"/tmp/evil.txt": inspect.ArchiveAbsolutePath, `C:\evil.txt`: inspect.ArchiveAbsolutePath}},
		{"symlink.tar", []string{"notes.txt"}, map[string]inspect.Check{"passwd": inspect.ArchiveSymlink}},
		{"hardlink.tar", []string{"notes.txt"}, map[string]inspect.Check{"shadow": inspect.ArchiveHardlink}},
		{"device.tar", []string{"notes.txt"}, map[string]inspect.Check{
			"null": inspect.ArchiveDevice, "sda": inspect.ArchiveDevice, "pipe": inspect.ArchiveDevice,
		}},
	} {
		d, zs, _ := archiveDispatcher(t, strict)
		if ev := dispatchAll(t, d, inspectFixture(c.fixture), nil)[0]; ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 0 {
			t.Errorf("%s: %s, the stage called %d times; want quarantined before the stage", c.fixture, ev.Outcome, zs.calls.Load())
		}
		root := t.TempDir()
		work := filepath.Join(root, "a", "b", "work")
		if err := os.MkdirAll(work, 0o700); err != nil {
			t.Fatal(err)
		}
		checkUnsafe(t, testArchiveStage(t, inspect.Options{}), inspectFixture(c.fixture), root, work, c.parts, c.children)
	}
}

func checkUnsafe(t *testing.T, stage *stages.ArchiveStage, path, root, work string, parts []string, children map[string]inspect.Check) {
	t.Helper()
	fixture := filepath.Base(path)
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages: dispatch.Stages{route.Archive: stage}, Index: dispatch.NewMemoryIndex(0), Dir: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	var written []string
	unreadChecks := map[string]inspect.Check{}
	events := dispatchAll(t, d, path, func(ev *dispatch.Event) {
		if ev.Part.Depth == 0 || ev.Output != nil {
			return
		}
		if ev.Part.SHA256 == "" {
			if ev.Outcome != dispatch.Quarantined || len(ev.Part.Flags) != 1 || ev.Part.Flags[0].Class != inspect.Active {
				t.Errorf("%s: %q: %s, flags %v", fixture, ev.Part.Name, ev.Outcome, ev.Part.Flags)
			} else {
				unreadChecks[ev.Part.Name] = ev.Part.Flags[0].Check
			}
			return
		}
		written = append(written, ev.Part.Name)
		if filepath.Base(ev.Part.Path) != ev.Part.SHA256 || !inside(work, ev.Part.Path) {
			t.Errorf("%s: %q at %s, want its SHA-256 inside the working directory", fixture, ev.Part.Name, ev.Part.Path)
		}
		if info, err := os.Lstat(ev.Part.Path); err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s: %q isn't a regular file: %v", fixture, ev.Part.Name, err)
		}
	})
	if last := events[len(events)-1]; last.Outcome != dispatch.Prepared || !slices.Equal(written, parts) {
		t.Errorf("%s let proceed: %s, parts %q, want %q", fixture, last.Outcome, written, parts)
	}
	if !maps.Equal(unreadChecks, children) {
		t.Errorf("%s: unread children %v, want %v", fixture, unreadChecks, children)
	}
	if left := files(t, root); len(left) > 0 {
		t.Errorf("%s: files left: %q", fixture, left)
	}
}

func TestTarGzipUnsafe(t *testing.T) {
	d, zs, work := archiveDispatcher(t, inspect.Options{Policy: map[inspect.Check]inspect.Verdict{inspect.ArchiveTraversal: inspect.Quarantine}})
	events := dispatchAll(t, d, inspectFixture("traversal.tar.gz"), nil)
	wantSteps(t, events, []step{{"traversal.tar", 1, dispatch.Quarantined}, {"traversal.tar.gz", 0, dispatch.Prepared}})
	if ev := events[0]; !slices.ContainsFunc(ev.Part.Flags, func(f inspect.Flag) bool { return f.Check == inspect.ArchiveTraversal }) {
		t.Errorf("traversal.tar's flags %v", ev.Part.Flags)
	}
	if n := zs.calls.Load(); n != 1 {
		t.Errorf("the archive stage prepared %d parts, want the gzip alone", n)
	}
	wantEmpty(t, work)
}

func TestTarBombAsWritten(t *testing.T) {
	lowered := inspect.Options{BombSize: 1 << 20, BombRatio: 100}
	d, zs, _ := archiveDispatcher(t, lowered)
	if ev := dispatchAll(t, d, inspectFixture("sparse-bomb.tar"), nil)[0]; ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 0 {
		t.Errorf("sparse-bomb.tar: %s, the stage called %d times", ev.Outcome, zs.calls.Load())
	}

	lowered.Policy = map[inspect.Check]inspect.Verdict{inspect.ArchiveBomb: inspect.Proceed}
	d, _, work := archiveDispatcher(t, lowered)
	events := dispatchAll(t, d, inspectFixture("sparse-bomb.tar"), func(ev *dispatch.Event) {
		if left := files(t, work); len(left) > 0 {
			t.Errorf("files at the tar's event: %q, want the partial file removed", left)
		}
	})
	wantSteps(t, events, []step{{"zeros.bin", 1, dispatch.Quarantined}, {"sparse-bomb.tar", 0, dispatch.Prepared}})
	wantBomb(t, events[0], "tar entry zeros.bin", "from 2048", "declared 4294967296")
	wantEmpty(t, work)
}

func TestGzipBombAsWritten(t *testing.T) {
	d, _, work := archiveDispatcher(t, inspect.Options{BombSize: 1 << 20, BombRatio: 100})
	events := dispatchAll(t, d, inspectFixture("bomb.tar.gz"), func(ev *dispatch.Event) {
		if left := files(t, work); len(left) > 0 {
			t.Errorf("files at the stream's event: %q, want the partial file removed", left)
		}
	})
	wantSteps(t, events, []step{{"bomb.tar", 1, dispatch.Quarantined}, {"bomb.tar.gz", 0, dispatch.Prepared}})
	wantBomb(t, events[0], "gzip content bomb.tar", `content "bomb.tar"`)
	wantEmpty(t, work)

	d, _, work = archiveDispatcher(t, inspect.Options{})
	events = dispatchAll(t, d, inspectFixture("bomb.tar.gz"), nil)
	wantSteps(t, events, []step{
		{"zeros.bin", 2, dispatch.NoProcessor},
		{"bomb.tar", 1, dispatch.Prepared},
		{"bomb.tar.gz", 0, dispatch.Prepared},
	})
	if events[0].Part.Size != 8<<20 {
		t.Errorf("zeros.bin: %d bytes, want %d", events[0].Part.Size, 8<<20)
	}
	wantEmpty(t, work)
}

func TestArchiveDamaged(t *testing.T) {
	gz, err := os.ReadFile(inspectFixture("clean.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	d, _, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, input(t, "cut.tar.gz", string(gz[:len(gz)-20])), nil)
	wantSteps(t, events, []step{{"clean.tar", 1, dispatch.Failed}, {"cut.tar.gz", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Part.Where != "gzip content clean.tar" || !strings.HasPrefix(ev.Reason, "doesn't decompress") {
		t.Errorf("cut.tar.gz's content: %q, %q", ev.Part.Where, ev.Reason)
	}
	events = dispatchAll(t, d, input(t, "bad.gz", "\x1f\x8b\x09 not deflate"), nil)
	if ev := events[0]; ev.Outcome != dispatch.Failed || !strings.Contains(ev.Reason, "gzip") || !errors.Is(ev.Err, dispatch.ErrRefused) {
		t.Errorf("bad.gz: %s, %q; want failed, recorded", ev.Outcome, ev.Reason)
	}
	wantEmpty(t, work)

	whole, err := os.ReadFile(inspectFixture("clean.tar"))
	if err != nil {
		t.Fatal(err)
	}
	opts := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{inspect.TarUnreadable: inspect.Proceed}}
	d, _, work = archiveDispatcher(t, opts)
	events = dispatchAll(t, d, input(t, "cut.tar", string(whole[:1030])), nil)
	wantSteps(t, events, []step{{"docs/notes.txt", 1, dispatch.Failed}, {"cut.tar", 0, dispatch.Failed}})
	if ev := events[0]; !strings.HasPrefix(ev.Reason, "doesn't read") {
		t.Errorf("docs/notes.txt: %q", ev.Reason)
	}
	if !errors.Is(events[1].Err, dispatch.ErrRefused) {
		t.Errorf("cut.tar: %v, want recorded", events[1].Err)
	}
	wantEmpty(t, work)
}

func wantBomb(t *testing.T, ev dispatched, where string, evidence ...string) {
	t.Helper()
	flags := ev.Part.Flags
	if ev.Part.Where != where || ev.Outcome != dispatch.Quarantined || ev.Part.SHA256 != "" || len(flags) != 1 ||
		flags[0].Check != inspect.ArchiveBomb || flags[0].Class != inspect.Unsafe {
		t.Fatalf("%s: %q, %s, %q, flags %v", ev.Part.Name, ev.Part.Where, ev.Outcome, ev.Part.SHA256, flags)
	}
	for _, e := range evidence {
		if !strings.Contains(flags[0].Evidence, e) {
			t.Errorf("%s's evidence %q, want %q in it", ev.Part.Name, flags[0].Evidence, e)
		}
	}
}
