package picture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// ErrMetadata is the cause of a Metadata whose magick output isn't read: past 1 MiB, or with no
// first line giving the picture's size.
var ErrMetadata = errors.New("magick's EXIF output isn't read")

// EXIFReader reads a picture's EXIF metadata through ImageMagick's magick: one magick run for
// each picture, which reads the picture's first image from its standard input and decodes it, and
// writes nothing. Every run carries its Config's Limits. Its methods are safe for concurrent use.
type EXIFReader struct {
	magick *tool.Tool
	limits []string
}

// NewEXIFReader builds an EXIFReader over cfg's Magick and Limits; it reads no other field. A nil
// Magick is the caller's mistake, and panics naming the field; a negative limit is an error
// matching ErrRequest. It refuses a Magick older than MagickMinimum, whatever minimum the Tool's
// own Spec declared, with a *tool.VersionError wrapping tool.ErrTooOld. It runs nothing.
func NewEXIFReader(cfg Config) (*EXIFReader, error) {
	if cfg.Magick == nil {
		panic("picture: Config.Magick is nil")
	}
	limits, err := cfg.Limits.args()
	if err != nil {
		return nil, err
	}
	if err := cfg.Magick.AtLeast(MagickMinimum); err != nil {
		return nil, err
	}
	return &EXIFReader{magick: cfg.Magick, limits: limits}, nil
}

var exifCoders = map[route.Kind]string{
	route.JPEG: "jpeg",
	route.PNG:  "png",
	route.TIFF: "tiff",
	route.HEIC: "heic",
	route.WebP: "webp",
	route.AVIF: "avif",
}

// See docs/api.md, "Running the tools".
const exifFormat = "%w %h\n%[EXIF:*]"

const exifOutputLimit = 1 << 20

// Metadata returns the EXIF metadata of the picture at path, read as kind, as a metadata.Picture,
// or nil when the picture holds no EXIF. A JPEG, PNG, TIFF, HEIC, WebP, or AVIF is read through
// the coder its kind names, whatever its content, its first image alone: a TIFF's first page, a
// HEIC's primary image, a WebP's or AVIF's first frame. A GIF, BMP, SVG, EMF, or WMF holds no EXIF
// magick reads, and returns nil with nothing run. The picture is read from magick's standard
// input, so magick reads no part of its path as a format, a frame, or a pattern, and is never
// changed.
//
// magick prints the picture's width and height, then each EXIF property on a line of its own,
// "exif:", its key, "=", and its value; a line that doesn't begin so continues the value before
// it, so a value holding a line break reads whole. The record keeps every property as printed, and
// types the capture time, the camera's make and model, the orientation, and the GPS position from
// them, as metadata.Picture says. A run that fails is an error naming the picture's file, with the
// first line of magick's message; output past 1 MiB, or with no size on its first line, an error
// wrapping ErrMetadata. When ctx ends the run, the error matches ctx's.
func (r *EXIFReader) Metadata(ctx context.Context, path string, kind route.Kind) (*metadata.Picture, error) {
	coder, ok := exifCoders[kind]
	if !ok {
		return nil, nil
	}
	name := filepath.Base(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("picture: %w", err)
	}
	defer func() { _ = f.Close() }()
	out := &cappedWriter{limit: exifOutputLimit}
	stderr := &cli.LimitedBuffer{Limit: magickOutputLimit}
	err = r.magick.Run(ctx, tool.Invocation{
		Args:   slices.Concat(r.limits, []string{coder + ":-[0]", "-format", exifFormat, "info:"}),
		Stdin:  f,
		Stdout: out,
		Stderr: stderr,
	})
	switch {
	case out.over:
		return nil, fmt.Errorf("picture: magick: %s's EXIF: output past %d bytes: %w", name, exifOutputLimit, ErrMetadata)
	case err != nil:
		err = fmt.Errorf("picture: magick: %s's EXIF: %w", name, err)
		if msg := firstLine(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	rec, err := parseEXIF(out.b.String())
	if err != nil {
		return nil, fmt.Errorf("picture: magick: %s's EXIF: %w", name, err)
	}
	return rec, nil
}

type cappedWriter struct {
	b     strings.Builder
	limit int
	over  bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.b.Len()+len(p) > w.limit {
		w.over = true
		return 0, fmt.Errorf("output past %d bytes", w.limit)
	}
	return w.b.Write(p)
}

var exifLine = regexp.MustCompile(`^exif:([^=\s]+)=(.*)$`)

func parseEXIF(out string) (*metadata.Picture, error) {
	size, rest, _ := strings.Cut(out, "\n")
	width, height, ok := pictureSize(size)
	if !ok {
		return nil, fmt.Errorf("first line %q isn't a size: %w", size, ErrMetadata)
	}
	var keys, values []string
	for line := range strings.Lines(rest) {
		line = strings.TrimSuffix(line, "\n")
		if m := exifLine.FindStringSubmatch(line); m != nil {
			keys, values = append(keys, m[1]), append(values, m[2])
			continue
		}
		if n := len(values); n > 0 {
			values[n-1] += "\n" + line
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	rec := &metadata.Picture{Width: width, Height: height, EXIF: make(metadata.Properties, len(keys))}
	stored := make(map[string]string, len(keys))
	for i, key := range keys {
		rec.EXIF[i] = metadata.Property{Key: key, Value: values[i]}
		stored[key] = values[i]
	}
	rec.CaptureTime = captureTime(stored["DateTimeOriginal"], stored["OffsetTimeOriginal"])
	rec.Make, rec.Model = strings.TrimSpace(stored["Make"]), strings.TrimSpace(stored["Model"])
	if o, err := strconv.Atoi(strings.TrimSpace(stored["Orientation"])); err == nil && o >= 1 && o <= 8 {
		rec.Orientation = o
	}
	lat, latOK := gpsDegrees(stored["GPSLatitude"], stored["GPSLatitudeRef"], "N", "S", 90)
	lon, lonOK := gpsDegrees(stored["GPSLongitude"], stored["GPSLongitudeRef"], "E", "W", 180)
	if latOK && lonOK {
		rec.GPS = &metadata.GPS{Latitude: lat, Longitude: lon}
	}
	return rec, nil
}

func pictureSize(line string) (int, int, bool) {
	w, h, ok := strings.Cut(strings.TrimSpace(line), " ")
	if !ok {
		return 0, 0, false
	}
	width, err := strconv.Atoi(w)
	if err != nil || width < 0 {
		return 0, 0, false
	}
	height, err := strconv.Atoi(h)
	if err != nil || height < 0 {
		return 0, 0, false
	}
	return width, height, true
}

const exifDateTime = "2006:01:02 15:04:05"

var exifOffset = regexp.MustCompile(`^([+-])(\d{2}):(\d{2})$`)

func captureTime(value, offset string) time.Time {
	zone := time.UTC
	if m := exifOffset.FindStringSubmatch(strings.TrimSpace(offset)); m != nil {
		hours, _ := strconv.Atoi(m[2])
		minutes, _ := strconv.Atoi(m[3])
		seconds := hours*3600 + minutes*60
		if m[1] == "-" {
			seconds = -seconds
		}
		if hours <= 14 && minutes < 60 {
			zone = time.FixedZone(m[0], seconds)
		}
	}
	t, err := time.ParseInLocation(exifDateTime, strings.TrimSpace(value), zone)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func gpsDegrees(value, ref, positive, negative string, most float64) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 3 {
		return 0, false
	}
	var dms [3]float64
	for i, part := range parts {
		r, ok := rational(part)
		if !ok {
			return 0, false
		}
		dms[i] = r
	}
	degrees := dms[0] + dms[1]/60 + dms[2]/3600
	if degrees > most {
		return 0, false
	}
	switch strings.TrimSpace(ref) {
	case positive:
		return degrees, true
	case negative:
		return -degrees, true
	}
	return 0, false
}

func rational(s string) (float64, bool) {
	n, d, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok {
		return 0, false
	}
	num, err := strconv.ParseUint(n, 10, 32)
	if err != nil {
		return 0, false
	}
	den, err := strconv.ParseUint(d, 10, 32)
	if err != nil || den == 0 {
		return 0, false
	}
	return float64(num) / float64(den), true
}
