package route_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/route"
)

func zipEntry(name string, method uint16, content string) string {
	b := make([]byte, 30)
	copy(b, "PK\x03\x04")
	binary.LittleEndian.PutUint16(b[4:], 20)
	binary.LittleEndian.PutUint16(b[8:], method)
	binary.LittleEndian.PutUint32(b[18:], uint32(len(content)))
	binary.LittleEndian.PutUint32(b[22:], uint32(len(content)))
	binary.LittleEndian.PutUint16(b[26:], uint16(len(name)))
	return string(b) + name + content + "PK\x03\x04"
}

var odfText = zipEntry("mimetype", 0, "application/vnd.oasis.opendocument.text")

func ftyp(major string, compatible ...string) string {
	body := major + "\x00\x00\x02\x00" + strings.Join(compatible, "")
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	return string(b) + "ftyp" + body + "\x00\x00\x00\x08moov"
}

func ebml(docType string) string {
	return "\x1a\x45\xdf\xa3\xa3\x42\x86\x81\x01\x42\xf7\x81\x01\x42\x82" + string([]byte{byte(0x80 | len(docType))}) + docType
}

func riffForm(form string) string {
	return "RIFF\x24\x00\x00\x00" + form + "fmt "
}

func tarHeader(name, magic string) string {
	b := make([]byte, 512)
	copy(b, name)
	copy(b[257:], magic)
	return string(b)
}

func pstHeader(client string) string {
	return "!BDN\x00\x00\x00\x00" + client + "\x17\x00\x13\x00"
}

func rar4(flags uint16) string {
	b := []byte("Rar!\x1a\x07\x00\xcf\x90\x73\x00\x00\x0d\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(b[10:], flags)
	return string(b)
}

func rar5(header ...byte) string {
	return "Rar!\x1a\x07\x01\x00\x33\x92\xb5\xe5" + string(header)
}

func oggPage(headerType byte, packet string) string {
	b := make([]byte, 27)
	copy(b, "OggS")
	b[5] = headerType
	b[26] = 1
	return string(b) + string([]byte{byte(len(packet))}) + packet
}

const (
	oggBOS       = 0x02
	vorbisPacket = "\x01vorbis\x00\x00\x00\x00\x02\x44\xac\x00\x00"
	opusPacket   = "OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00"
	theoraPacket = "\x80theora\x03\x02\x01\x00\x14\x00\x0f"
)

var (
	wordPerfect = "\xffWPC\x10\x00\x00\x00\x01\x0a"
	mboxBytes   = "From ann@example.org Sat Jan  3 01:05:34 1996\nFrom: ann@example.org\nDate: Sat, 3 Jan 1996 01:05:34 +0000\n\nbody\n"
	oneSection  = "\xe4\x52\x5c\x7b\x8c\xd8\xa7\x4d\xae\xb1\x53\x78\xd0\x29\x96\xd3" + strings.Repeat("\x00", 16)
	oneTOC      = "\xa1\x2f\xff\x43\xd9\xef\x76\x4c\x9e\xe2\x10\xea\x57\x22\x76\x5f" + strings.Repeat("\x00", 16)
	mscf        = "MSCF\x00\x00\x00\x00\x2c\x01\x00\x00"
	svgPlain    = `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`
	svgProlog   = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- a comment -->\n" +
		"<!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd\">\n" +
		"<svg xmlns=\"http://www.w3.org/2000/svg\">"
)

func utf16LE(s string) string {
	b := []byte{0xff, 0xfe}
	for i := range len(s) {
		b = append(b, s[i], 0)
	}
	return string(b)
}

func utf16BE(s string) string {
	b := []byte{0xfe, 0xff}
	for i := range len(s) {
		b = append(b, 0, s[i])
	}
	return string(b)
}

func TestIdentify(t *testing.T) {
	cases := []struct {
		name string
		head string
		want route.Kind
	}{
		{"pdf", pdfBytes, route.PDF},
		{"png", pngBytes, route.PNG},
		{"jpeg", jpegBytes, route.JPEG},
		{"gif87a", gif87, route.GIF},
		{"gif89a", gif89, route.GIF},
		{"bmp", bmp(40), route.BMP},
		{"tiff little-endian", tiffLE, route.TIFF},
		{"tiff big-endian", tiffBE, route.TIFF},
		{"emf", emf(1, " EMF"), route.EMF},
		{"wmf placeable", wmfPlace, route.WMF},
		{"wmf standard", wmf(1, 9, 0x0300), route.WMF},
		{"svg", svgPlain, route.SVG},
		{"svg after a prolog", svgProlog, route.SVG},
		{"svg after a byte order mark", "\xef\xbb\xbf" + svgPlain, route.SVG},
		{"svg after a doctype's internal subset", `<!DOCTYPE svg [ <!ENTITY a "b"> ]><svg>`, route.SVG},
		{"heic", ftyp("heic", "mif1", "heic"), route.HEIC},
		{"heif structural brand", ftyp("mif1", "mif1", "heic", "hevc"), route.HEIC},
		{"avif", ftyp("avif", "avif", "mif1", "miaf"), route.AVIF},
		{"avif under a structural brand", ftyp("mif1", "mif1", "avif", "miaf"), route.AVIF},
		{"webp", riffForm("WEBP"), route.WebP},
		{"zip", zipBytes, route.ZIP},
		{"ole2", ole2Bytes, route.OLE2},
		{"odf", odfText, route.ODF},
		{"odf spreadsheet", zipEntry("mimetype", 0, "application/vnd.oasis.opendocument.spreadsheet"), route.ODF},
		{"rtf", `{\rtf1\ansi`, route.RTF},
		{"wordperfect", wordPerfect, route.WordPerfect},
		{"pst", pstHeader("SM"), route.PST},
		{"ost", pstHeader("SO"), route.OST},
		{"mbox", mboxBytes, route.Mbox},
		{"tar posix", tarHeader("a.txt", "ustar\x0000"), route.Tar},
		{"tar gnu", tarHeader("a.txt", "ustar  \x00"), route.Tar},
		{"tar whose first member is named like a pe", tarHeader("MZ.exe", "ustar\x0000"), route.Tar},
		{"onestore section", oneSection, route.OneStore},
		{"onestore table of contents", oneTOC, route.OneStore},
		{"cabinet", mscf, route.MSCF},
		{"wav", riffForm("WAVE"), route.WAV},
		{"flac", "fLaC\x00\x00\x00\x22", route.FLAC},
		{"mp3 id3", "ID3\x04\x00\x00\x00\x00\x00\x22", route.MP3},
		{"mp3 mpeg-1 frame", "\xff\xfb\x90\x64\x00", route.MP3},
		{"mp3 mpeg-2 frame", "\xff\xf3\x88\xc0\x00", route.MP3},
		{"ogg", "OggS\x00\x02\x00\x00", route.Ogg},
		{"ogg vorbis", oggPage(oggBOS, vorbisPacket) + oggPage(0, "\x03vorbis"), route.Ogg},
		{"ogg opus", oggPage(oggBOS, opusPacket), route.Ogg},
		{"ogg theora", oggPage(oggBOS, theoraPacket), route.OggVideo},
		{"ogg vp8", oggPage(oggBOS, "OVP80\x01\x01\x00"), route.OggVideo},
		{"ogg dirac", oggPage(oggBOS, "BBCD\x00\x00\x00\x00"), route.OggVideo},
		{"ogg ogm video", oggPage(oggBOS, "\x01video\x00\x00\x00DIV3"), route.OggVideo},
		{"ogg theora after a vorbis stream's first page", oggPage(oggBOS, vorbisPacket) + oggPage(oggBOS, theoraPacket), route.OggVideo},
		{"ogg theora after the first pages", oggPage(oggBOS, vorbisPacket) + oggPage(0, theoraPacket), route.Ogg},
		{"ogg theora past the head", oggPage(oggBOS, vorbisPacket) + strings.Repeat("\x00", route.HeadLen) + oggPage(oggBOS, theoraPacket), route.Ogg},
		{"ogg theora cut short", oggPage(oggBOS, theoraPacket)[:30], route.Ogg},
		{"mpeg-1 program stream", "\x00\x00\x01\xba\x21\x00\x01\x00\x01\x80\x1b\x91", route.MPEGPS},
		{"mpeg-2 program stream", "\x00\x00\x01\xba\x44\x00\x04\x00\x04\x01\x01\x89\xc3\xf8", route.MPEGPS},
		{"mpeg video sequence, no pack", "\x00\x00\x01\xb3\x16\x00\xf0\x15", route.Unknown},
		{"m4a", ftyp("M4A ", "M4A ", "isom"), route.M4A},
		{"m4b with a nul-padded brand", ftyp("M4A\x00", "3gp5", "isom"), route.M4A},
		{"mp4", ftyp("isom", "isom", "iso2", "avc1", "mp41"), route.MP4},
		{"quicktime", ftyp("qt  ", "qt  "), route.MP4},
		{"matroska", ebml("matroska"), route.MKV},
		{"webm", ebml("webm"), route.WebM},
		{"ebml without a doctype in its head", "\x1a\x45\xdf\xa3\x9f", route.MKV},
		{"avi", riffForm("AVI "), route.AVI},
		{"gzip", "\x1f\x8b\x08\x00", route.Gzip},
		{"zstd", "\x28\xb5\x2f\xfd\x04", route.Zstd},
		{"7z", "7z\xbc\xaf\x27\x1c\x00\x04", route.SevenZip},
		{"rar4", rar4(0), route.RAR4},
		{"rar4 volume", rar4(0x0001), route.RAR4},
		{"rar5", rar5(0x03, 0x01, 0x00, 0x00), route.RAR5},
		{"rar5 with encrypted headers", rar5(0x0c, 0x04, 0x00, 0x00, 0x00), route.RAR5},
		{"rar 1.5 signature cut short", "Rar!\x1a\x07", route.Unknown},
		{"rar of an unknown version", "Rar!\x1a\x07\x02\x00", route.Unknown},
		{"xz", "\xfd7zXZ\x00\x00\x04\xe6\xd6\xb4\x46", route.XZ},
		{"bzip2", "BZh91AY&SY\x8a\x1b", route.Bzip2},
		{"bzip2 of block size 1", "BZh11AY&SY", route.Bzip2},
		{"bzip2 empty stream", "BZh9\x17\x72\x45\x38\x50\x90\x00\x00\x00\x00", route.Bzip2},
		{"bzh prose", "BZh9 is how a bzip2 stream begins", route.Unknown},
		{"bzh with block size 0", "BZh01AY&SY", route.Unknown},
		{"bzip2 block magic cut short", "BZh91AY&S", route.Unknown},
		{"pe", peBytes, route.PE},
		{"elf", "\x7fELF\x02\x01\x01", route.ELF},
		{"mach-o 32-bit", "\xfe\xed\xfa\xce\x00", route.MachO},
		{"mach-o 64-bit", "\xfe\xed\xfa\xcf\x00", route.MachO},
		{"mach-o 32-bit little-endian", "\xce\xfa\xed\xfe\x00", route.MachO},
		{"mach-o 64-bit little-endian", "\xcf\xfa\xed\xfe\x00", route.MachO},
		{"mach-o universal", "\xca\xfe\xba\xbe\x00", route.MachO},
		{"shebang", "#!/bin/sh\necho hi\n", route.Shebang},
		{"shebang with a space", "#! \t/usr/bin/env python3\n", route.Shebang},
		{"pdf after white space", "\r\n \x00\t" + pdfBytes, route.PDF},
		{"pdf after a byte order mark", "\xef\xbb\xbf\n" + pdfBytes, route.PDF},
		{"pdf header at 1,023", strings.Repeat(" ", 1023) + pdfBytes, route.PDF},
		{"svg in utf-16 little-endian", utf16LE(svgProlog), route.SVG},
		{"svg in utf-16 big-endian", utf16BE(svgPlain), route.SVG},
		{"utf-8 bom", "\xef\xbb\xbfplain text", route.UTF8BOM},
		{"utf-16 little-endian bom", "\xff\xfea\x00", route.UTF16BOM},
		{"utf-16 big-endian bom", "\xfe\xff\x00a", route.UTF16BOM},
		{"utf-32 little-endian bom", "\xff\xfe\x00\x00a\x00\x00\x00", route.UTF32BOM},
		{"utf-32 big-endian bom", "\x00\x00\xfe\xff\x00\x00\x00a", route.UTF32BOM},

		{"empty", "", route.Unknown},
		{"text", "plain text", route.Unknown},
		{"odf mimetype compressed", zipEntry("mimetype", 8, "application/vnd.oasis.opendocument.text"), route.ZIP},
		{"odf mimetype not first", zipEntry("content.xml", 0, "application/vnd.oasis.opendocument.text"), route.ZIP},
		{"epub mimetype", zipEntry("mimetype", 0, "application/epub+zip"), route.ZIP},
		{"odf mimetype cut short", odfText[:40], route.ZIP},
		{"riff of another form", riffForm("CDXA"), route.Unknown},
		{"riff cut short", "RIFF\x24\x00", route.Unknown},
		{"aac adts frame, layer 0", "\xff\xf1\x50\x80", route.Unknown},
		{"mpeg frame with bitrate 15", "\xff\xfb\xf0\x64", route.Unknown},
		{"mpeg frame with reserved sample rate", "\xff\xfb\x9c\x64", route.Unknown},
		{"mpeg frame of the reserved version", "\xff\xeb\x90\x64", route.Unknown},
		{"html with inline svg", "<html><svg></svg></html>", route.Unknown},
		{"svg-like element", "<svgfoo/>", route.Unknown},
		{"svg after an unterminated comment", "<!-- <svg>", route.Unknown},
		{"svg past the head", "<!--" + strings.Repeat("x", route.HeadLen) + "--><svg>", route.Unknown},
		{"ftyp cut short", "\x00\x00\x00\x18ftyp", route.Unknown},
		{"onestore guid cut short", oneSection[:8], route.Unknown},
		{"cabinet with reserved bytes set", "MSCF\x01\x00\x00\x00", route.Unknown},
		{"pst of another client", pstHeader("XX"), route.Unknown},
		{"pst magic cut short", "!BDN\x00\x00\x00\x00S", route.Unknown},
		{"from field, no mbox line", "From: ann@example.org\n", route.Unknown},
		{"tar magic cut short", tarHeader("a.txt", "ustar")[:261], route.Unknown},
		{"tar magic elsewhere", tarHeader("a.txt", "")[:256] + "ustar", route.Unknown},
		{"wordperfect without its ff", "WPC\x10", route.Unknown},
		{"pdf header at 1,024", strings.Repeat(" ", 1024) + pdfBytes, route.Unknown},
		{"pdf header after text", "a report\n" + pdfBytes, route.Unknown},
		{"mz text", "MZ is a prefix, and this is prose that runs past the DOS header's sixty-four bytes", route.Unknown},
		{"mz without a pe signature", pe(0x40)[:0x40] + strings.Repeat("\x00", 0x40), route.Unknown},
		{"pe signature past the head", pe(route.HeadLen), route.Unknown},
		{"id3 text", "ID3 tags name a track's artist", route.Unknown},
		{"id3 of an unknown version", "ID3\x09\x00\x00\x00\x00\x00\x22", route.Unknown},
		{"id3 with a size's high bit set", "ID3\x04\x00\x00\x80\x00\x00\x22", route.Unknown},
		{"id3 cut short", "ID3\x04\x00\x00", route.Unknown},
		{"shebang without a path", "#!python\n", route.Unknown},
		{"shebang prose", "#! is a shebang\n", route.Unknown},
		{"utf-16 text", utf16LE("plain text"), route.UTF16BOM},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := route.Identify([]byte(c.head)); got != c.want {
				t.Errorf("Identify = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPNGSignature(t *testing.T) {
	if got := route.Identify([]byte(route.PNGSignature)); got != route.PNG {
		t.Errorf("Identify(PNGSignature) = %q, want %q", got, route.PNG)
	}
	if got := route.Identify([]byte(route.PNGSignature[:7])); got == route.PNG {
		t.Errorf("Identify of seven bytes = %q, want no PNG", got)
	}
}

func TestRARVolume(t *testing.T) {
	cases := []struct {
		name string
		head string
		want bool
	}{
		{"rar4", rar4(0), false},
		{"rar4 volume", rar4(0x0001), true},
		{"rar4 volume, first", rar4(0x0101), true},
		{"rar4 solid", rar4(0x0008), false},
		{"rar4 of another first header", rar4(0x0001)[:9] + "\x74\x01\x00", false},
		{"rar4 cut short", rar4(0x0001)[:11], false},
		{"rar5", rar5(0x03, 0x01, 0x00, 0x00), false},
		{"rar5 volume", rar5(0x03, 0x01, 0x00, 0x01), true},
		{"rar5 volume with a volume number", rar5(0x04, 0x01, 0x00, 0x03, 0x02), true},
		{"rar5 solid", rar5(0x03, 0x01, 0x00, 0x04), false},
		{"rar5 volume after an extra area's size", rar5(0x05, 0x01, 0x01, 0x02, 0x01), true},
		{"rar5 volume after an extra area's and a data area's sizes", rar5(0x06, 0x01, 0x03, 0x02, 0x00, 0x01), true},
		{"rar5 volume after a two-byte header size", rar5(0x80, 0x01, 0x01, 0x00, 0x01), true},
		{"rar5 with encrypted headers", rar5(0x0c, 0x04, 0x00, 0x00, 0x00), false},
		{"rar5 cut short", rar5(0x03, 0x01, 0x00), false},
		{"rar5 of an endless vint", rar5(0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01), false},
		{"7z", "7z\xbc\xaf\x27\x1c\x00\x04", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := route.RARVolume([]byte(c.head)); got != c.want {
				t.Errorf("RARVolume = %v, want %v", got, c.want)
			}
		})
	}
}
