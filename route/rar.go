package route

import (
	"bytes"
	"encoding/binary"
)

const (
	rar4MainHeader = 0x73
	rar4Volume     = 0x0001
	rar5MainHeader = 1
	rar5Volume     = 0x0001
	rar5Extra      = 0x0001
	rar5Data       = 0x0002
	rar5CRCLen     = 4
	vintMax        = 10
)

// RARVolume reports whether head, a file's first bytes, begins a RAR archive whose main archive
// header, the first after its signature, marks it one volume of a split set: a RAR4's by its
// MHD_VOLUME flag, 0x0001, and a RAR5's by its archive flags' volume bit, 0x0001. A RAR5 whose
// headers are encrypted begins with its encryption header instead, which hides the flag, so it
// is none; so is a head cut short of the flag, and content [Identify] doesn't name [RAR4] or
// [RAR5].
func RARVolume(head []byte) bool {
	head = head[:min(len(head), HeadLen)]
	switch {
	case bytes.HasPrefix(head, rar4Signature):
		h := head[len(rar4Signature):]
		return len(h) >= 5 && h[2] == rar4MainHeader && binary.LittleEndian.Uint16(h[3:])&rar4Volume != 0
	case bytes.HasPrefix(head, rar5Signature):
		return rar5Volume&rar5ArchiveFlags(head[len(rar5Signature):]) != 0
	}
	return false
}

func rar5ArchiveFlags(h []byte) uint64 {
	if len(h) < rar5CRCLen {
		return 0
	}
	h = h[rar5CRCLen:]
	var fields [3]uint64
	for i := range fields {
		var ok bool
		if fields[i], h, ok = vint(h); !ok {
			return 0
		}
	}
	typ, flags := fields[1], fields[2]
	if typ != rar5MainHeader {
		return 0
	}
	for _, present := range []uint64{rar5Extra, rar5Data} {
		if flags&present != 0 {
			var ok bool
			if _, h, ok = vint(h); !ok {
				return 0
			}
		}
	}
	archive, _, ok := vint(h)
	if !ok {
		return 0
	}
	return archive
}

func vint(b []byte) (uint64, []byte, bool) {
	var v uint64
	for i := 0; i < min(len(b), vintMax); i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, b[i+1:], true
		}
	}
	return 0, b, false
}
