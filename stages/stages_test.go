//go:build linux

package stages_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

// TestParseRoutes: every route Routes names parses, in the order given, and a name that is no
// route is refused, matching ErrRequest and naming it.
func TestParseRoutes(t *testing.T) {
	t.Parallel()
	known := stages.Routes()
	names := make([]string, 0, len(known))
	for _, r := range slices.Backward(known) {
		names = append(names, string(r))
	}
	got, err := stages.ParseRoutes(names)
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(known)
	slices.Reverse(want)
	if !slices.Equal(got, want) {
		t.Errorf("parsed %v, want %v", got, want)
	}
	_, err = stages.ParseRoutes([]string{string(known[0]), "telepathy"})
	if !errors.Is(err, stages.ErrRequest) || !strings.Contains(err.Error(), `"telepathy"`) {
		t.Errorf("an unknown route: %v, want ErrRequest naming it", err)
	}
}

// TestBuildRows: with no tool on the host, Build wires the vision and text stages, which run none,
// marks every other route missing, naming the tool that isn't found, and an excluded route
// excluded, looking up no tool only it requires; each tool it tried is a row with its error; and
// Set.Inspect is the options' own.
func TestBuildRows(t *testing.T) {
	t.Parallel()
	var looked []string
	lookup := func(program string) (string, error) {
		looked = append(looked, program)
		return "", errors.New("not here")
	}
	opts := stages.Options{Exclude: []route.Route{route.Archive}, Inspect: inspect.Options{Entries: 10}}
	set, err := stages.Build(t.Context(), lookup, opts)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(looked, "7zz") || set.Inspect.Entries != 10 {
		t.Errorf("looked up %q; inspection %+v", looked, set.Inspect)
	}
	for _, row := range set.Rows {
		var want dispatch.State
		switch row.Route {
		case route.Vision, route.Text:
			want = dispatch.Wired
		case route.Archive:
			want = dispatch.Excluded
		default:
			want = dispatch.Missing
		}
		if row.State != want || (want == dispatch.Missing) != strings.Contains(row.Reason, "not found") {
			t.Errorf("%s: %s, %q; want %s", row.Route, row.State, row.Reason, want)
		}
		if _, wired := set.Stages[row.Route]; wired != (want == dispatch.Wired) {
			t.Errorf("%s: wired %t, row %s", row.Route, wired, row.State)
		}
	}
	for _, tl := range set.Tools {
		if tl.Err == nil || tl.Error != tl.Err.Error() || tl.Version != "" || tl.Minimum == "" {
			t.Errorf("tool %+v: want its error, its text, and its minimum", tl)
		}
	}
}

// TestBuildRefuses: Build refuses options it can't take before it looks anything up, each
// matching ErrRequest: inspection options inspect refuses, a negative bound, and a request its
// own package refuses.
func TestBuildRefuses(t *testing.T) {
	t.Parallel()
	lookup := func(program string) (string, error) {
		t.Errorf("looked up %s", program)
		return "", errors.New("not here")
	}
	for name, opts := range map[string]stages.Options{
		"a negative bomb ratio":     {Inspect: inspect.Options{BombRatio: -1, BombSize: 1}},
		"a negative timeout":        {Timeout: -time.Second},
		"a negative idle bound":     {Idle: -time.Second},
		"a negative memory bound":   {Memory: -1},
		"a negative concurrency":    {Concurrency: -1},
		"a negative DPI":            {Document: document.ImagesRequest{DPI: -1}},
		"a negative MaxPixels":      {Document: document.ImagesRequest{MaxPixels: -1}},
		"a metafile scale past 8":   {Picture: picture.Request{MetafileScale: picture.MaxMetafileScale + 1}},
		"a negative density":        {Picture: picture.Request{Density: -1}},
		"a negative magick limit":   {Magick: picture.Limits{Disk: -1}},
		"an audio overlap too long": {Audio: audio.Request{Overlap: audio.DefaultWindowLimit / 2}},
		"a video negative limit":    {Video: media.Request{Limit: -time.Second}},
	} {
		if _, err := stages.Build(t.Context(), lookup, opts); !errors.Is(err, stages.ErrRequest) {
			t.Errorf("%s: %v, want ErrRequest", name, err)
		}
	}
}

// TestStagesPanic: each stage constructor panics on a config whose required field is nil, naming
// the field, the wiring's mistake.
func TestStagesPanic(t *testing.T) {
	t.Parallel()
	for want, build := range map[string]func(){
		"stages: documentConfig.Preparer is nil": func() { stages.NewDocumentStage(stages.DocumentConfig{Version: "gs"}) },
		"stages: documentConfig.QPDF is nil":     func() { stages.NewDocumentStage(stages.DocumentConfig{Preparer: &document.Preparer{}, Version: "gs"}) },
		"stages: documentConfig.PDFDetach is nil": func() {
			stages.NewDocumentStage(stages.DocumentConfig{Preparer: &document.Preparer{}, QPDF: &fakeQPDF{}})
		},
		"stages: officeConfig.Converter is nil":  func() { stages.NewOfficeStage(stages.OfficeConfig{Version: "x"}) },
		"stages: pictureConfig.Converter is nil": func() { stages.NewPictureStage(stages.PictureConfig{Version: "x"}) },
		"stages: emailConfig.Reader is nil":      func() { stages.NewEmailStage(stages.EmailConfig{Version: "x"}) },
		"stages: onenoteConfig.Reader is nil":    func() { stages.NewOneNoteStage(stages.OneNoteConfig{Version: "x"}) },
		"stages: archiveConfig.SevenZip is nil":  func() { stages.NewArchiveStage(stages.ArchiveConfig{Version: "x"}) },
		"stages: audioConfig.Splitter is nil":    func() { stages.NewAudioStage(stages.AudioConfig{Version: "x"}) },
		"stages: videoConfig.Streamer is nil":    func() { stages.NewVideoStage(stages.VideoConfig{Version: "x"}) },
		"stages: archive stage: no version":      func() { stages.NewArchiveStage(stages.ArchiveConfig{SevenZip: &fakeSevenZip{}}) },
	} {
		wantPanic(t, want, build)
	}
}
