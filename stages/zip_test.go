package stages_test

import (
	"archive/zip"
	"bytes"
	"cmp"
	"compress/flate"
	"context"
	"hash/crc32"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

// inspectFixtures are inspect's synthesized hazard fixtures.
var inspectFixtures = filepath.Join("..", "inspect", "testdata")

// pngBytes is a 1x1 PNG's signature and IHDR chunk, a picture by its signature.
const pngBytes = "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde"

// zent is one entry of a test ZIP: its name and content, deflated; flags, when set, its general
// purpose flags; declared, when set, the expanded size its central directory declares in place
// of the true one; and mode, when set, its Unix mode.
type zent struct {
	name     string
	body     string
	flags    uint16
	declared uint64
	mode     fs.FileMode
}

// zipBytes returns a ZIP of entries, in order, each deflated, written raw when it has flags or a
// declared size, which archive/zip's writer would otherwise set itself.
func zipBytes(t *testing.T, entries ...zent) string {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		if e.flags == 0 && e.declared == 0 {
			w, err := zw.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var packed bytes.Buffer
		fw, err := flate.NewWriter(&packed, flate.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
		if err := fw.Close(); err != nil {
			t.Fatal(err)
		}
		h.Flags |= e.flags
		h.CRC32 = crc32.ChecksumIEEE([]byte(e.body))
		h.CompressedSize64 = uint64(packed.Len())
		h.UncompressedSize64 = cmp.Or(e.declared, uint64(len(e.body)))
		w, err := zw.CreateRaw(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(packed.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// counting is a stage that counts the parts it is given before handing them to its own.
type counting struct {
	dispatch.Stage
	calls atomic.Int32
}

func (c *counting) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	c.calls.Add(1)
	return c.Stage.Prepare(ctx, p, dir)
}

// testArchiveStage returns an archive stage under opts over a fake 7-Zip that lists and holds
// nothing, for the kinds the standard library reads.
func testArchiveStage(t *testing.T, opts inspect.Options) *stages.ArchiveStage {
	t.Helper()
	return stages.NewArchiveStage(stages.ArchiveConfig{SevenZip: &fakeSevenZip{}, Version: "7-Zip (z) 26.06 (x64)", Inspect: opts})
}

// archiveDispatcher returns a dispatcher whose archive route is an archive stage over a fake
// 7-Zip that holds nothing, counted, and whose vision route is the vision stage, both under
// opts, in a working directory of its own, which it returns.
func archiveDispatcher(t *testing.T, opts inspect.Options) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	return archiveDispatcherWith(t, stages.ArchiveConfig{Inspect: opts})
}

// archiveDispatcherWith returns a dispatcher as archiveDispatcher does, its archive stage built
// over cfg, a fake 7-Zip that holds nothing when it names none, the dispatcher's inspection
// under cfg.Inspect.
func archiveDispatcherWith(t *testing.T, cfg stages.ArchiveConfig) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	return archiveDispatcherLimited(t, cfg, dispatch.Limits{})
}

// archiveDispatcherLimited returns a dispatcher as archiveDispatcherWith does, under limits.
func archiveDispatcherLimited(t *testing.T, cfg stages.ArchiveConfig, limits dispatch.Limits) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	if cfg.SevenZip == nil {
		cfg.SevenZip = &fakeSevenZip{}
	}
	if cfg.Version == "" {
		cfg.Version = "7-Zip (z) 26.06 (x64)"
	}
	s := stages.NewArchiveStage(cfg)
	zs := &counting{Stage: s}
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages:  dispatch.Stages{route.Archive: zs, route.Vision: stages.NewVisionStage()},
		Index:   dispatch.NewMemoryIndex(0),
		Dir:     work,
		Inspect: cfg.Inspect,
		Limits:  limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d, zs, work
}

// files returns the regular files under dir.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// wantEmpty fails the test unless the working directory holds no file.
func wantEmpty(t *testing.T, work string) {
	t.Helper()
	if left := files(t, work); len(left) > 0 {
		t.Errorf("files left in the working directory: %q", left)
	}
}

// inside reports whether path is under dir.
func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// TestZipStage dispatches a ZIP holding a ZIP, a picture, a Unicode-named text, a directory,
// and the picture again under the directory: the inner ZIP is routed to the archive stage again and
// its entries dispatched below it; each entry is named by its stored name, found "zip entry
// <name>", and written under its SHA-256 inside the working directory; the directory is no part;
// the picture met again is seen; and nothing is left once the dispatch is done.
func TestZipStage(t *testing.T) {
	const unicode = "résumé 日本 שלום.txt"
	inner := zipBytes(t, zent{name: "pic.png", body: pngBytes}, zent{name: unicode, body: "hello\n"})
	outer := zipBytes(t,
		zent{name: "inner.zip", body: inner},
		zent{name: "notes.txt", body: "an ordinary entry\n"},
		zent{name: "pictures/", mode: fs.ModeDir | 0o755},
		zent{name: "pictures/again.png", body: pngBytes},
	)
	d, zs, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, input(t, "bundle.zip", outer), func(ev *dispatch.Event) {
		if ev.Part.Depth == 0 {
			return
		}
		if filepath.Base(ev.Part.Path) != ev.Part.SHA256 || !inside(work, ev.Part.Path) {
			t.Errorf("%s at %s: want its SHA-256 inside the working directory", ev.Part.Name, ev.Part.Path)
		}
	})
	wantSteps(t, events, []step{
		{"pic.png", 2, dispatch.Prepared},
		{unicode, 2, dispatch.NoProcessor},
		{"inner.zip", 1, dispatch.Prepared},
		{"notes.txt", 1, dispatch.NoProcessor},
		{"pictures/again.png", 1, dispatch.Seen},
		{"bundle.zip", 0, dispatch.Prepared},
	})
	want := []string{"zip entry pic.png", "zip entry " + unicode, "zip entry inner.zip", "zip entry notes.txt", "zip entry pictures/again.png"}
	if got := wheres(events); !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if ev := byName(t, events, "inner.zip"); ev.Part.ContentType.Route != route.Archive || ev.Stage.Name != "archive" || ev.Children != 2 {
		t.Errorf("inner.zip: %s by %v, %d children", ev.Part.ContentType.Route, ev.Stage, ev.Children)
	}
	if ev := byName(t, events, unicode); ev.Part.ContentType.Route != route.Text {
		t.Errorf("%s routed %s, want text", unicode, ev.Part.ContentType.Route)
	}
	if n := zs.calls.Load(); n != 2 {
		t.Errorf("the archive stage prepared %d parts, want 2", n)
	}
	wantEmpty(t, work)
}

// TestZipEncrypted dispatches inspect's encrypted.zip, an ordinary entry beside an encrypted one:
// inspect flags it and lets it proceed; the ordinary entry is a part; the encrypted one is an
// unread child, encrypted, flagged archive-encrypted; and the ZIP is encrypted, its reason
// counting its encrypted entries.
func TestZipEncrypted(t *testing.T) {
	d, _, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, filepath.Join(inspectFixtures, "encrypted.zip"), nil)
	wantSteps(t, events, []step{{"notes.txt", 1, dispatch.NoProcessor}, {"secret.txt", 1, dispatch.Encrypted}, {"encrypted.zip", 0, dispatch.Encrypted}})
	z := events[2]
	if !strings.Contains(z.Reason, "1 of 2 entries") || z.Err == nil || z.Children != 2 {
		t.Errorf("encrypted.zip: %q, %v, %d children", z.Reason, z.Err, z.Children)
	}
	if ev := events[1]; ev.Part.Where != "zip entry secret.txt" || ev.Reason != "encrypted" || !hasFlag(ev.Part.Flags, inspect.ArchiveEncrypted) {
		t.Errorf("secret.txt: %q, %q, flags %v", ev.Part.Where, ev.Reason, ev.Part.Flags)
	}
	wantEmpty(t, work)
}

// TestZipBombAsWritten dispatches an entry whose central directory declares 1 KiB while it
// expands to 8 MiB: inspect, reading the declared sizes, lets it proceed; under lowered
// thresholds the stage stops writing it once its bytes pass both, removes the partial file, and
// makes it an unread child, quarantined with its evidence. With no bomb rule it is written in
// full, and failed for the lie: its bytes aren't what its central directory declares. inspect's bomb.zip, honest
// and past lowered thresholds, is quarantined before the stage, but under a policy that lets
// archive-bomb proceed the stage stops it as written.
func TestZipBombAsWritten(t *testing.T) {
	lying := zipBytes(t, zent{name: "zeros.bin", body: string(make([]byte, 8<<20)), declared: 1024})
	lowered := inspect.Options{BombSize: 1 << 20, BombRatio: 100}

	d, _, work := archiveDispatcher(t, lowered)
	events := dispatchAll(t, d, input(t, "lying.zip", lying), func(ev *dispatch.Event) {
		if left := files(t, work); len(left) > 0 {
			t.Errorf("files at the ZIP's event: %q, want the partial file removed", left)
		}
	})
	wantSteps(t, events, []step{{"zeros.bin", 1, dispatch.Quarantined}, {"lying.zip", 0, dispatch.Prepared}})
	wantBomb(t, events[0], "zip entry zeros.bin", "declared 1024")
	wantEmpty(t, work)

	d, _, work = archiveDispatcher(t, inspect.Options{})
	events = dispatchAll(t, d, input(t, "lying.zip", lying), nil)
	wantSteps(t, events, []step{{"zeros.bin", 1, dispatch.Failed}, {"lying.zip", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Reason != "8388608 bytes written, 1024 declared" || len(ev.Part.Flags) != 0 {
		t.Errorf("zeros.bin with no bomb rule: %q, flags %v", ev.Reason, ev.Part.Flags)
	}
	wantEmpty(t, work)

	bomb := filepath.Join(inspectFixtures, "bomb.zip")
	d, zs, _ := archiveDispatcher(t, lowered)
	if ev := dispatchAll(t, d, bomb, nil)[0]; ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 0 {
		t.Errorf("bomb.zip at lowered thresholds: %s, the stage called %d times", ev.Outcome, zs.calls.Load())
	}
	lowered.Policy = map[inspect.Check]inspect.Verdict{inspect.ArchiveBomb: inspect.Proceed}
	d, _, _ = archiveDispatcher(t, lowered)
	events = dispatchAll(t, d, bomb, nil)
	wantSteps(t, events, []step{{"zeros.bin", 1, dispatch.Quarantined}, {"bomb.zip", 0, dispatch.Prepared}})
	wantBomb(t, events[0], "zip entry zeros.bin")
}

// TestZipUnsafeNames: under a policy that quarantines them, inspect quarantines a ZIP with a
// traversing, an absolute, or a drive letter name, or a symbolic link, and the archive stage never
// sees it. By default, each flag active, each safe entry is written by its SHA-256 inside the
// working directory, its stored name only its name, and each flagged entry is an unread child,
// quarantined with its flag, no file or link made, and nothing lands outside.
func TestZipUnsafeNames(t *testing.T) {
	strict := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{
		inspect.ArchiveTraversal: inspect.Quarantine, inspect.ArchiveAbsolutePath: inspect.Quarantine, inspect.ArchiveSymlink: inspect.Quarantine,
	}}
	for _, c := range []struct {
		fixture  string
		parts    []string
		children map[string]inspect.Check
	}{
		{"traversal.zip", []string{"notes.txt"}, map[string]inspect.Check{"docs/../../evil.txt": inspect.ArchiveTraversal}},
		{"absolute.zip", nil, map[string]inspect.Check{"/tmp/evil.txt": inspect.ArchiveAbsolutePath, `C:\evil.txt`: inspect.ArchiveAbsolutePath}},
		{"symlink.zip", []string{"notes.txt"}, map[string]inspect.Check{"passwd": inspect.ArchiveSymlink}},
	} {
		d, zs, _ := archiveDispatcher(t, strict)
		if ev := dispatchAll(t, d, filepath.Join(inspectFixtures, c.fixture), nil)[0]; ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 0 {
			t.Errorf("%s: %s, the stage called %d times; want quarantined before the stage", c.fixture, ev.Outcome, zs.calls.Load())
		}
		root := t.TempDir()
		work := filepath.Join(root, "a", "b", "work")
		if err := os.MkdirAll(work, 0o700); err != nil {
			t.Fatal(err)
		}
		checkUnsafe(t, testArchiveStage(t, inspect.Options{}), filepath.Join(inspectFixtures, c.fixture), root, work, c.parts, c.children)
	}
}

// TestArchiveIdentity: the archive stage names itself and 7zz's version line, and hashes its
// bomb rule and its entry bound, but not the policy, the dispatcher's.
func TestArchiveIdentity(t *testing.T) {
	id := func(opts inspect.Options) dispatch.Identity {
		t.Helper()
		return stages.NewArchiveStage(stages.ArchiveConfig{SevenZip: &fakeSevenZip{}, Version: "7-Zip (z) 26.06 (x64)", Inspect: opts}).Identity()
	}
	base := id(inspect.Options{})
	if base.Name != "archive" || base.Version != "7-Zip (z) 26.06 (x64)" || len(base.Config) != 64 {
		t.Errorf("archive identity %v", base)
	}
	if id(inspect.Options{Policy: map[inspect.Check]inspect.Verdict{inspect.ArchiveBomb: inspect.Proceed}}) != base {
		t.Error("a policy, the dispatcher's, changed the archive identity")
	}
	ruled := id(inspect.Options{BombRatio: 1000, BombSize: 1 << 30})
	if ruled.Config == base.Config ||
		id(inspect.Options{BombRatio: 1000, BombSize: 1 << 20}).Config == ruled.Config ||
		id(inspect.Options{BombRatio: 10, BombSize: 1 << 30}).Config == ruled.Config {
		t.Error("a bomb threshold left the archive identity's config unchanged")
	}
	if id(inspect.Options{Entries: 100}).Config == base.Config {
		t.Error("the entry bound left the archive identity's config unchanged")
	}
}
