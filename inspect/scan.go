package inspect

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/standards-lab/docproc/route"
)

const chunkSize = 1 << 16

var pdfNames = map[string]Check{
	"JavaScript": PDFJavaScript, "JS": PDFJS, "OpenAction": PDFOpenAction, "AA": PDFAA,
	"Launch": PDFLaunch, "EmbeddedFile": PDFEmbeddedFile, "RichMedia": PDFRichMedia,
	"XFA": PDFXFA, "Encrypt": PDFEncrypt,
	"EmbeddedFiles": PDFEmbeddedFile, "EF": PDFEmbeddedFile,
}

const maxPDFName = 64

var containerKinds = map[route.Kind]bool{
	route.ZIP: true, route.ODF: true, route.OLE2: true, route.PST: true, route.OST: true,
	route.Mbox: true, route.Tar: true, route.MSCF: true, route.OneStore: true,
	route.SevenZip: true, route.Gzip: true, route.Zstd: true,
	route.RAR4: true, route.RAR5: true, route.XZ: true, route.Bzip2: true,
}

var textKinds = map[route.Kind]bool{
	route.Unknown: true, route.UTF8BOM: true, route.UTF16BOM: true, route.UTF32BOM: true,
	route.Shebang: true,
}

var (
	eocdSignature = []byte("PK\x05\x06")
	pdfHeader     = []byte("%PDF-")
)

const eocdLen = 22

const maxEOCDCandidates = 64

type pattern struct {
	sig   []byte
	found func(off int64)
}

func (in *inspection) stream(ctx context.Context) error {
	var patterns []pattern
	var names *pdfScanner
	if in.kind == route.PDF {
		names = &pdfScanner{in: in}
	}
	var eocd []int64
	if !containerKinds[in.kind] {
		patterns = append(patterns, pattern{eocdSignature, func(off int64) {
			if len(eocd) == maxEOCDCandidates {
				eocd = eocd[1:]
			}
			eocd = append(eocd, off)
		}})
		if in.kind != route.PDF && !textKinds[in.kind] {
			patterns = append(patterns, pattern{pdfHeader, func(off int64) {
				in.flag(Polyglot, fmt.Sprintf("PDF header at offset %d after a %s signature", off, in.kind))
			}})
		}
	}
	if names == nil && len(patterns) == 0 {
		return nil
	}

	carryMax := 0
	for _, p := range patterns {
		carryMax = max(carryMax, len(p.sig)-1)
	}
	file := io.NewSectionReader(in.file, 0, in.size)
	buf := make([]byte, carryMax+chunkSize)
	carry := 0
	var base int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Read(buf[carry : carry+chunkSize])
		if n > 0 {
			data := buf[:carry+n]
			for _, p := range patterns {
				for i := 0; ; {
					j := bytes.Index(data[i:], p.sig)
					if j < 0 {
						break
					}
					at := i + j
					if at+len(p.sig) > carry {
						p.found(base + int64(at))
					}
					i = at + 1
				}
			}
			if names != nil {
				names.scan(data[carry:], base+int64(carry))
			}
			keep := min(carryMax, len(data))
			copy(buf, data[len(data)-keep:])
			base += int64(len(data) - keep)
			carry = keep
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("inspect: %w", err)
		}
	}
	if names != nil {
		names.end()
	}
	return in.checkEOCD(eocd)
}

func (in *inspection) checkEOCD(candidates []int64) error {
	rec := make([]byte, eocdLen)
	for _, off := range candidates {
		if off+eocdLen > in.size {
			continue
		}
		if _, err := in.file.ReadAt(rec, off); err != nil {
			return fmt.Errorf("inspect: %w", err)
		}
		cdSize := int64(binary.LittleEndian.Uint32(rec[12:]))
		cdOffset := int64(binary.LittleEndian.Uint32(rec[16:]))
		comment := int64(binary.LittleEndian.Uint16(rec[20:]))
		if cdSize > 0 && cdOffset+cdSize <= off && off+eocdLen+comment <= in.size {
			in.flag(Polyglot, fmt.Sprintf("ZIP end-of-central-directory record at offset %d in %s", off, kindPhrase(in.kind)))
			return nil
		}
	}
	return nil
}

func kindPhrase(k route.Kind) string {
	if k == route.Unknown {
		return "content of no signature"
	}
	return "a " + string(k)
}

type pdfScanner struct {
	in       *inspection
	raw      []byte
	inName   bool
	start    int64
	long     bool
	word     []byte
	inStream bool
	tail     []byte
}

var (
	streamKeyword    = []byte("stream")
	endstreamKeyword = []byte("endstream")
)

func pdfDelimiter(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ', '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func (s *pdfScanner) scan(chunk []byte, base int64) {
	for i := 0; i < len(chunk); {
		if s.inStream {
			n, ok := s.skip(chunk[i:])
			if !ok {
				return
			}
			i += n
			continue
		}
		b := chunk[i]
		i++
		if s.inName {
			if !pdfDelimiter(b) {
				if len(s.raw) < maxPDFName {
					s.raw = append(s.raw, b)
				} else {
					s.long = true
				}
				continue
			}
			s.end()
		}
		switch {
		case b == '/':
			s.begin(base + int64(i-1))
			s.word = s.word[:0]
		case pdfDelimiter(b):
			if (b == '\r' || b == '\n') && bytes.Equal(s.word, streamKeyword) {
				s.inStream = true
			}
			s.word = s.word[:0]
		case len(s.word) <= len(streamKeyword):
			s.word = append(s.word, b)
		}
	}
}

func (s *pdfScanner) skip(data []byte) (int, bool) {
	joined := append(slices.Clip(s.tail), data[:min(len(data), len(endstreamKeyword)-1)]...)
	if k := bytes.Index(joined, endstreamKeyword); k >= 0 {
		return s.close(k + len(endstreamKeyword) - len(s.tail)), true
	}
	if k := bytes.Index(data, endstreamKeyword); k >= 0 {
		return s.close(k + len(endstreamKeyword)), true
	}
	keep := len(endstreamKeyword) - 1
	if len(data) >= keep {
		s.tail = append(s.tail[:0], data[len(data)-keep:]...)
	} else {
		s.tail = append(s.tail, data...)
		s.tail = s.tail[max(0, len(s.tail)-keep):]
	}
	return len(data), false
}

func (s *pdfScanner) close(n int) int {
	s.inStream, s.tail, s.word = false, s.tail[:0], s.word[:0]
	return n
}

func (s *pdfScanner) begin(off int64) {
	s.inName, s.start, s.long, s.raw = true, off, false, s.raw[:0]
}

func (s *pdfScanner) end() {
	if !s.inName {
		return
	}
	s.inName = false
	if s.long {
		return
	}
	if c, ok := pdfNames[decodeName(s.raw)]; ok {
		s.in.flag(c, fmt.Sprintf("/%s at offset %d", s.raw, s.start))
	}
}

func decodeName(raw []byte) string {
	if bytes.IndexByte(raw, '#') < 0 {
		return string(raw)
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && i+2 < len(raw) {
			hi, ok1 := unhex(raw[i+1])
			lo, ok2 := unhex(raw[i+2])
			if ok1 && ok2 {
				out = append(out, hi<<4|lo)
				i += 2
				continue
			}
		}
		out = append(out, raw[i])
	}
	return string(out)
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
