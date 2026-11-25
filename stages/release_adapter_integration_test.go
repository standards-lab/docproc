//go:build linux && integration

package stages_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"math/rand/v2"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
)

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

func TestEmailStageReleases(t *testing.T) {
	stores := []struct {
		path    string
		bounded bool
	}{
		{filepath.Join(mailFixtures(t), "generated", "pst", "mailbox.pst"), false},
		{filepath.Join(mailFixtures(t), "pst", "tika", "testPST.pst"), false},
		{filepath.Join(mailFixtures(t), "pst", "libyal", "outlook.pst"), false},
		{spreadMbox(t), true},
	}
	for _, store := range stores {
		t.Run(filepath.Base(store.path), func(t *testing.T) {
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
			for ev, err := range d.Dispatch(t.Context(), dispatch.Input{Path: store.path}) {
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
			if store.bounded {
				if found != spreadMessages {
					t.Errorf("%d attachments found, want %d", found, spreadMessages)
				}
				if peak >= sum {
					t.Errorf("the working directory's peak %d bytes, want below the %d distinct attachments' %d", peak, len(distinct), sum)
				}
			}
		})
	}
}

const (
	spreadMessages   = 100
	spreadAttachment = 64 << 10
	spreadBody       = 8 << 10
)

func spreadMbox(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	line := strings.Repeat("spread ", 9) + "\n"
	body := strings.Repeat(line, spreadBody/len(line))
	for i := range spreadMessages {
		content := make([]byte, spreadAttachment)
		var seed [32]byte
		binary.LittleEndian.PutUint64(seed[:], uint64(i))
		_, _ = rand.NewChaCha8(seed).Read(content)

		var part bytes.Buffer
		mw := multipart.NewWriter(&part)
		w, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=us-ascii"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, body)
		w, err = mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"application/octet-stream"},
			"Content-Disposition":       {fmt.Sprintf(`attachment; filename="part-%d.bin"`, i+1)},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			t.Fatal(err)
		}
		encoded := base64.StdEncoding.EncodeToString(content)
		for len(encoded) > 76 {
			_, _ = io.WriteString(w, encoded[:76]+"\r\n")
			encoded = encoded[76:]
		}
		_, _ = io.WriteString(w, encoded+"\r\n")
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}

		buf.WriteString("From sender@example.com Mon Jan  1 00:00:00 2024\n")
		fmt.Fprintf(&buf, "From: sender@example.com\nTo: recipient@example.com\nDate: Mon, 01 Jan 2024 00:00:00 +0000\nSubject: message %d\n", i+1)
		fmt.Fprintf(&buf, "Message-ID: <%d@example.com>\nMIME-Version: 1.0\n", i+1)
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\n\n", mw.Boundary())
		buf.Write(part.Bytes())
		buf.WriteString("\n")
	}
	path := filepath.Join(t.TempDir(), "spread.mbox")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type sink struct{}

func (sink) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "sink", Version: "1", Config: "test"}
}

func (sink) Prepare(_ context.Context, p dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		yield(dispatch.OutputItem(dispatch.Output{Kind: dispatch.TextOutput, Name: "content", Value: []byte(p.SHA256)}), nil)
	}
}
