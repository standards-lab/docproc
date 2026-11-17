//go:build integration && linux

package audio_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/tool"
)

// TestImageCapabilities checks that FFmpegImage's ffmpeg has what media asks of it: the filters
// scdet, select, and showinfo, listed and run together over a lavfi test source, and the filters
// of media's run, settb, metadata, scale, format, and aresample beside select, listed and the
// frames' chain run as media runs it. The protocols and devices a live source needs are the live
// sources' own, which stay in spike-ai-pipeline-processing at v0.9.0.
func TestImageCapabilities(t *testing.T) {
	ffmpeg, err := tool.NewContainer(t.Context(), "", tool.Image{Ref: audio.FFmpegImage}, audio.FFmpegSpec())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %s in %s", ffmpeg.Name(), ffmpeg.Version(), audio.FFmpegImage)
	list := func(t *testing.T, flag string) string {
		t.Helper()
		var out bytes.Buffer
		err := ffmpeg.Run(t.Context(), tool.Invocation{Args: []string{"-nostdin", "-hide_banner", flag}, Stdout: &out})
		if err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	t.Run("filters", func(t *testing.T) {
		// A line of -filters is its flags, the filter's name, its pads, and its description.
		have := map[string]bool{}
		for line := range strings.Lines(list(t, "-filters")) {
			if f := strings.Fields(line); len(f) > 1 {
				have[f[1]] = true
			}
		}
		for _, name := range []string{"scdet", "select", "showinfo", "settb", "metadata", "scale", "format", "aresample"} {
			if !have[name] {
				t.Errorf("the image's ffmpeg has no %s filter", name)
			}
		}
		// Ten frames of a test source through the three filters: showinfo logs each frame select
		// passes, and select passes every frame whose scene score is at least zero.
		var stderr bytes.Buffer
		err := ffmpeg.Run(t.Context(), tool.Invocation{
			Args: []string{
				"-nostdin", "-hide_banner", "-f", "lavfi", "-i", "testsrc=duration=1:rate=10:size=160x120",
				"-vf", "scdet=threshold=1,select='gte(scene,0)',showinfo", "-f", "null", "-",
			},
			Stderr: &stderr,
		})
		if err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
		if n := strings.Count(stderr.String(), " pts_time:"); n != 10 {
			t.Errorf("showinfo logged %d frames, want 10", n)
		}
		// The same ten frames through media's chain of frame filters, scaled to fit 80 pixels:
		// metadata reports each frame select passes.
		stderr.Reset()
		err = ffmpeg.Run(t.Context(), tool.Invocation{
			Args: []string{
				"-nostdin", "-hide_banner", "-f", "lavfi", "-i", "testsrc=duration=1:rate=10:size=160x120",
				"-vf", "select='gte(scene,0)',settb=AVTB,metadata=mode=print:key=lavfi.scene_score," +
					"scale=w='min(iw,80)':h='min(ih,80)':force_original_aspect_ratio=decrease:" +
					"flags=bicubic+accurate_rnd+full_chroma_int+bitexact,format=rgb24",
				"-f", "null", "-",
			},
			Stderr: &stderr,
		})
		if err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
		if n := strings.Count(stderr.String(), "lavfi.scene_score="); n != 10 {
			t.Errorf("metadata reported %d frames, want 10", n)
		}
	})
}
