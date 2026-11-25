package stages

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/tool"
)

const adapterVersion = "7"

func versions(tools ...*tool.Tool) string {
	lines := make([]string, len(tools))
	for i, t := range tools {
		lines[i] = t.VersionLine()
	}
	return strings.Join(lines, ", ")
}

func identity(name, version string, request any) dispatch.Identity {
	if version == "" {
		panic("stages: " + name + " stage: no version")
	}
	hash, err := dispatch.HashConfig(struct {
		Adapter string `json:"adapter"`
		Request any    `json:"request"`
	}{adapterVersion, request})
	if err != nil {
		panic(err)
	}
	return dispatch.Identity{Name: name, Version: version, Config: hash}
}

func markdown(name, md string, meta map[string]string) dispatch.Output {
	return dispatch.Output{Kind: dispatch.MarkdownOutput, Name: name, MediaType: "text/markdown", Value: []byte(md), Meta: meta}
}

func unread(where, name string, outcome dispatch.Outcome, reason string, flags ...inspect.Flag) dispatch.Item {
	return dispatch.PartItem(dispatch.Found{Name: name, Where: where, Outcome: outcome, Reason: reason, Flags: flags})
}

type medium struct {
	path, sum, name, mime string
	none                  dispatch.Outcome
	why                   string
}

func (s *stash) item(m medium, where string) (dispatch.Item, error) {
	if m.path == "" {
		return unread(where, m.name, m.none, m.why), nil
	}
	f := dispatch.Found{Name: m.name, MIME: m.mime, Where: where}
	ok, err := exists(m.path)
	switch {
	case err != nil:
		return dispatch.Item{}, err
	case !ok:
		if sum, released := s.released[m.path]; released {
			f.SHA256 = sum
			return dispatch.PartItem(f), nil
		}
		return unread(where, m.name, dispatch.Failed, "its file isn't in the media directory"), nil
	}
	if f.Path, err = s.place(m.path); err != nil {
		return dispatch.Item{}, err
	}
	if m.sum != "" {
		f.SHA256 = m.sum
		f.Release = func() { s.release(m.path, m.sum) }
	}
	return dispatch.PartItem(f), nil
}

type stash struct {
	dir      string
	n        int
	last     string
	released map[string]string
}

func (s *stash) release(path, sum string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	if s.released == nil {
		s.released = map[string]string{}
	}
	s.released[path] = sum
}

func (s *stash) place(path string) (string, error) {
	if s.last != "" {
		_ = os.Remove(s.last)
	}
	s.n++
	dir := filepath.Join(s.dir, fmt.Sprintf("found-%d", s.n))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("stages: %w", err)
	}
	s.last = dir
	dst := filepath.Join(dir, filepath.Base(path))
	if err := os.Link(path, dst); err == nil {
		return dst, nil
	}
	if err := copyFile(path, dst); err != nil {
		return "", fmt.Errorf("stages: %w", err)
	}
	return dst, nil
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	}
	return false, fmt.Errorf("stages: %w", err)
}

func record(name string, v any, mediaType string, meta map[string]string) (dispatch.Output, error) {
	b, err := jsonv2.Marshal(v)
	if err != nil {
		return dispatch.Output{}, fmt.Errorf("stages: %s: %w", name, err)
	}
	return dispatch.Output{Kind: dispatch.RecordOutput, Name: name, MediaType: mediaType, Value: b, Meta: meta}, nil
}
