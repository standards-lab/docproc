//go:build ignore

// generate writes the sevenzip package's generated fixtures, which README.md describes, into
// generated/, beside this file. Run it from sevenzip/testdata with
// `mise exec -- go run generate.go`; the check never runs it, and reads the committed files.
package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const outDir = "generated"

const password = "infected"

const onenoteSectionGroup = "../../tools/onenote/tests/fixtures/onenote-rs/New Section Group"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	cleanTar, err := os.ReadFile("../../inspect/testdata/clean.tar")
	if err != nil {
		return err
	}
	section, err := os.ReadFile(filepath.Join(onenoteSectionGroup, "New Section 1.one"))
	if err != nil {
		return err
	}
	toc, err := os.ReadFile(filepath.Join(onenoteSectionGroup, "Open Notebook.onetoc2"))
	if err != nil {
		return err
	}
	zeros := make([]byte, 8<<20)
	files := map[string][]byte{
		"clean.tar.zst":     zstdOf(cleanTar, -1),
		"bomb.zst":          zstdOf(zeros, -1),
		"declared-bomb.zst": zstdOf(make([]byte, 1024), 4<<30),
		"notebook.cab":      cabOf(cabFile{"Open Notebook.onetoc2", toc}, cabFile{"New Section 1.one", section}),
		"traversal.cab": cabOf(
			cabFile{"notes.txt", []byte("a cabinet's ordinary entry\n")},
			cabFile{`docs\..\..\evil.txt`, []byte("written outside the extraction directory\n")},
		),
		"absolute.cab": cabOf(
			cabFile{`\tmp\evil.txt`, []byte("an absolute path\n")},
			cabFile{`C:\evil.txt`, []byte("a drive letter\n")},
		),
		"bomb.cab": cabOf(cabFile{"zeros.bin", zeros}),
		"linebreak.cab": cabOf(
			cabFile{"before.txt", []byte("the entry before\n")},
			cabFile{"line\nbreak.txt", []byte("a name with a line break\n")},
			cabFile{"return\r", []byte("a name ending in a carriage return\n")},
			cabFile{"after.txt", []byte("the entry after\n")},
		),
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), b, 0o644); err != nil {
			return err
		}
	}
	return sevenZips(zeros, cleanTar)
}

func zstdOf(content []byte, declare int64) []byte {
	const (
		blockMax                = 128 << 10
		descriptorContentSize8  = 0xc0
		descriptorNoContentSize = 0x00
		window128KiB            = 0x38
		rleBlock                = 1 << 1
	)
	b := []byte{0x28, 0xb5, 0x2f, 0xfd}
	if declare >= 0 {
		b = append(b, descriptorContentSize8, window128KiB)
		b = binary.LittleEndian.AppendUint64(b, uint64(declare))
	} else {
		b = append(b, descriptorNoContentSize, window128KiB)
	}
	for start := 0; start < len(content); start += blockMax {
		block := content[start:min(start+blockMax, len(content))]
		last := uint32(0)
		if start+blockMax >= len(content) {
			last = 1
		}
		if bytes.Count(block, block[:1]) == len(block) {
			header := last | rleBlock | uint32(len(block))<<3
			b = append(b, byte(header), byte(header>>8), byte(header>>16), block[0])
			continue
		}
		header := last | uint32(len(block))<<3
		b = append(b, byte(header), byte(header>>8), byte(header>>16))
		b = append(b, block...)
	}
	return b
}

type cabFile struct {
	name string
	body []byte
}

func cabOf(files ...cabFile) []byte {
	const (
		headerLen = 36
		folderLen = 8
		blockMax  = 32 << 10
		dosDate   = (2026-1980)<<9 | 11<<5 | 5
		attrArch  = 0x20

		firstFolder   = 0
		midnight      = 0
		noChecksum    = 0
		versionMinor  = 3
		versionMajor  = 1
		noFlags       = 0
		setID         = 0x5a7a
		compressMSZIP = 1
	)
	var folder []byte
	var entries []byte
	for _, f := range files {
		entries = binary.LittleEndian.AppendUint32(entries, uint32(len(f.body)))
		entries = binary.LittleEndian.AppendUint32(entries, uint32(len(folder)))
		entries = binary.LittleEndian.AppendUint16(entries, firstFolder)
		entries = binary.LittleEndian.AppendUint16(entries, dosDate)
		entries = binary.LittleEndian.AppendUint16(entries, midnight)
		entries = binary.LittleEndian.AppendUint16(entries, attrArch)
		entries = append(entries, f.name...)
		entries = append(entries, 0)
		folder = append(folder, f.body...)
	}
	var data []byte
	blocks := 0
	for start := 0; start < len(folder); start += blockMax {
		chunk := folder[start:min(start+blockMax, len(folder))]
		var packed bytes.Buffer
		packed.WriteString("CK")
		fw, err := flate.NewWriter(&packed, flate.BestCompression)
		if err != nil {
			panic(err)
		}
		fw.Write(chunk)
		fw.Close()
		data = binary.LittleEndian.AppendUint32(data, noChecksum)
		data = binary.LittleEndian.AppendUint16(data, uint16(packed.Len()))
		data = binary.LittleEndian.AppendUint16(data, uint16(len(chunk)))
		data = append(data, packed.Bytes()...)
		blocks++
	}
	filesAt := headerLen + folderLen
	dataAt := filesAt + len(entries)
	total := dataAt + len(data)
	var b []byte
	b = append(b, "MSCF"...)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(total))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(filesAt))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = append(b, versionMinor, versionMajor)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(files)))
	b = binary.LittleEndian.AppendUint16(b, noFlags)
	b = binary.LittleEndian.AppendUint16(b, setID)
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(dataAt))
	b = binary.LittleEndian.AppendUint16(b, uint16(blocks))
	b = binary.LittleEndian.AppendUint16(b, compressMSZIP)
	b = append(b, entries...)
	return append(b, data...)
}

func sevenZips(zeros, cleanTar []byte) error {
	src, err := os.MkdirTemp("", "sevenzip-fixtures-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(src)
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	write := func(name string, body []byte) error {
		path := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
		return os.Chmod(path, 0o644)
	}
	for name, body := range map[string][]byte{
		"docs/notes.txt": []byte("a 7z's ordinary entry\n"),
		"docs/pixel.png": []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x03\x00\x00\x00\x01\x08\x02\x00\x00\x00\x94\x82\x83\xe3"),
		"notes.txt":      []byte("an ordinary entry\n"),
		"evil.txt":       []byte("written outside the extraction directory\n"),
		"absolute.txt":   []byte("an absolute path\n"),
		"drive.txt":      []byte("a drive letter\n"),
		"secret.txt":     []byte("encrypted with the fixtures' password\n"),
		"zeros.bin":      zeros,
		"empty.txt":      nil,
		"clean.tar":      cleanTar,
	} {
		if err := write(name, body); err != nil {
			return err
		}
	}
	if err := os.Chmod(filepath.Join(src, "docs"), 0o755); err != nil {
		return err
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "passwd")); err != nil {
		return err
	}
	for i := range 12 {
		if err := write(fmt.Sprintf("blocks/f%02d.txt", i+1), lines(fmt.Sprintf("multi-block file %02d", i+1), 40+i*7)); err != nil {
			return err
		}
	}
	for _, method := range []string{"lzma2", "ppmd", "bzip2", "deflate", "copy"} {
		for _, n := range []string{"a", "b"} {
			name := fmt.Sprintf("%s/%s.txt", method, n)
			if err := write(name, lines("mixed-method file "+name, 30+len(method)*5+len(n))); err != nil {
				return err
			}
		}
	}
	if err := mkfifo(filepath.Join(src, "pipe")); err != nil {
		return err
	}
	addReproducibly := []string{"a", "-mtm-", "-mtc-", "-mta-", "-mmt=1", "-bd"}
	addStream := []string{"a", "-txz", "-mmt=1", "-bd"}
	addBzip2 := []string{"a", "-tbzip2", "-mmt=1", "-bd"}
	sz := func(args ...string) error {
		cmd := exec.Command("7zz", args...)
		cmd.Dir = src
		cmd.Stderr = os.Stderr
		if b, err := cmd.Output(); err != nil {
			return fmt.Errorf("7zz %v: %w\n%s", args, err, b)
		}
		return nil
	}
	archive := func(name string) string {
		path := filepath.Join(outAbs, name)
		_ = os.Remove(path)
		return path
	}
	steps := [][]string{
		append(append([]string{}, addReproducibly...), archive("clean.7z"), "docs"),
		append(append([]string{}, addReproducibly...), archive("traversal.7z"), "notes.txt", "evil.txt"),
		{"rn", filepath.Join(outAbs, "traversal.7z"), "evil.txt", "docs/../../evil.txt"},
		append(append([]string{}, addReproducibly...), archive("absolute.7z"), "absolute.txt", "drive.txt"),
		{"rn", filepath.Join(outAbs, "absolute.7z"), "absolute.txt", "/tmp/evil.txt"},
		{"rn", filepath.Join(outAbs, "absolute.7z"), "drive.txt", `C:\evil.txt`},
		append(append([]string{}, addReproducibly...), "-snl", archive("symlink.7z"), "notes.txt", "passwd"),
		append(append([]string{}, addReproducibly...), "-snl", archive("device.7z"), "notes.txt", "pipe"),
		append(append([]string{}, addReproducibly...), archive("bomb.7z"), "zeros.bin"),
		append(append([]string{}, addReproducibly...), archive("encrypted.7z"), "notes.txt"),
		append(append([]string{}, addReproducibly...), "-p"+password, filepath.Join(outAbs, "encrypted.7z"), "secret.txt"),
		append(append([]string{}, addReproducibly...), "-p"+password, "-mhe=on", archive("encrypted-headers.7z"), "secret.txt"),
		append(append([]string{}, addReproducibly...), "-ms=3f", archive("multiblock.7z"), "blocks"),
		append(append([]string{}, addReproducibly...), "-snl", "-m0=LZMA2", archive("mixed.7z"), "lzma2", "empty.txt", "passwd"),
		append(append([]string{}, addReproducibly...), "-m0=PPMd", filepath.Join(outAbs, "mixed.7z"), "ppmd"),
		append(append([]string{}, addReproducibly...), "-m0=BZip2", filepath.Join(outAbs, "mixed.7z"), "bzip2"),
		append(append([]string{}, addReproducibly...), "-m0=Deflate", filepath.Join(outAbs, "mixed.7z"), "deflate"),
		append(append([]string{}, addReproducibly...), "-m0=Copy", filepath.Join(outAbs, "mixed.7z"), "copy"),
		append(append([]string{}, addStream...), archive("notes.txt.xz"), "notes.txt"),
		append(append([]string{}, addStream...), archive("clean.txz"), "clean.tar"),
		append(append([]string{}, addStream...), archive("bomb.xz"), "zeros.bin"),
		append(append([]string{}, addBzip2...), archive("notes.txt.bz2"), "notes.txt"),
		append(append([]string{}, addBzip2...), archive("clean.tbz2"), "clean.tar"),
		append(append([]string{}, addBzip2...), archive("bomb.bz2"), "zeros.bin"),
		append(append([]string{}, addBzip2...), filepath.Join(src, "first.bz2"), "notes.txt"),
		append(append([]string{}, addBzip2...), filepath.Join(src, "second.bz2"), "evil.txt"),
	}
	for _, args := range steps {
		if err := sz(args...); err != nil {
			return err
		}
	}
	var concatenated []byte
	for _, name := range []string{"first.bz2", "second.bz2"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			return err
		}
		concatenated = append(concatenated, b...)
	}
	return os.WriteFile(filepath.Join(outAbs, "concatenated.txt.bz2"), concatenated, 0o644)
}

func lines(file string, n int) []byte {
	var b bytes.Buffer
	for i := range n {
		fmt.Fprintf(&b, "%s, line %03d\n", file, i+1)
	}
	return b.Bytes()
}

func mkfifo(path string) error {
	if b, err := exec.Command("mkfifo", "-m", "0644", path).CombinedOutput(); err != nil {
		return fmt.Errorf("mkfifo: %w: %s", err, b)
	}
	return nil
}
