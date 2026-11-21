//go:build linux

package audio_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"ffmpeg": standin, "ffprobe": standin})
}

type settings struct {
	Version   string `json:"version"`
	Probe     string `json:"probe,omitzero"`
	Fail      string `json:"fail,omitzero"`
	FailAfter int    `json:"fail_after,omitzero"`
	Decode    string `json:"decode,omitzero"`
	Synth     int    `json:"synth,omitzero"`
	PaceMS    int    `json:"pace_ms,omitzero"`
}

type run = tooltest.Logged

func standin(r tooltest.Run) int {
	var s settings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 1 && args[0] == "-version" {
		fmt.Printf("%s version n%s Copyright (c) 2000-2026 the FFmpeg developers\n", r.Program, s.Version)
		return 0
	}
	if r.Program == "ffprobe" {
		fmt.Print(s.Probe)
		return 0
	}
	if filter := args[slices.Index(args, "-af")+1]; filter != "aresample=16000" || !slices.Contains(args, "s16le") {
		fmt.Fprintf(os.Stderr, "stand-in: not a decoding run: %q\n", args)
		return 2
	}
	return standinDecode(s)
}

func standinDecode(s settings) int {
	var pcm []byte
	chunk := func(n int) int { return n*31%7919 + 1 }
	switch {
	case s.Decode != "":
		data, err := os.ReadFile(s.Decode)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		pcm = data[44:]
	default:
		for i := range s.Synth {
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(synthSample(i)))
		}
		chunk = func(int) int { return audio.SampleRate / 10 * 2 }
	}
	if s.Fail != "" {
		pcm = pcm[:min(len(pcm), s.FailAfter)]
	}
	for n := 1; len(pcm) > 0; n = chunk(n) {
		time.Sleep(time.Duration(s.PaceMS) * time.Millisecond)
		k := min(n, len(pcm))
		if _, err := os.Stdout.Write(pcm[:k]); err != nil {
			return 2
		}
		pcm = pcm[k:]
	}
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	return 0
}

func synthSample(i int) int16 { return int16(i%30011 - 15005) }

type standins struct {
	t               testing.TB
	settings        settings
	ffmpeg, ffprobe string
}

func installStandins(t testing.TB, version string) *standins {
	t.Helper()
	dir := t.TempDir()
	s := &standins{t: t, settings: settings{Version: version}}
	s.ffmpeg = tooltest.Link(t, dir, "ffmpeg", s.settings)
	s.ffprobe = tooltest.Link(t, dir, "ffprobe", s.settings)
	return s
}

func (s *standins) set(change func(*settings)) {
	s.t.Helper()
	change(&s.settings)
	tooltest.Set(s.t, s.ffmpeg, s.settings)
	tooltest.Set(s.t, s.ffprobe, s.settings)
}

func (s *standins) runs() []run {
	s.t.Helper()
	var out []run
	for _, link := range []string{s.ffprobe, s.ffmpeg} {
		for _, r := range tooltest.Runs(s.t, link) {
			if !slices.Equal(r.Args, []string{"-version"}) {
				out = append(out, r)
			}
		}
	}
	return out
}

func (s *standins) spec(spec tool.Spec) tool.Spec {
	spec.Program = filepath.Join(filepath.Dir(s.ffmpeg), spec.Program)
	return spec
}

func (s *standins) tools() (ffmpeg, ffprobe *tool.Tool) {
	s.t.Helper()
	build := func(spec tool.Spec) *tool.Tool {
		spec = s.spec(spec)
		spec.Minimum = "1"
		tl, err := tool.NewExec(s.t.Context(), spec)
		if err != nil {
			s.t.Fatal(err)
		}
		return tl
	}
	return build(audio.FFmpegSpec()), build(audio.FFprobeSpec())
}

func standinSplitter(t testing.TB) (*audio.Splitter, *standins) {
	t.Helper()
	st := installStandins(t, "9.0.2")
	ffmpeg, ffprobe := st.tools()
	s, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}
