//go:build integration && linux

package picture_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

func hostEXIF(t *testing.T) *picture.EXIFReader {
	t.Helper()
	mg, err := tool.NewExec(t.Context(), picture.MagickSpec())
	if err != nil {
		t.Fatal(err)
	}
	r, err := picture.NewEXIFReader(picture.Config{Magick: mg, Limits: hostLimits})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestEXIFHost(t *testing.T) {
	t.Parallel()
	r := hostEXIF(t)
	for _, c := range []struct {
		fixture     string
		kind        route.Kind
		capture     time.Time
		model       string
		orientation int
		width       int
		height      int
		lat, lon    float64
		keys        []string
	}{
		{
			"exif-gps.jpg", route.JPEG, time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC), "Fixture One", 6, 48, 32,
			-(33 + 51.0/60 + 35.64/3600), 151 + 12.0/60 + 40.2/3600,
			[]string{"DateTimeOriginal", "GPSAltitude", "GPSAltitudeRef", "ImageDescription", "OffsetTimeOriginal", "PixelXDimension"},
		},
		{
			"exif-zoneless.heic", route.HEIC, time.Date(2023, 12, 31, 23, 59, 58, 0, time.UTC), "Fixture Two", 0, 32, 32,
			51 + 30.0/60 + 15.48/3600, -(7.0/60 + 39.6/3600),
			[]string{"DateTimeOriginal", "GPSLatitude", "GPSLongitudeRef", "Make", "Model"},
		},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("testdata", c.fixture)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := r.Metadata(t.Context(), path, c.kind)
			if err != nil {
				t.Fatal(err)
			}
			if rec == nil || rec.GPS == nil {
				t.Fatalf("record %+v, want one with a GPS position", rec)
			}
			if !rec.CaptureTime.Equal(c.capture) || rec.CaptureTime.Location() != time.UTC || rec.Make != "Docproc" ||
				rec.Model != c.model || rec.Orientation != c.orientation || rec.Width != c.width || rec.Height != c.height {
				t.Errorf("record %+v; want captured %v in UTC by Docproc %s, orientation %d, %dx%d",
					rec, c.capture, c.model, c.orientation, c.width, c.height)
			}
			if !degreesNear(rec.GPS.Latitude, c.lat) || !degreesNear(rec.GPS.Longitude, c.lon) {
				t.Errorf("GPS %+v, want %v, %v", rec.GPS, c.lat, c.lon)
			}
			for _, key := range c.keys {
				if !slices.ContainsFunc(rec.EXIF, func(p metadata.Property) bool { return p.Key == key }) {
					t.Errorf("EXIF %v lacks %s", rec.EXIF, key)
				}
			}
			unchanged(t, path, string(data))
		})
	}
}

func TestEXIFHostNone(t *testing.T) {
	t.Parallel()
	r := hostEXIF(t)
	for name, kind := range map[string]route.Kind{"exif-none.png": route.PNG, "pages.tiff": route.TIFF, "animated.avif": route.AVIF} {
		rec, err := r.Metadata(t.Context(), filepath.Join("testdata", name), kind)
		if rec != nil || err != nil {
			t.Errorf("%s: %+v, %v; want no record", name, rec, err)
		}
	}
	rec, err := r.Metadata(t.Context(), filepath.Join("testdata", "oriented.webp"), route.WebP)
	if err != nil || rec == nil || rec.Orientation != 6 || rec.Width != 48 || rec.Height != 32 {
		t.Errorf("oriented.webp: %+v, %v; want orientation 6, 48x32", rec, err)
	}
}

func TestEXIFHostNotAPicture(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.jpg")
	if err := os.WriteFile(path, []byte("not a JPEG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec, err := hostEXIF(t).Metadata(t.Context(), path, route.JPEG); err == nil {
		t.Errorf("text read as a JPEG: %+v, want magick's error", rec)
	}
}
