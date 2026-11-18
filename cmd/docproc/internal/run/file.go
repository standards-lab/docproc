package run

import (
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/standards-lab/docproc/rundir/schema"
)

// fileOf returns the metadata of in's file and its sniffed type, as its input's part line
// carries them: its size, mode, and modified time through os.Stat, which follows a named
// argument that is a symbolic link, its birth time through birth, where the kernel gives one, and
// its detected type through detect, left out when its head can't be read.
func fileOf(in input) (*schema.File, error) {
	info, err := os.Stat(in.path)
	if err != nil {
		return nil, err
	}
	created, err := birth(in.path)
	if err != nil {
		return nil, err
	}
	f := &schema.File{
		Name:     info.Name(),
		Arg:      in.absArg,
		Path:     in.rel,
		Size:     info.Size(),
		Mode:     info.Mode().String(),
		Modified: schema.TimeOf(info.ModTime()),
		Created:  schema.TimeOf(created),
	}
	if mt, err := detect(in.path); err == nil {
		f.Detected = mt
	}
	return f, nil
}

// detectLen is how much of a file net/http.DetectContentType reads.
const detectLen = 512

// detect returns the MIME type net/http.DetectContentType names for the first 512 bytes of the
// file at path, such as "text/plain; charset=utf-8", or "application/octet-stream" when no rule
// matches.
func detect(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // read only: a close error loses nothing
	head := make([]byte, detectLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	return http.DetectContentType(head[:n]), nil
}
