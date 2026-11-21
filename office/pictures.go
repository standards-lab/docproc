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

var convertKinds = map[route.Kind]bool{route.EMF: true, route.WMF: true, route.TIFF: true}

func (c *Converter) convertPictures(ctx context.Context, dir string, scale int, media []Medium) error {
	var batch []picture.Picture
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
		if !convertKinds[kind] {
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

func rewriteLinks(md string, swaps *strings.Replacer) string {
	if swaps == nil || md == "" {
		return md
	}
	return swaps.Replace(md)
}
