package stages_test

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/stages"
)

func capDispatcher(t *testing.T, limit int64) (*dispatch.Dispatcher, string) {
	t.Helper()
	d, _, work := archiveDispatcherLimited(t, stages.ArchiveConfig{}, dispatch.Limits{Bytes: limit})
	return d, work
}

func noFilesAt(t *testing.T, work string) func(*dispatch.Event) {
	return func(ev *dispatch.Event) {
		if ev.Part.Depth != 0 {
			return
		}
		if left := files(t, work); len(left) > 0 {
			t.Errorf("files at the input's event: %q, want the partial file removed", left)
		}
	}
}

func wantLimited(t *testing.T, ev dispatched, where string, limit int64) {
	t.Helper()
	if ev.Part.Where != where || ev.Outcome != dispatch.Limited || ev.Reason != fmt.Sprintf("past the bytes cap, %d", limit) ||
		ev.Part.SHA256 != "" || ev.Part.Size != 0 || len(ev.Part.Flags) != 0 {
		t.Errorf("%s: %q, %s, %q, SHA-256 %q, size %d, flags %v",
			ev.Part.Name, ev.Part.Where, ev.Outcome, ev.Reason, ev.Part.SHA256, ev.Part.Size, ev.Part.Flags)
	}
}

func TestZipBytesCap(t *testing.T) {
	z := zipBytes(t,
		zent{name: "zeros.bin", body: string(make([]byte, 8<<20))},
		zent{name: "notes.txt", body: "an ordinary entry\n"},
	)
	limit := int64(len(z)) + 1<<20
	d, work := capDispatcher(t, limit)
	events := dispatchAll(t, d, input(t, "big.zip", z), noFilesAt(t, work))
	wantSteps(t, events, []step{
		{"zeros.bin", 1, dispatch.Limited}, {"notes.txt", 1, dispatch.NoProcessor}, {"big.zip", 0, dispatch.Prepared},
	})
	wantLimited(t, events[0], "zip entry zeros.bin", limit)
	wantEmpty(t, work)

	d, work = capDispatcher(t, int64(len(z))+8<<20+64)
	events = dispatchAll(t, d, input(t, "big.zip", z), nil)
	wantSteps(t, events, []step{
		{"zeros.bin", 1, dispatch.NoProcessor}, {"notes.txt", 1, dispatch.NoProcessor}, {"big.zip", 0, dispatch.Prepared},
	})
	if ev := events[0]; ev.Part.Size != 8<<20 || ev.Part.SHA256 != sum(string(make([]byte, 8<<20))) {
		t.Errorf("zeros.bin within the cap: %d bytes, SHA-256 %s", ev.Part.Size, ev.Part.SHA256)
	}
	wantEmpty(t, work)
}

func TestZipBytesCapSum(t *testing.T) {
	const mib = 1 << 20
	z := zipBytes(t,
		zent{name: "a.bin", body: strings.Repeat("a", mib)},
		zent{name: "b.bin", body: strings.Repeat("b", mib)},
		zent{name: "c.bin", body: strings.Repeat("c", mib)},
		zent{name: "small.bin", body: strings.Repeat("s", 1000)},
	)
	limit := int64(len(z)) + 2*mib + mib/2
	d, work := capDispatcher(t, limit)
	events := dispatchAll(t, d, input(t, "three.zip", z), noFilesAt(t, work))
	wantSteps(t, events, []step{
		{"a.bin", 1, dispatch.NoProcessor},
		{"b.bin", 1, dispatch.NoProcessor},
		{"c.bin", 1, dispatch.Limited},
		{"small.bin", 1, dispatch.NoProcessor},
		{"three.zip", 0, dispatch.Prepared},
	})
	wantLimited(t, events[2], "zip entry c.bin", limit)
	wantEmpty(t, work)
}

func tarBytes(t *testing.T, entries ...string) string {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for i := 0; i < len(entries); i += 2 {
		if err := tw.WriteHeader(&tar.Header{Name: entries[i], Mode: 0o644, Size: int64(len(entries[i+1])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, entries[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestTarBytesCap(t *testing.T) {
	const mib = 1 << 20
	tr := tarBytes(t, "a.bin", strings.Repeat("a", mib), "b.bin", strings.Repeat("b", mib), "c.bin", "c")
	limit := int64(len(tr)) + mib + mib/2
	d, work := capDispatcher(t, limit)
	events := dispatchAll(t, d, input(t, "two.tar", tr), noFilesAt(t, work))
	wantSteps(t, events, []step{
		{"a.bin", 1, dispatch.NoProcessor}, {"b.bin", 1, dispatch.Limited}, {"c.bin", 1, dispatch.NoProcessor},
		{"two.tar", 0, dispatch.Prepared},
	})
	wantLimited(t, events[1], "tar entry b.bin", limit)
	wantEmpty(t, work)

	gz := inspectFixture("bomb.tar.gz")
	info, err := os.Stat(gz)
	if err != nil {
		t.Fatal(err)
	}
	limit = info.Size() + mib
	d, work = capDispatcher(t, limit)
	events = dispatchAll(t, d, gz, noFilesAt(t, work))
	wantSteps(t, events, []step{{"bomb.tar", 1, dispatch.Limited}, {"bomb.tar.gz", 0, dispatch.Prepared}})
	wantLimited(t, events[0], "gzip content bomb.tar", limit)
	wantEmpty(t, work)

	limit = info.Size() + 9*mib
	d, work = capDispatcher(t, limit)
	events = dispatchAll(t, d, gz, nil)
	wantSteps(t, events, []step{
		{"zeros.bin", 2, dispatch.Limited}, {"bomb.tar", 1, dispatch.Prepared}, {"bomb.tar.gz", 0, dispatch.Prepared},
	})
	wantLimited(t, events[0], "tar entry zeros.bin", limit)
	wantEmpty(t, work)
}

type endless struct {
	written atomic.Int64
}

func (e *endless) List(context.Context, sevenzip.ListRequest) (sevenzip.Listing, error) {
	return sevenzip.Listing{Entries: []sevenzip.Entry{{}}}, nil
}

func (e *endless) Read(context.Context, sevenzip.ReadRequest) iter.Seq2[sevenzip.Item, error] {
	return func(func(sevenzip.Item, error) bool) {}
}

func (e *endless) Extract(ctx context.Context, _ sevenzip.ExtractRequest, w io.Writer) error {
	chunk := make([]byte, 32<<10)
	for ctx.Err() == nil {
		n, err := w.Write(chunk)
		e.written.Add(int64(n))
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func TestZstdBytesCap(t *testing.T) {
	const mib = 1 << 20
	stream := input(t, "endless.zst", "\x28\xb5\x2f\xfd and a frame that never ends")
	info, err := os.Stat(stream)
	if err != nil {
		t.Fatal(err)
	}
	limit := info.Size() + 4*mib
	seven := &endless{}
	d, _, work := archiveDispatcherLimited(t, stages.ArchiveConfig{SevenZip: seven}, dispatch.Limits{Bytes: limit})
	events := dispatchAll(t, d, stream, noFilesAt(t, work))
	wantSteps(t, events, []step{{"endless", 1, dispatch.Limited}, {"endless.zst", 0, dispatch.Prepared}})
	wantLimited(t, events[0], "zstd content endless", limit)
	if n, room := seven.written.Load(), limit-info.Size(); n > room || n < room-32<<10 {
		t.Errorf("7zz wrote %d bytes, want at most %d, within a write of it", n, room)
	}
	if left, err := filepath.Glob(filepath.Join(work, "*", "entry-*")); err != nil || len(left) > 0 {
		t.Errorf("partial files left: %q, %v", left, err)
	}
	wantEmpty(t, work)
}
