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
		"docproc-mail":    tooltest.Link(t, t.TempDir(), "docproc-mail", onenoteSettings{}),
		"docproc-onenote": tooltest.Link(t, t.TempDir(), "docproc-onenote", onenoteSettings{}),
		"7zz":             tooltest.Link(t, t.TempDir(), "7zz", sevenZipSettings{}),
		"ffmpeg":          tooltest.Link(t, t.TempDir(), "ffmpeg", ffSettings{}),
		"ffprobe":         tooltest.Link(t, t.TempDir(), "ffprobe", ffSettings{}),
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
		officeBefore = "406f2f9c1434d127e1edb0650f70db15daa3668bf91bcdb28350b8c0f9fea1bb"
		nestedBefore = "9fe416eecf2b6ec8bc2b9d046beb095e15b5b2404cd4e33d16b78e502bfbf5b1"
	)
	want := map[route.Route]string{
		route.Document:        "776d533ec07b950f8346e688554e5cfa21085cb6788b6047b0e0946a91689937",
		route.Office:          "b1b9d4a0803c67734e8745a39ea3808a377a695d6b562448f49e24a2735e8b54",
		route.VisionConverted: "e0f3f024fe0aa65fa4f403099bb0a4327827a3db6643357d6b3dfd99725f1e07",
		route.Vision:          "c953ea6e98fa93d682dab2a14cdfe1455cf8c995cb7d276006a137b25a958835",
		route.Nested:          "580ca77071f32324e9eac20836b2a5180ef31bee737c14b448b7494d5b33aeb3",
		route.OneNote:         "65d639819be88c73860479cd13b2b72a684277d3e00f0c3fcbf793bb6931216a",
		route.Archive:         "c88904149201469bfd191e144ba4081f3ff51980790d2872476f4d3ca71aa9f9",
		route.Audio:           "a577e560d03c66543be383f6d92e51c88b274b153dd442824cff45b0572b6553",
		route.Video:           "bfcfb63f00a5631944dd6e107fb187d3a0a5fa754304480ac18d6e3d828c0456",
		route.Text:            "16d15a494f8ca3d92d400f6c5df9cae9dcfbe28530f925ce2cae16e5139c86ad",
	}
	if got[route.Office] == officeBefore {
		t.Errorf("office: config %s, the one that yielded no metadata record", officeBefore)
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
