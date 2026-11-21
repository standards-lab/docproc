package picture

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// ErrNoReader is the cause of the error a conversion returns for a HEIC, WebP, or AVIF when the
// Converter's magick has no reader for it: its format listing, read when the Converter was built,
// doesn't mark the format readable, as when ImageMagick was built without libheif or libwebp, or
// libheif without an AV1 decoder. Nothing runs for it.
var ErrNoReader = errors.New("no magick reader")

var rasterFormats = map[route.Kind]string{
	route.HEIC: "HEIC",
	route.WebP: "WEBP",
	route.AVIF: "AVIF",
}

func raster(kind route.Kind) bool {
	_, ok := rasterFormats[kind]
	return ok
}

// See docs/api.md, "Running the tools".
var listFormatArgs = []string{"-list", "format", "null:"}

const listFormatLimit = 1 << 20

var formatLine = regexp.MustCompile(`^\s*([A-Z0-9-]+)\*?\s+\S+\s+([r-])[w-][+-](?:\s|$)`)

func listReaders(ctx context.Context, magick *tool.Tool, limits []string) (map[route.Kind]bool, error) {
	out := &cli.LimitedBuffer{Limit: listFormatLimit}
	var stderr strings.Builder
	if err := magick.Run(ctx, tool.Invocation{Args: slices.Concat(limits, listFormatArgs), Stdout: out, Stderr: &stderr}); err != nil {
		err = fmt.Errorf("picture: magick's format listing: %w", err)
		if msg := firstLine(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return parseReaders(out.String()), nil
}

func parseReaders(listing string) map[route.Kind]bool {
	readable := map[string]bool{}
	for line := range strings.Lines(listing) {
		if m := formatLine.FindStringSubmatch(line); m != nil && m[2] == "r" {
			readable[m[1]] = true
		}
	}
	readers := map[route.Kind]bool{}
	for kind, format := range rasterFormats {
		if readable[format] {
			readers[kind] = true
		}
	}
	return readers
}

func rasterArgs(s staged) []string {
	coder := strings.ToLower(rasterFormats[s.Kind])
	return slices.Concat([]string{coder + ":" + s.copied + "[0]", "-auto-orient"}, reproducible, []string{"png:" + s.png})
}
