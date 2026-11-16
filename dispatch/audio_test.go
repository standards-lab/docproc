//go:build linux

package dispatch_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/mediatest"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// The audio stage's hermetic tests run its Splitter over stand-ins, internal/tooltest's links named
// ffmpeg and ffprobe, acting on their arguments as FFmpeg's tools would, as the audio package's own
// tests do; the video stage's run media's Streamer over the same stand-ins.

// ffSettings are the stand-ins' settings, which both links share.
type ffSettings struct {
	// Probe is the report ffprobe writes to stdout.
	Probe string `json:"probe"`
	// FailWindow, when set, makes the window run whose first sample it names write a line to
	// stderr and exit 1.
	FailWindow string `json:"fail_window,omitzero"`
	// PCMMS is how many milliseconds of PCM media's run writes to its stdout when it has an audio
	// output, each sample its index modulo 30011.
	PCMMS int `json:"pcm_ms,omitzero"`
	// Frames are the frames media's run takes when it has a video output.
	Frames []mediatest.Frame `json:"frames,omitzero"`
	// Fail, when set, makes media's run, once it has written its output, write it to stderr and
	// exit 1.
	Fail string `json:"fail,omitzero"`
	// HoldMS, when set, is how many milliseconds media's run sleeps once it has written its first
	// frame.
	HoldMS int `json:"hold_ms,omitzero"`
}

// ffStandins are one test's ffmpeg and ffprobe links and the settings they share.
type ffStandins struct {
	ffmpeg, ffprobe string
	settings        ffSettings
}

// set changes the stand-ins' settings through change.
func (s *ffStandins) set(t *testing.T, change func(*ffSettings)) {
	t.Helper()
	change(&s.settings)
	tooltest.Set(t, s.ffmpeg, s.settings)
	tooltest.Set(t, s.ffprobe, s.settings)
}

// runs returns the programs the stand-ins ran as, ffprobe's runs before ffmpeg's, version
// commands aside.
func (s *ffStandins) runs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, link := range []string{s.ffprobe, s.ffmpeg} {
		for _, r := range tooltest.Runs(t, link) {
			if !slices.Equal(r.Args, []string{"-version"}) {
				out = append(out, r.Program)
			}
		}
	}
	return out
}

// The stand-in's reports: ffprobe's of a 49.82 s FLAC whose audio is its second stream, after a
// cover picture, and volumedetect's and silencedetect's of it, which plan two windows cut in the
// silence at 24.2 s.
const (
	audioProbe = `{"streams":[` +
		`{"index":0,"codec_name":"mjpeg","codec_type":"video"},` +
		`{"index":1,"codec_name":"flac","codec_type":"audio","sample_rate":"16000","channels":1,"duration":"49.820000"}],` +
		`"format":{"duration":"49.820000"}}`
	audioVolume   = "[Parsed_volumedetect_0 @ 0x7f9254003c80] mean_volume: -25.1 dB\n"
	audioSilences = `[Parsed_silencedetect_0 @ 0x7f6e94003d40] silence_start: 24.22575
[Parsed_silencedetect_0 @ 0x7f6e94003d40] silence_end: 25.732625 | silence_duration: 1.506875
`
	// audioNoStream is ffprobe's report of an M4A container holding no audio stream.
	audioNoStream = `{"streams":[{"index":0,"codec_name":"h264","codec_type":"video"}],"format":{"duration":"3.000000"}}`
)

// The inputs the audio tests dispatch, by their signatures: a FLAC, an M4A, and an MP4, each
// routed as its kind and read by none but the stand-ins, which don't read them.
const (
	flacBytes = "fLaC\x00\x00\x00\x22" + "stand-in FLAC"
	m4aBytes  = "\x00\x00\x00\x14ftypM4A \x00\x00\x00\x00M4A \x00\x00\x00\x08moov"
	mp4Bytes  = "\x00\x00\x00\x14ftypisom\x00\x00\x02\x00isom\x00\x00\x00\x08moov"
)

// audioWindowFilter matches a window run's filter, its first sample and the sample it ends before.
var audioWindowFilter = regexp.MustCompile(`^aresample=16000,atrim=start_sample=(\d+):end_sample=(\d+)$`)

// ffprobeStandin is the stand-in ffprobe: it prints its version at 9.0.2, or its settings' report.
func ffprobeStandin(r tooltest.Run) int {
	if printFFVersion(r) {
		return 0
	}
	var settings ffSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Print(settings.Probe)
	return 0
}

// printFFVersion prints the stand-in's version, 9.0.2, for a version command, and reports whether
// the run was one.
func printFFVersion(r tooltest.Run) bool {
	if !slices.Equal(r.Args, []string{"-version"}) {
		return false
	}
	fmt.Printf("%s version n9.0.2 Copyright (c) 2000-2026 the FFmpeg developers\n", r.Program)
	return true
}

// ffmpegStandin is the stand-in ffmpeg: it prints its version at 9.0.2, volumedetect's and
// silencedetect's reports, a window's samples, each sample its index in the source modulo 30011,
// or, for media's run, its PCM and frames, as mediaStandin does.
func ffmpegStandin(r tooltest.Run) int {
	if printFFVersion(r) {
		return 0
	}
	var settings ffSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if slices.Contains(args, "-protocol_whitelist") {
		// Only media's run allows its input's protocols.
		return mediaStandin(args, settings)
	}
	filter := args[slices.Index(args, "-af")+1]
	switch {
	case filter == "volumedetect":
		fmt.Fprint(os.Stderr, audioVolume)
		return 0
	case strings.HasPrefix(filter, "silencedetect="):
		fmt.Fprint(os.Stderr, audioSilences)
		return 0
	}
	m := audioWindowFilter.FindStringSubmatch(filter)
	if m == nil {
		fmt.Fprintf(os.Stderr, "stand-in: filter %q\n", filter)
		return 2
	}
	if settings.FailWindow == m[1] {
		fmt.Fprint(os.Stderr, "[aist#0:0/pcm_s16le] Error while filtering: Cannot allocate memory\n")
		return 1
	}
	first, _ := strconv.Atoi(m[1])
	end, _ := strconv.Atoi(m[2])
	pcm := make([]byte, 0, 2*(end-first))
	for i := first; i < end; i++ {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(i%30011))
	}
	if _, err := os.Stdout.Write(pcm); err != nil {
		return 2
	}
	return 0
}

// standinSplitter links the stand-ins, ffprobe reporting probe, and builds a Splitter over them.
func standinSplitter(t *testing.T, probe string) (*audio.Splitter, *ffStandins) {
	t.Helper()
	ffmpeg, ffprobe, standins := standinTools(t, probe)
	s, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	return s, standins
}

// standinTools links the stand-ins as ffmpeg and ffprobe in a directory of their own, ffprobe
// reporting probe, and builds them through the exec runner.
func standinTools(t *testing.T, probe string) (ffmpeg, ffprobe *tool.Tool, standins *ffStandins) {
	t.Helper()
	dir := t.TempDir()
	standins = &ffStandins{settings: ffSettings{Probe: probe}}
	standins.ffmpeg = tooltest.Link(t, dir, "ffmpeg", standins.settings)
	standins.ffprobe = tooltest.Link(t, dir, "ffprobe", standins.settings)
	var tools []*tool.Tool
	for _, spec := range []tool.Spec{audio.FFmpegSpec(), audio.FFprobeSpec()} {
		spec.Program = filepath.Join(dir, spec.Program)
		tl, err := tool.NewExec(t.Context(), spec)
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, tl)
	}
	return tools[0], tools[1], standins
}

// audioStage returns an audio stage over the stand-ins, ffprobe reporting probe, cutting two
// windows at once, and the stand-ins.
func audioStage(t *testing.T, probe string) (*dispatch.AudioStage, *ffStandins) {
	t.Helper()
	splitter, standins := standinSplitter(t, probe)
	s, err := dispatch.NewAudioStage(dispatch.AudioConfig{
		Splitter: splitter, Version: "ffmpeg 9.0.2, ffprobe 9.0.2", Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, standins
}

// writeInput writes content to a file named name in a directory of the test's, returning its path.
func writeInput(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The stand-in source's plan: two windows, the first cut at the middle of the silence at
// 24.22575 s, the next starting 2 s before it, and their seam.
var (
	audioWindows = []dispatch.AudioWindow{
		{Index: 0, Start: 0, End: 24.9791875, Cut: "silence"},
		{Index: 1, Start: 22.9791875, End: 49.82, Cut: "end"},
	}
	audioSeams = []dispatch.AudioSpan{{Start: 22.9791875, End: 24.9791875}}
)

// samplesOf returns the samples at 16 kHz a window from start to end seconds holds, as the
// Splitter rounds each to the nearest sample.
func samplesOf(start, end float64) int {
	at := func(s float64) int { return int(s*audio.SampleRate + 0.5) }
	return at(end) - at(start)
}

// checkWAV fails the test unless the file at o.Path, read while its part is in flight, is a
// 16 kHz mono 16-bit WAV of o's samples, named by its SHA-256; it returns the file's bytes.
func checkWAV(t *testing.T, o dispatch.Output) []byte {
	t.Helper()
	b, err := os.ReadFile(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if name := hex.EncodeToString(sum[:]) + ".wav"; filepath.Base(o.Path) != name {
		t.Errorf("%s: named %s, not by its SHA-256", o.Name, filepath.Base(o.Path))
	}
	samples, _ := strconv.Atoi(o.Meta["samples"])
	if len(b) != 44+2*samples || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" ||
		binary.LittleEndian.Uint32(b[24:28]) != audio.SampleRate || binary.LittleEndian.Uint16(b[22:24]) != 1 {
		t.Fatalf("%s: %d bytes, %q, not a 16 kHz mono WAV of %d samples", o.Name, len(b), b[:min(len(b), 12)], samples)
	}
	return b
}

// checkStandinWAV fails the test unless o's WAV, as checkWAV checks it, holds the stand-in
// source's samples from o's start: each sample its index in the source modulo 30011.
func checkStandinWAV(t *testing.T, o dispatch.Output) {
	t.Helper()
	b := checkWAV(t, o)
	start, _ := strconv.ParseFloat(o.Meta["start"], 64)
	first := int(start*audio.SampleRate + 0.5)
	for i := 0; 44+2*i+2 <= len(b); i++ {
		if got := int(binary.LittleEndian.Uint16(b[44+2*i:])); got != (first+i)%30011 {
			t.Fatalf("%s: its sample %d is %d, want the source's sample %d", o.Name, i, got, first+i)
		}
	}
}

// TestAudioStage: a FLAC's plan is its first output, a record of the plan's source, silences,
// windows, and seams; each window follows in plan order, a WAV in the part's directory, its Meta
// its index, count, span, cut, samples, and seams, so a caller can transcribe and join it.
func TestAudioStage(t *testing.T) {
	t.Parallel()
	stage, _ := audioStage(t, audioProbe)
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), func(ev *dispatch.Event) {
		if o := ev.Output; o != nil && o.Kind == dispatch.AudioOutput {
			checkStandinWAV(t, *o)
		}
	})
	wantSteps(t, events, []step{{"speech.flac", 0, dispatch.Prepared}})
	ev := events[0]
	if ev.Part.ContentType.Route != route.Audio || ev.Stage != stage.Identity() || stage.Identity().Name != "audio" {
		t.Errorf("the part's route %s, stage %v", ev.Part.ContentType.Route, ev.Stage)
	}
	var got []string
	for _, o := range ev.Outputs {
		got = append(got, string(o.Kind)+" "+o.Name+" "+o.MediaType)
	}
	want := []string{"record plan application/json", "audio window 0 audio/wav", "audio window 1 audio/wav"}
	if !slices.Equal(got, want) {
		t.Fatalf("outputs %q, want %q", got, want)
	}

	plan := ev.Outputs[0]
	if plan.Meta["windows"] != "2" || plan.Meta["duration"] != "49.82" {
		t.Errorf("the plan's Meta %v", plan.Meta)
	}
	var p dispatch.AudioPlan
	if err := json.Unmarshal(plan.Value, &p); err != nil {
		t.Fatal(err)
	}
	if p.Duration != 49.82 || p.Overlap != 2 || p.Stream != 1 || p.Codec != "flac" || p.SampleRate != 16000 ||
		p.Channels != 1 || p.MeanVolume != -25.1 || p.Threshold != -40.1 || p.MinSilence != 0.3 {
		t.Errorf("the plan's source and detection %+v", p)
	}
	if !slices.Equal(p.Silences, []dispatch.AudioSpan{{Start: 24.22575, End: 25.732625}}) {
		t.Errorf("silences %v", p.Silences)
	}
	if !slices.Equal(p.Windows, audioWindows) || !slices.Equal(p.Seams, audioSeams) {
		t.Errorf("windows %v, seams %v; want %v, %v", p.Windows, p.Seams, audioWindows, audioSeams)
	}

	seam := "22.9791875-24.9791875"
	wantMeta := []map[string]string{
		{"window": "0", "count": "2", "start": "0", "end": "24.9791875", "cut": "silence",
			"samples": strconv.Itoa(samplesOf(0, 24.9791875)), "seam_after": seam},
		{"window": "1", "count": "2", "start": "22.9791875", "end": "49.82", "cut": "end",
			"samples": strconv.Itoa(samplesOf(22.9791875, 49.82)), "seam_before": seam},
	}
	for i, o := range ev.Outputs[1:] {
		if !maps.Equal(o.Meta, wantMeta[i]) {
			t.Errorf("%s's Meta %v, want %v", o.Name, o.Meta, wantMeta[i])
		}
	}
}

// TestAudioStageInArchive: a FLAC in a ZIP is found by the archive stage and prepared by the audio
// stage, as any part is.
func TestAudioStageInArchive(t *testing.T) {
	t.Parallel()
	zs, err := dispatch.NewArchiveStage(dispatch.ArchiveConfig{})
	if err != nil {
		t.Fatal(err)
	}
	stage, _ := audioStage(t, audioProbe)
	stages := dispatch.Stages{route.Audio: stage, route.Archive: zs}
	d := newDispatcher(t, stages, dispatch.NewMemoryIndex(0))
	path := writeInput(t, "recordings.zip", zipBytes(t, zent{name: "talks/speech.flac", body: flacBytes}))
	events := dispatchAll(t, d, path, nil)
	wantSteps(t, events, []step{{"talks/speech.flac", 1, dispatch.Prepared}, {"recordings.zip", 0, dispatch.Prepared}})
	if n := len(events[0].Outputs); n != 3 {
		t.Errorf("the FLAC's %d outputs, want its plan and two windows", n)
	}
}

// TestAudioNoProcessor: with no audio stage, as a caller with no ffmpeg and ffprobe registers,
// an audio part is no processor yet; a video part is no processor yet with one, which takes only
// route.Audio; and NewAudioStage panics on a config with no Splitter, naming the field.
func TestAudioNoProcessor(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, dispatch.Stages{}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), nil)
	wantSteps(t, events, []step{{"speech.flac", 0, dispatch.NoProcessor}})
	if ev := events[0]; ev.Reason != "no stage for route audio" || len(ev.Outputs) != 0 {
		t.Errorf("reason %q, %d outputs", ev.Reason, len(ev.Outputs))
	}

	stage, standins := audioStage(t, audioProbe)
	d = newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events = dispatchAll(t, d, writeInput(t, "clip.mp4", mp4Bytes), nil)
	wantSteps(t, events, []step{{"clip.mp4", 0, dispatch.NoProcessor}})
	if ev := events[0]; ev.Part.ContentType.Route != route.Video || ev.Reason != "no stage for route video" {
		t.Errorf("route %s, reason %q", ev.Part.ContentType.Route, ev.Reason)
	}
	if runs := standins.runs(t); len(runs) != 0 {
		t.Errorf("the video part ran %q", runs)
	}

	wantPanic(t, "dispatch: AudioConfig.Splitter is nil", func() {
		_, _ = dispatch.NewAudioStage(dispatch.AudioConfig{Version: "ffmpeg 9.0.2, ffprobe 9.0.2"})
	})
}

// TestAudioNoStream: an M4A container holding no audio stream fails, its error wrapping
// audio.ErrNoAudio, as a part a tool refuses does, after ffprobe alone ran: it has no outputs,
// and its entry isn't recorded, so a later dispatch tries it again.
func TestAudioNoStream(t *testing.T) {
	t.Parallel()
	index := dispatch.NewMemoryIndex(0)
	stage, standins := audioStage(t, audioNoStream)
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, index)
	events := dispatchAll(t, d, writeInput(t, "silent.m4a", m4aBytes), nil)
	wantSteps(t, events, []step{{"silent.m4a", 0, dispatch.Failed}})
	ev := events[0]
	if ev.Part.ContentType.Route != route.Audio || !errors.Is(ev.Err, audio.ErrNoAudio) || len(ev.Outputs) != 0 {
		t.Errorf("route %s, err %v, %d outputs; want audio, audio.ErrNoAudio, none", ev.Part.ContentType.Route, ev.Err, len(ev.Outputs))
	}
	if runs := standins.runs(t); !slices.Equal(runs, []string{"ffprobe"}) {
		t.Errorf("runs %q, want ffprobe alone", runs)
	}
	if _, ok, err := index.Lookup(t.Context(), dispatch.Key{SHA256: ev.Part.SHA256, Stage: ev.Stage}); err != nil || ok {
		t.Errorf("the failed part's entry is recorded: %v, %v", ok, err)
	}
}

// TestAudioFailedWindow: a window whose ffmpeg run fails doesn't stop the next: the part fails,
// its error naming the window and ffmpeg's last line, after its plan and the windows that were
// cut.
func TestAudioFailedWindow(t *testing.T) {
	t.Parallel()
	stage, standins := audioStage(t, audioProbe)
	standins.set(t, func(s *ffSettings) {
		s.FailWindow = strconv.Itoa(int(audioWindows[0].Start*audio.SampleRate + 0.5))
	})
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), nil)
	wantSteps(t, events, []step{{"speech.flac", 0, dispatch.Failed}})
	ev := events[0]
	var got []string
	for _, o := range ev.Outputs {
		got = append(got, o.Name)
	}
	if !slices.Equal(got, []string{"plan", "window 1"}) {
		t.Errorf("outputs %q, want the plan and window 1", got)
	}
	var exit *tool.ExitError
	if !errors.As(ev.Err, &exit) || !strings.Contains(ev.Reason, "1 of 2 windows failed") ||
		!strings.Contains(ev.Reason, "window 0 of 2") || !strings.Contains(ev.Reason, "Cannot allocate memory") {
		t.Errorf("err %v, want a *tool.ExitError naming window 0 and ffmpeg's last line", ev.Err)
	}
}

// TestAudioStageIdentity: the audio stage hashes the request's detection and overlap, its
// defaults applied, and not its Path or the concurrency; it refuses no version, an overlap
// audio.CheckOverlap refuses, and a negative concurrency.
func TestAudioStageIdentity(t *testing.T) {
	t.Parallel()
	s, _ := standinSplitter(t, audioProbe)
	const version = "ffmpeg 9.0.2, ffprobe 9.0.2"
	id := func(cfg dispatch.AudioConfig) dispatch.Identity {
		t.Helper()
		cfg.Splitter, cfg.Version = s, version
		st, err := dispatch.NewAudioStage(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return st.Identity()
	}
	base := id(dispatch.AudioConfig{})
	if base.Name != "audio" || base.Version != version || len(base.Config) != 64 {
		t.Errorf("identity %v", base)
	}
	same := dispatch.AudioConfig{
		Request: audio.Request{Path: "elsewhere.flac", Overlap: audio.DefaultOverlap,
			MinSilence: audio.DefaultMinSilence, Below: audio.DefaultBelow},
		Concurrency: 4,
	}
	if id(same) != base {
		t.Error("the defaults, a Path, or a concurrency changed the audio identity")
	}
	for name, req := range map[string]audio.Request{
		"overlap":     {Overlap: time.Second},
		"min silence": {MinSilence: time.Second},
		"below":       {Below: 20},
		"threshold":   {Threshold: -40},
	} {
		if id(dispatch.AudioConfig{Request: req}).Config == base.Config {
			t.Errorf("%s left the audio identity's config unchanged", name)
		}
	}
	for name, cfg := range map[string]dispatch.AudioConfig{
		"no version":           {Splitter: s},
		"overlap of the limit": {Splitter: s, Version: version, Request: audio.Request{Overlap: audio.WindowLimit / 2}},
		"negative concurrency": {Splitter: s, Version: version, Concurrency: -1},
	} {
		if _, err := dispatch.NewAudioStage(cfg); err == nil {
			t.Errorf("%s: an audio stage was built", name)
		}
	}
}
