package route

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

const contentTypesName = "[Content_Types].xml"

const (
	xpsSequenceName  = "FixedDocumentSequence.fdseq"
	xpsSequencePiece = xpsSequenceName + "/[0].piece"
)

var (
	eocdSignature         = []byte("PK\x05\x06")
	zip64LocatorSig       = []byte("PK\x06\x07")
	zip64EOCDSignature    = []byte("PK\x06\x06")
	directoryHeaderSig    = []byte("PK\x01\x02")
	eocdLen               = 22
	zip64LocatorLen       = 20
	zip64EOCDLen          = 56
	directoryHeaderLen    = 46
	maxZIPComment         = 0xffff
	zip64Marker32         = uint32(0xffffffff)
	errNoCentralDirectory = errors.New("no central directory")
)

func zipHolds(r io.ReaderAt, size int64, names ...string) (held []bool, err error) {
	held = make([]bool, len(names))
	offset, length, err := centralDirectory(r, size)
	if errors.Is(err, errNoCentralDirectory) {
		return held, nil
	}
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(io.NewSectionReader(r, offset, length))
	fixed := make([]byte, directoryHeaderLen)
	var got []byte
	for found := 0; found < len(names); {
		if _, err := io.ReadFull(br, fixed); err != nil || !bytes.HasPrefix(fixed, directoryHeaderSig) {
			return held, ignoreEOF(err)
		}
		nameLen := int(binary.LittleEndian.Uint16(fixed[28:]))
		rest := int(binary.LittleEndian.Uint16(fixed[30:])) + int(binary.LittleEndian.Uint16(fixed[32:]))
		got = append(got[:0], make([]byte, nameLen)...)
		if _, err := io.ReadFull(br, got); err != nil {
			return held, ignoreEOF(err)
		}
		for i, name := range names {
			if !held[i] && string(got) == name {
				held[i] = true
				found++
			}
		}
		if _, err := br.Discard(rest); err != nil {
			return held, ignoreEOF(err)
		}
	}
	return held, nil
}

func centralDirectory(r io.ReaderAt, size int64) (offset, length int64, err error) {
	tail := make([]byte, min(size, int64(eocdLen+maxZIPComment)))
	if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil && !errors.Is(err, io.EOF) {
		return 0, 0, err
	}
	at := -1
	for i := len(tail) - eocdLen; i >= 0; i-- {
		if bytes.HasPrefix(tail[i:], eocdSignature) && i+eocdLen+int(binary.LittleEndian.Uint16(tail[i+20:])) <= len(tail) {
			at = i
			break
		}
	}
	if at < 0 {
		return 0, 0, errNoCentralDirectory
	}
	rec := tail[at:]
	length, offset = int64(binary.LittleEndian.Uint32(rec[12:])), int64(binary.LittleEndian.Uint32(rec[16:]))
	if binary.LittleEndian.Uint32(rec[12:]) == zip64Marker32 || binary.LittleEndian.Uint32(rec[16:]) == zip64Marker32 {
		eocdAt := size - int64(len(tail)) + int64(at)
		if offset, length, err = zip64Directory(r, eocdAt); err != nil {
			return 0, 0, err
		}
	}
	if offset < 0 || length < 0 || offset+length > size {
		return 0, 0, errNoCentralDirectory
	}
	return offset, length, nil
}

func zip64Directory(r io.ReaderAt, eocdAt int64) (offset, length int64, err error) {
	if eocdAt < int64(zip64LocatorLen) {
		return 0, 0, errNoCentralDirectory
	}
	loc := make([]byte, zip64LocatorLen)
	if _, err := r.ReadAt(loc, eocdAt-int64(zip64LocatorLen)); err != nil {
		return 0, 0, err
	}
	if !bytes.HasPrefix(loc, zip64LocatorSig) {
		return 0, 0, errNoCentralDirectory
	}
	rec := make([]byte, zip64EOCDLen)
	recAt := binary.LittleEndian.Uint64(loc[8:])
	if recAt > uint64(eocdAt) {
		return 0, 0, errNoCentralDirectory
	}
	if _, err := r.ReadAt(rec, int64(recAt)); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, 0, errNoCentralDirectory
		}
		return 0, 0, err
	}
	if !bytes.HasPrefix(rec, zip64EOCDSignature) {
		return 0, 0, errNoCentralDirectory
	}
	l, o := binary.LittleEndian.Uint64(rec[40:]), binary.LittleEndian.Uint64(rec[48:])
	if l > uint64(eocdAt) || o > uint64(eocdAt) {
		return 0, 0, errNoCentralDirectory
	}
	return int64(o), int64(l), nil
}
