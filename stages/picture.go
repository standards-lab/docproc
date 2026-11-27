package stages

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"strconv"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

const pictureVersion = "2"

type pictureConfig struct {
	Converter *picture.Converter
	EXIF      exifReader
	Version   string
	Request   picture.Request
	Memory    int64
}

type pictureStage struct {
	converter *picture.Converter
	exif      exifReader
	request   picture.Request
	memory    int64
	id        dispatch.Identity
}

func newPictureStage(cfg pictureConfig) *pictureStage {
	switch {
	case cfg.Converter == nil:
		panic("stages: pictureConfig.Converter is nil")
	case cfg.EXIF == nil:
		panic("stages: pictureConfig.EXIF is nil")
	}
	req := picture.Request{
		MetafileScale: cmp.Or(cfg.Request.MetafileScale, picture.DefaultMetafileScale),
		Density:       cmp.Or(cfg.Request.Density, picture.DefaultDensity),
	}
	return &pictureStage{converter: cfg.Converter, exif: cfg.EXIF, request: req, memory: cfg.Memory, id: identity("picture", cfg.Version, struct {
		Stage   string          `json:"stage"`
		Request picture.Request `json:"request"`
	}{pictureVersion, req})}
}

func (s *pictureStage) Identity() dispatch.Identity { return s.id }

func (s *pictureStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		req := s.request
		req.Dir, req.Path, req.Kind, req.Stem = dir, p.Path, p.ContentType.Kind, p.SHA256
		pngs, err := s.converter.ConvertPages(ctx, req)
		switch {
		case errors.Is(err, picture.ErrNoReader):
			yield(dispatch.Item{}, fmt.Errorf("picture stage: %w: %w", dispatch.ErrNoProcessor, err))
			return
		case err != nil:
			yield(dispatch.Item{}, partErr(ctx, fmt.Errorf("picture stage: %w", err), s.memory))
			return
		}
		if !pictureMetadata(ctx, "picture", s.exif, p, s.memory, yield) {
			return
		}
		for i, png := range pngs {
			o := dispatch.Output{
				Kind: dispatch.ImageOutput, Name: "picture", MediaType: "image/png", Path: png,
				Meta: map[string]string{dispatch.MetaFrom: string(p.ContentType.Kind)},
			}
			if p.ContentType.Kind == route.TIFF {
				o.Name = "page " + strconv.Itoa(i+1)
				o.Meta[dispatch.MetaPage], o.Meta[dispatch.MetaCount] = strconv.Itoa(i+1), strconv.Itoa(len(pngs))
			}
			if !yield(dispatch.OutputItem(o), nil) {
				return
			}
		}
	}
}
