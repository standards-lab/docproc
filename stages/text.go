package stages

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/standards-lab/docproc/dispatch"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

const textVersion = "2"

const (
	textByBOM    = " (byte order mark)"
	textFallback = "windows-1252 (fallback)"
)

type textEncoding struct {
	name string
	bom  []byte
	enc  encoding.Encoding
	fffd []byte
	unit int
}

var textBOMs = []textEncoding{
	{"utf-8", []byte{0xef, 0xbb, 0xbf}, unicode.UTF8, []byte{0xef, 0xbf, 0xbd}, 1},
	{"utf-32le", []byte{0xff, 0xfe, 0x00, 0x00}, utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM), []byte{0xfd, 0xff, 0x00, 0x00}, 4},
	{"utf-32be", []byte{0x00, 0x00, 0xfe, 0xff}, utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM), []byte{0x00, 0x00, 0xff, 0xfd}, 4},
	{"utf-16le", []byte{0xff, 0xfe}, unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), []byte{0xfd, 0xff}, 2},
	{"utf-16be", []byte{0xfe, 0xff}, unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), []byte{0xff, 0xfd}, 2},
}

var textWindows1252 = textEncoding{name: textFallback, enc: charmap.Windows1252, unit: 1}

const textChunk = 64 << 10

type textStage struct {
	id dispatch.Identity
}

func newTextStage() *textStage {
	hash, err := dispatch.HashConfig(struct {
		Adapter string `json:"adapter"`
	}{textVersion})
	if err != nil {
		panic(err)
	}
	return &textStage{id: dispatch.Identity{Name: "text", Version: textVersion, Config: hash}}
}

func (s *textStage) Identity() dispatch.Identity { return s.id }

func (s *textStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		d, err := decodeText(ctx, p.Path, dir)
		if err != nil {
			yield(dispatch.Item{}, fmt.Errorf("text stage: %w", err))
			return
		}
		if !yield(dispatch.OutputItem(d.output), nil) {
			return
		}
		for _, w := range d.warnings {
			if !yield(dispatch.WarningItem(dispatch.Warning{Tool: "text", Where: "text", What: w}), nil) {
				return
			}
		}
	}
}

type decoded struct {
	output   dispatch.Output
	warnings []string
}

func decodeText(ctx context.Context, path, dir string) (decoded, error) {
	f, err := os.Open(path)
	if err != nil {
		return decoded{}, err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 4)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return decoded{}, err
	}
	head = head[:n]
	for _, e := range textBOMs {
		if bytes.HasPrefix(head, e.bom) {
			e.name += textByBOM
			return decodeFile(ctx, f, int64(len(e.bom)), e, dir, nil)
		}
	}
	valid, err := validUTF8(ctx, f)
	if err != nil {
		return decoded{}, err
	}
	if valid {
		return textOutput(path, "utf-8", nil), nil
	}
	return decodeFile(ctx, f, 0, textWindows1252, dir,
		[]string{"no byte order mark and not UTF-8: decoded as windows-1252, which may not be the text's encoding"})
}

func decodeFile(ctx context.Context, f *os.File, offset int64, e textEncoding, dir string, warnings []string) (d decoded, err error) {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return decoded{}, err
	}
	stored := &fffdCounter{pattern: e.fffd, unit: e.unit}
	src := io.TeeReader(&ctxReader{ctx: ctx, r: f}, stored)
	path := filepath.Join(dir, "text.txt")
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return decoded{}, err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	counted := &fffdCounter{pattern: []byte(string(utf8.RuneError)), unit: 1}
	bw := bufio.NewWriterSize(io.MultiWriter(out, counted), textChunk)
	if _, err := io.Copy(bw, e.enc.NewDecoder().Reader(src)); err != nil {
		return decoded{}, fmt.Errorf("decode as %s: %w", e.name, err)
	}
	if err := bw.Flush(); err != nil {
		return decoded{}, err
	}
	if put := counted.n - stored.n; put > 0 {
		name, _, _ := strings.Cut(e.name, " ")
		warnings = append(warnings, strconv.Itoa(put)+" U+FFFD replacing what "+name+" can't decode")
	}
	return textOutput(path, e.name, warnings), nil
}

func textOutput(path, encoding string, warnings []string) decoded {
	return decoded{
		output: dispatch.Output{
			Kind: dispatch.TextOutput, Name: "text", MediaType: "text/plain", Path: path,
			Meta: map[string]string{dispatch.MetaEncoding: encoding},
		},
		warnings: warnings,
	}
}

func validUTF8(ctx context.Context, f *os.File) (bool, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	r := &ctxReader{ctx: ctx, r: f}
	buf := make([]byte, textChunk+utf8.UTFMax)
	carry := 0
	for {
		n, err := r.Read(buf[carry : carry+textChunk])
		n += carry
		if err == io.EOF {
			return utf8.Valid(buf[:n]), nil
		}
		if err != nil {
			return false, err
		}
		end := n
		for i := n - 1; i >= 0 && i >= n-utf8.UTFMax; i-- {
			if utf8.RuneStart(buf[i]) {
				if !utf8.FullRune(buf[i:n]) {
					end = i
				}
				break
			}
		}
		if !utf8.Valid(buf[:end]) {
			return false, nil
		}
		carry = copy(buf, buf[end:n])
	}
}

type fffdCounter struct {
	pattern []byte
	unit    int
	n       int
	off     int64
	window  []byte
}

func (c *fffdCounter) Write(p []byte) (int, error) {
	if len(c.pattern) == 0 {
		return len(p), nil
	}
	k := len(c.pattern)
	for _, b := range p {
		if len(c.window) == k {
			copy(c.window, c.window[1:])
			c.window[k-1] = b
		} else {
			c.window = append(c.window, b)
		}
		c.off++
		if len(c.window) == k && (c.off-int64(k))%int64(c.unit) == 0 && bytes.Equal(c.window, c.pattern) {
			c.n++
		}
	}
	return len(p), nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
