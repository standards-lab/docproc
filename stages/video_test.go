//go:build linux

package stages_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
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
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

func mediaStandin(args []string, settings ffSettings) int {
	if err := mediaStandinRun(args, settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if settings.Fail != "" {
		fmt.Fprintln(os.Stderr, settings.Fail)
		return 1
	}
	return 0
}

func mediaPattern(args []string) string {
	if slices.Contains(args, "image2") {
		return args[len(args)-1]
	}
	return ""
}

func mediaStandinRun(args []string, settings ffSettings) error {
	pattern := mediaPattern(args)
	var pcm []byte
	if slices.Contains(args, "pipe:1") {
		n := settings.PCMMS * audio.SampleRate / 1000
		for i := range n {
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(i%30011))
		}
	}
	var frames []mediatest.Frame
	if pattern != "" {
		frames = settings.Frames
	}
	return mediatest.Stream(pcm, frames,
		func(at, end int) error {
			_, err := os.Stdout.Write(pcm[at:end])
			return err
		},
		func(i int) error {
			mediatest.Report(i, frames[i])
			if err := mediatest.WritePNG(pattern, i, frames[i]); err != nil {
				return err
			}
			if i == 0 {
				time.Sleep(time.Duration(settings.HoldMS) * time.Millisecond)
			}
			return nil
		})
}

const (
	videoProbeBoth  = `{"streams":[{"index":0,"codec_type":"video"},{"index":1,"codec_type":"audio"}]}`
	videoProbeVideo = `{"streams":[{"index":0,"codec_type":"video"}]}`
	videoProbeAudio = `{"streams":[{"index":0,"codec_type":"audio"}]}`
	videoProbeNone  = `{"streams":[{"index":0,"codec_type":"subtitle"}]}`
)

const (
	mkvBytes  = "\x1a\x45\xdf\xa3\x9f\x42\x82\x88matroska" + "stand-in Matroska"
	webmBytes = "\x1a\x45\xdf\xa3\x9f\x42\x82\x84webm" + "stand-in WebM"
	aviBytes  = "RIFF\x24\x00\x00\x00AVI LIST" + "stand-in AVI"
)

var videoFrames = []mediatest.Frame{
	{MS: 0, Score: 0, Width: 32, Height: 18},
	{MS: 1200, Score: 0.9, Width: 32, Height: 18},
	{MS: 11200, Score: 0.05, Width: 32, Height: 18},
}

func testVideoStage(t *testing.T, probe string, req media.Request) (*stages.VideoStage, *ffStandins) {
	t.Helper()
	ffmpeg, ffprobe, standins := standinTools(t, probe)
	standins.set(t, func(s *ffSettings) { s.PCMMS, s.Frames = 3000, videoFrames })
	s, err := media.NewStreamer(media.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	st := stages.NewVideoStage(stages.VideoConfig{Streamer: s, Version: "ffmpeg 9.0.2, ffprobe 9.0.2", Request: req})
	return st, standins
}

func videoArgs(t *testing.T, standins *ffStandins) []string {
	t.Helper()
	var out []string
	for _, r := range tooltest.Runs(t, standins.ffmpeg) {
		if slices.Contains(r.Args, "-protocol_whitelist") {
			out = append(out, r.Args[slices.Index(r.Args, "-i")+1], mediaPattern(r.Args))
		}
	}
	return out
}

func videoDispatch(t *testing.T, stages dispatch.Stages, path string) ([]dispatched, string) {
	t.Helper()
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: stages, Index: dispatch.NewMemoryIndex(0), Dir: work})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, d.Dispatch(t.Context(), dispatch.Input{Path: path}), func(ev *dispatch.Event) {
		o := ev.Output
		if o == nil || o.Path == "" {
			return
		}
		if !strings.HasPrefix(o.Path, work+string(filepath.Separator)) {
			t.Errorf("%s at %s, outside the working directory %s", o.Name, o.Path, work)
		}
		switch o.Kind {
		case dispatch.ImageOutput:
			checkFrame(t, *o)
		case dispatch.AudioOutput:
			checkStandinWAV(t, *o)
		}
	})
	return events, work
}

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

func checkFrame(t *testing.T, o dispatch.Output) {
	t.Helper()
	b, err := os.ReadFile(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if name := hex.EncodeToString(sum[:]) + ".png"; filepath.Base(o.Path) != name {
		t.Errorf("%s: named %s, not by its SHA-256", o.Name, filepath.Base(o.Path))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || strconv.Itoa(cfg.Width) != o.Meta["width"] || strconv.Itoa(cfg.Height) != o.Meta["height"] {
		t.Errorf("%s: %dx%d, %v; want a PNG of %sx%s", o.Name, cfg.Width, cfg.Height, err, o.Meta["width"], o.Meta["height"])
	}
}

func wantClean(t *testing.T, work, path string) {
	t.Helper()
	if entries, err := os.ReadDir(work); err != nil || len(entries) != 0 {
		t.Errorf("the working directory holds %v, %v", entries, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Errorf("the input's directory holds %v, %v; want the input alone", entries, err)
	}
}

func kinds(outs []dispatch.Output) []string {
	var got []string
	for _, o := range outs {
		got = append(got, string(o.Kind)+" "+o.Name)
	}
	return got
}

func TestVideoStage(t *testing.T) {
	t.Parallel()
	stage, standins := testVideoStage(t, videoProbeBoth, media.Request{})
	path := writeInput(t, "talk.mkv", mkvBytes)
	events, work := videoDispatch(t, dispatch.Stages{route.Video: stage}, path)
	wantSteps(t, events, []step{{"talk.mkv", 0, dispatch.Prepared}})
	ev := events[0]
	if ev.Part.ContentType.Route != route.Video || ev.Stage != stage.Identity() || stage.Identity().Name != "video" {
		t.Errorf("the part's route %s, stage %v", ev.Part.ContentType.Route, ev.Stage)
	}
	got := kinds(ev.Outputs)
	if len(got) < 2 || got[0] != "record metadata" || got[1] != "record plan" {
		t.Fatalf("outputs %q, want the metadata record, then the plan, first", got)
	}
	slices.Sort(got[2:])
	want := []string{"record metadata", "record plan", "audio window 0", "image frame 0", "image frame 1", "image frame 2"}
	if !slices.Equal(got, want) {
		t.Fatalf("outputs %q, want %q", got, want)
	}
	if ev.Outputs[0].MediaType != dispatch.MediaMediaMetadata {
		t.Errorf("the metadata record's media type %q", ev.Outputs[0].MediaType)
	}

	var p media.VideoPlan
	if err := json.Unmarshal(ev.Outputs[1].Value, &p); err != nil {
		t.Fatal(err)
	}
	wantPlan := media.VideoPlan{
		Windows: media.VideoWindowing{
			Detector: "tracker", Limit: 30, Overlap: 2, MinSilence: 0.3,
			Percentile: audio.DefaultPercentile, FloorWindow: audio.DefaultFloorWindow.Seconds(), Margin: audio.DefaultMargin,
		},
		Frames: media.VideoSampling{SceneThreshold: media.DefaultSceneThreshold, MaxGap: 10},
	}
	if p != wantPlan || ev.Outputs[1].MediaType != dispatch.MediaVideoPlan {
		t.Errorf("the plan %+v, want %+v", p, wantPlan)
	}

	wantMeta := map[string]map[string]string{
		"frame 0": {"frame": "0", "time": "0", "reason": "first", "width": "32", "height": "18"},
		"frame 1": {"frame": "1", "time": "1.2", "reason": "scene", "width": "32", "height": "18"},
		"frame 2": {"frame": "2", "time": "11.2", "reason": "interval", "width": "32", "height": "18"},
	}
	for _, o := range ev.Outputs[2:] {
		if o.Kind == dispatch.AudioOutput {
			threshold, err := strconv.ParseFloat(o.Meta["threshold"], 64)
			if o.MediaType != "audio/wav" || err != nil || threshold >= 0 {
				t.Errorf("%s: %s, threshold %q", o.Name, o.MediaType, o.Meta["threshold"])
			}
			delete(o.Meta, "threshold")
			want := map[string]string{"window": "0", "start": "0", "end": "3", "cut": "end", "samples": "48000"}
			if !maps.Equal(o.Meta, want) {
				t.Errorf("%s's Meta %v, want %v", o.Name, o.Meta, want)
			}
			continue
		}
		if o.MediaType != "image/png" || !maps.Equal(o.Meta, wantMeta[o.Name]) {
			t.Errorf("%s: %s, Meta %v; want image/png, %v", o.Name, o.MediaType, o.Meta, wantMeta[o.Name])
		}
	}

	args := videoArgs(t, standins)
	if len(args) != 2 {
		t.Fatalf("the runs' input and pattern %q, want one run's", args)
	}
	in, pattern := args[0], args[1]
	if filepath.Dir(in) == filepath.Dir(path) || !strings.HasPrefix(in, work+string(filepath.Separator)) {
		t.Errorf("ffmpeg read %s, not a link in the working directory %s", in, work)
	}
	if filepath.Dir(filepath.Dir(pattern)) != filepath.Dir(in) || !strings.HasPrefix(filepath.Base(filepath.Dir(pattern)), ".media-") {
		t.Errorf("ffmpeg wrote its frames to %s, not media's directory beside %s", pattern, in)
	}
	wantClean(t, work, path)
}

func TestVideoStageRoutes(t *testing.T) {
	t.Parallel()
	stage, _ := testVideoStage(t, videoProbeBoth, media.Request{})
	for _, in := range []struct{ name, content string }{
		{"a.mp4", mp4Bytes}, {"a.mkv", mkvBytes}, {"a.webm", webmBytes}, {"a.avi", aviBytes},
	} {
		events, _ := videoDispatch(t, dispatch.Stages{route.Video: stage}, writeInput(t, in.name, in.content))
		wantSteps(t, events, []step{{in.name, 0, dispatch.Prepared}})
		if ev := events[0]; ev.Part.ContentType.Route != route.Video || len(ev.Outputs) != 6 {
			t.Errorf("%s: route %s, %d outputs", in.name, ev.Part.ContentType.Route, len(ev.Outputs))
		}
	}
}

func TestVideoStageTracks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, probe string
		want        []string
	}{
		{"video only", videoProbeVideo, []string{"record metadata", "record plan", "image frame 0", "image frame 1", "image frame 2"}},
		{"audio only", videoProbeAudio, []string{"record metadata", "record plan", "audio window 0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, _ := testVideoStage(t, tc.probe, media.Request{})
			path := writeInput(t, "clip.mp4", mp4Bytes)
			events, work := videoDispatch(t, dispatch.Stages{route.Video: stage}, path)
			wantSteps(t, events, []step{{"clip.mp4", 0, dispatch.Prepared}})
			if got := kinds(events[0].Outputs); !slices.Equal(got, tc.want) {
				t.Errorf("outputs %q, want %q", got, tc.want)
			}
			wantClean(t, work, path)
		})
	}
}

func TestVideoNoStream(t *testing.T) {
	t.Parallel()
	index := dispatch.NewMemoryIndex(0)
	stage, standins := testVideoStage(t, videoProbeNone, media.Request{})
	d := newDispatcher(t, dispatch.Stages{route.Video: stage}, index)
	events := dispatchAll(t, d, writeInput(t, "subtitles.mkv", mkvBytes), nil)
	wantSteps(t, events, []step{{"subtitles.mkv", 0, dispatch.Failed}})
	ev := events[0]
	if !errors.Is(ev.Err, media.ErrNoMedia) || !errors.Is(ev.Err, dispatch.ErrRefused) || len(ev.Outputs) != 0 {
		t.Errorf("err %v, %d outputs; want media.ErrNoMedia, refused, none", ev.Err, len(ev.Outputs))
	}
	if runs := standins.runs(t); !slices.Equal(runs, []string{"ffprobe"}) {
		t.Errorf("runs %q, want ffprobe alone", runs)
	}
	if e, ok, err := index.Lookup(t.Context(), dispatch.Key{SHA256: ev.Part.SHA256, Stage: ev.Stage}); err != nil || !ok || e.Outcome != dispatch.Failed {
		t.Errorf("the failed part's entry: %+v, %v, %v; want failed, recorded", e, ok, err)
	}
}

func TestVideoFailedRun(t *testing.T) {
	t.Parallel()
	stage, standins := testVideoStage(t, videoProbeBoth, media.Request{})
	standins.set(t, func(s *ffSettings) { s.Fail = "[vist#0:0/mpeg4] Error while decoding stream: Invalid data found" })
	path := writeInput(t, "broken.mkv", mkvBytes)
	events, work := videoDispatch(t, dispatch.Stages{route.Video: stage}, path)
	wantSteps(t, events, []step{{"broken.mkv", 0, dispatch.Prepared}})
	ev := events[0]
	if len(ev.Warnings) != 1 || ev.Warnings[0].Tool != "ffmpeg" || ev.Warnings[0].Where != "stream" ||
		!strings.Contains(ev.Warnings[0].What, "Invalid data found") || len(ev.Outputs) < 2 || ev.Outputs[0].Name != "metadata" ||
		ev.Outputs[1].Name != "plan" {
		t.Errorf("warnings %+v, outputs %q; want ffmpeg's last line, after the metadata record and the plan", ev.Warnings, kinds(ev.Outputs))
	}
	wantClean(t, work, path)
}

func TestVideoCancel(t *testing.T) {
	t.Parallel()
	stage, standins := testVideoStage(t, videoProbeBoth, media.Request{})
	standins.set(t, func(s *ffSettings) { s.HoldMS = 30000 })
	path := writeInput(t, "held.mkv", mkvBytes)
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: dispatch.Stages{route.Video: stage}, Index: dispatch.NewMemoryIndex(0), Dir: work})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	start := time.Now()
	var last error
	var outputs []string
	for ev, err := range d.Dispatch(ctx, dispatch.Input{Path: path}) {
		if err != nil {
			last = err
			continue
		}
		if ev.Output != nil {
			outputs = append(outputs, ev.Output.Name)
			if ev.Output.Kind == dispatch.ImageOutput {
				cancel()
			}
		}
	}
	if !errors.Is(last, context.Canceled) || !slices.Equal(outputs, []string{"metadata", "plan", "frame 0"}) {
		t.Errorf("last error %v, outputs %q; want context.Canceled after the metadata record, the plan, and frame 0", last, outputs)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("the dispatch took %v: ffmpeg wasn't stopped", elapsed)
	}
	wantClean(t, work, path)
}

func TestVideoStageInArchive(t *testing.T) {
	t.Parallel()
	zs := testArchiveStage(t, inspect.Options{})
	stage, _ := testVideoStage(t, videoProbeBoth, media.Request{})
	stages := dispatch.Stages{route.Video: stage, route.Archive: zs}
	path := writeInput(t, "clips.zip", zipBytes(t, zent{name: "clips/a.avi", body: aviBytes}))
	events, work := videoDispatch(t, stages, path)
	wantSteps(t, events, []step{{"clips/a.avi", 1, dispatch.Prepared}, {"clips.zip", 0, dispatch.Prepared}})
	if n := len(events[0].Outputs); n != 6 {
		t.Errorf("the AVI's %d outputs, want its metadata record, plan, three frames, and a window", n)
	}
	wantClean(t, work, path)
}

func TestVideoNoProcessor(t *testing.T) {
	t.Parallel()
	_, standins := testVideoStage(t, videoProbeBoth, media.Request{})
	d := newDispatcher(t, dispatch.Stages{}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, writeInput(t, "talk.webm", webmBytes), nil)
	wantSteps(t, events, []step{{"talk.webm", 0, dispatch.NoProcessor}})
	if ev := events[0]; ev.Part.ContentType.Route != route.Video || ev.Reason != "no stage for route video" {
		t.Errorf("route %s, reason %q", ev.Part.ContentType.Route, ev.Reason)
	}
	if runs := standins.runs(t); len(runs) != 0 {
		t.Errorf("the video part ran %q", runs)
	}
}

func TestVideoStageIdentity(t *testing.T) {
	t.Parallel()
	ffmpeg, ffprobe, _ := standinTools(t, videoProbeBoth)
	s, err := media.NewStreamer(media.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	const version = "ffmpeg 9.0.2, ffprobe 9.0.2"
	id := func(req media.Request) dispatch.Identity {
		t.Helper()
		st := stages.NewVideoStage(stages.VideoConfig{Streamer: s, Version: version, Request: req})
		return st.Identity()
	}
	base := id(media.Request{})
	if base.Name != "video" || base.Version != version || len(base.Config) != 64 {
		t.Errorf("identity %v", base)
	}
	same := media.Request{
		Dir: "elsewhere", Limit: audio.DefaultWindowLimit, Overlap: audio.DefaultOverlap, MinSilence: audio.DefaultMinSilence, Percentile: audio.DefaultPercentile,
		FloorWindow: audio.DefaultFloorWindow, Margin: audio.DefaultMargin,
		SceneThreshold: media.DefaultSceneThreshold, MaxGap: media.DefaultMaxGap,
	}
	if id(same) != base {
		t.Error("the defaults or a Dir changed the video identity")
	}
	for name, req := range map[string]media.Request{
		"limit":           {Limit: 20 * time.Second},
		"overlap":         {Overlap: time.Second},
		"min silence":     {MinSilence: time.Second},
		"threshold":       {Threshold: -40},
		"percentile":      {Percentile: 20},
		"floor window":    {FloorWindow: 8 * time.Second},
		"margin":          {Margin: 6},
		"scene threshold": {SceneThreshold: 0.4},
		"max gap":         {MaxGap: 5 * time.Second},
		"max side":        {MaxSide: 768},
	} {
		if id(req).Config == base.Config {
			t.Errorf("%s left the video identity's config unchanged", name)
		}
	}
	wantPanic(t, "stages: videoConfig.Streamer is nil", func() {
		stages.NewVideoStage(stages.VideoConfig{Version: version})
	})
}
