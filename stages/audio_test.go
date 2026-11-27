//go:build linux

package stages_test

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
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/internal/mediatest"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

type ffSettings struct {
	Probe    string            `json:"probe"`
	Decode   string            `json:"decode,omitzero"`
	DecodeMS int               `json:"decode_ms,omitzero"`
	PCMMS    int               `json:"pcm_ms,omitzero"`
	Frames   []mediatest.Frame `json:"frames,omitzero"`
	Fail     string            `json:"fail,omitzero"`
	HoldMS   int               `json:"hold_ms,omitzero"`
}

type ffStandins struct {
	ffmpeg, ffprobe string
	settings        ffSettings
}

func (s *ffStandins) set(t *testing.T, change func(*ffSettings)) {
	t.Helper()
	change(&s.settings)
	tooltest.Set(t, s.ffmpeg, s.settings)
	tooltest.Set(t, s.ffprobe, s.settings)
}

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

const (
	audioProbe = `{"streams":[` +
		`{"index":0,"codec_name":"mjpeg","codec_type":"video"},` +
		`{"index":1,"codec_name":"flac","codec_type":"audio","sample_rate":"16000","channels":1,"duration":"75.230000"}],` +
		`"format":{"duration":"75.230000"}}`
	audioNoStream = `{"streams":[{"index":0,"codec_name":"h264","codec_type":"video"}],"format":{"duration":"3.000000"}}`
)

var gainWAV = filepath.Join("..", "audio", "testdata", "speech-gain.wav")

const (
	flacBytes = "fLaC\x00\x00\x00\x22" + "stand-in FLAC"
	m4aBytes  = "\x00\x00\x00\x14ftypM4A \x00\x00\x00\x00M4A \x00\x00\x00\x08moov"
	mp4Bytes  = "\x00\x00\x00\x14ftypisom\x00\x00\x02\x00isom\x00\x00\x00\x08moov"
)

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

func printFFVersion(r tooltest.Run) bool {
	if !slices.Equal(r.Args, []string{"-version"}) {
		return false
	}
	fmt.Printf("%s version n9.0.2 Copyright (c) 2000-2026 the FFmpeg developers\n", r.Program)
	return true
}

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

func standinSplitter(t *testing.T, probe string) (*audio.Splitter, *ffStandins) {
	t.Helper()
	ffmpeg, ffprobe, standins := standinTools(t, probe)
	s, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	return s, standins
}

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

func testAudioStage(t *testing.T, probe string) (*stages.AudioStage, *ffStandins) {
	t.Helper()
	splitter, standins := standinSplitter(t, probe)
	s := stages.NewAudioStage(stages.AudioConfig{
		Splitter: splitter, Version: "ffmpeg 9.0.2, ffprobe 9.0.2", Request: audio.Request{Concurrency: 2},
	})
	return s, standins
}

func writeInput(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var (
	audioWindows = []audio.WindowRecord{
		{Index: 0, Start: 0, End: 24.985, Cut: "silence"},
		{Index: 1, Start: 22.985, End: 50.395, Cut: "silence"},
		{Index: 2, Start: 48.395, End: 75.23, Cut: "end"},
	}
	audioSeams   = []audio.SpanRecord{{Start: 22.985, End: 24.985}, {Start: 48.395, End: 50.395}}
	audioSamples = []string{"399760", "438560", "429360"}
)

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

func TestAudioStage(t *testing.T) {
	t.Parallel()
	stage, standins := testAudioStage(t, audioProbe)
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
	want := []string{
		"record metadata " + dispatch.MediaMediaMetadata,
		"audio window 0 audio/wav", "audio window 1 audio/wav", "audio window 2 audio/wav", "record plan " + dispatch.MediaAudioPlan,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("outputs %q, want %q", got, want)
	}
	if runs := standins.runs(t); !slices.Equal(runs, []string{"ffprobe", "ffmpeg"}) {
		t.Errorf("runs %q, want ffprobe, then one decode", runs)
	}

	plan := ev.Outputs[4]
	if len(plan.Meta) != 0 {
		t.Errorf("the plan's Meta %v, want its counts in its record alone", plan.Meta)
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
	for i, o := range ev.Outputs[1:4] {
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

func TestAudioStageInArchive(t *testing.T) {
	t.Parallel()
	zs := testArchiveStage(t, inspect.Options{})
	stage, _ := testAudioStage(t, audioProbe)
	stages := dispatch.Stages{route.Audio: stage, route.Archive: zs}
	d := newDispatcher(t, stages, dispatch.NewMemoryIndex(0))
	path := writeInput(t, "recordings.zip", zipBytes(t, zent{name: "talks/speech.flac", body: flacBytes}))
	events := dispatchAll(t, d, path, nil)
	wantSteps(t, events, []step{{"talks/speech.flac", 1, dispatch.Prepared}, {"recordings.zip", 0, dispatch.Prepared}})
	if n := len(events[0].Outputs); n != 5 {
		t.Errorf("the FLAC's %d outputs, want its metadata record, three windows, and its plan", n)
	}
}

func TestAudioNoProcessor(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, dispatch.Stages{}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), nil)
	wantSteps(t, events, []step{{"speech.flac", 0, dispatch.NoProcessor}})
	if ev := events[0]; ev.Reason != "no stage for route audio" || len(ev.Outputs) != 0 {
		t.Errorf("reason %q, %d outputs", ev.Reason, len(ev.Outputs))
	}

	stage, standins := testAudioStage(t, audioProbe)
	d = newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events = dispatchAll(t, d, writeInput(t, "clip.mp4", mp4Bytes), nil)
	wantSteps(t, events, []step{{"clip.mp4", 0, dispatch.NoProcessor}})
	if ev := events[0]; ev.Part.ContentType.Route != route.Video || ev.Reason != "no stage for route video" {
		t.Errorf("route %s, reason %q", ev.Part.ContentType.Route, ev.Reason)
	}
	if runs := standins.runs(t); len(runs) != 0 {
		t.Errorf("the video part ran %q", runs)
	}

	wantPanic(t, "stages: audioConfig.Splitter is nil", func() {
		stages.NewAudioStage(stages.AudioConfig{Version: "ffmpeg 9.0.2, ffprobe 9.0.2"})
	})
}

func TestAudioNoStream(t *testing.T) {
	t.Parallel()
	index := dispatch.NewMemoryIndex(0)
	stage, standins := testAudioStage(t, audioNoStream)
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, index)
	events := dispatchAll(t, d, writeInput(t, "silent.m4a", m4aBytes), nil)
	wantSteps(t, events, []step{{"silent.m4a", 0, dispatch.Failed}})
	ev := events[0]
	if ev.Part.ContentType.Route != route.Audio || !errors.Is(ev.Err, audio.ErrNoAudio) || !errors.Is(ev.Err, dispatch.ErrRefused) || len(ev.Outputs) != 0 {
		t.Errorf("route %s, err %v, %d outputs; want audio, audio.ErrNoAudio, refused, none", ev.Part.ContentType.Route, ev.Err, len(ev.Outputs))
	}
	if runs := standins.runs(t); !slices.Equal(runs, []string{"ffprobe"}) {
		t.Errorf("runs %q, want ffprobe alone", runs)
	}
	if e, ok, err := index.Lookup(t.Context(), dispatch.Key{SHA256: ev.Part.SHA256, Stage: ev.Stage}); err != nil || !ok || e.Outcome != dispatch.Failed {
		t.Errorf("the failed part's entry: %+v, %v, %v; want failed, recorded", e, ok, err)
	}
}

func TestAudioFailedDecode(t *testing.T) {
	t.Parallel()
	stage, standins := testAudioStage(t, audioProbe)
	standins.set(t, func(s *ffSettings) {
		s.DecodeMS, s.Fail = 40000, "[aist#0:1/flac] Error while decoding: Cannot allocate memory"
	})
	d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "speech.flac", flacBytes), nil)
	wantSteps(t, events, []step{{"speech.flac", 0, dispatch.Prepared}})
	ev := events[0]
	var got []string
	for _, o := range ev.Outputs {
		got = append(got, o.Name)
	}
	if !slices.Equal(got, []string{"metadata", "window 0"}) {
		t.Errorf("outputs %q, want the metadata record and window 0 alone, cut before the failure", got)
	}
	if len(ev.Warnings) != 1 || ev.Warnings[0].Tool != "ffmpeg" || ev.Warnings[0].Where != "window 1" ||
		!strings.Contains(ev.Warnings[0].What, "Cannot allocate memory") {
		t.Errorf("warnings %+v, want one at window 1 carrying ffmpeg's last line", ev.Warnings)
	}
}

func TestAudioStageIdentity(t *testing.T) {
	t.Parallel()
	s, _ := standinSplitter(t, audioProbe)
	const version = "ffmpeg version 9.0.2, ffprobe version 9.0.2"
	id := func(req audio.Request) dispatch.Identity {
		t.Helper()
		return stages.NewAudioStage(stages.AudioConfig{Splitter: s, Version: version, Request: req}).Identity()
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
}
