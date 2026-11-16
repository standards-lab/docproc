package dispatch_test

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

// archiveDispatcher returns a dispatcher whose archive route is an archive stage with no 7-Zip,
// counted, and whose vision route is the vision stage, both under opts, in a working directory of
// its own, which it returns.
func archiveDispatcher(t *testing.T, opts inspect.Options) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	return archiveDispatcherWith(t, dispatch.ArchiveConfig{Inspect: opts})
}

// archiveDispatcherWith returns a dispatcher as archiveDispatcher does, its archive stage built
// over cfg, the dispatcher's inspection under cfg.Inspect.
func archiveDispatcherWith(t *testing.T, cfg dispatch.ArchiveConfig) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	s, err := dispatch.NewArchiveStage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	zs := &counting{Stage: s}
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{
		Stages:  dispatch.Stages{route.Archive: zs, route.Vision: dispatch.NewVisionStage()},
		Index:   dispatch.NewMemoryIndex(),
		Dir:     work,
		Inspect: cfg.Inspect,
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
	if ev := byName(t, events, "inner.zip"); ev.Part.Route != route.Archive || ev.Stage.Name != "archive" || ev.Children != 2 {
		t.Errorf("inner.zip: %s by %v, %d children", ev.Part.Route, ev.Stage, ev.Children)
	}
	if ev := byName(t, events, unicode); ev.Part.Route != route.Text {
		t.Errorf("%s routed %s, want text", unicode, ev.Part.Route)
	}
	if n := zs.calls.Load(); n != 2 {
		t.Errorf("the archive stage prepared %d parts, want 2", n)
	}
	wantEmpty(t, work)
}

// TestZipEncrypted dispatches inspect's encrypted.zip, an ordinary entry beside an encrypted one:
// inspect flags it and lets it proceed; the ordinary entry is a part; the encrypted one is a
// record naming it, its outcome encrypted; and the ZIP is encrypted, its reason counting its
// encrypted entries.
func TestZipEncrypted(t *testing.T) {
	d, _, work := archiveDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, filepath.Join(inspectFixtures, "encrypted.zip"), nil)
	wantSteps(t, events, []step{{"notes.txt", 1, dispatch.NoProcessor}, {"encrypted.zip", 0, dispatch.Encrypted}})
	z := events[1]
	if !strings.Contains(z.Reason, "1 of 2 entries") || z.Err == nil || z.Children != 1 {
		t.Errorf("encrypted.zip: %q, %v, %d children", z.Reason, z.Err, z.Children)
	}
	rec := outputNamed(t, z, dispatch.RecordOutput, "zip entry secret.txt")
	if rec.Meta["check"] != string(inspect.ZIPEncrypted) || rec.Meta["reason"] != "encrypted" || rec.Meta["name"] != "secret.txt" ||
		rec.Meta["outcome"] != string(dispatch.Encrypted) {
		t.Errorf("secret.txt's record meta %v", rec.Meta)
	}
	wantEmpty(t, work)
}

// TestZipBombAsWritten dispatches an entry whose central directory declares 1 KiB while it
// expands to 8 MiB: inspect, reading the declared sizes, lets it proceed; under lowered
// thresholds the stage stops writing it once its bytes pass both, removes the partial file, and
// records it quarantined with its evidence. At the defaults it is written in full, and recorded
// for the lie: its bytes aren't what its central directory declares. inspect's bomb.zip, honest
// and past lowered thresholds, is quarantined before the stage, but under a policy that lets
// zip-bomb proceed the stage stops it as written.
func TestZipBombAsWritten(t *testing.T) {
	lying := zipBytes(t, zent{name: "zeros.bin", body: string(make([]byte, 8<<20)), declared: 1024})
	lowered := inspect.Options{BombSize: 1 << 20, BombRatio: 100}

	d, _, work := archiveDispatcher(t, lowered)
	events := dispatchAll(t, d, input(t, "lying.zip", lying), func(ev *dispatch.Event) {
		if left := files(t, work); len(left) > 0 {
			t.Errorf("files at the ZIP's event: %q, want the partial file removed", left)
		}
	})
	wantSteps(t, events, []step{{"lying.zip", 0, dispatch.Prepared}})
	rec := outputNamed(t, events[0], dispatch.RecordOutput, "zip entry zeros.bin")
	if rec.Meta["check"] != string(inspect.ZIPBomb) || rec.Meta["class"] != string(inspect.Unsafe) ||
		rec.Meta["outcome"] != string(dispatch.Quarantined) || !strings.Contains(rec.Meta["evidence"], "declared 1024") {
		t.Errorf("zeros.bin's record meta %v", rec.Meta)
	}
	wantEmpty(t, work)

	d, _, work = archiveDispatcher(t, inspect.Options{})
	events = dispatchAll(t, d, input(t, "lying.zip", lying), nil)
	wantSteps(t, events, []step{{"lying.zip", 0, dispatch.Prepared}})
	if m := outputNamed(t, events[0], dispatch.RecordOutput, "zip entry zeros.bin").Meta; m["reason"] != "8388608 bytes written, 1024 declared" || m["check"] != "" {
		t.Errorf("zeros.bin at the defaults: meta %v", m)
	}
	wantEmpty(t, work)

	bomb := filepath.Join(inspectFixtures, "bomb.zip")
	d, zs, _ := archiveDispatcher(t, lowered)
	if ev := dispatchAll(t, d, bomb, nil)[0]; ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 0 {
		t.Errorf("bomb.zip at lowered thresholds: %s, the stage called %d times", ev.Outcome, zs.calls.Load())
	}
	lowered.Policy = map[inspect.Check]inspect.Verdict{inspect.ZIPBomb: inspect.Proceed}
	d, _, _ = archiveDispatcher(t, lowered)
	ev := dispatchAll(t, d, bomb, nil)[0]
	if m := outputNamed(t, ev, dispatch.RecordOutput, "zip entry zeros.bin").Meta; ev.Outcome != dispatch.Prepared || m["check"] != string(inspect.ZIPBomb) {
		t.Errorf("bomb.zip let proceed: %s, meta %v", ev.Outcome, m)
	}
}

// TestZipUnsafeNames: inspect quarantines a ZIP with a traversing, an absolute, or a drive letter
// name, or a symbolic link, and the archive stage never sees it. Under a policy that lets them
// proceed, each entry is written by its SHA-256 inside the working directory, its stored name only
// its name, no file lands outside, and the link is a file holding its target's text.
func TestZipUnsafeNames(t *testing.T) {
	permissive := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{
		inspect.ZIPTraversal: inspect.Proceed, inspect.ZIPAbsolutePath: inspect.Proceed, inspect.ZIPSymlink: inspect.Proceed,
	}}
	for _, c := range []struct {
		fixture string
		names   []string
	}{
		{"traversal.zip", []string{"notes.txt", "docs/../../evil.txt"}},
		{"absolute.zip", []string{"/tmp/evil.txt", `C:\evil.txt`}},
		{"symlink.zip", []string{"notes.txt", "passwd"}},
	} {
		path := filepath.Join(inspectFixtures, c.fixture)
		d, zs, _ := archiveDispatcher(t, inspect.Options{})
		if ev := dispatchAll(t, d, path, nil)[0]; ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 0 {
			t.Errorf("%s: %s, the stage called %d times; want quarantined before the stage", c.fixture, ev.Outcome, zs.calls.Load())
		}

		root := t.TempDir()
		work := filepath.Join(root, "a", "b", "work")
		if err := os.MkdirAll(work, 0o700); err != nil {
			t.Fatal(err)
		}
		s, err := dispatch.NewArchiveStage(dispatch.ArchiveConfig{Inspect: permissive})
		if err != nil {
			t.Fatal(err)
		}
		d, err = dispatch.New(dispatch.Config{
			Stages: dispatch.Stages{route.Archive: s}, Index: dispatch.NewMemoryIndex(), Dir: work, Inspect: permissive,
		})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		events := dispatchAll(t, d, path, func(ev *dispatch.Event) {
			if ev.Part.Depth == 0 {
				return
			}
			names = append(names, ev.Part.Name)
			if filepath.Base(ev.Part.Path) != ev.Part.SHA256 || !inside(work, ev.Part.Path) {
				t.Errorf("%s: %q at %s, want its SHA-256 inside the working directory", c.fixture, ev.Part.Name, ev.Part.Path)
			}
			if info, err := os.Lstat(ev.Part.Path); err != nil || !info.Mode().IsRegular() {
				t.Errorf("%s: %q isn't a regular file: %v", c.fixture, ev.Part.Name, err)
			}
		})
		if last := events[len(events)-1]; last.Outcome != dispatch.Prepared || !slices.Equal(names, c.names) {
			t.Errorf("%s let proceed: %s, entries %q, want %q", c.fixture, last.Outcome, names, c.names)
		}
		if left := files(t, root); len(left) > 0 {
			t.Errorf("%s: files left: %q", c.fixture, left)
		}
	}
}

// TestArchiveNoSevenZip: with no 7-Zip, a zstd stream, a 7z archive, and a cabinet, the
// archive kinds the stage reads through it, have no processor, each reason naming the kind.
func TestArchiveNoSevenZip(t *testing.T) {
	d, _, work := archiveDispatcher(t, inspect.Options{})
	for name, c := range map[string]struct{ content, reason string }{
		"a.tar.zst": {"\x28\xb5\x2f\xfd" + strings.Repeat("\x00", 32), "a zstd stream without 7-Zip"},
		"a.7z":      {"7z\xbc\xaf\x27\x1c" + strings.Repeat("\x00", 32), "a 7z archive without 7-Zip"},
		"a.cab":     {"MSCF" + strings.Repeat("\x00", 32), "a cabinet without 7-Zip"},
	} {
		ev := dispatchAll(t, d, input(t, name, c.content), nil)[0]
		if ev.Part.Route != route.Archive || ev.Outcome != dispatch.NoProcessor || !strings.Contains(ev.Reason, c.reason) {
			t.Errorf("%s: %s %s, %q; want archive, no processor, %q", name, ev.Part.Route, ev.Outcome, ev.Reason, c.reason)
		}
	}
	wantEmpty(t, work)
}

// TestArchiveIdentity: the archive stage names itself and this package's version, 3 since it
// reads 7-Zip's kinds, hashes its bomb thresholds, inspect's defaults alike whether given or zero,
// and its 7-Zip's version, but not the policy, and refuses a negative ratio, a policy inspect
// refuses, and a 7-Zip with no version.
func TestArchiveIdentity(t *testing.T) {
	id := func(cfg dispatch.ArchiveConfig) dispatch.Identity {
		t.Helper()
		s, err := dispatch.NewArchiveStage(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return s.Identity()
	}
	base := id(dispatch.ArchiveConfig{})
	if base.Name != "archive" || base.Version != "2" || len(base.Config) != 64 {
		t.Errorf("archive identity %v", base)
	}
	if id(dispatch.ArchiveConfig{Inspect: inspect.Options{BombRatio: inspect.DefaultBombRatio, BombSize: inspect.DefaultBombSize}}) != base {
		t.Error("inspect's defaults, given, changed the archive identity")
	}
	policy := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{inspect.ZIPBomb: inspect.Proceed}}
	if id(dispatch.ArchiveConfig{Inspect: policy}) != base {
		t.Error("a policy, the dispatcher's, changed the archive identity")
	}
	if id(dispatch.ArchiveConfig{Inspect: inspect.Options{BombSize: 1 << 20}}).Config == base.Config ||
		id(dispatch.ArchiveConfig{Inspect: inspect.Options{BombRatio: 10}}).Config == base.Config {
		t.Error("a bomb threshold left the archive identity's config unchanged")
	}
	sz := &fakeSevenZip{}
	with := id(dispatch.ArchiveConfig{SevenZip: sz, Version: "7zz 26.05"})
	if with.Config == base.Config || id(dispatch.ArchiveConfig{SevenZip: sz, Version: "7zz 24.01"}).Config == with.Config {
		t.Error("7-Zip's version left the archive identity's config unchanged")
	}
	if id(dispatch.ArchiveConfig{Version: "7zz 26.05"}) != base {
		t.Error("a version with no 7-Zip changed the archive identity")
	}
	for name, cfg := range map[string]dispatch.ArchiveConfig{
		"a negative bomb ratio": {Inspect: inspect.Options{BombRatio: -1}},
		"a policy of no check":  {Inspect: inspect.Options{Policy: map[inspect.Check]inspect.Verdict{"no-such-check": inspect.Proceed}}},
		"7-Zip with no version": {SevenZip: sz},
	} {
		if _, err := dispatch.NewArchiveStage(cfg); err == nil {
			t.Errorf("an archive stage with %s was built", name)
		}
	}
}
