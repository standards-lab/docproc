package stages

import (
	"context"
	"fmt"
	"iter"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
)

const visionVersion = "1"

var visionTypes = map[route.Kind]string{
	route.PNG:  "image/png",
	route.JPEG: "image/jpeg",
	route.GIF:  "image/gif",
	route.BMP:  "image/bmp",
}

type visionStage struct {
	id dispatch.Identity
}

func newVisionStage() *visionStage {
	hash, err := dispatch.HashConfig(struct {
		Adapter string `json:"adapter"`
	}{visionVersion})
	if err != nil {
		panic(err)
	}
	return &visionStage{id: dispatch.Identity{Name: "vision", Version: visionVersion, Config: hash}}
}

func (s *visionStage) Identity() dispatch.Identity { return s.id }

func (s *visionStage) Prepare(_ context.Context, p dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		mime, ok := visionTypes[p.ContentType.Kind]
		if !ok {
			yield(dispatch.Item{}, fmt.Errorf("vision stage: %s isn't a PNG, JPEG, GIF, or BMP", p.Name))
			return
		}
		yield(dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.ImageOutput, Name: "picture", MediaType: mime, Path: p.Path,
			Meta: map[string]string{dispatch.MetaKind: string(p.ContentType.Kind)},
		}), nil)
	}
}
