package inspect

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
)

// maxRels is the most of a package's relationship parts, expanded and summed, the OOXML checks
// read: one budget for every part, so a package of many parts costs no more than one of one.
// The part that passes it is unreadable past it.
const maxRels = 16 << 20

// relHyperlink is the last element of a hyperlink relationship's type URI: an external
// hyperlink is a link a reader follows by choice, not content fetched when the document opens,
// so it isn't active content.
const relHyperlink = "hyperlink"

// zip reads a ZIP's central directory, extracting no entry, and checks each entry's name, mode,
// declared sizes, and encryption, then the sums of its declared sizes; an OOXML package, one
// with a [Content_Types].xml entry, has its parts and relationships checked too. A ZIP whose
// end record lists more entries than the options allow is [ZIPUnreadable] before archive/zip
// reads its central directory, as is one archive/zip can't read.
func (in *inspection) zip(ctx context.Context) error {
	if listed, ok := zipListed(in.file, in.size); ok && in.opts.Entries > 0 && listed > uint64(in.opts.Entries) {
		in.flag(ZIPUnreadable, fmt.Sprintf("%d entries listed, past %d", listed, in.opts.Entries))
		return nil
	}
	r, err := zip.NewReader(in.file, in.size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		in.flag(ZIPUnreadable, fmt.Sprintf("ZIP: %v", err))
		return nil
	}
	ooxml := slices.ContainsFunc(r.File, func(f *zip.File) bool { return f.Name == "[Content_Types].xml" })
	in.rels = maxRels
	var expanded, compressed uint64
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		in.zipEntry(f)
		expanded, compressed = addSat(expanded, f.UncompressedSize64), addSat(compressed, f.CompressedSize64)
		if ooxml {
			in.ooxmlPart(f)
		}
	}
	whole := uint64(max(in.size, 0))
	if compressed > whole {
		in.flag(ArchiveBomb, fmt.Sprintf("declared compressed sizes sum to %d bytes, past the %d-byte ZIP", compressed, in.size))
	}
	if in.opts.Bomb().Past(expanded, whole) {
		in.flag(ArchiveBomb, fmt.Sprintf("declared sizes sum to %d bytes over %d entries in a %d-byte ZIP, %.0f:1", expanded, len(r.File), in.size, ratio(expanded, whole)))
	}
	return nil
}

// addSat returns a+b, or the largest uint64 when the sum would pass it, so a sum of declared
// sizes that lie can't wrap.
func addSat(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// ratio returns expanded over against, against at least one.
func ratio(expanded, against uint64) float64 { return float64(expanded) / float64(max(against, 1)) }

// zipEntry checks one entry's name, mode, sizes, and encryption. The bomb rule reads the sizes
// the central directory declares: the stage that extracts the entry enforces the same rule on
// the bytes it writes, since a declared size may lie.
func (in *inspection) zipEntry(f *zip.File) {
	quoted := fmt.Sprintf("entry %q", f.Name)
	in.entryName(f.Name)
	if f.Mode()&fs.ModeSymlink != 0 {
		in.flag(ArchiveSymlink, quoted)
	}
	if in.opts.Bomb().Past(f.UncompressedSize64, f.CompressedSize64) {
		in.flag(ArchiveBomb, fmt.Sprintf("%s: %d bytes from %d, %.0f:1", quoted, f.UncompressedSize64, f.CompressedSize64,
			ratio(f.UncompressedSize64, f.CompressedSize64)))
	}
	if f.Flags&0x1 != 0 {
		in.flag(ArchiveEncrypted, quoted)
	}
}

// entryName checks an archive entry's name: a ".." element, by either separator, and an
// absolute name or one that begins with a drive letter.
func (in *inspection) entryName(name string) {
	quoted := fmt.Sprintf("entry %q", name)
	if traverses(name) {
		in.flag(ArchiveTraversal, quoted)
	}
	if absolute(name) {
		in.flag(ArchiveAbsolutePath, quoted)
	}
}

// traverses reports whether an entry's name has a ".." element, either separator dividing it.
func traverses(name string) bool {
	elements := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	return slices.Contains(elements, "..")
}

// absolute reports whether an entry's name is absolute, by either separator, or begins with a
// drive letter.
func absolute(name string) bool {
	return strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || driveLetter(name)
}

// driveLetter reports whether name begins with a drive letter and a colon, as "C:" does.
func driveLetter(name string) bool {
	return len(name) >= 2 && name[1] == ':' &&
		(name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z')
}

// The ZIP records zipListed reads, by their signatures and lengths.
var (
	zip64Locator = []byte("PK\x06\x07")
	zip64Record  = []byte("PK\x06\x06")
)

const (
	zip64LocatorLen = 20
	zip64RecordLen  = 56
	maxZIPComment   = 0xffff
)

// zipListed returns how many entries the ZIP r holds, size bytes, lists, by its end record, the
// last one whose comment ends within the file, or ZIP64's record when the end record's count is
// its marker; false when no record gives a count, which archive/zip then reports.
func zipListed(r io.ReaderAt, size int64) (uint64, bool) {
	tail := make([]byte, min(max(size, 0), int64(eocdLen+maxZIPComment)))
	if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil && !errors.Is(err, io.EOF) {
		return 0, false
	}
	for i := len(tail) - eocdLen; i >= 0; i-- {
		if !bytes.HasPrefix(tail[i:], eocdSignature) || i+eocdLen+int(binary.LittleEndian.Uint16(tail[i+20:])) > len(tail) {
			continue
		}
		if n := binary.LittleEndian.Uint16(tail[i+10:]); n != 0xffff {
			return uint64(n), true
		}
		at := size - int64(len(tail)) + int64(i)
		loc := make([]byte, zip64LocatorLen)
		if at < zip64LocatorLen {
			return 0, false
		}
		if _, err := r.ReadAt(loc, at-zip64LocatorLen); err != nil || !bytes.HasPrefix(loc, zip64Locator) {
			return 0, false
		}
		rec := make([]byte, zip64RecordLen)
		recAt := binary.LittleEndian.Uint64(loc[8:])
		if recAt > uint64(at) {
			return 0, false
		}
		if _, err := r.ReadAt(rec, int64(recAt)); err != nil || !bytes.HasPrefix(rec, zip64Record) {
			return 0, false
		}
		return binary.LittleEndian.Uint64(rec[32:]), true
	}
	return 0, false
}

// ooxmlPart checks one part of an OOXML package, by its name: a VBA project, a macro sheet, an
// ActiveX control, or an OLE object; and a relationships part's external relationships.
func (in *inspection) ooxmlPart(f *zip.File) {
	lower := strings.ToLower(f.Name)
	base := path.Base(lower)
	quoted := fmt.Sprintf("part %q", f.Name)
	switch {
	case base == "vbaproject.bin":
		in.flag(VBAProject, quoted)
	case strings.HasPrefix(lower, "xl/macrosheets/"):
		in.flag(MacroSheet, quoted)
	case strings.Contains(lower, "/activex/") || strings.HasPrefix(lower, "activex/"):
		in.flag(ActiveX, quoted)
	case strings.Contains(lower, "embeddings/") && strings.HasPrefix(base, "oleobject"):
		in.flag(OLEObject, quoted)
	}
	if strings.HasSuffix(lower, ".rels") && f.Flags&0x1 == 0 {
		in.relationships(f)
	}
}

// relationship is one Relationship element of a relationships part.
type relationship struct {
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

// relationships checks a relationships part for an external relationship, other than a
// hyperlink: a remote template, a linked picture, an OLE link. It decodes the part's charset,
// a declared one or a UTF-16 byte order mark's, and reads it from what remains of the package's
// budget. A part that doesn't open, doesn't decode to its end, or passes the budget is
// [RelationshipsUnreadable], checked as far as it was read.
func (in *inspection) relationships(f *zip.File) {
	quoted := fmt.Sprintf("part %q", f.Name)
	rc, err := f.Open()
	if err != nil {
		in.flag(RelationshipsUnreadable, fmt.Sprintf("%s: %v", quoted, err))
		return
	}
	defer func() { _ = rc.Close() }() // read only: a close error loses nothing
	budget := &budgetReader{r: rc, n: in.rels}
	defer func() { in.rels = budget.n }()
	d := xmlDecoder(budget)
	for {
		tok, err := d.Token()
		if err != nil {
			switch {
			case budget.past:
				in.flag(RelationshipsUnreadable, fmt.Sprintf("%s: past the package's %d-byte budget for its relationship parts", quoted, maxRels))
			case !errors.Is(err, io.EOF):
				in.flag(RelationshipsUnreadable, fmt.Sprintf("%s: %s", quoted, clip(err.Error())))
			}
			return
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var rel relationship
		if err := d.DecodeElement(&rel, &start); err != nil {
			in.flag(RelationshipsUnreadable, fmt.Sprintf("%s: %s", quoted, clip(err.Error())))
			return
		}
		if typ := path.Base(rel.Type); strings.EqualFold(rel.TargetMode, "External") && typ != relHyperlink {
			in.flag(ExternalRelationship, fmt.Sprintf("%s: %s %s", f.Name, typ, rel.Target))
		}
	}
}

// budgetReader reads r until n bytes are spent, then ends as if r had, setting past when r held
// more.
type budgetReader struct {
	r    io.Reader
	n    int64
	past bool
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.n <= 0 {
		// One byte more tells a part that ends at the budget from one past it.
		var one [1]byte
		if n, _ := b.r.Read(one[:]); n > 0 {
			b.past = true
		}
		return 0, io.EOF
	}
	n, err := b.r.Read(p[:min(int64(len(p)), b.n)])
	b.n -= int64(n)
	return n, err
}
