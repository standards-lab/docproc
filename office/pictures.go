package office

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

// convertKinds are the kinds of picture the picture package converts within a document, by the
// stored file's signature: a metafile, EMF or WMF, rendered through LibreOffice and trimmed
// through ImageMagick, and a TIFF through ImageMagick. Every other picture is handed on as
// stored, an SVG among them, since an Office application stores a PNG beside the SVG it shows.
var convertKinds = map[route.Kind]bool{route.EMF: true, route.WMF: true, route.TIFF: true}

// convertPictures converts the pictures among media, the document's media resolved in the media
// directory of a conversion whose working directory is dir, to PNG, as the picture standard says,
// through the picture package's ConvertBatch, in one batch, metafiles rendered at scale: each EMF
// and WMF rendered through LibreOffice, all of them in one soffice run, and trimmed to the
// picture through magick, and each TIFF through magick. Only a Picture medium with a file is
// converted, by its stored file's signature; an embedded package and an OLE object are handed on
// as stored. A converted picture's PNG is named by its stored file's stem, the SHA-256, and .png,
// and written into the media directory beside the stored file, which stays; its media's Path
// becomes the PNG and Converted true. A picture whose converter the Converter wasn't given, for a
// metafile either of LibreOffice and magick, is handed on as stored. A file named by several media,
// or two stored files of the same bytes under two extensions, is converted once. A conversion that
// fails is an error.
func (c *Converter) convertPictures(ctx context.Context, dir string, scale int, media []Medium) error {
	if c.magick == nil {
		return nil
	}
	var batch []picture.Picture
	// converted holds each PNG's media, and kept each stored file handed on as it is.
	converted := map[string][]int{}
	kept := map[string]bool{}
	for i, m := range media {
		if m.Kind != Picture || m.Path == "" || kept[m.Path] {
			continue
		}
		png := filepath.Join(filepath.Dir(m.Path), strings.TrimSuffix(m.File, filepath.Ext(m.File))+".png")
		if _, ok := converted[png]; ok {
			converted[png] = append(converted[png], i)
			continue
		}
		kind, err := identify(m.Path)
		if err != nil {
			return err
		}
		if !convertKinds[kind] || kind != route.TIFF && c.libreoffice == nil {
			kept[m.Path] = true
			continue
		}
		batch = append(batch, picture.Picture{Path: m.Path, Kind: kind, PNG: png})
		converted[png] = append(converted[png], i)
	}
	if len(batch) == 0 {
		return nil
	}
	if err := c.pictures.ConvertBatch(ctx, picture.Batch{Dir: dir, MetafileScale: scale, Pictures: batch}); err != nil {
		return fmt.Errorf("office: %w", err)
	}
	for png, indexes := range converted {
		for _, i := range indexes {
			media[i].Path, media[i].Converted = png, true
		}
	}
	return nil
}

// identify returns the kind the signature of the file at path names, reading its first
// route.HeadLen bytes.
func identify(path string) (route.Kind, error) {
	f, err := os.Open(path)
	if err != nil {
		return route.Unknown, fmt.Errorf("office: %w", err)
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, route.HeadLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return route.Unknown, fmt.Errorf("office: %w", err)
	}
	return route.Identify(head[:n]), nil
}

// linkSwaps returns the replacer that swaps each converted picture's stored name, as media name
// it, for its PNG's: nil when none was converted. The longer of two names that begin alike, such
// as a stored .tif and .tiff of the same bytes, is swapped first, and a swapped-in name is never
// swapped again.
func linkSwaps(media []Medium) *strings.Replacer {
	seen := map[string]bool{}
	var swaps [][2]string
	for _, m := range media {
		if m.Converted && !seen[m.File] {
			seen[m.File] = true
			swaps = append(swaps, [2]string{m.File, filepath.Base(m.Path)})
		}
	}
	if len(swaps) == 0 {
		return nil
	}
	slices.SortStableFunc(swaps, func(a, b [2]string) int { return cmp.Compare(len(b[0]), len(a[0])) })
	var pairs []string
	for _, s := range swaps {
		pairs = append(pairs, s[0], s[1])
	}
	return strings.NewReplacer(pairs...)
}

// rewriteLinks returns md, a record's Markdown, with each converted picture's stored name swapped
// for its PNG's wherever it appears, through swaps, linkSwaps' replacer; it parses no Markdown. A
// nil swaps leaves md as it is.
func rewriteLinks(md string, swaps *strings.Replacer) string {
	if swaps == nil || md == "" {
		return md
	}
	return swaps.Replace(md)
}
