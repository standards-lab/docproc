//go:build linux && integration

package stages_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

var (
	scenesFixture = filepath.Join("..", "audio", "testdata", "speech-scenes.mkv")
	scenesTruth   = filepath.Join("..", "audio", "testdata", "speech-scenes.truth.json")
)

func scenesFrames(t *testing.T) []struct {
	Reason string `json:"reason"`
	TimeMS int64  `json:"time_ms"`
} {
	t.Helper()
	b, err := os.ReadFile(scenesTruth)
	if err != nil {
		t.Fatal(err)
	}
	var truth struct {
		Video struct {
			Frames []struct {
				Reason string `json:"reason"`
				TimeMS int64  `json:"time_ms"`
			} `json:"frames"`
		} `json:"video"`
	}
	if err := json.Unmarshal(b, &truth); err != nil {
		t.Fatal(err)
	}
	return truth.Video.Frames
}

func sumOf(o dispatch.Output) string {
	sum, _, _ := strings.Cut(filepath.Base(o.Path), ".")
	return sum
}

func streamed(t *testing.T, s *media.Streamer, path string) (frames, windows []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.WriteFile(src, b, 0o600); err != nil {
		t.Fatal(err)
	}
	for ev, err := range s.Events(t.Context(), src, media.Request{Dir: t.TempDir()}) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Frame != nil {
			frames = append(frames, ev.Frame.SHA256)
		}
		if ev.Window != nil {
			windows = append(windows, ev.Window.SHA256)
		}
	}
	return frames, windows
}

func stripAudio(t *testing.T, ffmpeg *tool.Tool, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "with-audio.mkv"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	err = ffmpeg.Run(t.Context(), tool.Invocation{
		Args: []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", "with-audio.mkv", "-map", "0:v", "-c", "copy", "video-only.mkv"},
		Dir:  dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "with-audio.mkv")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "video-only.mkv")
}

func TestVideoStageReal(t *testing.T) {
	truth := scenesFrames(t)
	path, err := filepath.Abs(scenesFixture)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	sums := map[string][]string{}
	for name, build := range audioRunners {
		t.Run(name, func(t *testing.T) {
			ffmpeg, err := build(t, audio.FFmpegSpec())
			if err != nil {
				t.Fatal(err)
			}
			ffprobe, err := build(t, audio.FFprobeSpec())
			if err != nil {
				t.Fatal(err)
			}
			s, err := media.NewStreamer(media.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
			if err != nil {
				t.Fatal(err)
			}
			stage := stages.NewVideoStage(stages.VideoConfig{Streamer: s, Version: stages.Versions(ffmpeg, ffprobe)})
			wantFrames, wantWindows := streamed(t, s, path)

			d := newDispatcher(t, dispatch.Stages{route.Video: stage}, dispatch.NewMemoryIndex(0))
			events := dispatchAll(t, d, path, func(ev *dispatch.Event) {
				o := ev.Output
				switch {
				case o == nil:
				case o.Kind == dispatch.ImageOutput:
					wantPNG(t, *o)
				case o.Kind == dispatch.AudioOutput:
					checkWAV(t, *o)
				}
			})
			wantSteps(t, events, []step{{"speech-scenes.mkv", 0, dispatch.Prepared}})
			outs := events[0].Outputs
			if len(outs) == 0 || outs[0].Name != "plan" {
				t.Fatalf("outputs %q, want the plan first", kinds(outs))
			}
			t.Logf("recorded: plan %s", outs[0].Value)
			var frames, windows []string
			for _, o := range outs[1:] {
				t.Logf("recorded: %s %s, %s, Meta %v", o.Kind, o.Name, filepath.Base(o.Path), o.Meta)
				switch o.Kind {
				case dispatch.ImageOutput:
					i := len(frames)
					if i < len(truth) {
						want := truth[i]
						if o.Name != "frame "+strconv.Itoa(i) || o.Meta["reason"] != want.Reason ||
							o.Meta["time"] != secsOf(time.Duration(want.TimeMS)*time.Millisecond) {
							t.Errorf("%s: %s at %s; want frame %d, %s at %d ms", o.Name, o.Meta["reason"], o.Meta["time"], i, want.Reason, want.TimeMS)
						}
					}
					frames = append(frames, sumOf(o))
				case dispatch.AudioOutput:
					if o.Name != "window "+strconv.Itoa(len(windows)) {
						t.Errorf("%s, want window %d", o.Name, len(windows))
					}
					windows = append(windows, sumOf(o))
				}
			}
			if len(frames) != len(truth) {
				t.Errorf("%d frames, want the truth's %d", len(frames), len(truth))
			}
			if !slices.Equal(frames, wantFrames) || !slices.Equal(windows, wantWindows) {
				t.Errorf("frames %q and windows %q; media's %q and %q", frames, windows, wantFrames, wantWindows)
			}
			sums[name] = append(frames, windows...)

			only := stripAudio(t, ffmpeg, path)
			events = dispatchAll(t, d, only, nil)
			wantSteps(t, events, []step{{"video-only.mkv", 0, dispatch.Prepared}})
			outs = events[0].Outputs
			var onlyFrames []string
			for _, o := range outs[1:] {
				if o.Kind != dispatch.ImageOutput {
					t.Errorf("the video-only copy yielded %s %s", o.Kind, o.Name)
				}
				onlyFrames = append(onlyFrames, sumOf(o))
			}
			if outs[0].Name != "plan" || !slices.Equal(onlyFrames, frames) {
				t.Errorf("the video-only copy's outputs %q, frames %q; want the plan and frames %q", kinds(outs), onlyFrames, frames)
			}
			if entries, err := os.ReadDir(filepath.Dir(only)); err != nil || len(entries) != 1 {
				t.Errorf("the video-only copy's directory holds %v, %v", entries, err)
			}
		})
	}
	if e, c := sums["exec"], sums["container"]; len(sums) == 2 && !slices.Equal(e, c) {
		t.Errorf("the exec runner prepared %q, the container runner %q", e, c)
	}
	after, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("the input's directory held %d entries, now %d: the run wrote beside the input", len(before), len(after))
	}
}

func secsOf(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) }
