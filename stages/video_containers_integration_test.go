//go:build linux && integration

package stages_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

var containerCases = []struct {
	name  string
	args  []string
	pack  byte
	kind  route.Kind
	route route.Route
}{
	{"mpeg1.mpg", []string{"-r", "25", "-c:v", "mpeg1video", "-q:v", "4", "-c:a", "mp2", "-ar", "44100", "-f", "mpeg"}, 0x20, route.MPEGPS, route.Video},
	{"mpeg2.vob", []string{"-r", "25", "-c:v", "mpeg2video", "-q:v", "4", "-c:a", "mp2", "-ar", "48000", "-f", "vob"}, 0x40, route.MPEGPS, route.Video},
	{"theora.ogv", []string{"-c:v", "libtheora", "-q:v", "6", "-c:a", "libvorbis"}, 0, route.OggVideo, route.Video},
	{"vorbis.ogg", []string{"-vn", "-c:a", "libvorbis"}, 0, route.Ogg, route.Audio},
}

func packVersion(t *testing.T, path string) byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 5 || !bytes.HasPrefix(b, []byte{0, 0, 1, 0xba}) {
		return 0
	}
	if b[4]&0xc0 == 0x40 {
		return 0x40
	}
	return b[4] & 0xf0
}

func transcode(t *testing.T, ffmpeg *tool.Tool, src, name string, codec []string) string {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source.mkv"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", "source.mkv"}, codec...)
	if err := ffmpeg.Run(t.Context(), tool.Invocation{Args: append(args, name), Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "source.mkv")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func TestVideoContainersReal(t *testing.T) {
	src, err := filepath.Abs(scenesFixture)
	if err != nil {
		t.Fatal(err)
	}
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
			streamer, err := media.NewStreamer(media.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
			if err != nil {
				t.Fatal(err)
			}
			splitter, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
			if err != nil {
				t.Fatal(err)
			}
			version := stages.Versions(ffmpeg, ffprobe)
			d := newDispatcher(t, dispatch.Stages{
				route.Video: stages.NewVideoStage(stages.VideoConfig{Streamer: streamer, Version: version}),
				route.Audio: stages.NewAudioStage(stages.AudioConfig{Splitter: splitter, Version: version, Request: audio.Request{Concurrency: 2}}),
			}, dispatch.NewMemoryIndex(0))
			for _, c := range containerCases {
				t.Run(c.name, func(t *testing.T) {
					path := transcode(t, ffmpeg, src, c.name, c.args)
					if got := packVersion(t, path); got != c.pack {
						t.Fatalf("pack header %#x, want %#x", got, c.pack)
					}
					events := dispatchAll(t, d, path, func(ev *dispatch.Event) {
						switch o := ev.Output; {
						case o == nil:
						case o.Kind == dispatch.ImageOutput:
							wantPNG(t, *o)
						case o.Kind == dispatch.AudioOutput:
							checkWAV(t, *o)
						}
					})
					wantSteps(t, events, []step{{c.name, 0, dispatch.Prepared}})
					ct := events[0].Part.ContentType
					if ct.Kind != c.kind || ct.Route != c.route {
						t.Errorf("recorded kind %q, route %s; want %q, %s", ct.Kind, ct.Route, c.kind, c.route)
					}
					var frames, windows int
					for _, o := range events[0].Outputs {
						switch o.Kind {
						case dispatch.ImageOutput:
							frames++
						case dispatch.AudioOutput:
							windows++
						}
					}
					t.Logf("%s: kind %s, route %s, %d frames, %d windows", c.name, ct.Kind, ct.Route, frames, windows)
					if windows == 0 {
						t.Error("no audio windows")
					}
					if (c.route == route.Video) != (frames > 0) {
						t.Errorf("%d frames on the %s route", frames, c.route)
					}
				})
			}
		})
	}
}
