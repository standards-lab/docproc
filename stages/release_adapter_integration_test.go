//go:build linux && integration

package stages_test

import (
	"context"
	"errors"
	"io/fs"
	"iter"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
)

// diskBytes returns the bytes the regular files under dir hold, a hard link's once.
func diskBytes(t *testing.T, dir string) int64 {
	t.Helper()
	seen := map[uint64]bool{}
	var size int64
	err := filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !e.Type().IsRegular() {
			return nil
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			if seen[st.Ino] {
				return nil
			}
			seen[st.Ino] = true
		}
		size += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return size
}

// TestEmailStageReleases dispatches real stores through the email stage, a sink stage on every
// other route so that each attachment is recorded, probing the working directory's bytes at
// every event: no attachment fails or is lost, and a repeated one is seen. The tika store gives
// mail no attachment content, and libyal's repeats one message's eleven attachments in a second.
// No committed store spreads its attachments over messages enough that the peak must fall below
// their sum, mail writing a small store's ahead of the dispatch, so the test logs the peak;
// dispatch's TestReleaseBoundsDisk holds that each original is released once it is recorded.
func TestEmailStageReleases(t *testing.T) {
	stores := []string{
		filepath.Join(mailFixtures(t), "generated", "pst", "mailbox.pst"),
		filepath.Join(mailFixtures(t), "pst", "tika", "testPST.pst"),
		filepath.Join(mailFixtures(t), "pst", "libyal", "outlook.pst"),
	}
	for _, store := range stores {
		t.Run(filepath.Base(store), func(t *testing.T) {
			stages := dispatch.Stages{route.Nested: testEmailStage(t)}
			for _, r := range []route.Route{
				route.Document, route.Office, route.Vision, route.VisionConverted, route.OneNote,
				route.Archive, route.Audio, route.Video, route.Text, route.Unrouted,
			} {
				stages[r] = sink{}
			}
			dir := t.TempDir()
			d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: stages, Index: dispatch.NewMemoryIndex(0), Dir: dir})
			if err != nil {
				t.Fatal(err)
			}
			var peak, sum int64
			distinct := map[string]bool{}
			found, seen := 0, 0
			var last dispatch.Event
			for ev, err := range d.Dispatch(t.Context(), dispatch.Input{Path: store}) {
				if err != nil {
					t.Fatal(err)
				}
				peak = max(peak, diskBytes(t, dir))
				switch {
				case ev.Output != nil:
				case ev.Part.Depth == 0:
					last = ev
				case ev.Part.Depth == 1:
					found++
					if ev.Outcome == dispatch.Failed {
						t.Errorf("%s: %s", ev.Part.Where, ev.Reason)
					}
					if ev.Outcome == dispatch.Seen {
						seen++
					}
					if !distinct[ev.Part.SHA256] {
						distinct[ev.Part.SHA256] = true
						sum += ev.Part.Size
					}
				}
			}
			if last.Outcome != dispatch.Prepared || last.Children != found {
				t.Fatalf("the store: %s, %q, %d children of %d found", last.Outcome, last.Reason, last.Children, found)
			}
			if found-len(distinct) != seen {
				t.Errorf("%d found, %d distinct, %d seen: each repeat should be seen", found, len(distinct), seen)
			}
			t.Logf("%d attachments, %d distinct, %d bytes; the working directory's peak %d bytes", found, len(distinct), sum, peak)
		})
	}
}

// sink is a stage that yields one output for each part, so each is recorded.
type sink struct{}

func (sink) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "sink", Version: "1", Config: "test"}
}

func (sink) Prepare(_ context.Context, p dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		yield(dispatch.OutputItem(dispatch.Output{Kind: dispatch.TextOutput, Name: "content", Value: []byte(p.SHA256)}), nil)
	}
}
