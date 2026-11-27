//go:build linux

package media_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/internal/mediatest"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/tool"
)

const (
	probeBoth    = `{"streams":[{"index":0,"codec_type":"video","disposition":{"attached_pic":0}},{"index":1,"codec_type":"audio","disposition":{"attached_pic":0}}]}`
	probeAudio   = `{"streams":[{"index":0,"codec_type":"audio","disposition":{"attached_pic":0}}]}`
	probeVideo   = `{"streams":[{"index":0,"codec_type":"video","disposition":{"attached_pic":0}}]}`
	probePicture = `{"streams":[{"index":0,"codec_type":"video","disposition":{"attached_pic":1}},{"index":1,"codec_type":"data"}]}`
)

var sceneFrames = []mediatest.Frame{
	{MS: 0, Score: 0, Width: 32, Height: 18},
	{MS: 6000, Score: 1, Width: 32, Height: 18},
	{MS: 9000, Score: 0.9, Width: 32, Height: 18},
	{MS: 19000, Score: 0, Width: 32, Height: 18},
	{MS: 29000, Score: 0.01, Width: 32, Height: 18},
	{MS: 31000, Score: media.DefaultSceneThreshold, Width: 32, Height: 18},
	{MS: 37000, Score: 0.5, Width: 32, Height: 18},
	{MS: 47000, Score: 0, Width: 32, Height: 18},
}

var sceneReasons = []media.Reason{
	media.ReasonFirst, media.ReasonScene, media.ReasonScene, media.ReasonInterval,
	media.ReasonInterval, media.ReasonScene, media.ReasonScene, media.ReasonInterval,
}

var gainWAV = filepath.Join("..", "audio", "testdata", "speech-gain.wav")

func source(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.mkv")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setStandin(t *testing.T, st *standins, probe string, frames []mediatest.Frame) {
	t.Helper()
	pcm, err := filepath.Abs(gainWAV)
	if err != nil {
		t.Fatal(err)
	}
	st.set(func(c *settings) { c.Probe, c.PCM, c.Frames = probe, pcm, frames })
}

func collect(t *testing.T, events func(func(media.Event, error) bool)) []media.Event {
	t.Helper()
	var out []media.Event
	records := 0
	for e, err := range events {
		if err != nil {
			t.Fatal(err)
		}
		if e.Metadata != nil {
			if records++; len(out) != 0 || e.Window != nil || e.Frame != nil {
				t.Errorf("a metadata event %+v after %d events", e, len(out))
			}
			continue
		}
		out = append(out, e)
	}
	if records != 1 {
		t.Errorf("%d metadata events, want one, first", records)
	}
	return out
}

func split(events []media.Event) (windows, frames []media.Event) {
	for _, e := range events {
		switch {
		case e.Window != nil && e.Frame == nil:
			windows = append(windows, e)
		case e.Frame != nil && e.Window == nil:
			frames = append(frames, e)
		}
	}
	return windows, frames
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func checkDir(t *testing.T, dir string, events []media.Event) {
	t.Helper()
	var want []string
	for _, e := range events {
		switch {
		case e.Window != nil:
			want = append(want, filepath.Base(e.Window.Path))
		case e.Frame != nil:
			want = append(want, filepath.Base(e.Frame.Path))
		}
	}
	slices.Sort(want)
	want = slices.Compact(want)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("the media directory holds %q, want the events' %q", got, want)
	}
}

func checkNoScratch(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("the source's directory holds %s", e.Name())
		}
	}
}

func expectedWindows(t *testing.T, req audio.Request) ([]audio.Window, [][]byte) {
	t.Helper()
	windows, wavs, _ := expectedThresholds(t, req)
	return windows, wavs
}

const frameBytes = 2 * audio.SampleRate * int(audio.FrameLength) / int(time.Second)

func expectedThresholds(t *testing.T, req audio.Request) ([]audio.Window, [][]byte, []float64) {
	t.Helper()
	data, err := os.ReadFile(gainWAV)
	if err != nil {
		t.Fatal(err)
	}
	return expectedOf(t, data[44:], req)
}

func expectedOf(t *testing.T, pcm []byte, req audio.Request) ([]audio.Window, [][]byte, []float64) {
	t.Helper()
	tr, err := audio.NewTracker(req)
	if err != nil {
		t.Fatal(err)
	}
	var silences []audio.Silence
	var after []float64
	for at := 0; at < len(pcm); at += frameBytes {
		silences = append(silences, tr.Feed(pcm[at:min(at+frameBytes, len(pcm))])...)
		after = append(after, tr.Threshold())
	}
	silences = append(silences, tr.End()...)
	var spans []audio.Span
	for _, s := range silences {
		spans = append(spans, s.Span)
	}
	plan, err := audio.PlanWindows(tr.Duration(), spans, req)
	if err != nil {
		t.Fatal(err)
	}
	var wavs [][]byte
	var thresholds []float64
	for _, w := range plan.Windows {
		wavs = append(wavs, audio.WAV(pcm[2*audio.Sample(w.Start):2*audio.Sample(w.End)]))
		threshold := tr.Threshold()
		switch w.Cut {
		case audio.CutLimit:
			threshold = after[(audio.Sample(w.End)-1)*2/int64(frameBytes)]
		case audio.CutSilence:
			for _, s := range silences {
				end := min(s.End, w.Start+plan.Limit)
				if s.Start >= w.Start && s.Start+(end-s.Start)/2 == w.End {
					threshold = s.Threshold
				}
			}
		}
		thresholds = append(thresholds, threshold)
	}
	return plan.Windows, wavs, thresholds
}

func TestNewStreamer(t *testing.T) {
	t.Parallel()
	cfg := installStandins(t, "8.1").config()
	for _, c := range []struct {
		cfg  media.Config
		want string
	}{
		{media.Config{FFprobe: cfg.FFprobe}, "media: Config.FFmpeg is nil"},
		{media.Config{FFmpeg: cfg.FFmpeg}, "media: Config.FFprobe is nil"},
	} {
		func() {
			defer func() {
				if r := recover(); r != c.want {
					t.Errorf("panic %v, want %q", r, c.want)
				}
			}()
			_, _ = media.NewStreamer(c.cfg)
		}()
	}
	var verr *tool.VersionError
	if _, err := media.NewStreamer(cfg); !errors.As(err, &verr) || !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("ffmpeg 8.1: %v, want a VersionError wrapping ErrTooOld", err)
	}
}

func TestEventsRefused(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeBoth, sceneFrames)
	path := source(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ok := media.Request{Dir: dir}
	for name, change := range map[string]func(*media.Request){
		"no media directory":   func(r *media.Request) { r.Dir = "" },
		"missing directory":    func(r *media.Request) { r.Dir = filepath.Join(dir, "missing") },
		"directory is a file":  func(r *media.Request) { r.Dir = file },
		"overlap":              func(r *media.Request) { r.Overlap = audio.DefaultWindowLimit / 2 },
		"negative limit":       func(r *media.Request) { r.Limit = -time.Second },
		"overlap of a limit":   func(r *media.Request) { r.Limit, r.Overlap = 10*time.Second, 5*time.Second },
		"min silence":          func(r *media.Request) { r.MinSilence = -time.Second },
		"threshold above 0":    func(r *media.Request) { r.Threshold = 3 },
		"threshold too low":    func(r *media.Request) { r.Threshold = -121 },
		"threshold and margin": func(r *media.Request) { r.Threshold, r.Margin = -40, 6 },
		"percentile":           func(r *media.Request) { r.Percentile = 100 },
		"floor window":         func(r *media.Request) { r.FloorWindow = time.Millisecond },
		"margin":               func(r *media.Request) { r.Margin = -1 },
		"scene threshold":      func(r *media.Request) { r.SceneThreshold = 1 },
		"negative scene":       func(r *media.Request) { r.SceneThreshold = -0.1 },
		"NaN scene":            func(r *media.Request) { r.SceneThreshold = math.NaN() },
		"max gap":              func(r *media.Request) { r.MaxGap = -time.Second },
		"max side":             func(r *media.Request) { r.MaxSide = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			req := ok
			change(&req)
			var errs []error
			for e, err := range s.Events(t.Context(), path, req) {
				if err == nil || e.Window != nil || e.Frame != nil {
					t.Fatalf("event %+v, %v; want only the refusal", e, err)
				}
				errs = append(errs, err)
			}
			if len(errs) != 1 || !errors.Is(errs[0], media.ErrRequest) {
				t.Errorf("errors %v, want ErrRequest once", errs)
			}
		})
	}
	var errs []error
	for _, err := range s.Events(t.Context(), "", ok) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !errors.Is(errs[0], media.ErrRequest) {
		t.Errorf("no path: errors %v, want ErrRequest once", errs)
	}
	if r := st.runs(); len(r) != 0 {
		t.Errorf("runs %v, want none", r)
	}
}

func TestEventsNoMedia(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probePicture, nil)
	var errs []error
	for _, err := range s.Events(t.Context(), source(t), media.Request{Dir: t.TempDir()}) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !errors.Is(errs[0], media.ErrNoMedia) {
		t.Errorf("errors %v, want ErrNoMedia once", errs)
	}
	if r := st.runs(); len(r) != 1 || r[0].Program != "ffprobe" {
		t.Errorf("runs %v, want the probe alone", r)
	}
}

func TestEventsMetadata(t *testing.T) {
	t.Parallel()
	report, err := os.ReadFile(filepath.Join("..", "audio", "testdata", "speech-scenes.ffprobe.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, st := standinStreamer(t)
	setStandin(t, st, string(report), sceneFrames)
	var first *media.Event
	for e, err := range s.Events(t.Context(), source(t), media.Request{Dir: t.TempDir()}) {
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = &e
			if r := st.runs(); len(r) != 1 || r[0].Program != "ffprobe" {
				t.Errorf("runs %v before the first event, want the probe alone", r)
			}
		}
	}
	if first == nil || first.Metadata == nil || first.Window != nil || first.Frame != nil || first.Time != 0 {
		t.Fatalf("first event %+v, want the metadata record alone", first)
	}
	want := metadata.Media{
		Format: "matroska,webm", Duration: 50, BitRate: 135819,
		Tags: metadata.Properties{{Key: "encoder", Value: "Lavf"}},
		Streams: []metadata.MediaStream{
			{
				Index: 0, Type: "video", Codec: "mpeg4", Width: 320, Height: 180, FrameRate: 10,
				Tags: metadata.Properties{{Key: "ENCODER", Value: "Lavc mpeg4"}, {Key: "DURATION", Value: "00:00:50.000000000"}},
			},
			{
				Index: 1, Type: "audio", Codec: "flac", SampleRate: 16000, Channels: 1,
				Tags: metadata.Properties{{Key: "DURATION", Value: "00:00:49.820000000"}},
			},
		},
	}
	if !reflect.DeepEqual(*first.Metadata, want) {
		t.Errorf("metadata\n %+v\nwant\n %+v", *first.Metadata, want)
	}
}

func TestEventsStandin(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeBoth, sceneFrames)
	for name, req := range map[string]media.Request{
		"defaults": {},
		"limit":    {Limit: 12 * time.Second, Overlap: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			wantWindows, wavs, wantThresholds := expectedThresholds(t, audio.Request{Limit: req.Limit, Overlap: req.Overlap})
			path := source(t)
			dir := t.TempDir()
			st.mark()
			req.Dir = dir
			events := collect(t, s.Events(t.Context(), path, req))
			ws, fs := split(events)
			if len(ws)+len(fs) != len(events) {
				t.Fatalf("%d events neither or both a window and a frame", len(events)-len(ws)-len(fs))
			}
			if len(fs) != len(sceneFrames) {
				t.Fatalf("%d frames, want %d", len(fs), len(sceneFrames))
			}
			for i, e := range fs {
				f := e.Frame
				if f.Index != i || f.Reason != sceneReasons[i] || e.Time != time.Duration(sceneFrames[i].MS)*time.Millisecond ||
					f.Width != 32 || f.Height != 18 {
					t.Errorf("frame %d: %d, %s at %v, %dx%d; want %s at %d ms, 32x18", i, f.Index, f.Reason, e.Time, f.Width, f.Height,
						sceneReasons[i], sceneFrames[i].MS)
				}
				b, err := os.ReadFile(f.Path)
				if err != nil {
					t.Fatal(err)
				}
				if filepath.Base(f.Path) != f.SHA256+".png" || sum(b) != f.SHA256 || filepath.Dir(f.Path) != dir {
					t.Errorf("frame %d's file %s isn't named by its SHA-256 in the media directory", i, f.Path)
				}
			}
			if len(ws) != len(wantWindows) {
				t.Fatalf("%d windows, want %d", len(ws), len(wantWindows))
			}
			for i, e := range ws {
				w, want := e.Window, wantWindows[i]
				if w.Index != i || w.Span != want.Span || w.Cut != want.Cut || e.Time != want.Start || w.Samples != (len(wavs[i])-44)/2 {
					t.Errorf("window %d: %d, %v %s, %d samples at %v; want %v %s, %d samples", i, w.Index, w.Span, w.Cut, w.Samples, e.Time,
						want.Span, want.Cut, (len(wavs[i])-44)/2)
				}
				b, err := os.ReadFile(w.Path)
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != string(wavs[i]) || w.SHA256 != sum(b) || filepath.Base(w.Path) != w.SHA256+".wav" {
					t.Errorf("window %d's file %s isn't the planned samples' WAV, named by its SHA-256", i, w.Path)
				}
				var before, after audio.Span
				if i > 0 {
					before = audio.Span{Start: want.Start, End: wantWindows[i-1].End}
				}
				if i < len(wantWindows)-1 {
					after = audio.Span{Start: wantWindows[i+1].Start, End: want.End}
				}
				if w.SeamBefore != before || w.SeamAfter != after {
					t.Errorf("window %d's seams %v and %v, want %v and %v", i, w.SeamBefore, w.SeamAfter, before, after)
				}
				if w.Threshold != wantThresholds[i] || w.Threshold >= 0 || w.Threshold < -120 {
					t.Errorf("window %d, cut at the %s, names threshold %v dB, want %v dB", i, w.Cut, w.Threshold, wantThresholds[i])
				}
			}
			checkDir(t, dir, events)
			checkNoScratch(t, filepath.Dir(path))
			r := st.runs()
			if len(r) != 2 || r[0].Program != "ffprobe" || r[1].Program != "ffmpeg" {
				t.Fatalf("runs %v, want the probe and one ffmpeg run", r)
			}
			args := strings.Join(r[1].Args, " ")
			for _, want := range []string{
				"-protocol_whitelist file -i " + path,
				"-map 0:1 -af aresample=16000 -ac 1 -ar 16000 -c:a pcm_s16le",
				"-f s16le pipe:1",
				"-map 0:0 -vf select='eq(n,0)+gt(scene,0.2)+gte(t-prev_selected_t,10)',settb=AVTB,metadata=mode=print:key=lavfi.scene_score,",
				"-fps_mode passthrough -c:v png",
				"-f image2 -start_number 0 -atomic_writing 1 " + filepath.Dir(path) + "/.media-",
			} {
				if !strings.Contains(args, want) {
					t.Errorf("ffmpeg's arguments %q lack %q", args, want)
				}
			}
			if strings.Contains(args, "-readrate") {
				t.Errorf("ffmpeg's arguments %q pace the file", args)
			}
		})
	}
}

func TestEventsFixedThreshold(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeAudio, nil)
	wantWindows, _ := expectedWindows(t, audio.Request{Threshold: -45})
	events := collect(t, s.Events(t.Context(), source(t), media.Request{Dir: t.TempDir(), Threshold: -45}))
	if len(events) != len(wantWindows) {
		t.Fatalf("%d events, want %d windows", len(events), len(wantWindows))
	}
	for i, e := range events {
		if e.Window == nil || e.Window.Span != wantWindows[i].Span || e.Window.Threshold != -45 {
			t.Errorf("event %d: %+v, want window %v at -45 dB", i, e.Window, wantWindows[i].Span)
		}
	}
}

func TestEventsLimitThreshold(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeAudio, nil)
	const minSilence = 20 * time.Second
	wantWindows, _, wantThresholds := expectedThresholds(t, audio.Request{MinSilence: minSilence})
	events := collect(t, s.Events(t.Context(), source(t), media.Request{Dir: t.TempDir(), MinSilence: minSilence}))
	if len(events) != len(wantWindows) || len(events) < 3 {
		t.Fatalf("%d events, want %d windows, at least 3", len(events), len(wantWindows))
	}
	for i, e := range events {
		w := e.Window
		if w == nil || w.Span != wantWindows[i].Span || w.Threshold != wantThresholds[i] || w.Threshold >= 0 {
			t.Errorf("event %d: %+v, want window %v cut at the %s under %v dB", i, w, wantWindows[i].Span, wantWindows[i].Cut, wantThresholds[i])
		}
	}
	if first, last := events[0].Window, events[len(events)-1].Window; first.Cut != audio.CutLimit || last.Cut != audio.CutEnd || first.Threshold == last.Threshold {
		t.Errorf("first window cut at the %s under %v dB, last at the %s under %v dB; want the limit and the end, the gain step between",
			first.Cut, first.Threshold, last.Cut, last.Threshold)
	}
}

func TestEventsOpenSilence(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeAudio, nil)
	data, err := os.ReadFile(gainWAV)
	if err != nil {
		t.Fatal(err)
	}
	speech := data[44:]
	at := func(d time.Duration) int { return 2 * int(audio.Sample(d)) }
	pcm := slices.Concat(speech[:at(25*time.Second)], make([]byte, at(70*time.Second)), speech[at(25*time.Second):at(45*time.Second)])
	wav := filepath.Join(t.TempDir(), "open.wav")
	if err := os.WriteFile(wav, audio.WAV(pcm), 0o600); err != nil {
		t.Fatal(err)
	}
	st.set(func(c *settings) { c.PCM = wav })
	wantWindows, wavs, wantThresholds := expectedOf(t, pcm, audio.Request{})
	gateDir := t.TempDir()
	var gates []standinGate
	open := map[int]string{}
	for i, w := range wantWindows {
		if limit := w.Start + audio.DefaultWindowLimit; w.Cut != audio.CutEnd && limit > 25*time.Second && limit < 95*time.Second {
			path := filepath.Join(gateDir, fmt.Sprint(i))
			gates = append(gates, standinGate{At: (int(limit/audio.FrameLength) + 1) * frameBytes, Path: path})
			open[i] = path
		}
	}
	if len(gates) != 3 || wantWindows[0].Cut != audio.CutSilence || wantWindows[1].Cut != audio.CutLimit || wantWindows[2].Cut != audio.CutLimit {
		t.Fatalf("windows %v; want the first cut at the silence and the next two at their limits inside it", wantWindows)
	}
	st.set(func(c *settings) { c.Gates = gates })
	var ws []media.Event
	for e, err := range s.Events(t.Context(), source(t), media.Request{Dir: t.TempDir()}) {
		if err != nil {
			t.Fatalf("after %d windows: %v", len(ws), err)
		}
		if e.Metadata != nil {
			continue
		}
		if path, ok := open[len(ws)]; ok {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		ws = append(ws, e)
	}
	if len(ws) != len(wantWindows) {
		t.Fatalf("%d windows, want %d", len(ws), len(wantWindows))
	}
	for i, e := range ws {
		w := e.Window
		if w.Span != wantWindows[i].Span || w.Cut != wantWindows[i].Cut || w.Threshold != wantThresholds[i] || w.SHA256 != sum(wavs[i]) {
			t.Errorf("window %d: %v %s under %v dB; want %v %s under %v dB, the planned samples", i, w.Span, w.Cut, w.Threshold,
				wantWindows[i].Span, wantWindows[i].Cut, wantThresholds[i])
		}
	}
}

func TestEventsOneStream(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	for _, c := range []struct {
		name, probe string
		windows     bool
	}{{"audio", probeAudio, true}, {"video", probeVideo, false}} {
		t.Run(c.name, func(t *testing.T) {
			setStandin(t, st, c.probe, sceneFrames)
			st.mark()
			dir := t.TempDir()
			events := collect(t, s.Events(t.Context(), source(t), media.Request{Dir: dir}))
			ws, fs := split(events)
			if c.windows && (len(ws) == 0 || len(fs) != 0) || !c.windows && (len(ws) != 0 || len(fs) != len(sceneFrames)) {
				t.Errorf("%d windows and %d frames", len(ws), len(fs))
			}
			checkDir(t, dir, events)
			r := st.runs()
			if len(r) != 2 {
				t.Fatalf("runs %v, want the probe and one ffmpeg run", r)
			}
			args := strings.Join(r[1].Args, " ")
			if strings.Contains(args, "pipe:1") != c.windows || strings.Contains(args, "image2") == c.windows {
				t.Errorf("ffmpeg's arguments %q", args)
			}
		})
	}
}

func TestEventsBreak(t *testing.T) {
	s, st := standinStreamer(t)
	setStandin(t, st, probeBoth, sceneFrames)
	st.set(func(c *settings) { c.Hold = 60000 })
	path := source(t)
	dir := t.TempDir()
	start := time.Now()
	var events []media.Event
	for e, err := range s.Events(t.Context(), path, media.Request{Dir: dir}) {
		if err != nil {
			t.Fatal(err)
		}
		if e.Metadata != nil {
			continue
		}
		events = append(events, e)
		break
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("the loop returned %v after the start; the stand-in holds 60 s", took)
	}
	checkDir(t, dir, events)
	checkNoScratch(t, filepath.Dir(path))
}

func TestEventsCancel(t *testing.T) {
	s, st := standinStreamer(t)
	setStandin(t, st, probeBoth, sceneFrames)
	st.set(func(c *settings) { c.Hold = 60000 })
	path := source(t)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	start := time.Now()
	var events []media.Event
	var errs []error
	for e, err := range s.Events(ctx, path, media.Request{Dir: dir}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if e.Metadata != nil {
			continue
		}
		events = append(events, e)
		cancel()
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("the stream ended %v after the start; the stand-in holds 60 s", took)
	}
	if len(events) != 1 || len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Errorf("%d events and errors %v, want one event and then context.Canceled once", len(events), errs)
	}
	checkDir(t, dir, events)
	checkNoScratch(t, filepath.Dir(path))
}

func TestEventsFail(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeBoth, sceneFrames)
	st.set(func(c *settings) { c.Fail = "[in#0/matroska] Error during demuxing: Invalid data found" })
	wantWindows, _ := expectedWindows(t, audio.Request{})
	dir := t.TempDir()
	var events []media.Event
	var errs []error
	for e, err := range s.Events(t.Context(), source(t), media.Request{Dir: dir}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(errs) > 0 {
			t.Error("an event after the error")
		}
		events = append(events, e)
	}
	ws, fs := split(events)
	if len(fs) != len(sceneFrames) || len(ws) != len(wantWindows)-1 {
		t.Errorf("%d frames and %d windows, want %d and %d", len(fs), len(ws), len(sceneFrames), len(wantWindows)-1)
	}
	var exit *tool.ExitError
	if len(errs) != 1 || !errors.As(errs[0], &exit) || !strings.Contains(errs[0].Error(), "Invalid data found") {
		t.Errorf("errors %v, want ffmpeg's exit once, with its last line", errs)
	}
	checkDir(t, dir, events)
}

func TestEventsNoPNG(t *testing.T) {
	t.Parallel()
	s, st := standinStreamer(t)
	setStandin(t, st, probeVideo, sceneFrames)
	st.set(func(c *settings) { c.NoPNG = true })
	dir := t.TempDir()
	var errs []error
	for e, err := range s.Events(t.Context(), source(t), media.Request{Dir: dir}) {
		if err == nil {
			if e.Metadata == nil {
				t.Errorf("event %+v", e)
			}
			continue
		}
		errs = append(errs, err)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "no PNG for frame 0") {
		t.Errorf("errors %v, want the missing PNG once", errs)
	}
	checkDir(t, dir, nil)
}

func TestReasonString(t *testing.T) {
	t.Parallel()
	for r, want := range map[media.Reason]string{
		media.ReasonFirst: "first", media.ReasonScene: "scene", media.ReasonInterval: "interval", media.Reason(9): "Reason(9)",
	} {
		if got := r.String(); got != want {
			t.Errorf("%d: %q, want %q", int(r), got, want)
		}
	}
}
