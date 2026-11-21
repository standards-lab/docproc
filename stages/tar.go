package stages

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
)

var tarTypes = map[byte]inspect.EntryType{
	tar.TypeSymlink: inspect.SymlinkEntry,
	tar.TypeLink:    inspect.HardlinkEntry,
	tar.TypeChar:    inspect.DeviceEntry,
	tar.TypeBlock:   inspect.DeviceEntry,
	tar.TypeFifo:    inspect.DeviceEntry,
}

func (s *archiveStage) tar(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	f, err := os.Open(p.Path)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return
	}
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(f)
	for {
		if err := ctx.Err(); err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		h, err := tr.Next()
		switch {
		case errors.Is(err, io.EOF):
			return
		case err != nil && !errors.Is(err, tar.ErrInsecurePath):
			yield(dispatch.Item{}, fmt.Errorf("archive stage: tar: %w: %w", err, dispatch.ErrRefused))
			return
		}
		if h.Typeflag == tar.TypeDir || h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		item, err := s.tarEntry(ctx, h, tr, uint64(p.Size), dir)
		if err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		if !yield(item, nil) {
			return
		}
	}
}

func (s *archiveStage) tarEntry(ctx context.Context, h *tar.Header, r io.Reader, whole uint64, dir string) (dispatch.Item, error) {
	where := "tar entry " + h.Name
	typ, unwritten := tarTypes[h.Typeflag]
	if !unwritten {
		typ = inspect.FileEntry
	}
	if flags := entryFlags(h.Name, typ, h.Linkname, false); len(flags) > 0 {
		return flagged(where, h.Name, typ, flags), nil
	}
	if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeGNUSparse {
		return unread(where, h.Name, dispatch.NoProcessor, fmt.Sprintf("entry type %q has no processor yet", h.Typeflag)), nil
	}
	path, written, why, err := s.extract(ctx, copyFrom(r), dir, whole, func(w *bombWriter, copyErr error) error {
		switch {
		case copyErr != nil:
			return fmt.Errorf("doesn't read: %w", copyErr)
		case int64(w.n) != h.Size:
			return fmt.Errorf("%d bytes written, %d declared", w.n, h.Size)
		}
		return nil
	})
	switch {
	case err != nil:
		return dispatch.Item{}, err
	case errors.Is(why, errBomb):
		return s.bombChild(where, h.Name, fmt.Sprintf("entry %q in a tar, declared %d", h.Name, h.Size), written, whole), nil
	case why != nil:
		return notWritten(where, h.Name, why), nil
	}
	return dispatch.PartItem(dispatch.Found{Path: path, Name: h.Name, Where: where}), nil
}

func (s *archiveStage) gzip(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	f, err := os.Open(p.Path)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: gzip: %w: %w", err, dispatch.ErrRefused))
		return
	}
	defer func() { _ = zr.Close() }()
	name := zr.Name
	if name == "" {
		name = streamStem(p.Name, ".gz", ".tgz")
	}
	where := "gzip content " + name
	compressed := uint64(p.Size)
	path, written, why, err := s.extract(ctx, copyFrom(zr), dir, compressed, func(_ *bombWriter, copyErr error) error {
		if copyErr != nil {
			return fmt.Errorf("doesn't decompress: %w", copyErr)
		}
		return nil
	})
	switch {
	case err != nil:
		yield(dispatch.Item{}, err)
	case errors.Is(why, errBomb):
		yield(s.bombChild(where, name, fmt.Sprintf("content %q", name), written, compressed), nil)
	case why != nil:
		yield(notWritten(where, name, why), nil)
	default:
		yield(dispatch.PartItem(dispatch.Found{Path: path, Name: name, Where: where}), nil)
	}
}
