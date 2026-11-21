package stages

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
)

type sevenZipKind struct {
	what  string
	where string
}

var sevenZipKinds = map[route.Kind]sevenZipKind{
	route.SevenZip: {what: "a 7z archive", where: "7z entry "},
	route.MSCF:     {what: "a cabinet", where: "cabinet entry "},
	route.Zstd:     {what: "a zstd stream", where: "zstd content "},
}

func listedType(e sevenzip.Entry) (inspect.EntryType, string) {
	switch {
	case e.Folder:
		return inspect.DirEntry, ""
	case e.HardLink != "":
		return inspect.HardlinkEntry, e.HardLink
	case e.IsSymlink():
		return inspect.SymlinkEntry, e.Symlink
	case e.IsDevice():
		return inspect.DeviceEntry, ""
	}
	return inspect.FileEntry, ""
}

func (s *archiveStage) listedChecks(p dispatch.Part, entries []inspect.Listed, yield func(dispatch.Item, error) bool) bool {
	rep, err := inspect.CheckListing(p.ContentType.Kind, p.Size, entries, s.opts)
	if err != nil {
		yield(dispatch.Item{}, fmt.Errorf("archive stage: %w", err))
		return false
	}
	for _, f := range rep.Flags {
		if !yield(dispatch.FlagItem(f), nil) {
			return false
		}
	}
	return true
}

func (s *archiveStage) list(ctx context.Context, p dispatch.Part, k sevenZipKind) (sevenzip.Listing, error) {
	l, err := s.sevenZip.List(ctx, sevenzip.ListRequest{Path: p.Path, Kind: p.ContentType.Kind, Entries: s.opts.Entries})
	switch {
	case errors.Is(err, sevenzip.ErrEncrypted):
		return l, fmt.Errorf("archive stage: %s whose headers are encrypted: %w: %w", k.what, dispatch.ErrEncrypted, err)
	case err != nil:
		return l, partErr(ctx, fmt.Errorf("archive stage: %w", err), s.memory)
	}
	return l, nil
}

func (s *archiveStage) listed(ctx context.Context, p dispatch.Part, dir string, k sevenZipKind, yield func(dispatch.Item, error) bool) {
	l, err := s.list(ctx, p, k)
	if err != nil {
		yield(dispatch.Item{}, err)
		return
	}
	checked := make([]inspect.Listed, len(l.Entries))
	for i, e := range l.Entries {
		typ, link := listedType(e)
		checked[i] = inspect.Listed{Name: e.Path, Type: typ, Link: link, Declared: e.Size, Sized: e.Sized, Encrypted: e.Encrypted}
	}
	if !s.listedChecks(p, checked, yield) {
		return
	}
	files, encrypted := 0, 0
	for read, err := range s.sevenZip.Read(ctx, sevenzip.ReadRequest{Path: p.Path, Kind: p.ContentType.Kind, Listing: l}) {
		if err != nil {
			yield(dispatch.Item{}, partErr(ctx, fmt.Errorf("archive stage: %w", err), s.memory))
			return
		}
		e := read.Entry
		if e.Folder {
			continue
		}
		files++
		if e.Encrypted {
			encrypted++
		}
		item, err := s.listedEntry(ctx, p, k, read, dir)
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

func (s *archiveStage) listedEntry(ctx context.Context, p dispatch.Part, k sevenZipKind, read sevenzip.Item, dir string) (dispatch.Item, error) {
	e := read.Entry
	where := k.where + e.Path
	typ, link := listedType(e)
	if flags := entryFlags(e.Path, typ, link, e.Encrypted); len(flags) > 0 {
		return flagged(where, e.Path, typ, flags), nil
	}
	if typ != inspect.FileEntry {
		return unread(where, e.Path, dispatch.NoProcessor, string(typ)+": no processor yet"), nil
	}
	whole := uint64(p.Size)
	path, written, why, err := s.extract(ctx, copyFrom(read.Content), dir, whole, func(w *bombWriter, fillErr error) error {
		switch {
		case fillErr != nil:
			return fmt.Errorf("doesn't extract: %w", fillErr)
		case e.Sized && w.n != e.Size:
			return fmt.Errorf("%d bytes written, %d listed", w.n, e.Size)
		case e.HasCRC && w.crc.Sum32() != e.CRC:
			return fmt.Errorf("CRC-32 %08x, %08x listed", w.crc.Sum32(), e.CRC)
		}
		return nil
	})
	switch {
	case err != nil:
		return dispatch.Item{}, err
	case errors.Is(why, errBomb):
		return s.bombChild(where, e.Path, fmt.Sprintf("entry %q in a %d-byte archive, listed %d", e.Path, whole, e.Size), written, whole), nil
	case why != nil:
		return notWritten(where, e.Path, why), nil
	}
	return dispatch.PartItem(dispatch.Found{Path: path, Name: e.Path, Where: where}), nil
}

func (s *archiveStage) zstd(ctx context.Context, p dispatch.Part, dir string, yield func(dispatch.Item, error) bool) {
	k := sevenZipKinds[route.Zstd]
	l, err := s.list(ctx, p, k)
	if err != nil {
		yield(dispatch.Item{}, err)
		return
	}
	name := streamStem(p.Name, ".zst", ".tzst")
	var content sevenzip.Entry
	if len(l.Entries) > 0 {
		content = l.Entries[0]
	}
	checked := []inspect.Listed{{Name: name, Type: inspect.FileEntry, Declared: content.Size, Sized: content.Sized}}
	if !s.listedChecks(p, checked, yield) {
		return
	}
	where := k.where + name
	compressed := uint64(p.Size)
	path, written, why, err := s.extract(ctx, func(w io.Writer) error {
		return s.sevenZip.Extract(ctx, sevenzip.ExtractRequest{Path: p.Path, Kind: route.Zstd}, w)
	}, dir, compressed, func(w *bombWriter, fillErr error) error {
		switch {
		case fillErr != nil:
			return fmt.Errorf("doesn't decompress: %w", fillErr)
		case content.Sized && w.n != content.Size:
			return fmt.Errorf("%d bytes written, %d declared", w.n, content.Size)
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
