//go:build linux

package picture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func standinEXIF(t *testing.T, limits picture.Limits, opts ...func(*settings)) (*picture.EXIFReader, *standins) {
	t.Helper()
	st := newStandins(t, "7.1.2-34", opts...)
	r, err := picture.NewEXIFReader(picture.Config{Magick: st.mustTool(picture.MagickSpec()), Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	return r, st
}

func degreesNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestEXIFGoldens(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		fixture, golden string
		kind            route.Kind
		want            *metadata.Picture
		lat, lon        float64
	}{
		{
			"exif-gps.jpg", "exif-gps.magick.txt", route.JPEG,
			&metadata.Picture{
				CaptureTime: time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC), Make: "Docproc", Model: "Fixture One",
				Orientation: 6, Width: 48, Height: 32,
				EXIF: metadata.Properties{
					{Key: "DateTimeOriginal", Value: "2024:05:06 07:08:09"}, {Key: "ExifOffset", Value: "122"},
					{Key: "GPSAltitude", Value: "585/10"}, {Key: "GPSAltitudeRef", Value: "."}, {Key: "GPSInfo", Value: "204"},
					{Key: "GPSLatitude", Value: "33/1,51/1,3564/100"}, {Key: "GPSLatitudeRef", Value: "S"},
					{Key: "GPSLongitude", Value: "151/1,12/1,4020/100"}, {Key: "GPSLongitudeRef", Value: "E"},
					{Key: "ImageDescription", Value: "Harbor, Z..rich"}, {Key: "Make", Value: "Docproc"},
					{Key: "Model", Value: "Fixture One"}, {Key: "OffsetTimeOriginal", Value: "+02:00"},
					{Key: "Orientation", Value: "6"}, {Key: "PixelXDimension", Value: "48"}, {Key: "PixelYDimension", Value: "32"},
				},
			},
			-(33 + 51.0/60 + 35.64/3600), 151 + 12.0/60 + 40.2/3600,
		},
		{
			"exif-zoneless.heic", "exif-zoneless.magick.txt", route.HEIC,
			&metadata.Picture{
				CaptureTime: time.Date(2023, 12, 31, 23, 59, 58, 0, time.UTC), Make: "Docproc", Model: "Fixture Two",
				Width: 32, Height: 32,
				EXIF: metadata.Properties{
					{Key: "DateTimeOriginal", Value: "2023:12:31 23:59:58"}, {Key: "ExifOffset", Value: "82"},
					{Key: "GPSInfo", Value: "120"}, {Key: "GPSLatitude", Value: "51/1,30/1,1548/100"},
					{Key: "GPSLatitudeRef", Value: "N"}, {Key: "GPSLongitude", Value: "0/1,7/1,3960/100"},
					{Key: "GPSLongitudeRef", Value: "W"}, {Key: "Make", Value: "Docproc"}, {Key: "Model", Value: "Fixture Two"},
				},
			},
			51 + 30.0/60 + 15.48/3600, -(7.0/60 + 39.6/3600),
		},
		{"exif-none.png", "exif-none.magick.txt", route.PNG, nil, 0, 0},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			t.Parallel()
			limits := picture.Limits{Area: 1 << 24}
			r, st := standinEXIF(t, limits, func(s *settings) { s.EXIF = golden(t, c.golden) })
			path := filepath.Join("testdata", c.fixture)
			got, err := r.Metadata(t.Context(), path, c.kind)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == nil {
				if got != nil {
					t.Errorf("a picture with no EXIF: %+v, want none", got)
				}
			} else {
				if got == nil || got.GPS == nil || !degreesNear(got.GPS.Latitude, c.lat) || !degreesNear(got.GPS.Longitude, c.lon) {
					t.Fatalf("record %+v; want GPS %v, %v", got, c.lat, c.lon)
				}
				gps := *got
				gps.GPS = nil
				if !reflect.DeepEqual(&gps, c.want) {
					t.Errorf("record\n %+v\nwant\n %+v", &gps, c.want)
				}
				if got.CaptureTime.Location() != time.UTC {
					t.Errorf("capture time %v isn't in UTC", got.CaptureTime)
				}
			}
			runs := st.all()
			want := []string{"-limit", "area", "16777216", string(c.kind) + ":-[0]", "-format", picture.EXIFFormat, "info:"}
			if len(runs) != 1 || !slices.Equal(runs[0].Args, want) || runs[0].Input != inputSum([]byte(golden(t, c.fixture))) {
				t.Errorf("runs %+v; want one run %q reading the picture from its standard input", runs, want)
			}
		})
	}
}

func TestEXIFKindsWithout(t *testing.T) {
	t.Parallel()
	r, st := standinEXIF(t, picture.Limits{})
	for _, kind := range []route.Kind{route.GIF, route.BMP, route.SVG, route.EMF, route.WMF} {
		got, err := r.Metadata(t.Context(), filepath.Join("testdata", "missing"), kind)
		if got != nil || err != nil {
			t.Errorf("%s: %+v, %v; want none", kind, got, err)
		}
	}
	if runs := st.all(); len(runs) != 0 {
		t.Errorf("runs %+v, want none", runs)
	}
}

func TestEXIFContinuedValue(t *testing.T) {
	t.Parallel()
	out := "8 6\nexif:ImageDescription=first\nsecond\nexif:Make= Docproc \nexif:Orientation=12\n"
	r, _ := standinEXIF(t, picture.Limits{}, func(s *settings) { s.EXIF = out })
	got, err := r.Metadata(t.Context(), filepath.Join("testdata", "exif-gps.jpg"), route.JPEG)
	if err != nil {
		t.Fatal(err)
	}
	want := &metadata.Picture{
		Make: "Docproc", Width: 8, Height: 6,
		EXIF: metadata.Properties{
			{Key: "ImageDescription", Value: "first\nsecond"}, {Key: "Make", Value: " Docproc "}, {Key: "Orientation", Value: "12"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("record\n %+v\nwant\n %+v", got, want)
	}
}

func TestEXIFFails(t *testing.T) {
	t.Parallel()
	jpeg := filepath.Join("testdata", "exif-gps.jpg")
	r, _ := standinEXIF(t, picture.Limits{}, func(s *settings) {
		s.Fail = "magick: insufficient image data in file `/tmp/magick-x' @ error/jpeg.c/ReadOneJPEGImage/1555.\n"
	})
	_, err := r.Metadata(t.Context(), jpeg, route.JPEG)
	var exit *tool.ExitError
	if !errors.As(err, &exit) || !strings.Contains(err.Error(), "exif-gps.jpg's EXIF") || !strings.Contains(err.Error(), "insufficient image data") {
		t.Errorf("a failed run: %v; want its exit, naming the picture, with magick's message", err)
	}

	r, _ = standinEXIF(t, picture.Limits{}, func(s *settings) { s.Flood = true })
	if _, err := r.Metadata(t.Context(), jpeg, route.JPEG); !errors.Is(err, picture.ErrMetadata) {
		t.Errorf("output past its cap: %v, want ErrMetadata", err)
	}

	r, _ = standinEXIF(t, picture.Limits{}, func(s *settings) { s.EXIF = "exif:Make=Docproc\n" })
	if _, err := r.Metadata(t.Context(), jpeg, route.JPEG); !errors.Is(err, picture.ErrMetadata) {
		t.Errorf("output without a size: %v, want ErrMetadata", err)
	}

	if _, err := r.Metadata(t.Context(), filepath.Join("testdata", "missing.jpg"), route.JPEG); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing picture: %v, want ErrNotExist", err)
	}
}

func TestNewEXIFReader(t *testing.T) {
	t.Parallel()
	old := newStandins(t, "7.0.11-14")
	lenient := picture.MagickSpec()
	lenient.Minimum = "6"
	var verr *tool.VersionError
	_, err := picture.NewEXIFReader(picture.Config{Magick: old.mustTool(lenient)})
	if !errors.As(err, &verr) || verr.Minimum != picture.MagickMinimum || !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewEXIFReader over magick 7.0.11-14 returned %v, want a VersionError of minimum %s", err, picture.MagickMinimum)
	}
	current := newStandins(t, "7.1.2-34")
	if _, err := picture.NewEXIFReader(picture.Config{Magick: current.mustTool(picture.MagickSpec()), Limits: picture.Limits{Map: -1}}); !errors.Is(err, picture.ErrRequest) {
		t.Errorf("a negative limit: %v, want ErrRequest", err)
	}
	if runs := current.all(); len(runs) != 0 {
		t.Errorf("NewEXIFReader ran %+v, want nothing", runs)
	}
	func() {
		want := "picture: Config.Magick is nil"
		defer func() {
			if r := recover(); r != want {
				t.Errorf("NewEXIFReader panicked with %v, want %q", r, want)
			}
		}()
		_, _ = picture.NewEXIFReader(picture.Config{})
		t.Errorf("NewEXIFReader returned, want a panic %q", want)
	}()
}

func TestGPSDegrees(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		value, ref string
		lat        bool
		want       float64
		ok         bool
	}{
		{"33/1,51/1,3564/100", "S", true, -(33 + 51.0/60 + 35.64/3600), true},
		{"40/1,26/1,4614/100", "N", true, 40 + 26.0/60 + 46.14/3600, true},
		{"151/1,12/1,4020/100", "E", false, 151 + 12.0/60 + 40.2/3600, true},
		{"0/1,7/1,3960/100", "W", false, -(7.0/60 + 39.6/3600), true},
		{"4061/100,0/1,0/1", "N", true, 40.61, true},
		{"90/1,0/1,0/1", "S", true, -90, true},
		{"180/1,0/1,0/1", "W", false, -180, true},
		{"90/1,0/1,1/1", "N", true, 0, false},
		{"180/1,1/1,0/1", "E", false, 0, false},
		{"33/1,51/1,3564/100", "", true, 0, false},
		{"33/1,51/1,3564/100", "E", true, 0, false},
		{"151/1,12/1,4020/100", "N", false, 0, false},
		{"33/1,51/1", "S", true, 0, false},
		{"33/1,51/0,0/1", "S", true, 0, false},
		{"33,51,35", "S", true, 0, false},
		{"-33/1,0/1,0/1", "S", true, 0, false},
		{"", "N", true, 0, false},
	} {
		positive, negative, most := "E", "W", 180.0
		if c.lat {
			positive, negative, most = "N", "S", 90
		}
		got, ok := picture.GPSDegrees(c.value, c.ref, positive, negative, most)
		if ok != c.ok || !degreesNear(got, c.want) {
			t.Errorf("%q %q: %v, %t; want %v, %t", c.value, c.ref, got, ok, c.want, c.ok)
		}
	}
}

func TestCaptureTime(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		value, offset string
		want          time.Time
	}{
		{"2024:05:06 07:08:09", "+02:00", time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC)},
		{"2024:05:06 07:08:09", "-05:30", time.Date(2024, 5, 6, 12, 38, 9, 0, time.UTC)},
		{"2024:05:06 07:08:09", "", time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)},
		{"2024:05:06 07:08:09", "local", time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)},
		{"2024:05:06 07:08:09", "+15:00", time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)},
		{"2024:01:01 00:30:00", "+01:00", time.Date(2023, 12, 31, 23, 30, 0, 0, time.UTC)},
		{"0000:00:00 00:00:00", "", time.Time{}},
		{"    :  :     :  :  ", "", time.Time{}},
		{"2024-05-06T07:08:09", "", time.Time{}},
		{"", "+02:00", time.Time{}},
	} {
		got := picture.CaptureTime(c.value, c.offset)
		if !got.Equal(c.want) || (!got.IsZero() && got.Location() != time.UTC) {
			t.Errorf("%q %q: %v, want %v in UTC", c.value, c.offset, got, c.want)
		}
	}
}

var fixtureRow = regexp.MustCompile("^\\| `([^`]+)` \\|.*\\| (\\d+) \\| `([0-9a-f]{64})` \\|$")

func TestFixtureSums(t *testing.T) {
	t.Parallel()
	readme := golden(t, "README.md")
	listed := map[string]bool{"README.md": true}
	for line := range strings.Lines(readme) {
		m := fixtureRow.FindStringSubmatch(strings.TrimRight(line, "\n"))
		if m == nil {
			continue
		}
		name, size, sum := m[1], m[2], m[3]
		listed[name] = true
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Error(err)
			continue
		}
		if got := strconv.Itoa(len(data)); got != size {
			t.Errorf("%s: %s bytes, the README records %s", name, got, size)
		}
		if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: SHA-256 %x, the README records %s", name, got, sum)
		}
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !listed[e.Name()] {
			t.Errorf("%s is in testdata and not in its README's table", e.Name())
		}
	}
}
