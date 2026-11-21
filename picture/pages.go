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

	"github.com/standards-lab/docproc/route"
)

func (c *Converter) tiffPages(ctx context.Context, dir string, req Request) ([]string, error) {
	work, err := os.MkdirTemp(dir, "picture-*")
	if err != nil {
		return nil, fmt.Errorf("picture: picture directory: %w", err)
	}
	var written []string
	err = c.convertPages(ctx, dir, work, req, &written)
	if rerr := os.RemoveAll(work); rerr != nil {
		err = errors.Join(err, fmt.Errorf("picture: removing the picture directory: %w", rerr))
	}
	if err != nil {
		for _, png := range written {
			err = errors.Join(err, os.Remove(png))
		}
		return nil, err
	}
	return written, nil
}

func (c *Converter) convertPages(ctx context.Context, dir, work string, req Request, written *[]string) error {
	stem := req.Stem
	s, err := stage(work, 0, Picture{Path: req.Path, Kind: route.TIFF, PNG: filepath.Join(dir, stem+".png")})
	if err != nil {
		return err
	}
	own := filepath.Dir(s.copied)
	if err := c.runMagick(ctx, own, s.name, pageArgs(s, stem), filepath.Join(own, pageOutput(stem, 0))); err != nil {
		return err
	}
	n, err := countPages(own, s.name, stem)
	if err != nil {
		return fmt.Errorf("picture: magick: %s: %w", s.name, err)
	}
	for i := range n {
		png := filepath.Join(dir, pageName(stem, i+1))
		if err := os.Rename(filepath.Join(own, pageOutput(stem, i)), png); err != nil {
			return fmt.Errorf("picture: %s: %w", s.name, err)
		}
		*written = append(*written, png)
	}
	return nil
}

func pageArgs(s staged, stem string) []string {
	return slices.Concat([]string{"tiff:" + s.copied, "+adjoin"}, reproducible, []string{"png:" + stem + pagePattern})
}

// See docs/api.md, "Running the tools".
const pagePattern = "-%d.png"

func pageOutput(stem string, index int) string {
	return stem + "-" + strconv.Itoa(index) + ".png"
}

func pageName(stem string, page int) string {
	return stem + "-page" + strconv.Itoa(page) + ".png"
}

func countPages(own, copied, stem string) (int, error) {
	list, err := os.ReadDir(own)
	if err != nil {
		return 0, err
	}
	output := regexp.MustCompile(`^` + regexp.QuoteMeta(stem) + `-(0|[1-9][0-9]*)\.png$`)
	seen := map[int]bool{}
	for _, e := range list {
		if e.Name() == copied {
			continue
		}
		m := output.FindStringSubmatch(e.Name())
		if m == nil {
			return 0, fmt.Errorf("wrote %s, which isn't a page", e.Name())
		}
		i, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, fmt.Errorf("wrote %s, which isn't a page", e.Name())
		}
		if err := checkPNG(filepath.Join(own, e.Name())); err != nil {
			return 0, err
		}
		seen[i] = true
	}
	for i := range len(seen) {
		if !seen[i] {
			return 0, fmt.Errorf("wrote %d pages without page %d", len(seen), i+1)
		}
	}
	return len(seen), nil
}
