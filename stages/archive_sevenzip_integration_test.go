//go:build linux && integration

package stages_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/stages"
)

// sevenZipFixtures are the sevenzip package's fixtures.
var sevenZipFixtures = filepath.Join("..", "sevenzip", "testdata")

// sevenZipFixture returns the path of one of the sevenzip package's fixtures.
func sevenZipFixture(name string) string { return filepath.Join(sevenZipFixtures, name) }

// sevenZipConfig returns an archive stage's configuration under opts over the 7zz mise pins.
func sevenZipConfig(t *testing.T, opts inspect.Options) stages.ArchiveConfig {
	t.Helper()
	tl := pinned(t, sevenzip.SevenZipSpec())
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	return stages.ArchiveConfig{Inspect: opts, SevenZip: r, Version: stages.Versions(tl)}
}

// sevenZipDispatcher returns a dispatcher as archiveDispatcher does, its archive stage reading
// through the 7zz mise pins.
func sevenZipDispatcher(t *testing.T, opts inspect.Options) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	return archiveDispatcherWith(t, sevenZipConfig(t, opts))
}

// TestSevenZipStage dispatches clean.7z, a directory, a text, and a picture: the directory is no
// part; each file is extracted through 7zz, named by its stored name, found "7z entry <name>",
// written under its SHA-256 inside the working directory, and routed by its content; and nothing
// is left once the dispatch is done.
func TestSevenZipStage(t *testing.T) {
	d, zs, work := sevenZipDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, sevenZipFixture("generated/clean.7z"), func(ev *dispatch.Event) {
		if ev.Part.Depth > 0 && (filepath.Base(ev.Part.Path) != ev.Part.SHA256 || !inside(work, ev.Part.Path)) {
			t.Errorf("%s at %s: want its SHA-256 inside the working directory", ev.Part.Name, ev.Part.Path)
		}
	})
	wantSteps(t, events, []step{
		{"docs/notes.txt", 1, dispatch.NoProcessor},
		{"docs/pixel.png", 1, dispatch.Prepared},
		{"clean.7z", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"7z entry docs/notes.txt", "7z entry docs/pixel.png"}; !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if ev := events[2]; ev.Part.ContentType.Kind != route.SevenZip || ev.Stage.Name != "archive" || ev.Children != 2 || len(ev.Outputs) != 0 {
		t.Errorf("clean.7z: %s by %v, %d children, %d outputs", ev.Part.ContentType.Kind, ev.Stage, ev.Children, len(ev.Outputs))
	}
	if events[0].Part.ContentType.Route != route.Text || events[0].Part.Size != 22 {
		t.Errorf("docs/notes.txt: %s, %d bytes", events[0].Part.ContentType.Route, events[0].Part.Size)
	}
	if n := zs.calls.Load(); n != 1 {
		t.Errorf("the archive stage prepared %d parts, want 1", n)
	}
	wantEmpty(t, work)
}

// TestSevenZipStreams: clean.tar.zst's content, named by the stream's stem, "clean.tar", is found
// "zstd content clean.tar" and dispatched as a tar below it; notebook.cab's files are found
// "cabinet entry <name>", each a OneNote file by its signature; and commons-compress's
// bla.deflate64.7z, Deflate64 in a 7z, gives its two XML files.
func TestSevenZipStreams(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, sevenZipFixture("generated/clean.tar.zst"), nil)
	wantSteps(t, events, []step{
		{"docs/notes.txt", 2, dispatch.NoProcessor},
		{"docs/pixel.png", 2, dispatch.Prepared},
		{"clean.tar", 1, dispatch.Prepared},
		{"clean.tar.zst", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"tar entry docs/notes.txt", "tar entry docs/pixel.png", "zstd content clean.tar"}; !slices.Equal(got, want) {
		t.Errorf("clean.tar.zst: wheres:\n got %q\nwant %q", got, want)
	}

	events = dispatchAll(t, d, sevenZipFixture("generated/notebook.cab"), nil)
	wantSteps(t, events, []step{
		{"Open Notebook.onetoc2", 1, dispatch.NoProcessor},
		{"New Section 1.one", 1, dispatch.NoProcessor},
		{"notebook.cab", 0, dispatch.Prepared},
	})
	for _, ev := range events[:2] {
		if ev.Part.ContentType.Route != route.OneNote || ev.Part.Where != "cabinet entry "+ev.Part.Name {
			t.Errorf("%s: %s at %q", ev.Part.Name, ev.Part.ContentType.Route, ev.Part.Where)
		}
	}
	section, err := os.ReadFile(filepath.Join(tooltest.Tools(t, "onenote/tests/fixtures/onenote-rs"), "New Section Group", "New Section 1.one"))
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(section); events[1].Part.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("New Section 1.one: SHA-256 %s, want the section's", events[1].Part.SHA256)
	}

	events = dispatchAll(t, d, sevenZipFixture("commons-compress/bla.deflate64.7z"), nil)
	wantSteps(t, events, []step{
		{"test1.xml", 1, dispatch.NoProcessor},
		{"test2.xml", 1, dispatch.NoProcessor},
		{"bla.deflate64.7z", 0, dispatch.Prepared},
	})
	if events[0].Part.Size != 610 || events[1].Part.Size != 82 {
		t.Errorf("bla.deflate64.7z: %d and %d bytes, want 610 and 82", events[0].Part.Size, events[1].Part.Size)
	}
	wantEmpty(t, work)
}

// zipMethods are the ZIP fixtures whose one entry archive/zip doesn't read, but bzip2-zip.zip's,
// which it reads through compress/bzip2: each fixture, its entry, the entry's size, and its
// method.
var zipMethods = []struct {
	fixture, entry string
	size           int64
	method         uint16
}{
	{"commons-compress/lorem-ipsum-deflate64.zip", "lorem-ipsum.txt", 144060, 9},
	{"commons-compress/lorem-ipsum-implode.zip", "LOREM.TXT", 144060, 6},
	{"commons-compress/test-method-xz.zip", "LICENSE.txt", 11357, 95},
	{"commons-compress/bzip2-zip.zip", "lots-of-as", 42, 12},
	{"libarchive/test_read_format_zip_lzma.zipx", "vimrc", 912, 14},
	{"libarchive/test_read_format_zip_ppmd8.zipx", "vimrc", 912, 98},
	{"libarchive/test_read_format_zip_zstd.zipx", "vimrc", 912, 93},
}

// TestZipMethods: each ZIP entry of a method archive/zip doesn't read is extracted through 7zz,
// held to its central directory's size and CRC-32, and found "zip entry <name>", bzip2's through
// compress/bzip2; the two lorem-ipsum ZIPs, Deflate64 and implode, give the same bytes.
func TestZipMethods(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	sums := map[string]string{}
	for _, c := range zipMethods {
		events := dispatchAll(t, d, sevenZipFixture(c.fixture), nil)
		if len(events) != 2 || events[0].Part.Name != c.entry || events[0].Part.Size != c.size ||
			events[0].Part.Where != "zip entry "+c.entry || events[1].Outcome != dispatch.Prepared || len(events[1].Outputs) != 0 {
			t.Errorf("%s: %v, %d bytes, %d records", c.fixture, steps(events), events[0].Part.Size, len(events[len(events)-1].Outputs))
			continue
		}
		sums[c.fixture] = events[0].Part.SHA256
	}
	if a, b := sums["commons-compress/lorem-ipsum-deflate64.zip"], sums["commons-compress/lorem-ipsum-implode.zip"]; a == "" || a != b {
		t.Errorf("lorem-ipsum: Deflate64 %s, implode %s; want the same bytes", a, b)
	}
	wantEmpty(t, work)
}

// TestSevenZipUnsafe: under a policy that quarantines them, the archive stage quarantines a 7z or
// a cabinet whose listing names a traversing, an absolute, or a drive letter name, a symbolic
// link, or a FIFO, before anything is extracted. By default, each flag active, each safe entry is
// extracted by its SHA-256 inside the working directory, and each flagged entry is an unread
// child, quarantined with its flag, never extracted, no link or node made.
func TestSevenZipUnsafe(t *testing.T) {
	strict := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{
		inspect.ArchiveTraversal: inspect.Quarantine, inspect.ArchiveAbsolutePath: inspect.Quarantine,
		inspect.ArchiveSymlink: inspect.Quarantine, inspect.ArchiveDevice: inspect.Quarantine,
	}}
	for _, c := range []struct {
		fixture  string
		check    inspect.Check
		parts    []string
		children map[string]inspect.Check
	}{
		{"traversal.7z", inspect.ArchiveTraversal, []string{"notes.txt"}, map[string]inspect.Check{"docs/../../evil.txt": inspect.ArchiveTraversal}},
		{"absolute.7z", inspect.ArchiveAbsolutePath, nil, map[string]inspect.Check{"/tmp/evil.txt": inspect.ArchiveAbsolutePath, `C:\evil.txt`: inspect.ArchiveAbsolutePath}},
		{"symlink.7z", inspect.ArchiveSymlink, []string{"notes.txt"}, map[string]inspect.Check{"passwd": inspect.ArchiveSymlink}},
		{"device.7z", inspect.ArchiveDevice, []string{"notes.txt"}, map[string]inspect.Check{"pipe": inspect.ArchiveDevice}},
		{"traversal.cab", inspect.ArchiveTraversal, []string{"notes.txt"}, map[string]inspect.Check{"docs/../../evil.txt": inspect.ArchiveTraversal}},
		{"absolute.cab", inspect.ArchiveAbsolutePath, nil, map[string]inspect.Check{"/tmp/evil.txt": inspect.ArchiveAbsolutePath, "C:/evil.txt": inspect.ArchiveAbsolutePath}},
	} {
		d, zs, work := sevenZipDispatcher(t, strict)
		events := dispatchAll(t, d, sevenZipFixture("generated/"+c.fixture), nil)
		ev := events[0]
		if len(events) != 1 || ev.Outcome != dispatch.Quarantined || zs.calls.Load() != 1 ||
			!slices.ContainsFunc(ev.Part.Flags, func(f inspect.Flag) bool { return f.Check == c.check }) ||
			ev.Reason != "quarantined by policy" {
			t.Errorf("%s: %v, flags %v, reason %q; want quarantined by the stage for %s", c.fixture, steps(events), ev.Part.Flags, ev.Reason, c.check)
		}
		wantEmpty(t, work)

		root := t.TempDir()
		work = filepath.Join(root, "a", "b", "work")
		if err := os.MkdirAll(work, 0o700); err != nil {
			t.Fatal(err)
		}
		checkUnsafe(t, stages.NewArchiveStage(sevenZipConfig(t, inspect.Options{})), sevenZipFixture("generated/"+c.fixture), root, work, c.parts, c.children)
	}
}

// TestSevenZipQuarantined: a 7z found inside a ZIP and quarantined by policy is removed once its
// event is done, as a part inspect quarantines is, and its flags, the listing's among them, are
// its event's.
func TestSevenZipQuarantined(t *testing.T) {
	traversal, err := os.ReadFile(sevenZipFixture("generated/traversal.7z"))
	if err != nil {
		t.Fatal(err)
	}
	strict := inspect.Options{Policy: map[inspect.Check]inspect.Verdict{inspect.ArchiveTraversal: inspect.Quarantine}}
	cfg := sevenZipConfig(t, strict)
	s := stages.NewArchiveStage(cfg)
	index := dispatch.NewMemoryIndex(0)
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: dispatch.Stages{route.Archive: s}, Index: index, Dir: work, Inspect: strict})
	if err != nil {
		t.Fatal(err)
	}
	events := dispatchAll(t, d, input(t, "outer.zip", zipBytes(t, zent{name: "traversal.7z", body: string(traversal)})), nil)
	wantSteps(t, events, []step{{"traversal.7z", 1, dispatch.Quarantined}, {"outer.zip", 0, dispatch.Prepared}})
	if flags := events[0].Part.Flags; !slices.ContainsFunc(flags, func(f inspect.Flag) bool { return f.Check == inspect.ArchiveTraversal }) {
		t.Errorf("traversal.7z's flags: %v", flags)
	}
	wantEmpty(t, work)
}

// TestSevenZipBombs: bomb.7z's and bomb.cab's listings declare 8 MiB and declared-bomb.zst's
// frame header 4 GiB; past a rule of 1,000:1 and 1 GiB, lowered for the 8 MiB, each is
// quarantined by its listing, nothing extracted. Under a policy that lets archive-bomb proceed,
// the stage stops writing the zeros 7zz gives once they pass both, removes the partial file, and
// records the entry quarantined, its check archive-bomb; bomb.zst, whose header declares no size,
// passes its listing and is stopped as written.
func TestSevenZipBombs(t *testing.T) {
	lowered := inspect.Options{BombSize: 1 << 20, BombRatio: 100}
	for _, c := range []struct {
		fixture string
		opts    inspect.Options
		check   inspect.Check
	}{
		{"bomb.7z", lowered, inspect.ArchiveBomb},
		{"bomb.cab", lowered, inspect.ArchiveBomb},
		{"declared-bomb.zst", inspect.Options{BombRatio: 1000, BombSize: 1 << 30}, inspect.ArchiveBomb},
	} {
		d, _, work := sevenZipDispatcher(t, c.opts)
		events := dispatchAll(t, d, sevenZipFixture("generated/"+c.fixture), nil)
		if len(events) != 1 || events[0].Outcome != dispatch.Quarantined || !strings.Contains(events[0].Reason, string(c.check)+": entry ") {
			t.Errorf("%s: %v, %q; want quarantined by its listing's %s", c.fixture, steps(events), events[0].Reason, c.check)
		}
		wantEmpty(t, work)
	}

	for _, c := range []struct {
		fixture, record string
		check           inspect.Check
	}{
		{"bomb.7z", "7z entry zeros.bin", inspect.ArchiveBomb},
		{"bomb.cab", "cabinet entry zeros.bin", inspect.ArchiveBomb},
		{"bomb.zst", "zstd content bomb", inspect.ArchiveBomb},
	} {
		opts := lowered
		opts.Policy = map[inspect.Check]inspect.Verdict{c.check: inspect.Proceed}
		d, _, work := sevenZipDispatcher(t, opts)
		events := dispatchAll(t, d, sevenZipFixture("generated/"+c.fixture), func(ev *dispatch.Event) {
			if left := files(t, work); len(left) > 0 {
				t.Errorf("%s: files at the archive's event: %q, want the partial file removed", c.fixture, left)
			}
		})
		if len(events) != 2 || events[1].Part.Name != c.fixture || events[1].Outcome != dispatch.Prepared {
			t.Fatalf("%s: %v", c.fixture, steps(events))
		}
		wantBomb(t, events[0], c.record, "bytes written from")
		if events[0].Reason != "past the bomb rule as written" {
			t.Errorf("%s: reason %q", c.fixture, events[0].Reason)
		}
		wantEmpty(t, work)
	}
}

// TestSevenZipEncrypted: encrypted.7z's clear entry is found and its encrypted one an unread
// child, encrypted, flagged archive-encrypted, and the archive is encrypted once its readable
// entry is dispatched; encrypted-headers.7z, which lists nothing without its password, is
// encrypted at once.
func TestSevenZipEncrypted(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, sevenZipFixture("generated/encrypted.7z"), nil)
	wantSteps(t, events, []step{{"notes.txt", 1, dispatch.NoProcessor}, {"secret.txt", 1, dispatch.Encrypted}, {"encrypted.7z", 0, dispatch.Encrypted}})
	last := events[2]
	if ev := events[1]; ev.Part.Where != "7z entry secret.txt" || len(ev.Part.Flags) != 1 ||
		ev.Part.Flags[0].Check != inspect.ArchiveEncrypted || ev.Part.Flags[0].Class != inspect.Active {
		t.Errorf("secret.txt: %q, flags %v", ev.Part.Where, ev.Part.Flags)
	}
	if !strings.Contains(last.Reason, "1 of 2 entries") {
		t.Errorf("encrypted.7z: reason %q", last.Reason)
	}
	events = dispatchAll(t, d, sevenZipFixture("generated/encrypted-headers.7z"), nil)
	wantSteps(t, events, []step{{"encrypted-headers.7z", 0, dispatch.Encrypted}})
	if !strings.Contains(events[0].Reason, "headers are encrypted") || len(events[0].Outputs) != 0 {
		t.Errorf("encrypted-headers.7z: %q, %d outputs", events[0].Reason, len(events[0].Outputs))
	}
	wantEmpty(t, work)
}

// TestSevenZipDamaged: a 7z cut short fails, recorded, 7-Zip unable to open it, and a zstd stream
// cut short is an unread child, failed, its content's partial file removed.
func TestSevenZipDamaged(t *testing.T) {
	whole, err := os.ReadFile(sevenZipFixture("generated/clean.7z"))
	if err != nil {
		t.Fatal(err)
	}
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, input(t, "cut.7z", string(whole[:100])), nil)
	if ev := events[0]; len(events) != 1 || ev.Outcome != dispatch.Failed || !strings.Contains(ev.Reason, "can't be opened") {
		t.Errorf("cut.7z: %v, %q; want failed", steps(events), ev.Reason)
	}
	zst, err := os.ReadFile(sevenZipFixture("generated/clean.tar.zst"))
	if err != nil {
		t.Fatal(err)
	}
	events = dispatchAll(t, d, input(t, "cut.tar.zst", string(zst[:1000])), nil)
	wantSteps(t, events, []step{{"cut.tar", 1, dispatch.Failed}, {"cut.tar.zst", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Part.Where != "zstd content cut.tar" || !strings.HasPrefix(ev.Reason, "doesn't decompress") {
		t.Errorf("cut.tar.zst's content: %q, %q", ev.Part.Where, ev.Reason)
	}
	wantEmpty(t, work)
}
