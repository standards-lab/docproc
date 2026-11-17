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

// The byte order marks an XML scan reads before its declaration.
var (
	utf8BOM    = []byte{0xef, 0xbb, 0xbf}
	utf16LEBOM = []byte{0xff, 0xfe}
	utf16BEBOM = []byte{0xfe, 0xff}
)

// xmlDecoder returns a decoder of the XML r holds in its own charset: UTF-16, either byte order,
// when it begins with a UTF-16 byte order mark, which then decides whatever its declaration
// says; else the charset its declaration names, by its IANA name or alias through x/text's
// ianaindex, UTF-8 when it names none. A UTF-8 byte order mark is dropped. A charset ianaindex
// doesn't know, or knows and can't decode, ends the decoder's scan with an error at the
// declaration, as markup that doesn't decode does.
func xmlDecoder(r io.Reader) *xml.Decoder {
	br := bufio.NewReader(r)
	var src io.Reader = br
	marked := false
	head, _ := br.Peek(len(utf8BOM))
	switch {
	case bytes.HasPrefix(head, utf8BOM):
		_, _ = br.Discard(len(utf8BOM))
	case bytes.HasPrefix(head, utf16LEBOM), bytes.HasPrefix(head, utf16BEBOM):
		// ExpectBOM reads the mark for the byte order, and drops it.
		src = transform.NewReader(br, unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder())
		marked = true
	}
	d := xml.NewDecoder(src)
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if marked {
			// The mark decided, and input is UTF-8 already.
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
