//go:build linux

package stages_test

import (
	"errors"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

func TestBuiltConfigs(t *testing.T) {
	t.Parallel()
	links := map[string]string{
		"gs":              tooltest.Link(t, t.TempDir(), "gs", gsSettings{}),
		"qpdf":            tooltest.Link(t, t.TempDir(), "qpdf", nil),
		"pdfdetach":       tooltest.Link(t, t.TempDir(), "pdfdetach", nil),
		"pdftotext":       tooltest.Link(t, t.TempDir(), "pdftotext", pdftotextSettings{}),
		"pdfinfo":         tooltest.Link(t, t.TempDir(), "pdfinfo", pdfinfoSettings{}),
		"docproc-mail":    tooltest.Link(t, t.TempDir(), "docproc-mail", onenoteSettings{}),
		"docproc-onenote": tooltest.Link(t, t.TempDir(), "docproc-onenote", onenoteSettings{}),
		"7zz":             tooltest.Link(t, t.TempDir(), "7zz", sevenZipSettings{}),
		"ffmpeg":          tooltest.Link(t, t.TempDir(), "ffmpeg", ffSettings{}),
		"ffprobe":         tooltest.Link(t, t.TempDir(), "ffprobe", ffSettings{}),
		"magick":          tooltest.Link(t, t.TempDir(), "magick", pictureSettings{}),
	}
	lookup := func(program string) (string, error) {
		if link, ok := links[program]; ok {
			return link, nil
		}
		return "", errors.New("not here")
	}
	set, err := stages.Build(t.Context(), lookup, stages.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[route.Route]string{
		route.Office:          standinOfficeStage(t, onenoteSettings{}).Identity().Config,
		route.VisionConverted: standinPictureStage(t, pictureListing, 1).Identity().Config,
	}
	for _, row := range set.Rows {
		if row.State == dispatch.Wired {
			got[row.Route] = row.Stage.Config
		}
	}
	const (
		audioBefore    = "a577e560d03c66543be383f6d92e51c88b274b153dd442824cff45b0572b6553"
		videoBefore    = "bfcfb63f00a5631944dd6e107fb187d3a0a5fa754304480ac18d6e3d828c0456"
		documentBefore = "776d533ec07b950f8346e688554e5cfa21085cb6788b6047b0e0946a91689937"
		officeBefore   = "406f2f9c1434d127e1edb0650f70db15daa3668bf91bcdb28350b8c0f9fea1bb"
		nestedBefore   = "9fe416eecf2b6ec8bc2b9d046beb095e15b5b2404cd4e33d16b78e502bfbf5b1"
		pictureBefore  = "e0f3f024fe0aa65fa4f403099bb0a4327827a3db6643357d6b3dfd99725f1e07"
		visionBefore   = "c953ea6e98fa93d682dab2a14cdfe1455cf8c995cb7d276006a137b25a958835"
	)
	want := map[route.Route]string{
		route.Document:        "d6a2b529f44c5df7dc9e4216f763a6ce910e3f2d62ca5c682055a00f64908872",
		route.Office:          "b1b9d4a0803c67734e8745a39ea3808a377a695d6b562448f49e24a2735e8b54",
		route.VisionConverted: "2444077d00a365abfa2657b2298d03b6eae89631290e8278346130a99b38b5d2",
		route.Vision:          "477f3648817f38332fb0a3e3cf085b8a36c7b983bc9b92d28224b73f458fd503",
		route.Nested:          "580ca77071f32324e9eac20836b2a5180ef31bee737c14b448b7494d5b33aeb3",
		route.OneNote:         "65d639819be88c73860479cd13b2b72a684277d3e00f0c3fcbf793bb6931216a",
		route.Archive:         "c88904149201469bfd191e144ba4081f3ff51980790d2872476f4d3ca71aa9f9",
		route.Audio:           "4641e746979f1e77d86581f8ff1a098ebc4e3068c6ba02e7502fb2f940ee6a67",
		route.Video:           "eb575b0bb598122ad41fe8146a316e42296c99861bd7cfbe1f442e8620698f4e",
		route.Text:            "16d15a494f8ca3d92d400f6c5df9cae9dcfbe28530f925ce2cae16e5139c86ad",
	}
	if got[route.Audio] == audioBefore {
		t.Errorf("audio: config %s, the one that yielded no media metadata record", audioBefore)
	}
	if got[route.Video] == videoBefore {
		t.Errorf("video: config %s, the one that yielded no media metadata record", videoBefore)
	}
	if got[route.Document] == documentBefore {
		t.Errorf("document: config %s, the one that yielded no PDF metadata record", documentBefore)
	}
	if got[route.Office] == officeBefore {
		t.Errorf("office: config %s, the one that yielded no metadata record", officeBefore)
	}
	if got[route.VisionConverted] == pictureBefore {
		t.Errorf("vision-converted: config %s, the one that yielded no picture metadata record", pictureBefore)
	}
	if got[route.Vision] == visionBefore {
		t.Errorf("vision: config %s, the one that ran no tool and yielded no picture metadata record", visionBefore)
	}
	if got[route.Nested] == nestedBefore {
		t.Errorf("nested: config %s, the one whose messages carried no in_reply_to or references", nestedBefore)
	}
	for _, r := range stages.Routes() {
		if got[r] != want[r] {
			t.Errorf("%s: config %s, want %s", r, got[r], want[r])
		}
	}
}
