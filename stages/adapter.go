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

// adapterVersion is the version of the stage adapters, part of every adapter's configuration
// hash: a change to what an adapter yields for the same tools and request changes it, so the index
// prepares content again.
const adapterVersion = "6"

// versions returns the versions of the tools behind a stage, for its identity's Version: each
// tool's whole version line, as its version command printed it, joined by ", " in the order
// given, so a development build and a release of one version differ.
func versions(tools ...*tool.Tool) string {
	lines := make([]string, len(tools))
	for i, t := range tools {
		lines[i] = t.VersionLine()
	}
	return strings.Join(lines, ", ")
}

// identity returns the identity of the adapter named name over tools of version: its Config
// hashes request, the format package's request resolved, every default filled and each field
// that changes no output tagged out of its JSON, with adapterVersion. It panics on an empty
// version, which would key every tool version's outputs alike, the wiring's mistake.
func identity(name, version string, request any) dispatch.Identity {
	if version == "" {
		panic("stages: " + name + " stage: no version")
	}
	hash, err := dispatch.HashConfig(struct {
		Adapter string `json:"adapter"`
		Request any    `json:"request"`
	}{adapterVersion, request})
	if err != nil {
		// A request is plain data, which always marshals.
		panic(err)
	}
	return dispatch.Identity{Name: name, Version: version, Config: hash}
}

// markdown returns a Markdown output named name holding md inline, with meta.
func markdown(name, md string, meta map[string]string) dispatch.Output {
	return dispatch.Output{Kind: dispatch.MarkdownOutput, Name: name, MediaType: "text/markdown", Value: []byte(md), Meta: meta}
}

// unread returns the item of an unread child found at where, named name: no file, its outcome,
// and why, with flags, the checks that name it, if any.
func unread(where, name string, outcome dispatch.Outcome, reason string, flags ...inspect.Flag) dispatch.Item {
	return dispatch.PartItem(dispatch.Found{Name: name, Where: where, Outcome: outcome, Reason: reason, Flags: flags})
}

// medium is one medium or attachment a tool's line names, as its reader resolved it: its file,
// if the tool wrote one, and its SHA-256 as internal/cli's media name rule claims it, the name and
// stored type to find it by, and, when it has no file, the outcome and reason of its unread child.
type medium struct {
	path, sum, name, mime string
	// none is the outcome of a medium with no content to find, dispatch.Failed or
	// dispatch.NoProcessor, and why its reason.
	none dispatch.Outcome
	why  string
}

// item is the one rule that turns a line's medium into its item, found at where, "<line>, media
// <n>" or "<line>, attachment <n>": a found part, its file a link the stash places and its SHA-256
// claimed, when the tool wrote its content; a found part with no file, by its SHA-256, when the
// stash has released the content to the dispatcher; and otherwise an unread child, m's outcome
// and reason when its line names no content, failed when its file isn't in the media directory.
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

// stash places the files a CLI wrote into a directory of their own each, before a stage yields
// them as found parts, and removes each original once the dispatcher has settled its content.
//
// A CLI that writes each distinct content once, as mail and onenote do, names one file, by the
// content's SHA-256, for every record that holds that content, records the stage may meet after
// the first is dispatched; the dispatcher removes a found part's file once its event is done, so
// the stash yields a hard link, which is removed in the original's place, the original staying
// for those later records. A found part whose SHA-256 its reader claims, by internal/cli's media
// name rule, carries it, and a Release that removes the original: the dispatcher calls it once the index holds the content's
// entry, so the disk holds the originals of the records in flight, and of content the
// dispatcher couldn't settle, a later record of which is dispatched from the original again. A
// later record naming a released original is found with no file, by its SHA-256, which the
// dispatcher sees from the index.
type stash struct {
	dir string
	n   int
	// last is the directory of the last link placed, removed with the next place once the
	// dispatcher has removed its link.
	last string
	// released are the originals removed on the dispatcher's release, their SHA-256 by path.
	released map[string]string
}

// release removes the original at path, whose content's SHA-256 is sum, and remembers it, so a
// later record naming it is found with no file. An original that can't be removed stays, and a
// later record finds it there.
func (s *stash) release(path, sum string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	if s.released == nil {
		s.released = map[string]string{}
	}
	s.released[path] = sum
}

// place returns the path of a link to the file at path, in a new directory under the stash's,
// named as path is: a hard link, else a copy where the file system refuses one. It removes the
// last link's directory, which is empty once the dispatcher has removed the link.
func (s *stash) place(path string) (string, error) {
	if s.last != "" {
		_ = os.Remove(s.last) // a directory the caller left a file in stays
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

// copyFile copies the file at src to a new file at dst.
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

// exists reports whether a file is at path, an error other than its absence failing.
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

// record returns a record output named name holding v as JSON, through encoding/json/v2, as the
// format packages' records marshal back to their lines, of mediaType, with meta.
func record(name string, v any, mediaType string, meta map[string]string) (dispatch.Output, error) {
	b, err := jsonv2.Marshal(v)
	if err != nil {
		return dispatch.Output{}, fmt.Errorf("stages: %s: %w", name, err)
	}
	return dispatch.Output{Kind: dispatch.RecordOutput, Name: name, MediaType: mediaType, Value: b, Meta: meta}, nil
}
