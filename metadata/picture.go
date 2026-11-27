package metadata

import (
	"time"

	"github.com/standards-lab/docproc/dispatch"
)

// Picture is a picture's metadata record, one JSON object under [dispatch.MediaPictureMetadata],
// which the picture and vision stages yield as a record output named "metadata", before the
// picture's PNGs: what ImageMagick's magick reads of the EXIF the picture holds, from its first
// image, a HEIC's primary image or a TIFF's first page. A picture that holds no EXIF yields no
// record. Its fields:
//
//   - "capture_time": EXIF DateTimeOriginal, in UTC: read in the zone EXIF OffsetTimeOriginal
//     states, or as UTC when the picture states none; absent when the picture lacks it or its
//     value isn't a date and time.
//   - "make" and "model": EXIF Make and Model, the camera's maker and model, each its text as
//     magick prints it, without surrounding spaces; each absent when the picture lacks it or its
//     text is empty.
//   - "orientation": EXIF Orientation as magick holds it once the picture is read, from 1, upright,
//     to 8; absent when the picture lacks it or its value isn't one. libheif turns a HEIC or an AVIF
//     upright as it reads it, so magick holds such a picture's orientation as 1.
//   - "width" and "height": the picture's size in pixels as magick reads it, before its EXIF
//     orientation is applied; a HEIC's or an AVIF's after libheif's turn; each absent when magick
//     gives none.
//   - "gps": the position EXIF GPSLatitude, GPSLatitudeRef, GPSLongitude, and GPSLongitudeRef
//     state, a [GPS]; absent unless all four are present and each is a position.
//   - "exif": every EXIF key and value magick prints, each a string as magick prints it, in
//     magick's order, which sorts the keys, by its key without magick's "exif:" prefix: a rational
//     as numerator/denominator, a list's values separated by commas, and a byte magick can't print
//     as ".", a non-ASCII character in a text among them; always present.
type Picture struct {
	CaptureTime time.Time  `json:"capture_time,omitzero"`
	Make        string     `json:"make,omitzero"`
	Model       string     `json:"model,omitzero"`
	Orientation int        `json:"orientation,omitzero"`
	Width       int        `json:"width,omitzero"`
	Height      int        `json:"height,omitzero"`
	GPS         *GPS       `json:"gps,omitzero"`
	EXIF        Properties `json:"exif"`
}

// MediaType returns the record's media type, [dispatch.MediaPictureMetadata].
func (Picture) MediaType() string { return dispatch.MediaPictureMetadata }

// GPS is a [Picture]'s position, in decimal degrees on the datum the picture states, from EXIF's
// degrees, minutes, and seconds. Its altitude isn't read: magick prints GPSAltitudeRef, the byte
// that says whether an altitude is above or below sea level, as "." either way, so the altitude
// is in "exif" alone. Its fields:
//
//   - "latitude": the latitude, from -90 to 90, negative south of the equator; always present.
//   - "longitude": the longitude, from -180 to 180, negative west of the prime meridian; always
//     present.
type GPS struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}
