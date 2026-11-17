//go:build linux

package dispatch_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
	// Decode names a canonical 44-byte WAV whose samples the audio Splitter's decoding run writes
	// to its stdout; DecodeMS, when set, is how many milliseconds of them it writes before it
	// fails, as Fail says.
	Decode   string `json:"decode,omitzero"`
	DecodeMS int    `json:"decode_ms,omitzero"`
	// PCMMS is how many milliseconds of PCM media's run writes to its stdout when it has an audio
	// output, each sample its index modulo 30011.
	PCMMS int `json:"pcm_ms,omitzero"`
	// Frames are the frames media's run takes when it has a video output.
	Frames []mediatest.Frame `json:"frames,omitzero"`
	// Fail, when set, makes media's run, or the Splitter's run cut short by DecodeMS, once it has
	// written its output, write it to stderr and exit 1.
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

// The stand-in's reports: ffprobe's of a 75.23 s FLAC whose audio is its second stream, after a
// cover picture, the stand-in ffmpeg decoding it to speech-gain.wav's samples.
const (
	audioProbe = `{"streams":[` +
		`{"index":0,"codec_name":"mjpeg","codec_type":"video"},` +
		`{"index":1,"codec_name":"flac","codec_type":"audio","sample_rate":"16000","channels":1,"duration":"75.230000"}],` +
		`"format":{"duration":"75.230000"}}`
	// audioNoStream is ffprobe's report of an M4A container holding no audio stream.
	audioNoStream = `{"streams":[{"index":0,"codec_name":"h264","codec_type":"video"}],"format":{"duration":"3.000000"}}`
)

// gainWAV is the audio package's gain-step fixture, 75.23 s of canonical WAV, whose samples the
// stand-in decodes the FLAC to.
var gainWAV = filepath.Join("..", "audio", "testdata", "speech-gain.wav")

// The inputs the audio tests dispatch, by their signatures: a FLAC, an M4A, and an MP4, each
// routed as its kind and read by none but the stand-ins, which don't read them.
const (
	flacBytes = "fLaC\x00\x00\x00\x22" + "stand-in FLAC"
	m4aBytes  = "\x00\x00\x00\x14ftypM4A \x00\x00\x00\x00M4A \x00\x00\x00\x08moov"
	mp4Bytes  = "\x00\x00\x00\x14ftypisom\x00\x00\x02\x00isom\x00\x00\x00\x08moov"
)

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

// ffmpegStandin is the stand-in ffmpeg: it prints its version at 9.0.2; for the Splitter's
// decoding run, writes the samples of its settings' Decode, failing after DecodeMS of them when
// that is set; or, for media's run, its PCM and frames, as mediaStandin does.
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
	if filter := args[slices.Index(args, "-af")+1]; filter != "aresample=16000" {
		fmt.Fprintf(os.Stderr, "stand-in: filter %q\n", filter)
		return 2
	}
	data, err := os.ReadFile(settings.Decode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	pcm := data[44:]
	if settings.DecodeMS > 0 {
		pcm = pcm[:2*settings.DecodeMS*audio.SampleRate/1000]
	}
	if _, err := os.Stdout.Write(pcm); err != nil {
		return 2
	}
	if settings.DecodeMS > 0 {
		fmt.Fprintln(os.Stderr, settings.Fail)
		return 1
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
	decode, err := filepath.Abs(gainWAV)
	if err != nil {
		t.Fatal(err)
	}
	standins = &ffStandins{settings: ffSettings{Probe: probe, Decode: decode}}
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

// audioStage returns an audio stage over the stand-ins, ffprobe reporting probe, holding two
// windows at once, and the stand-ins.
func audioStage(t *testing.T, probe string) (*dispatch.AudioStage, *ffStandins) {
	t.Helper()
	splitter, standins := standinSplitter(t, probe)
	s, err := dispatch.NewAudioStage(dispatch.AudioConfig{
		Splitter: splitter, Version: "ffmpeg 9.0.2, ffprobe 9.0.2", Request: audio.Request{Concurrency: 2},
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

// The stand-in source's plan, v0.9.0's windows of speech-gain.wav under DetectorTracker: three
// windows, each after the first starting 2 s before the one before it ends, and their seams.
var (
	audioWindows = []audio.WindowRecord{
		{Index: 0, Start: 0, End: 24.985, Cut: "silence"},
		{Index: 1, Start: 22.985, End: 50.395, Cut: "silence"},
		{Index: 2, Start: 48.395, End: 75.23, Cut: "end"},
	}
	audioSeams   = []audio.SpanRecord{{Start: 22.985, End: 24.985}, {Start: 48.395, End: 50.395}}
	audioSamples = []string{"399760", "438560", "429360"}
)

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

// checkGainWAV fails the test unless o's WAV, as checkWAV checks it, holds the stand-in source's
// samples, speech-gain.wav's, from o's start.
func checkGainWAV(t *testing.T, o dispatch.Output) {
	t.Helper()
	b := checkWAV(t, o)
	data, err := os.ReadFile(gainWAV)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := strconv.ParseFloat(o.Meta["start"], 64)
	first := 44 + 2*int(start*audio.SampleRate+0.5)
	if !bytes.Equal(b[44:], data[first:first+len(b)-44]) {
		t.Fatalf("%s: its samples aren't the source's from %v s", o.Name, start)
	}
}

// TestAudioStage: a FLAC is decoded once, its windows following in plan order as they are cut,
// each a WAV in the part's directory, its Meta its index, span, cut, samples, seams, and
// threshold, so a caller can transcribe and join it; and its plan last, a record of the source,
// the silences, the windows, and the seams.
func TestAudioStage(t *testing.T) {
	t.Parallel()
	stage, standins := audioStage(t, audioProbe)
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), func(ev *dispatch.Event) {
		if o := ev.Output; o != nil && o.Kind == dispatch.AudioOutput {
			checkGainWAV(t, *o)
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
	want := []string{"audio window 0 audio/wav", "audio window 1 audio/wav", "audio window 2 audio/wav", "record plan application/json"}
	if !slices.Equal(got, want) {
		t.Fatalf("outputs %q, want %q", got, want)
	}
	if runs := standins.runs(t); !slices.Equal(runs, []string{"ffprobe", "ffmpeg"}) {
		t.Errorf("runs %q, want ffprobe, then one decode", runs)
	}

	plan := ev.Outputs[3]
	if plan.Meta["windows"] != "3" || plan.Meta["duration"] != "75.23" {
		t.Errorf("the plan's Meta %v", plan.Meta)
	}
	var p audio.PlanRecord
	if err := json.Unmarshal(plan.Value, &p); err != nil {
		t.Fatal(err)
	}
	if p.Duration != 75.23 || p.ProbedDuration != 0 || p.Limit != 30 || p.Overlap != 2 || p.Stream != 1 || p.Codec != "flac" ||
		p.SampleRate != 16000 || p.Channels != 1 || len(p.Silences) == 0 {
		t.Errorf("the plan's source and windowing %+v", p)
	}
	if !slices.Equal(p.Windows, audioWindows) || !slices.Equal(p.Seams, audioSeams) {
		t.Errorf("windows %v, seams %v; want %v, %v", p.Windows, p.Seams, audioWindows, audioSeams)
	}

	seams := []string{"22.985-24.985", "48.395-50.395"}
	wantMeta := []map[string]string{
		{"window": "0", "start": "0", "end": "24.985", "cut": "silence", "samples": audioSamples[0], "seam_after": seams[0]},
		{"window": "1", "start": "22.985", "end": "50.395", "cut": "silence", "samples": audioSamples[1],
			"seam_before": seams[0], "seam_after": seams[1]},
		{"window": "2", "start": "48.395", "end": "75.23", "cut": "end", "samples": audioSamples[2], "seam_before": seams[1]},
	}
	for i, o := range ev.Outputs[:3] {
		threshold, err := strconv.ParseFloat(o.Meta["threshold"], 64)
		if err != nil || threshold >= 0 {
			t.Errorf("%s's threshold %q", o.Name, o.Meta["threshold"])
		}
		meta := maps.Clone(o.Meta)
		delete(meta, "threshold")
		if !maps.Equal(meta, wantMeta[i]) {
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
	if n := len(events[0].Outputs); n != 4 {
		t.Errorf("the FLAC's %d outputs, want its three windows and its plan", n)
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

// TestAudioFailedDecode: a decode that fails partway fails the part, its error carrying
// ffmpeg's last line, after the windows cut before it, with no plan.
func TestAudioFailedDecode(t *testing.T) {
	t.Parallel()
	stage, standins := audioStage(t, audioProbe)
	standins.set(t, func(s *ffSettings) {
		s.DecodeMS, s.Fail = 40000, "[aist#0:1/flac] Error while decoding: Cannot allocate memory"
	})
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), nil)
	wantSteps(t, events, []step{{"speech.flac", 0, dispatch.Failed}})
	ev := events[0]
	var got []string
	for _, o := range ev.Outputs {
		got = append(got, o.Name)
	}
	if !slices.Equal(got, []string{"window 0"}) {
		t.Errorf("outputs %q, want window 0 alone, cut before the failure", got)
	}
	var exit *tool.ExitError
	if !errors.As(ev.Err, &exit) || !strings.Contains(ev.Reason, "Cannot allocate memory") {
		t.Errorf("err %v, want a *tool.ExitError carrying ffmpeg's last line", ev.Err)
	}
}

// TestAudioStageIdentity: the audio stage hashes the request's windowing, its defaults applied,
// and not its Dir or its concurrency; it refuses no version, a field audio's Cutter refuses, and a
// concurrency below 1.
func TestAudioStageIdentity(t *testing.T) {
	t.Parallel()
	s, _ := standinSplitter(t, audioProbe)
	const version = "ffmpeg 9.0.2, ffprobe 9.0.2"
	id := func(req audio.Request) dispatch.Identity {
		t.Helper()
		st, err := dispatch.NewAudioStage(dispatch.AudioConfig{Splitter: s, Version: version, Request: req})
		if err != nil {
			t.Fatal(err)
		}
		return st.Identity()
	}
	base := id(audio.Request{Concurrency: 1})
	if base.Name != "audio" || base.Version != version || len(base.Config) != 64 {
		t.Errorf("identity %v", base)
	}
	same := audio.Request{
		Dir: "elsewhere", Concurrency: 4, Limit: audio.DefaultWindowLimit, Overlap: audio.DefaultOverlap,
		MinSilence: audio.DefaultMinSilence, Percentile: audio.DefaultPercentile, FloorWindow: audio.DefaultFloorWindow,
		Margin: audio.DefaultMargin,
	}
	if id(same) != base {
		t.Error("the defaults, a Dir, or a concurrency changed the audio identity")
	}
	for name, req := range map[string]audio.Request{
		"limit":        {Limit: 20 * time.Second},
		"overlap":      {Overlap: time.Second},
		"min silence":  {MinSilence: time.Second},
		"threshold":    {Threshold: -40},
		"percentile":   {Percentile: 20},
		"floor window": {FloorWindow: 10 * time.Second},
		"margin":       {Margin: 6},
	} {
		req.Concurrency = 1
		if id(req).Config == base.Config {
			t.Errorf("%s left the audio identity's config unchanged", name)
		}
	}
	for name, cfg := range map[string]dispatch.AudioConfig{
		"no version":           {Splitter: s, Request: audio.Request{Concurrency: 1}},
		"overlap of the limit": {Splitter: s, Version: version, Request: audio.Request{Concurrency: 1, Overlap: audio.DefaultWindowLimit / 2}},
		"negative limit":       {Splitter: s, Version: version, Request: audio.Request{Concurrency: 1, Limit: -time.Second}},
		"zero concurrency":     {Splitter: s, Version: version},
		"negative concurrency": {Splitter: s, Version: version, Request: audio.Request{Concurrency: -1}},
	} {
		if _, err := dispatch.NewAudioStage(cfg); err == nil {
			t.Errorf("%s: an audio stage was built", name)
		}
	}
}
