package inspect

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
)

// tar reads a tar's headers, as zip reads a ZIP's central directory, extracting no entry, and
// checks each entry's name, type, and declared size, then the sum of the declared sizes.
// archive/tar seeks past each entry's content, so only the headers are read. A header
// archive/tar can't read, a truncated tar's among them, is [TarUnreadable], and the entries
// after it go unchecked; so is a header past the options' Entries.
func (in *inspection) tar(ctx context.Context) error {
	tr := tar.NewReader(io.NewSectionReader(in.file, 0, in.size))
	whole := uint64(max(in.size, 0))
	var sum uint64
	entries := 0
	defer func() {
		// One entry's sum is the entry, which tarEntry weighs.
		if entries > 1 && in.opts.Bomb().Past(sum, whole) {
			in.flag(ArchiveBomb, fmt.Sprintf("declared sizes sum to %d bytes over %d entries in a %d-byte tar, %.0f:1", sum, entries, in.size, ratio(sum, whole)))
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil && !errors.Is(err, tar.ErrInsecurePath):
			// ErrInsecurePath comes with its header, which tarEntry checks itself.
			in.flag(TarUnreadable, fmt.Sprintf("tar: %v", err))
			return nil
		case in.opts.Entries > 0 && entries == in.opts.Entries:
			in.flag(TarUnreadable, fmt.Sprintf("entries listed past %d", in.opts.Entries))
			return nil
		}
		in.tarEntry(h)
		entries++
		if h.Size > 0 {
			sum = addSat(sum, uint64(h.Size))
		}
	}
}

// tarEntry checks one entry's name, its type, and its size. The bomb rule reads the size the
// header declares against the whole tar's, since a tar stores an entry's bytes as they are: only
// a sparse entry, whose holes aren't stored, or a header that lies, declares more than the tar
// holds. The stage that extracts the entry enforces the same rule on the bytes it writes.
func (in *inspection) tarEntry(h *tar.Header) {
	quoted := fmt.Sprintf("entry %q", h.Name)
	in.entryName(h.Name)
	switch h.Typeflag {
	case tar.TypeSymlink:
		in.flag(ArchiveSymlink, fmt.Sprintf("%s to %q", quoted, h.Linkname))
	case tar.TypeLink:
		in.flag(ArchiveHardlink, fmt.Sprintf("%s to %q", quoted, h.Linkname))
	case tar.TypeChar:
		in.flag(ArchiveDevice, fmt.Sprintf("character device %s, %d:%d", quoted, h.Devmajor, h.Devminor))
	case tar.TypeBlock:
		in.flag(ArchiveDevice, fmt.Sprintf("block device %s, %d:%d", quoted, h.Devmajor, h.Devminor))
	case tar.TypeFifo:
		in.flag(ArchiveDevice, fmt.Sprintf("FIFO %s", quoted))
	}
	if h.Size < 0 {
		return
	}
	expanded, whole := uint64(h.Size), uint64(max(in.size, 0))
	if in.opts.Bomb().Past(expanded, whole) {
		in.flag(ArchiveBomb, fmt.Sprintf("%s: %d bytes declared in a %d-byte tar, %.0f:1", quoted, expanded, in.size, ratio(expanded, whole)))
	}
}
