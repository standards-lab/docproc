package stages

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/media"
)

// videoConfig is what a [videoStage] is built over: its Streamer, the version of the tools behind
// it, the request each part's windows and frames are taken under, and the memory bound the tools
// run under, which a death by a signal's reason names.
type videoConfig struct {
	// Streamer reads each part through one ffmpeg run; it is required.
	Streamer *media.Streamer
	// Version names ffmpeg's and ffprobe's version lines, as [versions] gives them; it is
	// required.
	Version string
	// Request is the request every part's run is made under, checked by Build: its windows'
	// Limit, Overlap, MinSilence, Threshold, Percentile, FloorWindow, and Margin, and its frames'
	// SceneThreshold, MaxGap, and MaxSide, each defaulted as media.Request says. Its Dir is each
	// part's own. Its plan, every default applied, is the stage's identity's request.
	Request media.Request
	Memory  int64
}

// videoStage prepares a video for a model's vision and audio inputs, route.Video's parts, an MP4,
// Matroska, WebM, or AVI, through media's Streamer, one ffmpeg run per part. It runs no model: it
// prepares the frames a layer above sends to a vision model, and the windows it transcribes and
// joins.
//
// It yields the plan first, a [dispatch.RecordOutput] of [dispatch.MediaVideoPlan] named "plan",
// the JSON of a media.VideoPlan: the settings the run's windows are planned and its frames
// sampled under, each default applied. media plans the windows as the audio arrives, so the plan
// record holds no windows: each window carries its own span and seams in its Meta. The plan is
// yielded once the run has a stream to read, just before its first frame or window, so a source
// media refuses has no outputs. It then yields each frame and each window as the run completes
// it, frames in source-time order and windows in plan order, interleaved as media yields them:
//
//   - A frame is a [dispatch.ImageOutput] named "frame N", N its index among the frames taken,
//     from 0, a PNG, "image/png", named by its SHA-256, as a document's page image is. Its Meta is
//     its index, [dispatch.MetaFrame]; its source time, "time"; why it was taken, "reason", first,
//     scene, or interval; and its size, "width" and "height".
//   - A window is a [dispatch.AudioOutput] named "window N", N its index in the plan, a 16 kHz
//     mono 16-bit PCM WAV, "audio/wav", named by its SHA-256, cut by audio's stream cutter. Its
//     Meta is its index, [dispatch.MetaWindow]; its span, "start" and "end"; what ended it, "cut";
//     its samples, "samples"; its seams, "seam_before" for every window but the first and
//     "seam_after" for every window but the last; and the silence threshold it was cut under, in
//     dB, "threshold".
//
// Every time is in seconds, as strconv's shortest decimal of them. A video with no audio stream
// yields frames only, and a part routed video with no video stream, such as an MP4 holding only
// audio, yields windows only, each after the plan; one with neither fails, recorded, before
// ffmpeg runs.
//
// media writes a file's frames into a hidden directory beside it, so the stage never runs it over
// the part's file, which is the caller's own for the input: it runs it over a hard link to the file
// in the part's directory, or a copy where the file system refuses one, and the windows, frames,
// and media's directory of frames are all made there, inside the dispatcher's working directory,
// and removed with the part's directory. A run that fails as the content's, by [classify], after a
// frame or window is a [dispatch.Warning] at "stream", and the part is prepared with what it has;
// with none, the part fails, recorded. A failure of the run's class fails the part unrecorded.
type videoStage struct {
	streamer *media.Streamer
	request  media.Request
	plan     media.VideoPlan
	memory   int64
	id       dispatch.Identity
}

// newVideoStage builds a videoStage over cfg, panicking naming Streamer when it is nil, the
// wiring's mistake.
func newVideoStage(cfg videoConfig) *videoStage {
	if cfg.Streamer == nil {
		panic("stages: videoConfig.Streamer is nil")
	}
	req := cfg.Request
	plan, err := req.Plan()
	if err != nil {
		// Build checks the request through the same Plan.
		panic(err)
	}
	req.Dir = ""
	return &videoStage{
		streamer: cfg.Streamer, request: req, plan: plan, memory: cfg.Memory,
		id: identity("video", cfg.Version, struct {
			Request media.VideoPlan `json:"request"`
		}{plan}),
	}
}

// Identity returns the stage's identity: "video", its tools' version lines, and its plan's hash.
func (s *videoStage) Identity() dispatch.Identity { return s.id }

// Prepare runs media over a link to the file at p.Path in dir, yielding its plan, frames, and
// windows, as the type's doc says.
func (s *videoStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		// The link is named as no window or frame is, each being named by its SHA-256.
		src := filepath.Join(dir, "source")
		if err := linkOrCopy(p.Path, src); err != nil {
			yield(dispatch.Item{}, fmt.Errorf("video stage: %w", err))
			return
		}
		defer func() { _ = os.Remove(src) }()
		// The plan comes before the first frame or window, once the probe has found a stream, so a
		// source media refuses fails with no outputs, as an audio part with no audio stream does.
		planned := false
		plan := func() bool {
			planned = true
			o, err := record("plan", s.plan, dispatch.MediaVideoPlan, nil)
			if err != nil {
				yield(dispatch.Item{}, fmt.Errorf("video stage: %w", err))
				return false
			}
			return yield(dispatch.OutputItem(o), nil)
		}
		req := s.request
		req.Dir = dir
		for ev, err := range s.streamer.Events(ctx, src, req) {
			if err != nil {
				err = fmt.Errorf("video stage: %w", err)
				if !planned {
					yield(dispatch.Item{}, partErr(ctx, err, s.memory))
					return
				}
				warning, runErr := unitWarning(ctx, "ffmpeg", "stream", err, s.memory)
				if runErr != nil {
					yield(dispatch.Item{}, runErr)
					return
				}
				yield(dispatch.WarningItem(warning), nil)
				return
			}
			var o dispatch.Output
			switch {
			case ev.Frame != nil:
				o = frameOutput(ev)
			case ev.Window != nil:
				o = videoWindowOutput(ev.Window)
			default:
				continue
			}
			if !planned && !plan() {
				return
			}
			if !yield(dispatch.OutputItem(o), nil) {
				return
			}
		}
		if !planned {
			plan()
		}
	}
}

// linkOrCopy makes the file at dst the file at src: a hard link, else a copy where the file
// system refuses one.
func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

// frameOutput returns a frame event's image output, as videoStage's doc says.
func frameOutput(ev media.Event) dispatch.Output {
	f := ev.Frame
	return dispatch.Output{
		Kind: dispatch.ImageOutput, Name: "frame " + strconv.Itoa(f.Index), MediaType: "image/png", Path: f.Path,
		Meta: map[string]string{
			dispatch.MetaFrame:  strconv.Itoa(f.Index),
			dispatch.MetaTime:   secs(ev.Time),
			dispatch.MetaReason: f.Reason.String(),
			dispatch.MetaWidth:  strconv.Itoa(f.Width),
			dispatch.MetaHeight: strconv.Itoa(f.Height),
		},
	}
}

// videoWindowOutput returns a window event's audio output, as videoStage's doc says.
func videoWindowOutput(w *media.Window) dispatch.Output {
	meta := map[string]string{
		dispatch.MetaWindow:    strconv.Itoa(w.Index),
		dispatch.MetaStart:     secs(w.Span.Start),
		dispatch.MetaEnd:       secs(w.Span.End),
		dispatch.MetaCut:       w.Cut.String(),
		dispatch.MetaSamples:   strconv.Itoa(w.Samples),
		dispatch.MetaThreshold: strconv.FormatFloat(w.Threshold, 'f', -1, 64),
	}
	if w.Index > 0 {
		meta[dispatch.MetaSeamBefore] = spanSecs(w.SeamBefore)
	}
	if w.Cut != audio.CutEnd {
		meta[dispatch.MetaSeamAfter] = spanSecs(w.SeamAfter)
	}
	return dispatch.Output{
		Kind: dispatch.AudioOutput, Name: "window " + strconv.Itoa(w.Index), MediaType: "audio/wav", Path: w.Path, Meta: meta,
	}
}
