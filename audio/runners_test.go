//go:build integration && linux

package audio_test

import (
	"testing"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/tool"
)

var speechRunners = map[string]func(t *testing.T) *audio.Splitter{
	"exec": func(t *testing.T) *audio.Splitter {
		return splitter(t, func(spec tool.Spec) (*tool.Tool, error) { return tool.NewExec(t.Context(), spec) })
	},
	"container": func(t *testing.T) *audio.Splitter {
		return splitter(t, func(spec tool.Spec) (*tool.Tool, error) {
			return tool.NewContainer(t.Context(), "", tool.Image{Ref: audio.FFmpegImage}, spec)
		})
	},
}

func splitter(t *testing.T, build func(tool.Spec) (*tool.Tool, error)) *audio.Splitter {
	t.Helper()
	ffmpeg, err := build(audio.FFmpegSpec())
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := build(audio.FFprobeSpec())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %s, %s %s", ffmpeg.Name(), ffmpeg.Version(), ffprobe.Name(), ffprobe.Version())
	s, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
