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

const maxRels = 16 << 20

const relHyperlink = "hyperlink"

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

func addSat(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

func ratio(expanded, against uint64) float64 { return float64(expanded) / float64(max(against, 1)) }

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

func (in *inspection) entryName(name string) {
	quoted := fmt.Sprintf("entry %q", name)
	if traverses(name) {
		in.flag(ArchiveTraversal, quoted)
	}
	if absolute(name) {
		in.flag(ArchiveAbsolutePath, quoted)
	}
}

func traverses(name string) bool {
	elements := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	return slices.Contains(elements, "..")
}

func absolute(name string) bool {
	return strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || driveLetter(name)
}

func driveLetter(name string) bool {
	return len(name) >= 2 && name[1] == ':' &&
		(name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z')
}

var (
	zip64Locator = []byte("PK\x06\x07")
	zip64Record  = []byte("PK\x06\x06")
)

const (
	zip64LocatorLen = 20
	zip64RecordLen  = 56
	maxZIPComment   = 0xffff
)

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

type relationship struct {
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

func (in *inspection) relationships(f *zip.File) {
	quoted := fmt.Sprintf("part %q", f.Name)
	rc, err := f.Open()
	if err != nil {
		in.flag(RelationshipsUnreadable, fmt.Sprintf("%s: %v", quoted, err))
		return
	}
	defer func() { _ = rc.Close() }()
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

type budgetReader struct {
	r    io.Reader
	n    int64
	past bool
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.n <= 0 {
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
