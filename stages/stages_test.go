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
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

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

func TestBuildDocumentNeedsPDFToText(t *testing.T) {
	t.Parallel()
	others := slices.DeleteFunc(stages.Routes(), func(r route.Route) bool { return r == route.Document })
	for name, c := range map[string]struct {
		version string
		text    bool
		want    dispatch.State
		reason  string
	}{
		"present":                 {version: "26.10.0", text: true, want: dispatch.Wired},
		"absent":                  {text: true, want: dispatch.Missing, reason: "pdftotext: not found"},
		"absent, reading no text": {want: dispatch.Missing, reason: "pdftotext: not found"},
		"too old":                 {version: "21.12.0", text: true, want: dispatch.Missing, reason: "older than"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			links := map[string]string{
				"gs":        tooltest.Link(t, t.TempDir(), "gs", gsSettings{}),
				"qpdf":      tooltest.Link(t, t.TempDir(), "qpdf", nil),
				"pdfdetach": tooltest.Link(t, t.TempDir(), "pdfdetach", nil),
				"pdfinfo":   tooltest.Link(t, t.TempDir(), "pdfinfo", pdfinfoSettings{}),
			}
			if c.version != "" {
				links["pdftotext"] = tooltest.Link(t, t.TempDir(), "pdftotext", pdftotextSettings{Version: c.version})
			}
			lookup := func(program string) (string, error) {
				if link, ok := links[program]; ok {
					return link, nil
				}
				return "", errors.New("not here")
			}
			opts := stages.Options{Exclude: others, Document: document.ImagesRequest{Text: c.text}}
			set, err := stages.Build(t.Context(), lookup, opts)
			if err != nil {
				t.Fatal(err)
			}
			row := set.Rows[slices.Index(stages.Routes(), route.Document)]
			_, wired := set.Stages[route.Document]
			if row.State != c.want || wired != (c.want == dispatch.Wired) || !strings.Contains(row.Reason, c.reason) {
				t.Errorf("document %s, %q, wired %t; want %s with %q", row.State, row.Reason, wired, c.want, c.reason)
			}
			var programs []string
			for _, tl := range set.Tools {
				programs = append(programs, tl.Program)
				if tl.Program != "pdftotext" && tl.Err != nil {
					t.Errorf("%s: %v, want it built", tl.Program, tl.Err)
				}
			}
			if !slices.Equal(programs, []string{"gs", "qpdf", "pdfdetach", "pdftotext", "pdfinfo"}) {
				t.Errorf("tools %q, want the document stage's five", programs)
			}
		})
	}
}

func TestBuildShim(t *testing.T) {
	t.Parallel()
	onenoteOnly := slices.DeleteFunc(stages.Routes(), func(r route.Route) bool { return r == route.OneNote })
	for name, c := range map[string]struct {
		env  func(home string) []string
		want dispatch.State
	}{
		"under the fixed environment":  {func(string) []string { return nil }, dispatch.Missing},
		"given the host's HOME in Env": {func(home string) []string { return []string{"HOME=" + home} }, dispatch.Wired},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			link := tooltest.Link(t, t.TempDir(), "docproc-onenote", onenoteSettings{Home: home})
			lookup := func(program string) (string, error) {
				if program == "docproc-onenote" {
					return link, nil
				}
				return "", errors.New("not here")
			}
			set, err := stages.Build(t.Context(), lookup, stages.Options{Exclude: onenoteOnly, Env: c.env(home)})
			if err != nil {
				t.Fatal(err)
			}
			row := set.Rows[slices.Index(stages.Routes(), route.OneNote)]
			_, wired := set.Stages[route.OneNote]
			if row.State != c.want || wired != (c.want == dispatch.Wired) {
				t.Fatalf("onenote: %s, %q, wired %t; want %s", row.State, row.Reason, wired, c.want)
			}
			if got := tooltest.Runs(t, link); len(got) != 1 || !slices.Equal(got[0].Args, []string{"--version"}) {
				t.Errorf("runs %v, want the version command alone", got)
			}
			if c.want == dispatch.Wired {
				return
			}
			if !strings.Contains(row.Reason, "not trusted") {
				t.Errorf("onenote reason %q, want the shim's refusal", row.Reason)
			}
			for _, tl := range set.Tools {
				if tl.Program == "docproc-onenote" && (tl.Err == nil || !strings.Contains(tl.Error, "not trusted") || tl.Version != "") {
					t.Errorf("tool %+v: want the shim's refusal and no version", tl)
				}
			}
		})
	}
}

func TestBuildRefuses(t *testing.T) {
	t.Parallel()
	lookup := func(program string) (string, error) {
		t.Errorf("looked up %s", program)
		return "", errors.New("not here")
	}
	for name, opts := range map[string]stages.Options{
		"a negative bomb ratio":      {Inspect: inspect.Options{BombRatio: -1, BombSize: 1}},
		"a negative timeout":         {Timeout: -time.Second},
		"a negative idle bound":      {Idle: -time.Second},
		"a negative memory bound":    {Memory: -1},
		"a negative concurrency":     {Concurrency: -1},
		"a negative DPI":             {Document: document.ImagesRequest{DPI: -1}},
		"a negative MaxPixels":       {Document: document.ImagesRequest{MaxPixels: -1}},
		"a negative PagesPerProcess": {Document: document.ImagesRequest{PagesPerProcess: -1}},
		"a metafile scale past 8":    {Picture: picture.Request{MetafileScale: picture.MaxMetafileScale + 1}},
		"a negative density":         {Picture: picture.Request{Density: -1}},
		"a negative magick limit":    {Magick: picture.Limits{Disk: -1}},
		"an audio overlap too long":  {Audio: audio.Request{Overlap: audio.DefaultWindowLimit / 2}},
		"a video negative limit":     {Video: media.Request{Limit: -time.Second}},
	} {
		if _, err := stages.Build(t.Context(), lookup, opts); !errors.Is(err, stages.ErrRequest) {
			t.Errorf("%s: %v, want ErrRequest", name, err)
		}
	}
}

func TestStagesPanic(t *testing.T) {
	t.Parallel()
	for want, build := range map[string]func(){
		"stages: documentConfig.Preparer is nil": func() { stages.NewDocumentStage(stages.DocumentConfig{Version: "gs"}) },
		"stages: documentConfig.QPDF is nil":     func() { stages.NewDocumentStage(stages.DocumentConfig{Preparer: &document.Preparer{}, Version: "gs"}) },
		"stages: documentConfig.PDFDetach is nil": func() {
			stages.NewDocumentStage(stages.DocumentConfig{Preparer: &document.Preparer{}, QPDF: &fakeQPDF{}})
		},
		"stages: documentConfig.PDFInfo is nil": func() {
			stages.NewDocumentStage(stages.DocumentConfig{Preparer: &document.Preparer{}, QPDF: &fakeQPDF{}, PDFDetach: &fakeDetach{}})
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
