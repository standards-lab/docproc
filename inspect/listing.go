package inspect

import (
	"fmt"

	"github.com/standards-lab/docproc/route"
)

// EntryType names what a listed entry is, as the program that lists the archive reports it.
type EntryType string

// The entry types a listing names.
const (
	// FileEntry is a regular file.
	FileEntry EntryType = "file"
	// DirEntry is a directory.
	DirEntry EntryType = "directory"
	// SymlinkEntry is a symbolic link.
	SymlinkEntry EntryType = "symbolic link"
	// HardlinkEntry is a hard link.
	HardlinkEntry EntryType = "hard link"
	// DeviceEntry is a node an extraction would make rather than a file: a character or block
	// device, a FIFO, or a socket.
	DeviceEntry EntryType = "device"
)

// Listed is one entry of an archive's listing, as a program that reads the archive, such as
// 7-Zip, lists it before anything is extracted: its stored name, its type, a link's target, the
// expanded size it declares, when Sized, and whether it is encrypted.
type Listed struct {
	Name      string
	Type      EntryType
	Link      string
	Declared  uint64
	Sized     bool
	Encrypted bool
}

var listed = map[route.Kind]bool{
	route.SevenZip: true, route.MSCF: true, route.RAR4: true, route.RAR5: true,
	route.Zstd: false, route.XZ: false,
}

// CheckListing checks an archive's listing, entries, as a program that reads the archive lists
// it, under the rules [Inspect] holds a tar's headers to, by the same checks: a name with a ".."
// element, an absolute one, or one that begins with a drive letter; a symbolic or hard link; a
// device; an encrypted entry; an entry past the bomb rule by the size it declares, its ratio
// against size, the whole archive's, since a 7z's or cabinet's entries share their packed
// blocks; and the sum of the declared sizes past it too. The program's reader bounds the
// listing's length as it parses it. kind is the archive's: [route.SevenZip], [route.MSCF],
// [route.RAR4], or [route.RAR5], or [route.Zstd] or [route.XZ], whose one entry is its content,
// checked by its declared size alone. It returns the report the flags come to under opts, its Kind kind, and an error for opts
// it refuses, matching [ErrOptions], or a kind it doesn't read.
//
// Inspect runs no program, so it never lists such an archive itself: the stage that reads the
// archive through its program calls CheckListing before it extracts anything, and quarantines
// the archive on the report's verdict.
func CheckListing(kind route.Kind, size int64, entries []Listed, opts Options) (Report, error) {
	if err := opts.Validate(); err != nil {
		return Report{}, err
	}
	named, ok := listed[kind]
	if !ok {
		return Report{}, fmt.Errorf("inspect: no listing checks for kind %s", kind)
	}
	in := &inspection{size: size, kind: kind, opts: opts, found: map[Check]*finding{}}
	whole := uint64(max(size, 0))
	var sum uint64
	for _, e := range entries {
		in.listed(e, named)
		if e.Sized {
			sum = addSat(sum, e.Declared)
		}
	}
	if len(entries) > 1 && opts.Bomb().Past(sum, whole) {
		in.flag(ArchiveBomb, fmt.Sprintf("declared sizes sum to %d bytes over %d entries in a %d-byte archive, %.0f:1", sum, len(entries), size, ratio(sum, whole)))
	}
	flags := in.flags()
	return Report{Kind: kind, Flags: flags, Verdict: opts.Verdict(flags)}, nil
}

func (in *inspection) listed(e Listed, named bool) {
	quoted := fmt.Sprintf("entry %q", e.Name)
	if named {
		in.entryName(e.Name)
		linked := quoted
		if e.Link != "" {
			linked = fmt.Sprintf("%s to %q", quoted, e.Link)
		}
		switch e.Type {
		case SymlinkEntry:
			in.flag(ArchiveSymlink, linked)
		case HardlinkEntry:
			in.flag(ArchiveHardlink, linked)
		case DeviceEntry:
			in.flag(ArchiveDevice, quoted)
		}
		if e.Encrypted {
			in.flag(ArchiveEncrypted, quoted)
		}
	}
	whole := uint64(max(in.size, 0))
	if e.Sized && in.opts.Bomb().Past(e.Declared, whole) {
		in.flag(ArchiveBomb, fmt.Sprintf("%s: %d bytes declared in a %d-byte archive, %.0f:1", quoted, e.Declared, in.size, ratio(e.Declared, whole)))
	}
}
