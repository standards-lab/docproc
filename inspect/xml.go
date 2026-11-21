package inspect

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"

	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var (
	utf8BOM    = []byte{0xef, 0xbb, 0xbf}
	utf16LEBOM = []byte{0xff, 0xfe}
	utf16BEBOM = []byte{0xfe, 0xff}
)

func xmlDecoder(r io.Reader) *xml.Decoder {
	br := bufio.NewReader(r)
	var src io.Reader = br
	marked := false
	head, _ := br.Peek(len(utf8BOM))
	switch {
	case bytes.HasPrefix(head, utf8BOM):
		_, _ = br.Discard(len(utf8BOM))
	case bytes.HasPrefix(head, utf16LEBOM), bytes.HasPrefix(head, utf16BEBOM):
		src = transform.NewReader(br, unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder())
		marked = true
	}
	d := xml.NewDecoder(src)
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if marked {
			return input, nil
		}
		enc, err := ianaindex.IANA.Encoding(label)
		if err != nil {
			return nil, err
		}
		if enc == nil {
			return nil, fmt.Errorf("charset %q has no decoder", label)
		}
		return enc.NewDecoder().Reader(input), nil
	}
	return d
}
