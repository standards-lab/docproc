package dispatch

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"strconv"
	"time"

	"github.com/standards-lab/docproc/audio"
)

// AudioConfig is what an [AudioStage] is built over: the caller's Splitter, the version of the
// tools behind it, and the request each source's windows are cut under.
type AudioConfig struct {
	// Splitter cuts the windows; it is required. A caller with no ffmpeg and ffprobe has no
	// Splitter, and registers no audio stage: its audio parts are no processor yet.
	Splitter *audio.Splitter
	// Version names the tools behind the Splitter, ffmpeg's and ffprobe's, as [Versions] gives
	// them; it is required.
	Version string
	// Request is the request every part's windows are cut under: its window Limit, Overlap,
	// MinSilence, and tracker fields, each defaulted as audio.Request says, and its Concurrency,
	// the windows held at once, at least 1. Its Dir is ignored: a part's windows are cut into the
	// part's own directory. Every field but Concurrency, which changes no window, is part of the
	// stage's identity, its defaults applied, so a zero field and its default are the same
	// identity. A field audio's Cutter refuses, or a Concurrency below 1, is refused when the stage
	// is built.
	Request audio.Request
}

// AudioStage prepares speech for a model's audio input, route.Audio's parts, a WAV, FLAC, MP3,
// Ogg, or M4A, through audio's Splitter, which decodes the part once. It runs no model: it
// prepares the windows a layer above transcribes and joins, and the plan that layer joins them by.
//
// It yields each window as the decode cuts it, in plan order, an [AudioOutput] named "window N",
// N its index in the plan from 0, a 16 kHz mono 16-bit PCM WAV, "audio/wav", named by its SHA-256,
// into the part's directory. A window's Meta carries what a caller needs to transcribe it and join
// it without the plan: its index, "window"; its span of the source, "start" and "end"; what ended
// it, "cut", silence, limit, or end; its samples at 16 kHz, "samples"; the seams it shares with
// its neighbours, "seam_before" for every window but the first and "seam_after" for every window
// but the last, each its span of the source as "<start>-<end>"; and the silence threshold it was
// cut under, in dB, "threshold". No window knows the plan's count before the decode ends. Every
// time is in seconds, as strconv's shortest decimal of the span's seconds, such as "24.91"; a
// window's seam_after is the next window's seam_before.
//
// Once the last window is yielded, it yields the plan, a [RecordOutput] named "plan", the JSON of
// an audio.PlanRecord, its Meta the window count, "windows", and the source's decoded duration,
// "duration", in seconds; the record carries ffprobe's duration beside it when the two differ.
//
// A decode that fails, or a window that can't be written, ends the stage with its error after the
// windows already cut, so the part fails with those windows among its outputs and no plan. A
// source with no audio stream, such as an M4A container holding none, ends the stage with an error
// wrapping audio.ErrNoAudio, before ffmpeg runs: the part fails, as a part whose tool refuses it
// does.
type AudioStage struct {
	splitter *audio.Splitter
	request  audio.Request
	id       Identity
}

// NewAudioStage builds an AudioStage over cfg, panicking naming Splitter when it is nil, the
// caller's mistake, and refusing one with no Version, a request field audio's Cutter refuses, or a
// Concurrency below 1.
func NewAudioStage(cfg AudioConfig) (*AudioStage, error) {
	if cfg.Splitter == nil {
		panic("dispatch: AudioConfig.Splitter is nil")
	}
	req := cfg.Request
	if req.Concurrency < 1 {
		return nil, fmt.Errorf("dispatch: audio stage: concurrency %d is below 1", req.Concurrency)
	}
	if _, err := audio.NewCutter(req); err != nil {
		return nil, fmt.Errorf("dispatch: audio stage: %w", err)
	}
	// The identity hashes the windowing the Splitter applies, each default applied: a fixed
	// threshold in place of the rolling one's fields.
	planner, _ := audio.NewPlanner(req)
	percentile, floorWindow, margin := 0.0, time.Duration(0), 0.0
	if req.Threshold == 0 {
		percentile = cmp.Or(req.Percentile, audio.DefaultPercentile)
		floorWindow = cmp.Or(req.FloorWindow, audio.DefaultFloorWindow)
		margin = cmp.Or(req.Margin, audio.DefaultMargin)
	}
	id, err := adapterIdentity("audio", cfg.Version, struct {
		Limit       time.Duration `json:"limit"`
		Overlap     time.Duration `json:"overlap"`
		MinSilence  time.Duration `json:"min_silence"`
		Threshold   float64       `json:"threshold"`
		Percentile  float64       `json:"percentile"`
		FloorWindow time.Duration `json:"floor_window"`
		Margin      float64       `json:"margin"`
	}{planner.Limit(), planner.Overlap(), cmp.Or(req.MinSilence, audio.DefaultMinSilence), req.Threshold, percentile, floorWindow, margin})
	if err != nil {
		return nil, err
	}
	req.Dir = ""
	return &AudioStage{splitter: cfg.Splitter, request: req, id: id}, nil
}

// Identity returns the stage's identity: "audio", its tools' version, and its request's hash.
func (s *AudioStage) Identity() Identity { return s.id }

// Prepare cuts the windows of the source at p.Path into dir, as the type's doc says.
func (s *AudioStage) Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error] {
	return func(yield func(Item, error) bool) {
		req := s.request
		req.Dir = dir
		for w, err := range s.splitter.Windows(ctx, p.Path, req) {
			if err != nil {
				yield(Item{}, fmt.Errorf("audio stage: %w", err))
				return
			}
			o := Output{
				Kind: AudioOutput, Name: "window " + strconv.Itoa(w.Index), MediaType: "audio/wav",
				Path: w.Path, Meta: windowMeta(w),
			}
			if !yield(OutputItem(o), nil) {
				return
			}
			if w.Plan == nil {
				continue
			}
			o, err := record("plan", w.Plan.Record(), map[string]string{
				"windows": strconv.Itoa(len(w.Plan.Windows)), "duration": secs(w.Plan.Duration),
			})
			if err != nil {
				yield(Item{}, fmt.Errorf("audio stage: %w", err))
				return
			}
			if !yield(OutputItem(o), nil) {
				return
			}
		}
	}
}

// windowMeta returns a window output's Meta, as AudioStage's doc says.
func windowMeta(w audio.WindowFile) map[string]string {
	meta := map[string]string{
		"window":    strconv.Itoa(w.Index),
		"start":     secs(w.Window.Start),
		"end":       secs(w.Window.End),
		"cut":       w.Window.Cut.String(),
		"samples":   strconv.Itoa(w.Samples),
		"threshold": strconv.FormatFloat(w.Threshold, 'f', -1, 64),
	}
	if w.Index > 0 {
		meta["seam_before"] = spanSecs(w.SeamBefore)
	}
	if w.Window.Cut != audio.CutEnd {
		meta["seam_after"] = spanSecs(w.SeamAfter)
	}
	return meta
}

// secs returns d in seconds, strconv's shortest decimal of them.
func secs(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) }

// spanSecs returns sp in seconds, as "<start>-<end>".
func spanSecs(sp audio.Span) string { return secs(sp.Start) + "-" + secs(sp.End) }
