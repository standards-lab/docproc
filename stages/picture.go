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

// pictureConfig is what a [pictureStage] is built over: its Converter, the version of the tools
// behind it, the request every picture is converted under, and the memory bound the tools run
// under, which a death by a signal's reason names.
type pictureConfig struct {
	// Converter converts the pictures; it is required.
	Converter *picture.Converter
	// Version names magick's and LibreOffice's version lines, as [versions] gives them; it is
	// required.
	Version string
	// Request is the request every picture is converted under, checked by Build: its
	// MetafileScale and Density, each zero its default. Its Dir, Path, Kind, and Stem are each
	// part's own.
	Request picture.Request
	Memory  int64
}

// pictureStage prepares a picture a vision model decodes only once converted, route.
// VisionConverted's parts, through picture's Converter, to PNGs in the part's directory: a TIFF
// to one [dispatch.ImageOutput] per page, in page order, named "page N", its Meta naming the page
// and the TIFF's page count, [dispatch.MetaPage] and [dispatch.MetaCount]; an EMF or WMF at the
// request's MetafileScale, an SVG at its Density, and a HEIC, WebP, or AVIF to one ImageOutput
// named "picture", a HEIC's primary image, a WebP's or AVIF's first frame, each read as the kind
// the part's content type names and named by the part's SHA-256. Every output's Meta names the
// kind it was converted from. It has no processor yet for a HEIC, WebP, or AVIF whose reader the
// Converter's magick lacks: it ends with dispatch.ErrNoProcessor, its reason naming why. Any other
// failure is classified once, by [partErr].
type pictureStage struct {
	converter *picture.Converter
	request   picture.Request
	memory    int64
	id        dispatch.Identity
}

// newPictureStage builds a pictureStage over cfg, panicking naming Converter when it is nil, the
// wiring's mistake.
func newPictureStage(cfg pictureConfig) *pictureStage {
	if cfg.Converter == nil {
		panic("stages: pictureConfig.Converter is nil")
	}
	req := picture.Request{
		MetafileScale: cmp.Or(cfg.Request.MetafileScale, picture.DefaultMetafileScale),
		Density:       cmp.Or(cfg.Request.Density, picture.DefaultDensity),
	}
	return &pictureStage{converter: cfg.Converter, request: req, memory: cfg.Memory, id: identity("picture", cfg.Version, struct {
		Request picture.Request `json:"request"`
	}{req})}
}

// Identity returns the stage's identity: "picture", its tools' version lines, and its request's
// hash.
func (s *pictureStage) Identity() dispatch.Identity { return s.id }

// Prepare converts the picture at p.Path, as the type's doc says.
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
		for i, png := range pngs {
			o := dispatch.Output{
				Kind: dispatch.ImageOutput, Name: "picture", MediaType: "image/png", Path: png,
				Meta: map[string]string{"from": string(p.ContentType.Kind)},
			}
			if p.ContentType.Kind == route.TIFF {
				// A TIFF yields its every page, as the document stage yields a PDF's.
				o.Name = "page " + strconv.Itoa(i+1)
				o.Meta[dispatch.MetaPage], o.Meta[dispatch.MetaCount] = strconv.Itoa(i+1), strconv.Itoa(len(pngs))
			}
			if !yield(dispatch.OutputItem(o), nil) {
				return
			}
		}
	}
}
