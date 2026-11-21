package inspect_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

func checksOf(r inspect.Report) []inspect.Check {
	var checks []inspect.Check
	for _, f := range r.Flags {
		checks = append(checks, f.Check)
	}
	return checks
}

func TestCheckListing(t *testing.T) {
	unsafe := []inspect.Listed{
		{Name: "notes.txt", Type: inspect.FileEntry, Declared: 18, Sized: true},
		{Name: "docs/../../evil.txt", Type: inspect.FileEntry},
		{Name: `C:\evil.txt`, Type: inspect.FileEntry},
		{Name: "passwd", Type: inspect.SymlinkEntry, Link: "/etc/passwd"},
		{Name: "shadow", Type: inspect.HardlinkEntry, Link: "/etc/shadow"},
		{Name: "pipe", Type: inspect.DeviceEntry},
		{Name: "secret.txt", Type: inspect.FileEntry, Encrypted: true},
		{Name: "zeros.bin", Type: inspect.FileEntry, Declared: 4 << 30, Sized: true},
	}
	all := []inspect.Check{
		inspect.ArchiveAbsolutePath, inspect.ArchiveBomb, inspect.ArchiveDevice, inspect.ArchiveEncrypted,
		inspect.ArchiveHardlink, inspect.ArchiveSymlink, inspect.ArchiveTraversal,
	}
	for _, c := range []struct {
		kind route.Kind
		want []inspect.Check
	}{
		{route.SevenZip, all},
		{route.MSCF, all},
		{route.Zstd, []inspect.Check{inspect.ArchiveBomb}},
	} {
		rep, err := inspect.CheckListing(c.kind, 1024, unsafe, rule)
		if err != nil {
			t.Fatal(err)
		}
		if got := checksOf(rep); !slices.Equal(got, c.want) || rep.Verdict != inspect.Quarantine || rep.Kind != c.kind {
			t.Errorf("%s: %v, %s, want %v, quarantine", c.kind, got, rep.Verdict, c.want)
		}
		for _, f := range rep.Flags {
			if (f.Class == inspect.Unsafe) != (f.Check == inspect.ArchiveBomb) || !strings.HasPrefix(f.Evidence, "entry ") {
				t.Errorf("%s: %s: class %s, evidence %q", c.kind, f.Check, f.Class, f.Evidence)
			}
		}
	}
	rep, err := inspect.CheckListing(route.SevenZip, 1024, unsafe[1:2], rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Flags) != 1 || rep.Flags[0].Evidence != `entry "docs/../../evil.txt"` || rep.Verdict != inspect.Proceed {
		t.Errorf("traversal's flags %v, %s; want one, proceed", rep.Flags, rep.Verdict)
	}
	rep, err = inspect.CheckListing(route.SevenZip, 1024, unsafe[7:], rule)
	if err != nil {
		t.Fatal(err)
	}
	if want := `entry "zeros.bin": 4294967296 bytes declared in a 1024-byte archive, 4194304:1`; len(rep.Flags) != 1 || rep.Flags[0].Evidence != want {
		t.Errorf("bomb's flags %v, want evidence %q", rep.Flags, want)
	}

	rep, err = inspect.CheckListing(route.SevenZip, 1024, unsafe[3:7], rule)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != inspect.Proceed || len(rep.Flags) != 4 {
		t.Errorf("links, a device, and encryption: %v, %s", rep.Flags, rep.Verdict)
	}
	rep, err = inspect.CheckListing(route.Zstd, 10, []inspect.Listed{{Name: "zeros", Type: inspect.FileEntry}}, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Flags) != 0 || rep.Verdict != inspect.Proceed {
		t.Errorf("unsized zstd: %v, %s", rep.Flags, rep.Verdict)
	}
}

func TestCheckListingSum(t *testing.T) {
	listing := func(each uint64) []inspect.Listed {
		var l []inspect.Listed
		for i := range 2000 {
			l = append(l, inspect.Listed{Name: fmt.Sprintf("f%d", i), Type: inspect.FileEntry, Declared: each, Sized: true})
		}
		return l
	}
	rep, err := inspect.CheckListing(route.SevenZip, 200<<10, listing(1<<30), rule)
	if err != nil {
		t.Fatal(err)
	}
	if want := "declared sizes sum to 2147483648000 bytes over 2000 entries in a 204800-byte archive, 10485760:1"; len(rep.Flags) != 1 || rep.Flags[0].Evidence != want {
		t.Errorf("2,000 GiB: %v, want %q", rep.Flags, want)
	}
	if rep, err := inspect.CheckListing(route.SevenZip, 200<<10, listing(256<<10), rule); err != nil || len(rep.Flags) != 0 {
		t.Errorf("500 MiB: %v, %v; want no flag", rep.Flags, err)
	}
}

func TestCheckListingOptions(t *testing.T) {
	bomb := []inspect.Listed{{Name: "zeros.bin", Type: inspect.FileEntry, Declared: 4 << 30, Sized: true}}
	permissive := rule
	permissive.Policy = map[inspect.Check]inspect.Verdict{inspect.ArchiveBomb: inspect.Proceed}
	rep, err := inspect.CheckListing(route.SevenZip, 100, bomb, permissive)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(checksOf(rep), []inspect.Check{inspect.ArchiveBomb}) || rep.Verdict != inspect.Proceed {
		t.Errorf("a bomb under a permissive policy: %v, %s", rep.Flags, rep.Verdict)
	}
	if rep, err := inspect.CheckListing(route.SevenZip, 100, bomb, inspect.Options{}); err != nil || len(rep.Flags) != 0 {
		t.Errorf("4 GiB under the zero Options: %v, %v; want no flag", rep.Flags, err)
	}
	eight := []inspect.Listed{{Name: "zeros.bin", Type: inspect.FileEntry, Declared: 8 << 20, Sized: true}}
	if rep, err := inspect.CheckListing(route.MSCF, 14662, eight, rule); err != nil || len(rep.Flags) != 0 {
		t.Errorf("8 MiB under rule: %v, %v", rep.Flags, err)
	}
	if rep, err := inspect.CheckListing(route.MSCF, 14662, eight, inspect.Options{BombSize: 1 << 20, BombRatio: 100}); err != nil || !slices.Contains(checksOf(rep), inspect.ArchiveBomb) {
		t.Errorf("8 MiB past lowered thresholds: %v, %v", rep.Flags, err)
	}
	if _, err := inspect.CheckListing(route.SevenZip, 1, nil, inspect.Options{BombRatio: -1, BombSize: 1}); !errors.Is(err, inspect.ErrOptions) {
		t.Errorf("a negative ratio: %v, want ErrOptions", err)
	}
	if _, err := inspect.CheckListing(route.Tar, 1, nil, inspect.Options{}); err == nil {
		t.Error("a tar's listing was checked; Inspect reads a tar's headers itself")
	}
}
