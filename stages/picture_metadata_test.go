//go:build linux

package stages_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

var (
	exifGPSJPEG      = filepath.Join("..", "picture", "testdata", "exif-gps.jpg")
	exifZonelessHEIC = filepath.Join("..", "picture", "testdata", "exif-zoneless.heic")
	exifNonePNG      = filepath.Join("..", "picture", "testdata", "exif-none.png")
)

const noEXIF = "1 1\n"

func pictureEXIFStandin(settings pictureSettings) int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if settings.EXIFFail != "" {
		fmt.Fprint(os.Stderr, settings.EXIFFail)
		return 1
	}
	sum := sha256.Sum256(input)
	out, ok := settings.EXIF[hex.EncodeToString(sum[:])]
	if !ok {
		out = noEXIF
	}
	fmt.Print(out)
	return 0
}

func standinEXIF(t *testing.T, settings pictureSettings) (*tool.Tool, *picture.EXIFReader) {
	t.Helper()
	spec := picture.MagickSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "magick", settings)
	mg, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := picture.NewEXIFReader(picture.Config{Magick: mg})
	if err != nil {
		t.Fatal(err)
	}
	return mg, r
}

func standinVisionStage(t *testing.T, settings pictureSettings) *stages.VisionStage {
	t.Helper()
	mg, r := standinEXIF(t, settings)
	return stages.NewVisionStage(stages.VisionConfig{EXIF: r, Version: stages.Versions(mg)})
}

func magickGoldens(t *testing.T, fixtures ...string) map[string]string {
	t.Helper()
	goldens := map[string]string{}
	for _, fixture := range fixtures {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(fixture)
		golden, err := os.ReadFile(filepath.Join(filepath.Dir(fixture), name[:len(name)-len(filepath.Ext(name))]+".magick.txt"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		goldens[hex.EncodeToString(sum[:])] = string(golden)
	}
	return goldens
}

func TestPictureMetadataRecords(t *testing.T) {
	t.Parallel()
	settings := pictureSettings{Formats: pictureListing, EXIF: magickGoldens(t, exifGPSJPEG, exifZonelessHEIC, exifNonePNG)}
	content := dispatch.NewMemoryContent()
	d, err := dispatch.New(dispatch.Config{
		Content: content, Puts: 2, Index: dispatch.NewMemoryIndex(0), Dir: t.TempDir(),
		Stages: dispatch.Stages{route.Vision: standinVisionStage(t, settings), route.VisionConverted: pictureStageOver(t, settings)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		fixture  string
		route    route.Route
		image    string
		want     *metadata.Picture
		lat, lon float64
	}{
		{
			exifGPSJPEG, route.Vision, "image/jpeg",
			&metadata.Picture{
				CaptureTime: time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC), Make: "Docproc", Model: "Fixture One",
				Orientation: 6, Width: 48, Height: 32,
			},
			-(33 + 51.0/60 + 35.64/3600), 151 + 12.0/60 + 40.2/3600,
		},
		{
			exifZonelessHEIC, route.VisionConverted, "image/png",
			&metadata.Picture{
				CaptureTime: time.Date(2023, 12, 31, 23, 59, 58, 0, time.UTC), Make: "Docproc", Model: "Fixture Two",
				Width: 32, Height: 32,
			},
			51 + 30.0/60 + 15.48/3600, -(7.0/60 + 39.6/3600),
		},
		{exifNonePNG, route.Vision, "image/png", nil, 0, 0},
	} {
		name := filepath.Base(c.fixture)
		var order []string
		events := dispatchAll(t, d, c.fixture, func(ev *dispatch.Event) {
			if ev.Output != nil {
				order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
			}
		})
		wantSteps(t, events, []step{{name, 0, dispatch.Prepared}})
		ev := events[0]
		if ev.Part.ContentType.Route != c.route || len(ev.Warnings) != 0 {
			t.Errorf("%s: route %s, warnings %+v", name, ev.Part.ContentType.Route, ev.Warnings)
		}
		last := ev.Outputs[len(ev.Outputs)-1]
		if last.Kind != dispatch.ImageOutput || last.Name != "picture" || last.MediaType != c.image {
			t.Errorf("%s: the last output %s %s %s, want the picture", name, last.Kind, last.Name, last.MediaType)
		}
		if c.want == nil {
			if !slices.Equal(order, []string{"image picture"}) {
				t.Errorf("%s, with no EXIF: outputs %q, want the picture alone", name, order)
			}
			continue
		}
		if !slices.Equal(order, []string{"record metadata", "image picture"}) {
			t.Errorf("%s: outputs %q, want the metadata record, then the picture", name, order)
		}
		got := storedRecord[metadata.Picture](t, content, ev.Outputs[0], dispatch.MediaPictureMetadata)
		if got.GPS == nil || math.Abs(got.GPS.Latitude-c.lat) > 1e-9 || math.Abs(got.GPS.Longitude-c.lon) > 1e-9 {
			t.Errorf("%s: GPS %+v, want %v, %v", name, got.GPS, c.lat, c.lon)
		}
		if got.CaptureTime.Location() != time.UTC {
			t.Errorf("%s: capture time %v isn't in UTC", name, got.CaptureTime)
		}
		if len(got.EXIF) == 0 || got.EXIF[0].Key != "DateTimeOriginal" {
			t.Errorf("%s: EXIF %v, want every key magick prints", name, got.EXIF)
		}
		got.GPS, got.EXIF = nil, nil
		if !reflect.DeepEqual(&got, c.want) {
			t.Errorf("%s: record\n %+v\nwant\n %+v", name, &got, c.want)
		}
	}
}

func TestPictureMetadataUnread(t *testing.T) {
	t.Parallel()
	settings := pictureSettings{Formats: pictureListing, EXIFFail: "magick: insufficient image data in file `/tmp/magick-x'\n"}
	d := newDispatcher(t, dispatch.Stages{
		route.Vision: standinVisionStage(t, settings), route.VisionConverted: pictureStageOver(t, settings),
	}, dispatch.NewMemoryIndex(0))
	for _, fixture := range []string{exifGPSJPEG, exifZonelessHEIC} {
		name := filepath.Base(fixture)
		events := dispatchAll(t, d, fixture, nil)
		wantSteps(t, events, []step{{name, 0, dispatch.Prepared}})
		ev := events[0]
		if len(ev.Warnings) != 1 || ev.Warnings[0].Tool != "magick" || ev.Warnings[0].Where != "metadata" {
			t.Errorf("%s: warnings %+v, want magick's metadata warning", name, ev.Warnings)
		}
		if len(ev.Outputs) != 1 || ev.Outputs[0].Kind != dispatch.ImageOutput || ev.Outputs[0].Name != "picture" {
			t.Errorf("%s: outputs %+v, want the picture alone", name, ev.Outputs)
		}
	}
}

func TestVisionStagePassesThrough(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, dispatch.Stages{route.Vision: standinVisionStage(t, pictureSettings{})}, dispatch.NewMemoryIndex(0))
	data, err := os.ReadFile(exifGPSJPEG)
	if err != nil {
		t.Fatal(err)
	}
	var held []byte
	events := dispatchAll(t, d, exifGPSJPEG, func(ev *dispatch.Event) {
		if ev.Output != nil && ev.Output.Kind == dispatch.ImageOutput {
			if held, err = os.ReadFile(ev.Output.Path); err != nil {
				t.Fatal(err)
			}
		}
	})
	wantSteps(t, events, []step{{"exif-gps.jpg", 0, dispatch.Prepared}})
	if !slices.Equal(held, data) {
		t.Errorf("the picture output holds %d bytes, want the JPEG's %d as stored", len(held), len(data))
	}
}

func TestNewVisionStageNil(t *testing.T) {
	t.Parallel()
	wantPanic(t, "stages: visionConfig.EXIF is nil", func() { stages.NewVisionStage(stages.VisionConfig{Version: "magick 7.1.2"}) })
	c := stages.PictureConverter(standinPictureStage(t, pictureListing, 1))
	wantPanic(t, "stages: pictureConfig.EXIF is nil", func() {
		stages.NewPictureStage(stages.PictureConfig{Converter: c, Version: "magick 7.1.2"})
	})
}

func TestPictureMetadataContentError(t *testing.T) {
	t.Parallel()
	content, reason := stages.Classify(t.Context(), fmt.Errorf("picture: magick: x.jpg's EXIF: %w", picture.ErrMetadata), 0)
	if !content || reason == "" {
		t.Errorf("ErrMetadata: content %t, %q; want a content error", content, reason)
	}
}
