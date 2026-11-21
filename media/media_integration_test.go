//go:build integration && linux

package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/media"
)

type scenesTruth struct {
	Video struct {
		Width    int `json:"width"`
		Height   int `json:"height"`
		Segments []struct {
			StartMS int64  `json:"start_ms"`
			EndMS   int64  `json:"end_ms"`
			Pattern string `json:"pattern"`
		} `json:"segments"`
		Frames []struct {
			Reason string `json:"reason"`
			TimeMS int64  `json:"time_ms"`
		} `json:"frames"`
	} `json:"video"`
	Audio struct {
		Transcript []string `json:"transcript"`
	} `json:"audio"`
}

func readTruth(t *testing.T) scenesTruth {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "audio", "testdata", "speech-scenes.truth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var truth scenesTruth
	if err := json.Unmarshal(b, &truth); err != nil {
		t.Fatal(err)
	}
	return truth
}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "audio", "testdata", "speech-scenes.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "speech-scenes.mkv")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScenes(t *testing.T) {
	path := fixture(t)
	truth := readTruth(t)
	got := map[string][]string{}
	for name, runner := range runners {
		t.Run(name, func(t *testing.T) {
			cfg := runner(t)
			dir := t.TempDir()
			events := collect(t, streamer(t, cfg).Events(t.Context(), path, media.Request{Dir: dir}))
			ws, fs := split(events)
			if len(fs) != len(truth.Video.Frames) {
				t.Errorf("%d frames, want the truth's %d", len(fs), len(truth.Video.Frames))
			}
			for i, e := range fs {
				f := e.Frame
				t.Logf("recorded: frame %d: %s at %v, %dx%d, sha256 %s", f.Index, f.Reason, e.Time, f.Width, f.Height, f.SHA256)
				got[name] = append(got[name], f.SHA256)
				if i < len(truth.Video.Frames) {
					want := truth.Video.Frames[i]
					if f.Index != i || f.Reason.String() != want.Reason || e.Time != time.Duration(want.TimeMS)*time.Millisecond {
						t.Errorf("frame %d: %d, %s at %v; want %d, %s at %d ms", i, f.Index, f.Reason, e.Time, i, want.Reason, want.TimeMS)
					}
				}
				if f.Width != truth.Video.Width || f.Height != truth.Video.Height {
					t.Errorf("frame %d is %dx%d, want %dx%d", i, f.Width, f.Height, truth.Video.Width, truth.Video.Height)
				}
				checkPNG(t, f)
			}
			sp, err := audio.NewSplitter(audio.Config{FFmpeg: cfg.FFmpeg, FFprobe: cfg.FFprobe})
			if err != nil {
				t.Fatal(err)
			}
			var want []audio.WindowFile
			for w, err := range sp.Windows(t.Context(), path, audio.Request{Dir: t.TempDir(), Concurrency: 1}) {
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, w)
			}
			if len(ws) != len(want) {
				t.Fatalf("%d windows, want audio's %d", len(ws), len(want))
			}
			for i, e := range ws {
				w := e.Window
				t.Logf("recorded: window %d: %v, %s, %d samples, threshold %.2f dB, sha256 %s", w.Index, w.Span, w.Cut, w.Samples, w.Threshold, w.SHA256)
				got[name] = append(got[name], w.SHA256)
				if w.Index != i || w.SHA256 != want[i].SHA256 || w.Samples != want[i].Samples || w.Cut != want[i].Window.Cut || w.Span != want[i].Window.Span ||
					w.SeamBefore != want[i].SeamBefore || w.SeamAfter != want[i].SeamAfter || w.Threshold != want[i].Threshold {
					t.Errorf("window %d: %d, %v %s, %d samples, %s; audio's %v %s, %d samples, %s",
						i, w.Index, w.Span, w.Cut, w.Samples, w.SHA256, want[i].Window.Span, want[i].Window.Cut, want[i].Samples, want[i].SHA256)
				}
				if w.Cut == audio.CutSilence && w.Threshold >= 0 {
					t.Errorf("window %d, cut at a silence, names threshold %v dB", i, w.Threshold)
				}
			}
			checkDir(t, dir, events)
			checkNoScratch(t, filepath.Dir(path))
		})
	}
	if e, c := got["exec"], got["container"]; len(got) == 2 && !slices.Equal(e, c) {
		t.Errorf("the exec runner wrote %q, the container runner %q", e, c)
	}
}

func TestScenesMaxSide(t *testing.T) {
	path := fixture(t)
	truth := readTruth(t)
	s := streamer(t, runners["exec"](t))
	events := collect(t, s.Events(t.Context(), path, media.Request{Dir: t.TempDir(), MaxSide: 160}))
	_, fs := split(events)
	if len(fs) != len(truth.Video.Frames) {
		t.Fatalf("%d frames, want %d", len(fs), len(truth.Video.Frames))
	}
	for i, e := range fs {
		if e.Frame.Width != 160 || e.Frame.Height != 90 || e.Time != time.Duration(truth.Video.Frames[i].TimeMS)*time.Millisecond {
			t.Errorf("frame %d: %dx%d at %v, want 160x90 at %d ms", i, e.Frame.Width, e.Frame.Height, e.Time, truth.Video.Frames[i].TimeMS)
		}
		checkPNG(t, e.Frame)
	}
}

func TestCancel(t *testing.T) {
	path := fixture(t)
	for name, runner := range runners {
		t.Run(name, func(t *testing.T) {
			s := streamer(t, runner(t))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dir := t.TempDir()
			var cancelled time.Time
			var events []media.Event
			var errs []error
			for e, err := range s.Events(ctx, path, media.Request{Dir: dir}) {
				if err != nil {
					errs = append(errs, err)
					continue
				}
				events = append(events, e)
				if cancelled.IsZero() {
					cancelled = time.Now()
				}
				cancel()
			}
			took := time.Since(cancelled)
			t.Logf("recorded: cancelled after %d events; the stream ended %v after the cancel", len(events), took)
			if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
				t.Errorf("errors %v, want context.Canceled once", errs)
			}
			if len(events) == 0 || took > 10*time.Second {
				t.Errorf("the stream ended %v after the cancel, after %d events; the source is 50 s", took, len(events))
			}
			checkDir(t, dir, events)
			checkNoScratch(t, filepath.Dir(path))
		})
	}
}

func checkPNG(t *testing.T, f *media.Frame) {
	t.Helper()
	b, err := os.ReadFile(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) || filepath.Base(f.Path) != f.SHA256+".png" || sum(b) != f.SHA256 {
		t.Errorf("frame %d's file %s isn't a PNG named by its SHA-256", f.Index, f.Path)
	}
}
