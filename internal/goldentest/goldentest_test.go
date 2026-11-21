package goldentest_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/goldentest"
)

func encoded(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func chunk(kind, data string) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	b = append(b, kind+data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE([]byte(kind+data)))
}

func withChunks(p []byte, chunks ...[]byte) []byte {
	at := 8 + 12 + int(binary.BigEndian.Uint32(p[8:]))
	out := append([]byte{}, p[:at]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, p[at:]...)
}

func TestCanonicalPNG(t *testing.T) {
	plain := encoded(t)
	timed := func(stamp string) []byte {
		return withChunks(plain,
			chunk("tIME", "\x07\xea\x0b\x05\x0a"+stamp),
			chunk("tEXt", "date:create\x002026-11-05T10:"+stamp),
			chunk("zTXt", "date:modify\x00\x00"+stamp),
			chunk("iTXt", "date:timestamp\x00\x00\x00\x00\x00"+stamp),
		)
	}
	first, second := goldentest.CanonicalPNG(timed("00")), goldentest.CanonicalPNG(timed("59"))
	if !bytes.Equal(first, plain) || !bytes.Equal(second, plain) {
		t.Errorf("timed PNGs canonicalized to %q and %q, want the untimed %q", first, second, plain)
	}
	kept := withChunks(plain, chunk("tEXt", "Software\x00magick"))
	if got := goldentest.CanonicalPNG(kept); !bytes.Equal(got, kept) {
		t.Errorf("a non-date text chunk: got %q, want it kept", got)
	}
	for _, bad := range [][]byte{
		[]byte("not a png"),
		plain[:len(plain)-3],
		append(append([]byte{}, plain[:8]...), 0xff, 0xff, 0xff, 0xff, 'I', 'D', 'A', 'T'),
	} {
		if got := goldentest.CanonicalPNG(bad); !bytes.Equal(got, bad) {
			t.Errorf("malformed %q: got %q, want it as it is", bad, got)
		}
	}
}

func TestCheckRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.sha256")
	got := map[string]string{"b/two": goldentest.Sum([]byte("2")), "a/one": goldentest.Sum([]byte("1"))}
	t.Setenv(goldentest.RecordEnv, "1")
	goldentest.Check(t, path, got)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b  a/one\n" +
		"d4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35  b/two\n"
	if string(data) != want {
		t.Errorf("recorded %q, want %q", data, want)
	}
	t.Setenv(goldentest.RecordEnv, "")
	goldentest.Check(t, path, got)
}

type errorsTB struct {
	testing.TB
	errs []string
}

func (e *errorsTB) Helper() {}

func (e *errorsTB) Errorf(format string, args ...any) {
	e.errs = append(e.errs, fmt.Sprintf(format, args...))
}

func TestCheckCompares(t *testing.T) {
	t.Setenv(goldentest.RecordEnv, "")
	path := filepath.Join(t.TempDir(), "golden.sha256")
	golden := goldentest.Sum([]byte("1")) + "  a/one\n" + goldentest.Sum([]byte("2")) + "  b/two\n"
	if err := os.WriteFile(path, []byte(golden), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		got  map[string]string
		want []string
	}{
		{"equal", map[string]string{"a/one": goldentest.Sum([]byte("1")), "b/two": goldentest.Sum([]byte("2"))}, nil},
		{"changed", map[string]string{"a/one": goldentest.Sum([]byte("1")), "b/two": goldentest.Sum([]byte("3"))}, []string{"b/two: sum"}},
		{"missing", map[string]string{"a/one": goldentest.Sum([]byte("1"))}, []string{"b/two: no output"}},
		{"new", map[string]string{
			"a/one": goldentest.Sum([]byte("1")), "b/two": goldentest.Sum([]byte("2")), "c/three": goldentest.Sum([]byte("3")),
		}, []string{"c/three: an output the golden file doesn't hold"}},
	} {
		tb := &errorsTB{TB: t}
		goldentest.Check(tb, path, c.got)
		if !slices.EqualFunc(tb.errs, c.want, strings.HasPrefix) {
			t.Errorf("%s: errors %q, want %q", c.name, tb.errs, c.want)
		}
	}
}
