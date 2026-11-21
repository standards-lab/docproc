//go:build linux

package media_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/internal/mediatest"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/tool"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"ffmpeg": standin, "ffprobe": standin})
}

type settings struct {
	Version string            `json:"version"`
	Probe   string            `json:"probe,omitzero"`
	PCM     string            `json:"pcm,omitzero"`
	Frames  []mediatest.Frame `json:"frames,omitzero"`
	Gates   []standinGate     `json:"gates,omitzero"`
	NoPNG   bool              `json:"no_png,omitzero"`
	Fail    string            `json:"fail,omitzero"`
	Hold    int               `json:"hold,omitzero"`
}

type run = tooltest.Logged

type standinGate struct {
	At   int    `json:"at"`
	Path string `json:"path"`
}

const gateWait = 20 * time.Second

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
	if err := standinRun(s, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	time.Sleep(time.Duration(s.Hold) * time.Millisecond)
	if s.Fail != "" {
		fmt.Fprintln(os.Stderr, s.Fail)
		return 1
	}
	return 0
}

func standinRun(s settings, args []string) error {
	var pcm []byte
	gates := s.Gates
	if slices.Contains(args, "pipe:1") {
		data, err := os.ReadFile(s.PCM)
		if err != nil {
			return err
		}
		pcm = data[44:]
	} else {
		gates = nil
	}
	var frames []mediatest.Frame
	pattern := ""
	if slices.Contains(args, "image2") {
		frames = s.Frames
		pattern = args[len(args)-1]
	}
	return mediatest.Stream(pcm, frames,
		func(at, end int) error {
			var err error
			gates, err = writeGated(pcm, at, end, gates)
			return err
		},
		func(i int) error {
			mediatest.Report(i, frames[i])
			if s.NoPNG {
				return nil
			}
			return mediatest.WritePNG(pattern, i, frames[i])
		})
}

func writeGated(pcm []byte, at, end int, gates []standinGate) ([]standinGate, error) {
	for len(gates) > 0 && gates[0].At <= end {
		g := gates[0]
		if _, err := os.Stdout.Write(pcm[at:g.At]); err != nil {
			return nil, err
		}
		at = g.At
		for deadline := time.Now().Add(gateWait); ; time.Sleep(5 * time.Millisecond) {
			if _, err := os.Stat(g.Path); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("the gate at byte %d, %s, was never opened", g.At, g.Path)
			}
		}
		gates = gates[1:]
	}
	_, err := os.Stdout.Write(pcm[at:end])
	return gates, err
}

type standins struct {
	t        testing.TB
	settings settings
	dir      string
	since    map[string]int
}

var links = []string{"ffprobe", "ffmpeg"}

func installStandins(t testing.TB, version string) *standins {
	t.Helper()
	s := &standins{t: t, settings: settings{Version: version}, dir: t.TempDir(), since: map[string]int{}}
	for _, name := range links {
		tooltest.Link(t, s.dir, name, s.settings)
	}
	return s
}

func (s *standins) link(name string) string { return filepath.Join(s.dir, name) }

func (s *standins) set(change func(*settings)) {
	s.t.Helper()
	change(&s.settings)
	for _, name := range links {
		tooltest.Set(s.t, s.link(name), s.settings)
	}
}

func (s *standins) mark() {
	s.t.Helper()
	for _, name := range links {
		s.since[name] = len(tooltest.Runs(s.t, s.link(name)))
	}
}

func (s *standins) runs() []run {
	s.t.Helper()
	var out []run
	for _, name := range links {
		for _, r := range tooltest.Runs(s.t, s.link(name))[s.since[name]:] {
			if !slices.Equal(r.Args, []string{"-version"}) {
				out = append(out, r)
			}
		}
	}
	return out
}

func (s *standins) config() media.Config {
	s.t.Helper()
	build := func(spec tool.Spec) *tool.Tool {
		spec.Program = s.link(spec.Program)
		spec.Minimum = "1"
		spec.KillAfter = time.Second
		tl, err := tool.NewExec(s.t.Context(), spec)
		if err != nil {
			s.t.Fatal(err)
		}
		return tl
	}
	return media.Config{FFmpeg: build(audio.FFmpegSpec()), FFprobe: build(audio.FFprobeSpec())}
}

func standinStreamer(t testing.TB) (*media.Streamer, *standins) {
	t.Helper()
	st := installStandins(t, "9.0.2")
	s, err := media.NewStreamer(st.config())
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}
