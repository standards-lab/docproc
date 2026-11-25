package sevenzip

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// Entry is one item of an archive as 7-Zip's technical listing, -slt, gives it: each field its
// property's value, and a property the format doesn't carry left zero.
type Entry struct {
	// Path is the item's listed name: its stored name, but for a line break, which 7-Zip lists
	// as "_". It is only ever a name: nothing is written to it. A zstd or xz stream's one item
	// has none.
	Path string
	// Folder is a directory: its Folder property is "+", or its attributes mark a directory.
	Folder bool
	// Size is the expanded size the archive declares, when Sized; a zstd stream declares none
	// unless its frame header carries its content size, and an xz stream's index declares it.
	Size  uint64
	Sized bool
	// CRC is the CRC-32 the archive declares for the item's content, when HasCRC.
	CRC    uint32
	HasCRC bool
	// Encrypted is an item 7-Zip reports encrypted, its Encrypted property "+".
	Encrypted bool
	// Method is the item's compression method, as 7-Zip names it, such as "LZMA2:24" or
	// "Deflate64".
	Method string
	// Mode is the Unix mode the item's attributes carry, its type and permission bits; zero when
	// they carry none, as a cabinet's don't.
	Mode fs.FileMode
	// Symlink is the target its Symbolic Link property names, for a format that lists one.
	Symlink string
	// HardLink is the target its Hard Link property names, for a format that lists one.
	HardLink string
}

// IsSymlink reports whether the item is a symbolic link, by its mode or a Symbolic Link property.
func (e Entry) IsSymlink() bool { return e.Mode&fs.ModeSymlink != 0 || e.Symlink != "" }

// IsDevice reports whether the item is a node an extraction would make rather than a file: a
// character or block device, a FIFO, or a socket, by its mode.
func (e Entry) IsDevice() bool {
	return e.Mode&(fs.ModeDevice|fs.ModeCharDevice|fs.ModeNamedPipe|fs.ModeSocket) != 0
}

// Listing is an archive as 7-Zip lists it: its method and its items, in the archive's order.
type Listing struct {
	// Method is the archive's Method property, as 7-Zip gives it: the methods a 7z's blocks
	// use, or what 7-Zip read of a zstd frame's header, such as
	// "header-open-only: ... content-size-total:4294967296".
	Method string
	// Entries are the archive's items, directories among them, in the archive's order.
	Entries []Entry
}

var (
	// ErrListing is the cause of a listing the package can't parse.
	ErrListing = errors.New("unparsable listing")
	// ErrEntries is the cause of a listing that holds more items than the request's Entries
	// allow: its parsing stopped, and 7zz with it, at the item past them.
	ErrEntries = errors.New("past the entries bound")
)

const separator = "----------"

const lineLimit = 1 << 20

type listingParser struct {
	limit   int
	listing Listing
	section int
	cur     map[string]string
	partial []byte
	err     error
}

func (lp *listingParser) Write(p []byte) (int, error) {
	if lp.err != nil {
		return 0, lp.err
	}
	n := len(p)
	for len(p) > 0 {
		before, after, found := bytes.Cut(p, []byte("\n"))
		if len(lp.partial)+len(before) > lineLimit {
			lp.err = fmt.Errorf("%w: a line past %d bytes", ErrListing, lineLimit)
			return 0, lp.err
		}
		lp.partial = append(lp.partial, before...)
		if !found {
			break
		}
		line := string(lp.partial)
		lp.partial = lp.partial[:0]
		if err := lp.line(line); err != nil {
			lp.err = err
			return 0, err
		}
		p = after
	}
	return n, nil
}

func (lp *listingParser) line(line string) error {
	switch {
	case lp.section == 0:
		if line == "--" {
			lp.section = 1
		}
		return nil
	case line == separator:
		lp.section = 2
		return nil
	}
	name, value, ok := strings.Cut(line, " = ")
	if !ok {
		name, ok = strings.CutSuffix(line, " =")
	}
	if lp.section == 1 {
		if ok && name == "Method" {
			lp.listing.Method = value
		}
		return nil
	}
	if line == "" {
		return lp.flush()
	}
	if !ok {
		return fmt.Errorf("%w: line %q", ErrListing, line)
	}
	if lp.cur == nil {
		lp.cur = map[string]string{}
	}
	lp.cur[name] = value
	return nil
}

func (lp *listingParser) flush() error {
	if lp.cur == nil {
		return nil
	}
	if lp.limit > 0 && len(lp.listing.Entries) == lp.limit {
		return fmt.Errorf("%w: an item past %d", ErrEntries, lp.limit)
	}
	e, err := entry(lp.cur)
	if err != nil {
		return err
	}
	lp.listing.Entries = append(lp.listing.Entries, e)
	lp.cur = nil
	return nil
}

func (lp *listingParser) end() (Listing, error) {
	if lp.err != nil {
		return Listing{}, lp.err
	}
	if len(lp.partial) > 0 {
		if err := lp.line(string(lp.partial)); err != nil {
			return Listing{}, err
		}
	}
	if err := lp.flush(); err != nil {
		return Listing{}, err
	}
	if lp.section != 2 {
		return Listing{}, fmt.Errorf("%w: no items section", ErrListing)
	}
	return lp.listing, nil
}

const contentSizeTotal = "content-size-total:"

func zstdSize(l *Listing) {
	if len(l.Entries) != 1 || l.Entries[0].Sized {
		return
	}
	for field := range strings.FieldsSeq(l.Method) {
		if v, ok := strings.CutPrefix(field, contentSizeTotal); ok {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				l.Entries[0].Size, l.Entries[0].Sized = n, true
			}
			return
		}
	}
}

func entry(props map[string]string) (Entry, error) {
	e := Entry{
		Path:      props["Path"],
		Encrypted: props["Encrypted"] == "+",
		Method:    props["Method"],
		Symlink:   props["Symbolic Link"],
		HardLink:  props["Hard Link"],
	}
	var err error
	if e.Size, e.Sized, err = number(props, "Size"); err != nil {
		return Entry{}, err
	}
	if v := props["CRC"]; v != "" {
		crc, err := strconv.ParseUint(v, 16, 32)
		if err != nil {
			return Entry{}, fmt.Errorf("%w: %s's CRC %q", ErrListing, e.Path, v)
		}
		e.CRC, e.HasCRC = uint32(crc), true
	}
	attrs := props["Attributes"]
	e.Mode = unixMode(attrs)
	e.Folder = props["Folder"] == "+" || e.Mode.IsDir() || windowsDirectory(attrs)
	return e, nil
}

func windowsDirectory(attrs string) bool {
	if attrs == "" || attrs[0] == ' ' {
		return false
	}
	letters, _, _ := strings.Cut(attrs, " ")
	return strings.Contains(letters, "D")
}

func number(props map[string]string, name string) (uint64, bool, error) {
	v := props[name]
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %s %q", ErrListing, name, v)
	}
	return n, true, nil
}

func unixMode(attrs string) fs.FileMode {
	for field := range strings.FieldsSeq(attrs) {
		if len(field) != 10 || strings.Trim(field[1:], "rwxsStT-") != "" {
			continue
		}
		var mode fs.FileMode
		switch field[0] {
		case '-':
		case 'd':
			mode = fs.ModeDir
		case 'l':
			mode = fs.ModeSymlink
		case 'c':
			mode = fs.ModeDevice | fs.ModeCharDevice
		case 'b':
			mode = fs.ModeDevice
		case 'p':
			mode = fs.ModeNamedPipe
		case 's':
			mode = fs.ModeSocket
		default:
			continue
		}
		for i, c := range field[1:] {
			if c != '-' && c != 'S' && c != 'T' {
				mode |= 1 << (8 - i)
			}
		}
		return mode
	}
	return 0
}
