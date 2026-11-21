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

type videoConfig struct {
	Streamer *media.Streamer
	Version  string
	Request  media.Request
	Memory   int64
}

type videoStage struct {
	streamer *media.Streamer
	request  media.Request
	plan     media.VideoPlan
	memory   int64
	id       dispatch.Identity
}

func newVideoStage(cfg videoConfig) *videoStage {
	if cfg.Streamer == nil {
		panic("stages: videoConfig.Streamer is nil")
	}
	req := cfg.Request
	plan, err := req.Plan()
	if err != nil {
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

func (s *videoStage) Identity() dispatch.Identity { return s.id }

func (s *videoStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		src := filepath.Join(dir, "source")
		if err := linkOrCopy(p.Path, src); err != nil {
			yield(dispatch.Item{}, fmt.Errorf("video stage: %w", err))
			return
		}
		defer func() { _ = os.Remove(src) }()
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

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

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
