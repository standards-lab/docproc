package stages

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"strconv"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
)

// audioConfig is what an [audioStage] is built over: its Splitter, the version of the tools
// behind it, the request each source's windows are cut under, and the memory bound the tools run
// under, which a death by a signal's reason names.
type audioConfig struct {
	// Splitter cuts the windows; it is required.
	Splitter *audio.Splitter
	// Version names ffmpeg's and ffprobe's version lines, as [versions] gives them; it is
	// required.
	Version string
	// Request is the request every part's windows are cut under, checked by Build: its window
	// Limit, Overlap, MinSilence, and tracker fields, each defaulted as audio.Request says, and
	// its Concurrency, the windows held at once, at least 1. Its Dir is each part's own.
	Request audio.Request
	Memory  int64
}

// audioStage prepares speech for a model's audio input, route.Audio's parts, a WAV, FLAC, MP3,
// Ogg, or M4A, through audio's Splitter, which decodes the part once. It runs no model: it
// prepares the windows a layer above transcribes and joins, and the plan that layer joins them by.
//
// It yields each window as the decode cuts it, in plan order, a [dispatch.AudioOutput] named
// "window N", N its index in the plan from 0, a 16 kHz mono 16-bit PCM WAV, "audio/wav", named by
// its SHA-256, into the part's directory. A window's Meta carries what a caller needs to
// transcribe it and join it before the plan arrives: its index, [dispatch.MetaWindow]; its span of
// the source, "start" and "end"; what ended it, "cut", silence, limit, or end; its samples at
// 16 kHz, "samples"; the seams it shares with its neighbours, "seam_before" for every window but
// the first and "seam_after" for every window but the last, each its span of the source as
// "<start>-<end>"; and the silence threshold it was cut under, in dB, "threshold". Every time is in
// seconds, as strconv's shortest decimal of the span's seconds, such as "24.91". Once the last
// window is yielded, it yields the plan, a [dispatch.RecordOutput] of [dispatch.MediaAudioPlan]
// named "plan", the JSON of an audio.PlanRecord, which carries ffprobe's duration beside the
// decoded one when the two differ; its counts, the windows and the duration, are in the record
// alone.
//
// A decode that fails as the content's, by [classify], after a window is cut is a
// [dispatch.Warning] at the next window, "window N", and the part is prepared with the windows it
// has and no plan; with none, the part fails, recorded. A source with no audio stream, such as an
// M4A container holding none, fails the part before ffmpeg runs. A failure of the run's class
// fails the part unrecorded.
type audioStage struct {
	splitter *audio.Splitter
	request  audio.Request
	memory   int64
	id       dispatch.Identity
}

// newAudioStage builds an audioStage over cfg, panicking naming Splitter when it is nil, the
// wiring's mistake.
func newAudioStage(cfg audioConfig) *audioStage {
	if cfg.Splitter == nil {
		panic("stages: audioConfig.Splitter is nil")
	}
	req := cfg.Request
	req.Dir = ""
	return &audioStage{splitter: cfg.Splitter, request: req, memory: cfg.Memory, id: identity("audio", cfg.Version, struct {
		Request audio.Request `json:"request"`
	}{resolvedAudio(req)})}
}

// resolvedAudio returns req's windowing with every default filled, as the Splitter applies it:
// its limit and overlap as the Planner takes them, and a fixed threshold in place of the rolling
// one's fields, which are then zero.
func resolvedAudio(req audio.Request) audio.Request {
	planner, err := audio.NewPlanner(req)
	if err != nil {
		// Build checks the request through audio's Cutter, which refuses what the Planner does.
		panic(err)
	}
	r := audio.Request{
		Limit: planner.Limit(), Overlap: planner.Overlap(),
		MinSilence: cmp.Or(req.MinSilence, audio.DefaultMinSilence), Threshold: req.Threshold,
	}
	if req.Threshold == 0 {
		r.Percentile = cmp.Or(req.Percentile, audio.DefaultPercentile)
		r.FloorWindow = cmp.Or(req.FloorWindow, audio.DefaultFloorWindow)
		r.Margin = cmp.Or(req.Margin, audio.DefaultMargin)
	}
	return r
}

// Identity returns the stage's identity: "audio", its tools' version lines, and its resolved
// request's hash.
func (s *audioStage) Identity() dispatch.Identity { return s.id }

// Prepare cuts the windows of the source at p.Path into dir, as the type's doc says.
func (s *audioStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		req := s.request
		req.Dir = dir
		cut := 0
		for w, err := range s.splitter.Windows(ctx, p.Path, req) {
			if err != nil {
				err = fmt.Errorf("audio stage: %w", err)
				if cut == 0 {
					yield(dispatch.Item{}, partErr(ctx, err, s.memory))
					return
				}
				warning, runErr := unitWarning(ctx, "ffmpeg", "window "+strconv.Itoa(cut), err, s.memory)
				if runErr != nil {
					yield(dispatch.Item{}, runErr)
					return
				}
				yield(dispatch.WarningItem(warning), nil)
				return
			}
			cut++
			o := dispatch.Output{
				Kind: dispatch.AudioOutput, Name: "window " + strconv.Itoa(w.Index), MediaType: "audio/wav",
				Path: w.Path, Meta: windowMeta(w),
			}
			if !yield(dispatch.OutputItem(o), nil) {
				return
			}
			if w.Plan == nil {
				continue
			}
			o, err := record("plan", w.Plan.Record(), dispatch.MediaAudioPlan, nil)
			if err != nil {
				yield(dispatch.Item{}, fmt.Errorf("audio stage: %w", err))
				return
			}
			if !yield(dispatch.OutputItem(o), nil) {
				return
			}
		}
	}
}

// windowMeta returns a window output's Meta, as audioStage's doc says.
func windowMeta(w audio.WindowFile) map[string]string {
	meta := map[string]string{
		dispatch.MetaWindow: strconv.Itoa(w.Index),
		"start":             secs(w.Window.Start),
		"end":               secs(w.Window.End),
		"cut":               w.Window.Cut.String(),
		"samples":           strconv.Itoa(w.Samples),
		"threshold":         strconv.FormatFloat(w.Threshold, 'f', -1, 64),
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
