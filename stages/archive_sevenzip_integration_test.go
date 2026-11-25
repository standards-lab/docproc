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

func sevenZipConfig(t *testing.T, opts inspect.Options) stages.ArchiveConfig {
	t.Helper()
	tl := pinned(t, sevenzip.SevenZipSpec())
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: tl})
	if err != nil {
		t.Fatal(err)
	}
	return stages.ArchiveConfig{Inspect: opts, SevenZip: r, Version: stages.Versions(tl)}
}

func sevenZipDispatcher(t *testing.T, opts inspect.Options) (*dispatch.Dispatcher, *counting, string) {
	t.Helper()
	return archiveDispatcherWith(t, sevenZipConfig(t, opts))
}

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
		{"bomb.xz", lowered, inspect.ArchiveBomb},
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
		{"bomb.xz", "xz content bomb", inspect.ArchiveBomb},
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

func TestRARStage(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	for _, c := range []struct {
		fixture string
		want    []step
		sizes   map[string]int64
	}{
		{"test_read_format_rar.rar", []step{
			{"test.txt", 1, dispatch.NoProcessor},
			{"testlink", 1, dispatch.Quarantined},
			{"testdir/test.txt", 1, dispatch.NoProcessor},
			{"test_read_format_rar.rar", 0, dispatch.Prepared},
		}, map[string]int64{"test.txt": 20, "testdir/test.txt": 20}},
		{"test_read_format_rar5_compressed.rar", []step{
			{"test.bin", 1, dispatch.NoProcessor},
			{"test_read_format_rar5_compressed.rar", 0, dispatch.Prepared},
		}, map[string]int64{"test.bin": 1200}},
		{"test_read_format_rar5_symlink.rar", []step{
			{"file.txt", 1, dispatch.NoProcessor},
			{"symlink.txt", 1, dispatch.Quarantined},
			{"dirlink", 1, dispatch.Quarantined},
			{"test_read_format_rar5_symlink.rar", 0, dispatch.Prepared},
		}, map[string]int64{"file.txt": 5}},
	} {
		events := dispatchAll(t, d, sevenZipFixture("libarchive/"+c.fixture), nil)
		wantSteps(t, events, c.want)
		for _, ev := range events[:len(events)-1] {
			if ev.Part.Where != "RAR entry "+ev.Part.Name {
				t.Errorf("%s: %s at %q", c.fixture, ev.Part.Name, ev.Part.Where)
			}
			if size, ok := c.sizes[ev.Part.Name]; ok && ev.Part.Size != size {
				t.Errorf("%s: %s, %d bytes, want %d", c.fixture, ev.Part.Name, ev.Part.Size, size)
			}
			if ev.Outcome == dispatch.Quarantined && !hasFlag(ev.Part.Flags, inspect.ArchiveSymlink) {
				t.Errorf("%s: %s, flags %v, want a symbolic link's", c.fixture, ev.Part.Name, ev.Part.Flags)
			}
		}
	}
	wantEmpty(t, work)
}

func TestRARSplitVolumes(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	for _, fixture := range []string{"test_read_format_rar_multivolume.part0001.rar", "test_read_format_rar5_multiarchive.part01.rar"} {
		events := dispatchAll(t, d, sevenZipFixture("libarchive/"+fixture), nil)
		wantSteps(t, events, []step{{fixture, 0, dispatch.NoProcessor}})
		if events[0].Reason != "archive stage: a split RAR volume: no processor yet" {
			t.Errorf("%s: reason %q", fixture, events[0].Reason)
		}
	}
	wantEmpty(t, work)
}

func TestRAREncrypted(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	for _, c := range []struct {
		fixture string
		want    []step
		reason  string
	}{
		{"test_read_format_rar_encryption_partially.rar", []step{
			{"foo.txt", 1, dispatch.Encrypted},
			{"bar.txt", 1, dispatch.NoProcessor},
			{"test_read_format_rar_encryption_partially.rar", 0, dispatch.Encrypted},
		}, "1 of 2 entries"},
		{"test_read_format_rar5_encrypted.rar", []step{
			{"a.txt", 1, dispatch.NoProcessor},
			{"b.txt", 1, dispatch.Encrypted},
			{"c.txt", 1, dispatch.NoProcessor},
			{"d.txt", 1, dispatch.Encrypted},
			{"test_read_format_rar5_encrypted.rar", 0, dispatch.Encrypted},
		}, "2 of 4 entries"},
		{"test_read_format_rar_encryption_header.rar", []step{
			{"test_read_format_rar_encryption_header.rar", 0, dispatch.Encrypted},
		}, "a RAR archive whose headers are encrypted"},
		{"test_read_format_rar5_encrypted_filenames.rar", []step{
			{"test_read_format_rar5_encrypted_filenames.rar", 0, dispatch.Encrypted},
		}, "a RAR archive whose headers are encrypted"},
	} {
		events := dispatchAll(t, d, sevenZipFixture("libarchive/"+c.fixture), nil)
		wantSteps(t, events, c.want)
		last := events[len(events)-1]
		if !strings.Contains(last.Reason, c.reason) {
			t.Errorf("%s: reason %q, want %q in it", c.fixture, last.Reason, c.reason)
		}
		for _, ev := range events[:len(events)-1] {
			read := ev.Outcome != dispatch.Encrypted
			if read == (ev.Part.SHA256 == "") || !read && !hasFlag(ev.Part.Flags, inspect.ArchiveEncrypted) {
				t.Errorf("%s: %s, SHA-256 %q, flags %v", c.fixture, ev.Part.Name, ev.Part.SHA256, ev.Part.Flags)
			}
		}
	}
	wantEmpty(t, work)
}

func TestXZStreams(t *testing.T) {
	d, _, work := sevenZipDispatcher(t, inspect.Options{})
	events := dispatchAll(t, d, sevenZipFixture("generated/notes.txt.xz"), nil)
	wantSteps(t, events, []step{{"notes.txt", 1, dispatch.NoProcessor}, {"notes.txt.xz", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Part.Where != "xz content notes.txt" || ev.Part.SHA256 != sum("an ordinary entry\n") {
		t.Errorf("notes.txt: %q, SHA-256 %s", ev.Part.Where, ev.Part.SHA256)
	}

	events = dispatchAll(t, d, sevenZipFixture("generated/clean.txz"), nil)
	wantSteps(t, events, []step{
		{"docs/notes.txt", 2, dispatch.NoProcessor},
		{"docs/pixel.png", 2, dispatch.Prepared},
		{"clean.tar", 1, dispatch.Prepared},
		{"clean.txz", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"tar entry docs/notes.txt", "tar entry docs/pixel.png", "xz content clean.tar"}; !slices.Equal(got, want) {
		t.Errorf("clean.txz: wheres:\n got %q\nwant %q", got, want)
	}
	if ev := events[2]; ev.Part.ContentType.Kind != route.Tar || ev.Part.Size != 3584 {
		t.Errorf("clean.tar: %s, %d bytes", ev.Part.ContentType.Kind, ev.Part.Size)
	}

	xz, err := os.ReadFile(sevenZipFixture("generated/clean.txz"))
	if err != nil {
		t.Fatal(err)
	}
	events = dispatchAll(t, d, input(t, "cut.txz", string(xz[:len(xz)-40])), nil)
	wantSteps(t, events, []step{{"cut.tar", 1, dispatch.Failed}, {"cut.txz", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Part.Where != "xz content cut.tar" || !strings.HasPrefix(ev.Reason, "doesn't decompress") {
		t.Errorf("cut.txz's content: %q, %q", ev.Part.Where, ev.Reason)
	}
	wantEmpty(t, work)
}
