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
	"github.com/standards-lab/docproc/metadata"
)

const audioVersion = "2"

type audioConfig struct {
	Splitter *audio.Splitter
	Version  string
	Request  audio.Request
	Memory   int64
}

type audioStage struct {
	splitter *audio.Splitter
	request  audio.Request
	memory   int64
	id       dispatch.Identity
}

func newAudioStage(cfg audioConfig) *audioStage {
	if cfg.Splitter == nil {
		panic("stages: audioConfig.Splitter is nil")
	}
	req := cfg.Request
	req.Dir = ""
	return &audioStage{splitter: cfg.Splitter, request: req, memory: cfg.Memory, id: identity("audio", cfg.Version, struct {
		Stage   string        `json:"stage"`
		Request audio.Request `json:"request"`
	}{audioVersion, resolvedAudio(req)})}
}

func resolvedAudio(req audio.Request) audio.Request {
	planner, err := audio.NewPlanner(req)
	if err != nil {
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

func (s *audioStage) Identity() dispatch.Identity { return s.id }

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
			if w.Metadata != nil && !emitMetadata(*w.Metadata, "audio stage", yield) {
				return
			}
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

func windowMeta(w audio.WindowFile) map[string]string {
	meta := map[string]string{
		dispatch.MetaWindow:    strconv.Itoa(w.Index),
		dispatch.MetaStart:     secs(w.Window.Start),
		dispatch.MetaEnd:       secs(w.Window.End),
		dispatch.MetaCut:       w.Window.Cut.String(),
		dispatch.MetaSamples:   strconv.Itoa(w.Samples),
		dispatch.MetaThreshold: strconv.FormatFloat(w.Threshold, 'f', -1, 64),
	}
	if w.Index > 0 {
		meta[dispatch.MetaSeamBefore] = spanSecs(w.SeamBefore)
	}
	if w.Window.Cut != audio.CutEnd {
		meta[dispatch.MetaSeamAfter] = spanSecs(w.SeamAfter)
	}
	return meta
}

func secs(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) }

func spanSecs(sp audio.Span) string { return secs(sp.Start) + "-" + secs(sp.End) }

func emitMetadata(md metadata.Media, stage string, yield func(dispatch.Item, error) bool) bool {
	o, err := record("metadata", md, md.MediaType(), nil)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("%s: %w", stage, err))
		return false
	}
	return yield(dispatch.OutputItem(o), nil)
}
