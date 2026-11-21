//go:build linux

package sevenzip_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
)

func listed(t *testing.T, kind route.Kind, out string, entries int) (sevenzip.Listing, error) {
	t.Helper()
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: standin(t, standinSettings{Version: "26.05", Listing: out})})
	if err != nil {
		t.Fatal(err)
	}
	return r.List(t.Context(), sevenzip.ListRequest{Path: "t." + string(kind), Kind: kind, Entries: entries})
}

const slt = `
7-Zip (z) 26.05 (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-10-21
 64-bit locale=en_US.UTF-8 Threads:22 OPEN_MAX:524288, ASM

Scanning the drive for archives:
1 file, 229 bytes (1 KiB)

Listing archive: t.7z

--
Path = t.7z
Type = 7z
Physical Size = 229
Headers Size = 202
Method = LZMA2:12 7zAES:19
Solid = +
Blocks = 1

----------
Path = docs
Size = 0
Packed Size = 0
Attributes = D drwxr-xr-x
CRC =
Encrypted = -
Method =
Block =

Path = docs/notes.txt
Size = 22
Packed Size = 27
Attributes = A -rw-r--r--
CRC = C389908F
Encrypted = -
Method = LZMA2:12
Block = 0

Path = passwd
Size = 11
Packed Size =
Attributes = A lrwxrwxrwx
CRC = 291FB90A
Encrypted = -
Method = LZMA2:12
Block = 0

Path = pipe
Size = 0
Attributes = A prw-r--r--
CRC =
Encrypted = -
Method =

Path = secret.txt
Size = 38
Attributes = A -rw-r--r--
CRC = 361CAAB9
Encrypted = +
Method = LZMA2:12 7zAES:19

Path = C:/evil.txt
Size = 15
Attributes = A
Method = MSZip
`

func TestListParses(t *testing.T) {
	t.Parallel()
	l, err := listed(t, route.SevenZip, slt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if l.Method != "LZMA2:12 7zAES:19" || len(l.Entries) != 6 {
		t.Fatalf("listing %+v", l)
	}
	want := []sevenzip.Entry{
		{Path: "docs", Folder: true, Sized: true, Mode: fs.ModeDir | 0o755},
		{Path: "docs/notes.txt", Size: 22, Sized: true, CRC: 0xC389908F, HasCRC: true, Method: "LZMA2:12", Mode: 0o644},
		{Path: "passwd", Size: 11, Sized: true, CRC: 0x291FB90A, HasCRC: true, Method: "LZMA2:12", Mode: fs.ModeSymlink | 0o777},
		{Path: "pipe", Sized: true, Mode: fs.ModeNamedPipe | 0o644},
		{Path: "secret.txt", Size: 38, Sized: true, CRC: 0x361CAAB9, HasCRC: true, Encrypted: true, Method: "LZMA2:12 7zAES:19", Mode: 0o644},
		{Path: "C:/evil.txt", Size: 15, Sized: true, Method: "MSZip"},
	}
	for i, w := range want {
		if l.Entries[i] != w {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, l.Entries[i], w)
		}
	}
	if !l.Entries[2].IsSymlink() || l.Entries[2].IsDevice() || !l.Entries[3].IsDevice() || l.Entries[1].IsSymlink() {
		t.Error("IsSymlink or IsDevice misread a mode")
	}
}

func TestListParseErrors(t *testing.T) {
	t.Parallel()
	for name, out := range map[string]string{
		"no items":   "--\nPath = a.7z\nType = 7z\n",
		"not a pair": "--\nType = 7z\n----------\nPath = a\nnot a property\n",
		"bad size":   "--\nType = 7z\n----------\nPath = a\nSize = many\n",
		"bad CRC":    "--\nType = 7z\n----------\nPath = a\nCRC = XYZ\n",
		"long line":  "--\nType = 7z\n----------\nPath = " + strings.Repeat("a", 1<<20) + "\n",
	} {
		if _, err := listed(t, route.SevenZip, out, 0); !errors.Is(err, sevenzip.ErrListing) {
			t.Errorf("%s: %v, want sevenzip.ErrListing", name, err)
		}
	}
}

func TestListEntries(t *testing.T) {
	t.Parallel()
	if _, err := listed(t, route.SevenZip, slt, 5); !errors.Is(err, sevenzip.ErrEntries) {
		t.Errorf("past Entries: %v, want sevenzip.ErrEntries", err)
	}
	for _, entries := range []int{6, 0} {
		if l, err := listed(t, route.SevenZip, slt, entries); err != nil || len(l.Entries) != 6 {
			t.Errorf("Entries %d: %d entries, %v", entries, len(l.Entries), err)
		}
	}
	if _, err := listed(t, route.SevenZip, slt, -1); !errors.Is(err, sevenzip.ErrRequest) {
		t.Errorf("negative Entries: %v, want sevenzip.ErrRequest", err)
	}
}

func TestListLineEnds(t *testing.T) {
	t.Parallel()
	l, err := listed(t, route.MSCF, "--\nType = Cab\n----------\nPath = return\r\nSize = 1\n\nPath = last\nSize = 2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 || l.Entries[0].Path != "return\r" || l.Entries[1].Path != "last" || l.Entries[1].Size != 2 {
		t.Errorf("entries %+v", l.Entries)
	}
}

func TestListZstdSize(t *testing.T) {
	t.Parallel()
	zstd := func(method, size string) sevenzip.Entry {
		t.Helper()
		out := "--\nPath = t.zst\nType = zstd\nMethod = " + method + "\n----------\nPath = t\nSize = " + size + "\n"
		l, err := listed(t, route.Zstd, out, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Entries) != 1 {
			t.Fatalf("%d entries, want 1", len(l.Entries))
		}
		return l.Entries[0]
	}
	declared := "header-open-only: NO-XXH64 wnd-desc-log-MAX:17 wnd-MAX:128KiB content-size-frame-max:4294967296 content-size-total:4294967296"
	if e := zstd(declared, ""); !e.Sized || e.Size != 4<<30 {
		t.Errorf("declared: %+v", e)
	}
	if e := zstd("header-open-only: XXH64 wnd-desc-log-MAX:21 wnd-MAX:2MiB unknown-content-size", ""); e.Sized {
		t.Errorf("unknown size: %+v", e)
	}
	if e := zstd(declared, "7"); e.Size != 7 {
		t.Errorf("listed size replaced: %+v", e)
	}
}
