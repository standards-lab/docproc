//go:build linux && integration

package stages_test

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/route"
)

func TestPictureMetadataHost(t *testing.T) {
	content := dispatch.NewMemoryContent()
	d, err := dispatch.New(dispatch.Config{
		Content: content, Puts: 2, Index: dispatch.NewMemoryIndex(0), Dir: t.TempDir(),
		Stages: dispatch.Stages{route.Vision: testVisionStage(t), route.VisionConverted: testPictureStage(t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		fixture  string
		capture  time.Time
		lat, lon float64
	}{
		{exifGPSJPEG, time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC), -(33 + 51.0/60 + 35.64/3600), 151 + 12.0/60 + 40.2/3600},
		{exifZonelessHEIC, time.Date(2023, 12, 31, 23, 59, 58, 0, time.UTC), 51 + 30.0/60 + 15.48/3600, -(7.0/60 + 39.6/3600)},
		{exifNonePNG, time.Time{}, 0, 0},
	} {
		name := filepath.Base(c.fixture)
		data, err := os.ReadFile(c.fixture)
		if err != nil {
			t.Fatal(err)
		}
		events := dispatchAll(t, d, c.fixture, nil)
		wantSteps(t, events, []step{{name, 0, dispatch.Prepared}})
		ev := events[0]
		records := slices.DeleteFunc(slices.Clone(ev.Outputs), func(o dispatch.Output) bool { return o.Kind != dispatch.RecordOutput })
		if c.capture.IsZero() {
			if len(records) != 0 || len(ev.Warnings) != 0 {
				t.Errorf("%s, with no EXIF: records %+v, warnings %+v; want none", name, records, ev.Warnings)
			}
			continue
		}
		if len(records) != 1 || ev.Outputs[0].Name != "metadata" || len(ev.Warnings) != 0 {
			t.Fatalf("%s: outputs %+v, warnings %+v; want the metadata record first", name, ev.Outputs, ev.Warnings)
		}
		got := storedRecord[metadata.Picture](t, content, ev.Outputs[0], dispatch.MediaPictureMetadata)
		if !got.CaptureTime.Equal(c.capture) || got.Make != "Docproc" || got.GPS == nil ||
			math.Abs(got.GPS.Latitude-c.lat) > 1e-9 || math.Abs(got.GPS.Longitude-c.lon) > 1e-9 {
			t.Errorf("%s: record %+v, GPS %+v; want captured %v at %v, %v", name, got, got.GPS, c.capture, c.lat, c.lon)
		}
		after, err := os.ReadFile(c.fixture)
		if err != nil || !slices.Equal(after, data) {
			t.Errorf("%s: changed by its EXIF read", name)
		}
	}
}
