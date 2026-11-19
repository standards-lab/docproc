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

// tarTypes are the tar entry types the stage writes no part for, each as inspect names it.
var tarTypes = map[byte]inspect.EntryType{
	tar.TypeSymlink: inspect.SymlinkEntry,
	tar.TypeLink:    inspect.HardlinkEntry,
	tar.TypeChar:    inspect.DeviceEntry,
	tar.TypeBlock:   inspect.DeviceEntry,
	tar.TypeFifo:    inspect.DeviceEntry,
}

// tar reads the tar at p.Path through archive/tar, one entry at a time, in the tar's order,
// yielding each entry's item. A regular file, a sparse one among them, is written by its
// SHA-256 and found; a directory or a global header is no part; any other entry is an unread
// child. A header the reader can't read fails the part, after the entries before it.
func (s *archiveStage) tar(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	f, err := os.Open(p.Path)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return
	}
	defer func() { _ = f.Close() }() // read only: a close error loses nothing
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
			// ErrInsecurePath comes with its header: the name is only ever a name here.
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

// tarEntry returns h's item, its content read from r: its part, written into dir by its
// SHA-256, or its unread child. The bomb rule weighs the bytes written against whole, the tar's
// size, since an entry's bytes come from no more than the whole tar: only a sparse entry's holes,
// read as zeros, can pass it. An error is the stage's own, the directory unwritable or ctx
// ending; an entry's content never fails it.
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

// gzip decompresses the gzip stream at p.Path through compress/gzip, its members concatenated as
// gzip concatenates them, into one part, written into dir by its SHA-256 and found, named by its
// header's name else by the stream's own name's stem, "gzip content <name>". The dispatcher
// routes it again, so a .tar.gz's content is a tar, read by this stage below it. The bomb rule
// weighs the bytes written against the stream's size. A stream whose header doesn't read fails
// the part; content that doesn't decompress, or passes the bomb rule, is an unread child.
func (s *archiveStage) gzip(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	f, err := os.Open(p.Path)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return
	}
	defer func() { _ = f.Close() }() // read only: a close error loses nothing
	zr, err := gzip.NewReader(f)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: gzip: %w: %w", err, dispatch.ErrRefused))
		return
	}
	defer func() { _ = zr.Close() }() // a gzip reader's close reports nothing it hasn't
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
