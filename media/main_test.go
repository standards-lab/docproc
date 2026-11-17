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

// settings are the stand-ins' settings, which ffmpeg's and ffprobe's links share.
type settings struct {
	// Version is the version ffmpeg's and ffprobe's -version print, after an n.
	Version string `json:"version"`
	// Probe is the report the stand-in ffprobe writes to stdout.
	Probe string `json:"probe,omitzero"`
	// PCM names a canonical 44-byte WAV whose samples the stand-in ffmpeg writes to its stdout,
	// when its arguments have an audio output.
	PCM string `json:"pcm,omitzero"`
	// Frames are the frames the stand-in ffmpeg takes, when its arguments have a video output.
	Frames []mediatest.Frame `json:"frames,omitzero"`
	// Gates, when set, are where the stand-in ffmpeg waits in its PCM.
	Gates []standinGate `json:"gates,omitzero"`
	// NoPNG makes the stand-in report its frames and write no PNG for them.
	NoPNG bool `json:"no_png,omitzero"`
	// Fail, when set, makes the stand-in ffmpeg, once it has written its output, write it and a
	// newline to stderr and exit 1.
	Fail string `json:"fail,omitzero"`
	// Hold is how many milliseconds the stand-in ffmpeg sleeps once it has written its output.
	Hold int `json:"hold,omitzero"`
}

// run is one stand-in run, as the links' logs record it.
type run = tooltest.Logged

// standinGate is a point in the stand-in ffmpeg's PCM it waits at: once it has written At bytes,
// it writes no more until a file exists at Path, and fails if none does within gateWait.
type standinGate struct {
	At   int    `json:"at"`
	Path string `json:"path"`
}

// gateWait is how long the stand-in ffmpeg waits at a gate before it fails: long enough for any
// window the PCM written has fixed to reach the test, under the race detector.
const gateWait = 20 * time.Second

// standin is the stand-in run as ffmpeg or ffprobe.
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

// standinRun is the stand-in ffmpeg's run: the PCM of s.PCM's WAV to stdout, when its arguments
// have an audio output, half a second at a time, waiting at s.Gates; and s.Frames, when they have
// a video output, its last argument the image2 pattern, each reported and written before the PCM
// from its time on.
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

// writeGated writes pcm's bytes from at to end to stdout, waiting at each of the gates, in order,
// whose byte is in that range, and returns the gates left.
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

// standins are a test's stand-in ffmpeg and ffprobe: two links in a directory of their own,
// sharing one settings.
type standins struct {
	t        testing.TB
	settings settings
	// dir is the links' directory.
	dir string
	// since is how many runs of each link a mark left out of runs.
	since map[string]int
}

// links names the stand-ins' links, in the order runs lists their runs.
var links = []string{"ffprobe", "ffmpeg"}

// installStandins links the stand-ins as ffmpeg and ffprobe in a directory of their own, at
// version.
func installStandins(t testing.TB, version string) *standins {
	t.Helper()
	s := &standins{t: t, settings: settings{Version: version}, dir: t.TempDir(), since: map[string]int{}}
	for _, name := range links {
		tooltest.Link(t, s.dir, name, s.settings)
	}
	return s
}

// link returns the path of the stand-in named name.
func (s *standins) link(name string) string { return filepath.Join(s.dir, name) }

// set changes the stand-ins' settings through change, for every run after it.
func (s *standins) set(change func(*settings)) {
	s.t.Helper()
	change(&s.settings)
	for _, name := range links {
		tooltest.Set(s.t, s.link(name), s.settings)
	}
}

// mark leaves the runs so far out of runs.
func (s *standins) mark() {
	s.t.Helper()
	for _, name := range links {
		s.since[name] = len(tooltest.Runs(s.t, s.link(name)))
	}
}

// runs returns the stand-ins' runs since the last mark: ffprobe's, then ffmpeg's, each in the
// order they began, and no version command. A probe precedes ffmpeg's run, so that is the order
// they ran in; the logs, one a link, don't order runs across links.
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

// config builds the stand-ins through the exec runner, each Spec's minimum lowered to 1 so any
// version passes the runner, leaving the Streamer's own minimum to decide.
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

// standinStreamer installs the stand-ins at 9.0.2 and builds a Streamer over them.
func standinStreamer(t testing.TB) (*media.Streamer, *standins) {
	t.Helper()
	st := installStandins(t, "9.0.2")
	s, err := media.NewStreamer(st.config())
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}
