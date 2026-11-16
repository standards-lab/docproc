//go:build linux

package audio_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"ffmpeg": standin, "ffprobe": standin})
}

// settings are the stand-ins' settings, which ffmpeg's and ffprobe's links share.
type settings struct {
	// Version is the version either stand-in's -version prints, after an n.
	Version string `json:"version"`
	// Probe is the report the stand-in ffprobe writes to stdout.
	Probe string `json:"probe,omitzero"`
	// Volume and Silence are what the stand-in ffmpeg writes to stderr for a run of volumedetect
	// and of silencedetect.
	Volume  string `json:"volume,omitzero"`
	Silence string `json:"silence,omitzero"`
	// Fail, when set, makes an ffmpeg run write it to stderr and exit 1.
	Fail string `json:"fail,omitzero"`
	// FailWindow, when set, makes the window run whose first sample it names write a line to
	// stderr and exit 1.
	FailWindow string `json:"fail_window,omitzero"`
	// Delays is how long, in milliseconds, a window run sleeps before writing its samples, by its
	// first sample, "*" for the rest; none sleeps without it.
	Delays map[string]int `json:"delays,omitzero"`
	// Decode names a canonical 44-byte WAV whose samples a decoding run, the tracker's, writes to
	// stdout, in chunks of uneven sizes.
	Decode string `json:"decode,omitzero"`
	// Flight, when set, names a directory each window run marks itself in, a file named by its
	// process ID, while it sleeps; halfway through, it appends the number of runs marked there to
	// the directory's counts file. A run that is killed leaves its mark.
	Flight string `json:"flight,omitzero"`
}

// run is one stand-in run, as the links' logs record it.
type run = tooltest.Logged

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
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	filter := args[slices.Index(args, "-af")+1]
	if slices.Contains(args, "s16le") {
		if filter == "aresample=16000" {
			return standinDecode(s)
		}
		return standinWindow(s, filter)
	}
	switch {
	case filter == "volumedetect":
		fmt.Fprint(os.Stderr, s.Volume)
	case strings.HasPrefix(filter, "silencedetect="):
		fmt.Fprint(os.Stderr, s.Silence)
	}
	return 0
}

// standinDecode is the stand-in ffmpeg's decoding run: it writes the samples of the WAV s.Decode
// names to stdout, in chunks of 1 to 7,919 bytes, so a sample is split across writes.
func standinDecode(s settings) int {
	data, err := os.ReadFile(s.Decode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	pcm := data[44:]
	for n := 1; len(pcm) > 0; n = n*31%7919 + 1 {
		k := min(n, len(pcm))
		if _, err := os.Stdout.Write(pcm[:k]); err != nil {
			return 2
		}
		pcm = pcm[k:]
	}
	return 0
}

// windowFilter matches a window run's filter, its first sample and the sample it ends before.
var windowFilter = regexp.MustCompile(`^aresample=16000,atrim=start_sample=(\d+):end_sample=(\d+)$`)

// standinWindow is the stand-in ffmpeg's window run on filter: it sleeps as s.Delays says, marked
// in s.Flight's directory, and writes the window's samples, windowSample's, to stdout as 16-bit
// little-endian PCM.
func standinWindow(s settings, filter string) int {
	m := windowFilter.FindStringSubmatch(filter)
	if m == nil {
		fmt.Fprintf(os.Stderr, "stand-in: window filter %q\n", filter)
		return 2
	}
	first, _ := strconv.Atoi(m[1])
	end, _ := strconv.Atoi(m[2])
	if s.FailWindow == m[1] {
		fmt.Fprint(os.Stderr, "[aist#0:0/pcm_s16le] Error while filtering: Cannot allocate memory\n")
		return 1
	}
	if err := sleepMarked(s, m[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	pcm := make([]byte, 0, 2*(end-first))
	for i := first; i < end; i++ {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(windowSample(i)))
	}
	if _, err := os.Stdout.Write(pcm); err != nil {
		return 2
	}
	return 0
}

// windowSample is the stand-in's sample i of the source: it names its own place, so a window's
// samples show which window they are.
func windowSample(i int) int16 { return int16(i % 30011) }

// sleepMarked sleeps as s.Delays says for the window run whose first sample is first, marked in
// s.Flight's directory when that is set.
func sleepMarked(s settings, first string) error {
	ms, ok := s.Delays[first]
	if !ok {
		ms = s.Delays["*"]
	}
	d := time.Duration(ms) * time.Millisecond
	dir := s.Flight
	if dir == "" {
		time.Sleep(d)
		return nil
	}
	mark := filepath.Join(dir, strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(mark, nil, 0o600); err != nil {
		return err
	}
	time.Sleep(d / 2)
	marks, err := filepath.Glob(filepath.Join(dir, "[0-9]*"))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "counts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(f, len(marks))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	time.Sleep(d - d/2)
	return os.Remove(mark)
}

// standins are a test's stand-in ffmpeg and ffprobe: two links in a directory of their own,
// sharing one settings.
type standins struct {
	t        testing.TB
	settings settings
	// ffmpeg and ffprobe are the links' paths.
	ffmpeg, ffprobe string
	// since is how many runs of each link a mark left out of runs.
	since map[string]int
}

// installStandins links the stand-ins as ffmpeg and ffprobe in a directory of their own, at
// version.
func installStandins(t testing.TB, version string) *standins {
	t.Helper()
	dir := t.TempDir()
	s := &standins{t: t, settings: settings{Version: version}, since: map[string]int{}}
	s.ffmpeg = tooltest.Link(t, dir, "ffmpeg", s.settings)
	s.ffprobe = tooltest.Link(t, dir, "ffprobe", s.settings)
	return s
}

// set changes the stand-ins' settings through change, for every run after it.
func (s *standins) set(change func(*settings)) {
	s.t.Helper()
	change(&s.settings)
	tooltest.Set(s.t, s.ffmpeg, s.settings)
	tooltest.Set(s.t, s.ffprobe, s.settings)
}

// mark leaves the runs so far out of runs.
func (s *standins) mark() {
	s.t.Helper()
	for _, link := range []string{s.ffprobe, s.ffmpeg} {
		s.since[link] = len(tooltest.Runs(s.t, link))
	}
}

// runs returns the stand-ins' runs since the last mark, version commands left out: ffprobe's,
// then ffmpeg's, each in the order they began. The package probes a source before ffmpeg runs on
// it, so that is the order they ran in; the logs, one a link, don't order runs across the two.
func (s *standins) runs() []run {
	s.t.Helper()
	var out []run
	for _, link := range []string{s.ffprobe, s.ffmpeg} {
		for _, r := range tooltest.Runs(s.t, link)[s.since[link]:] {
			if !slices.Equal(r.Args, []string{"-version"}) {
				out = append(out, r)
			}
		}
	}
	return out
}

// spec returns spec with its program the stand-in's link of the same name.
func (s *standins) spec(spec tool.Spec) tool.Spec {
	spec.Program = filepath.Join(filepath.Dir(s.ffmpeg), spec.Program)
	return spec
}

// tools builds the stand-ins through the exec runner, each Spec's minimum lowered to 1 so any
// version passes the runner, leaving the Splitter's own minimum to decide.
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

// standinSplitter installs the stand-ins at 9.0.2 and builds a Splitter over them.
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
