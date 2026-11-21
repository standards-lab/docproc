package inspect

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
)

func (in *inspection) tar(ctx context.Context) error {
	tr := tar.NewReader(io.NewSectionReader(in.file, 0, in.size))
	whole := uint64(max(in.size, 0))
	var sum uint64
	entries := 0
	defer func() {
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
