package stages

import (
	"context"
	"fmt"
	"iter"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/route"
)

const visionVersion = "2"

var visionTypes = map[route.Kind]string{
	route.PNG:  "image/png",
	route.JPEG: "image/jpeg",
	route.GIF:  "image/gif",
	route.BMP:  "image/bmp",
}

type exifReader interface {
	Metadata(ctx context.Context, path string, kind route.Kind) (*metadata.Picture, error)
}

type visionConfig struct {
	EXIF    exifReader
	Version string
	Memory  int64
}

type visionStage struct {
	exif   exifReader
	memory int64
	id     dispatch.Identity
}

func newVisionStage(cfg visionConfig) *visionStage {
	if cfg.EXIF == nil {
		panic("stages: visionConfig.EXIF is nil")
	}
	return &visionStage{exif: cfg.EXIF, memory: cfg.Memory, id: identity("vision", cfg.Version, struct {
		Stage string `json:"stage"`
	}{visionVersion})}
}

func (s *visionStage) Identity() dispatch.Identity { return s.id }

func (s *visionStage) Prepare(ctx context.Context, p dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		mime, ok := visionTypes[p.ContentType.Kind]
		if !ok {
			yield(dispatch.Item{}, fmt.Errorf("vision stage: %s isn't a PNG, JPEG, GIF, or BMP", p.Name))
			return
		}
		if !pictureMetadata(ctx, "vision", s.exif, p, s.memory, yield) {
			return
		}
		yield(dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.ImageOutput, Name: "picture", MediaType: mime, Path: p.Path,
			Meta: map[string]string{dispatch.MetaKind: string(p.ContentType.Kind)},
		}), nil)
	}
}

func pictureMetadata(ctx context.Context, stage string, r exifReader, p dispatch.Part, memory int64, yield func(dispatch.Item, error) bool) bool {
	rec, err := r.Metadata(ctx, p.Path, p.ContentType.Kind)
	if err != nil {
		w, runErr := unitWarning(ctx, "magick", "metadata", err, memory)
		if runErr != nil {
			yield(dispatch.Item{}, fmt.Errorf("%s stage: %w", stage, runErr))
			return false
		}
		return yield(dispatch.WarningItem(w), nil)
	}
	if rec == nil {
		return true
	}
	o, err := record("metadata", rec, rec.MediaType(), nil)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("%s stage: %w", stage, err))
		return false
	}
	return yield(dispatch.OutputItem(o), nil)
}
