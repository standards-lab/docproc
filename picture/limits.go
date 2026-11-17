package picture

import (
	"fmt"
	"strconv"
)

// Limits are ImageMagick's resource limits, which every magick run a Converter makes carries as
// -limit options before anything else, its format listing included, so magick refuses a picture
// past them before it decodes the picture whole. Each is the caller's: the package sets none of
// its own, and a zero field passes no -limit, leaving magick's own default and its policy.xml's.
// Every field is in pixels or bytes, never negative.
type Limits struct {
	// Area is the most pixels one image may hold in magick's pixel cache: -limit area.
	Area int64
	// Width and Height are the widest and tallest an image may be, in pixels: -limit width and
	// -limit height.
	Width, Height int64
	// Memory is the most bytes of heap the pixel cache may take: -limit memory. magick spills the
	// cache to a memory map, then to disk, past it.
	Memory int64
	// Map is the most bytes of the pixel cache that may be memory-mapped: -limit map.
	Map int64
	// Disk is the most bytes of the pixel cache that may be on disk, in magick's temporary
	// directory: -limit disk. A picture past every one of Memory, Map, and Disk fails.
	Disk int64
}

// args returns l as magick's -limit options, in a fixed order, a zero field leaving its option
// out, or an error wrapping ErrRequest naming a negative field.
func (l Limits) args() ([]string, error) {
	var args []string
	for _, f := range []struct {
		name  string
		value int64
	}{
		{"area", l.Area},
		{"width", l.Width},
		{"height", l.Height},
		{"memory", l.Memory},
		{"map", l.Map},
		{"disk", l.Disk},
	} {
		switch {
		case f.value < 0:
			return nil, fmt.Errorf("picture: magick's %s limit %d is negative: %w", f.name, f.value, ErrRequest)
		case f.value > 0:
			args = append(args, "-limit", f.name, strconv.FormatInt(f.value, 10))
		}
	}
	return args, nil
}
