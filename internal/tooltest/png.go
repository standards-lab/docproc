package tooltest

import (
	"encoding/binary"
	"hash/crc32"
)

// PNG returns a stand-in's PNG naming text: PNG's signature, a tEXt chunk whose keyword is
// "Comment" and whose text is text, and IEND, each chunk with its length and CRC, so a reader
// that splits PNGs by their chunks, as one splitting gs's stdout of many pages does, reads it as
// a whole PNG. It holds no image.
func PNG(text string) []byte {
	b := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range []struct{ kind, data string }{{"tEXt", "Comment\x00" + text}, {"IEND", ""}} {
		b = binary.BigEndian.AppendUint32(b, uint32(len(c.data)))
		chunk := []byte(c.kind + c.data)
		b = append(b, chunk...)
		b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(chunk))
	}
	return b
}
