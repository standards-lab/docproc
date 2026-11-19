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

// zipBzip2 is the ZIP compression method of a bzip2 entry, 12, which archive/zip names no
// constant for.
const zipBzip2 uint16 = 12

// zipDecompressors are the archive/zip decompressors the stage reads a ZIP entry's compressed
// bytes through, by method: archive/zip's own deflate, and compress/bzip2 for bzip2. They are the
// stage's, applied to an entry's raw bytes, so its own rules hold the bytes to the central
// directory; none is registered with archive/zip, whose registry is the process's. A stored
// entry's bytes need none, and an entry of any other method is read through 7-Zip.
var zipDecompressors = map[uint16]zip.Decompressor{
	zip.Deflate: flate.NewReader,
	zipBzip2:    func(r io.Reader) io.ReadCloser { return io.NopCloser(bzip2.NewReader(r)) },
}

// zip reads the ZIP at p.Path through archive/zip, one entry at a time, yielding each entry's
// item.
func (s *archiveStage) zip(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	zr, err := zip.OpenReader(p.Path)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		// A file that won't open is the run's; a central directory that won't read, the content's.
		if _, ok := errors.AsType[*fs.PathError](err); !ok {
			err = fmt.Errorf("%w: %w", err, dispatch.ErrRefused)
		}
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return
	}
	defer func() { _ = zr.Close() }() // read only: a close error loses nothing
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

// zipEntry returns f's item, of the ZIP at archive: its part, written into dir by its SHA-256, or
// its unread child. An error is the stage's own, the directory unwritable or ctx ending; an
// entry's content never fails it.
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

// zipWrite writes f's content into dir by extract, and holds the bytes written to the central
// directory's size and CRC-32. A stored entry's raw bytes are its content, and a deflated or
// bzip2 entry's are decompressed by its zipDecompressors entry; any other method's entry is
// extracted alone from archive, the ZIP's file, by 7-Zip, to its standard output. why is the
// reason the content has no file, errBomb, a mismatch with the central directory, or content
// that doesn't decompress, its partial file removed; err is the stage's own failure.
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
			defer func() { _ = rc.Close() }() // a decompressor's close frees it; it reports nothing
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
			// archive/zip checks a CRC-32 the central directory declares, none when it declares 0.
			return fmt.Errorf("CRC-32 %08x, %08x declared", w.crc.Sum32(), f.CRC32)
		}
		return nil
	})
}
