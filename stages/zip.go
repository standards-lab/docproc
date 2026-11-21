package stages

import (
	"archive/zip"
	"compress/bzip2"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
)

const zipBzip2 uint16 = 12

var zipDecompressors = map[uint16]zip.Decompressor{
	zip.Deflate: flate.NewReader,
	zipBzip2:    func(r io.Reader) io.ReadCloser { return io.NopCloser(bzip2.NewReader(r)) },
}

func (s *archiveStage) zip(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	zr, err := zip.OpenReader(p.Path)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		if _, ok := errors.AsType[*fs.PathError](err); !ok {
			err = fmt.Errorf("%w: %w", err, dispatch.ErrRefused)
		}
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return
	}
	defer func() { _ = zr.Close() }()
	files, encrypted := 0, 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		if f.Mode().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		files++
		if f.Flags&0x1 != 0 {
			encrypted++
		}
		item, err := s.zipEntry(ctx, p.Path, f, dir)
		if err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		if !yield(item, nil) {
			return
		}
	}
	if encrypted > 0 {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %d of %d entries: %w", encrypted, files, dispatch.ErrEncrypted))
	}
}

func (s *archiveStage) zipEntry(ctx context.Context, archive string, f *zip.File, dir string) (dispatch.Item, error) {
	where := "zip entry " + f.Name
	typ := inspect.FileEntry
	if f.Mode()&fs.ModeSymlink != 0 {
		typ = inspect.SymlinkEntry
	}
	if flags := entryFlags(f.Name, typ, "", f.Flags&0x1 != 0); len(flags) > 0 {
		return flagged(where, f.Name, typ, flags), nil
	}
	path, written, why, err := s.zipWrite(ctx, archive, f, dir)
	switch {
	case err != nil:
		return dispatch.Item{}, err
	case errors.Is(why, errBomb):
		return s.bombChild(where, f.Name, fmt.Sprintf("entry %q, declared %d", f.Name, f.UncompressedSize64), written, f.CompressedSize64), nil
	case why != nil:
		return notWritten(where, f.Name, why), nil
	}
	return dispatch.PartItem(dispatch.Found{Path: path, Name: f.Name, Where: where}), nil
}

func (s *archiveStage) zipWrite(ctx context.Context, archive string, f *zip.File, dir string) (path string, written uint64, why, err error) {
	var fill func(io.Writer) error
	switch decompress, standard := zipDecompressors[f.Method]; {
	case f.Method == zip.Store || standard:
		raw, err := f.OpenRaw()
		if err != nil {
			return "", 0, fmt.Errorf("unreadable: %w", err), nil
		}
		r := raw
		if standard {
			rc := decompress(raw)
			defer func() { _ = rc.Close() }()
			r = rc
		}
		fill = copyFrom(r)
	default:
		fill = func(w io.Writer) error {
			return s.sevenZip.Extract(ctx, sevenzip.ExtractRequest{Path: archive, Kind: route.ZIP, Name: f.Name}, w)
		}
	}
	return s.extract(ctx, fill, dir, f.CompressedSize64, func(w *bombWriter, fillErr error) error {
		switch {
		case fillErr != nil:
			return fmt.Errorf("doesn't decompress: %w", fillErr)
		case w.n != f.UncompressedSize64:
			return fmt.Errorf("%d bytes written, %d declared", w.n, f.UncompressedSize64)
		case f.CRC32 != 0 && w.crc.Sum32() != f.CRC32:
			return fmt.Errorf("CRC-32 %08x, %08x declared", w.crc.Sum32(), f.CRC32)
		}
		return nil
	})
}
