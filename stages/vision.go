package stages

import (
	"context"
	"fmt"
	"iter"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
)

// visionVersion is the version of the vision stage, which runs no tool: this package's.
const visionVersion = "1"

// visionTypes are the media types of the pictures a vision model decodes as stored.
var visionTypes = map[route.Kind]string{
	route.PNG:  "image/png",
	route.JPEG: "image/jpeg",
	route.GIF:  "image/gif",
	route.BMP:  "image/bmp",
}

// visionStage prepares a picture a vision model decodes as stored, route.Vision's parts, a PNG,
// JPEG, GIF, or BMP, by passing it through: an [dispatch.ImageOutput] named "picture" whose Path is the
// part's own file, so a layer above meets every picture, converted or not, as an image output.
// The dispatcher removes the part's file once the caller is done with the event, as it does
// any found part's; a caller that sends the picture on later reads it from the content store,
// by the output's SHA-256, or links the file in its event's body. It runs no tool,
// and its identity's version is this package's.
type visionStage struct {
	id dispatch.Identity
}

// newVisionStage builds a visionStage.
func newVisionStage() *visionStage {
	hash, err := dispatch.HashConfig(struct {
		Adapter string `json:"adapter"`
	}{visionVersion})
	if err != nil {
		// A struct of one string always marshals.
		panic(err)
	}
	return &visionStage{id: dispatch.Identity{Name: "vision", Version: visionVersion, Config: hash}}
}

// Identity returns the stage's identity: "vision", this package's version of it, and a hash of
// no options.
func (s *visionStage) Identity() dispatch.Identity { return s.id }

// Prepare yields the picture at p.Path as an image output, as the type's doc says; content of
// another kind, which route.Vision never names, fails.
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
