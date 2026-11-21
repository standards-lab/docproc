//go:build integration && linux

package audio_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/tool"
)

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
