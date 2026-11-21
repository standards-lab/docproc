package run

import (
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/standards-lab/docproc/rundir/schema"
)

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

const detectLen = 512

func detect(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, detectLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	return http.DetectContentType(head[:n]), nil
}
