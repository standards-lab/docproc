package rundir

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"uuid"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir/schema"
)

const (
	schemaFile    = "rundir.json"
	indexDir      = "index"
	contentDir    = "content"
	quarantineDir = "quarantine"
	runsDir       = "runs"
	manifestFile  = "manifest.json"
	recordFile    = "record.jsonl"
	reportDir     = "report"
	workPattern   = ".work-*"
)

// ErrNotEnded is what [Dir.Record] yields last for a run whose manifest has no end: it is still
// running, or it was killed, and its record may lack its last line.
var ErrNotEnded = errors.New("rundir: run has not ended")

// Dir is a run directory, opened to read by [Open] or to write by [Create]. Its reader's methods
// are safe for concurrent use; its writer's are not, and one process writes a run directory.
type Dir struct {
	root    string
	write   bool
	index   *dispatch.FileIndex
	content dispatch.Content
}

type rundirFile struct {
	Schema string `json:"schema"`
}

// Open opens the run directory at root to read it, creating nothing: it fails with an error
// matching fs.ErrNotExist when root is no run directory, holding no rundir.json, and refuses one of
// another schema, or one holding runs/ with no rundir.json, which schema "1" wrote, naming both
// versions. Its [Dir.Content] reads the content store and refuses a put; its [Dir.Index] is nil,
// and its [Dir.Begin] and [Dir.Quarantine] refuse.
func Open(root string) (*Dir, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("rundir: %s is no directory: %w", root, fs.ErrNotExist)
	}
	found, err := checkSchema(root)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("rundir: %s is no run directory, holding no %s: %w", root, schemaFile, fs.ErrNotExist)
	}
	var content *dispatch.FileContent
	dir := filepath.Join(root, contentDir)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		if content, err = dispatch.NewFileContent(dir, 0); err != nil {
			return nil, fmt.Errorf("rundir: %w", err)
		}
	}
	return &Dir{root: root, content: readContent{content}}, nil
}

// Create opens the run directory at root to write it, making the directory, its rundir.json, the
// index, and the content store when they are absent; over an existing directory it refuses
// another schema, or runs/ with no rundir.json, as [Open] does. Its content store never rewrites a
// content it holds.
func Create(root string) (*Dir, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	found, err := checkSchema(root)
	if err != nil {
		return nil, err
	}
	if !found {
		err := writeFile(filepath.Join(root, schemaFile), func(w io.Writer) error {
			return encodeIndented(w, rundirFile{Schema: schema.Version})
		})
		if err != nil {
			return nil, fmt.Errorf("rundir: %w", err)
		}
	}
	index, err := dispatch.NewFileIndex(filepath.Join(root, indexDir))
	if err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	content, err := dispatch.NewFileContent(filepath.Join(root, contentDir), 0)
	if err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	return &Dir{root: root, write: true, index: index, content: content}, nil
}

func checkSchema(root string) (bool, error) {
	path := filepath.Join(root, schemaFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(root, runsDir)); err == nil {
			return false, fmt.Errorf("rundir: %s holds %s/ but no %s, as schema \"1\" wrote it: schema \"1\", want %q",
				root, runsDir, schemaFile, schema.Version)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rundir: %w", err)
	}
	var f rundirFile
	if err := json.Unmarshal(b, &f); err != nil {
		return false, fmt.Errorf("rundir: %s: %w", path, err)
	}
	if f.Schema != schema.Version {
		return false, fmt.Errorf("rundir: %s is schema %q, want %q", root, f.Schema, schema.Version)
	}
	return true, nil
}

func validID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id && u[6]>>4 == 7
}

func runPath(id, name string) string { return filepath.Join(runsDir, id, name) }

func (d *Dir) checkRun(id string) error {
	if validID(id) {
		info, err := os.Stat(filepath.Join(d.root, runPath(id, manifestFile)))
		if err == nil && info.Mode().IsRegular() {
			return nil
		}
	}
	return fmt.Errorf("rundir: %q is no run of %s: %w", id, d.root, fs.ErrNotExist)
}

// Runs returns the ids of d's runs, in the order they began, which is the ids' own. A directory
// under runs/ that isn't a run, its name no valid id or its manifest absent, is left out.
func (d *Dir) Runs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.root, runsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && d.checkRun(e.Name()) == nil {
			ids = append(ids, e.Name())
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// Latest returns the id of d's latest run that ended, its manifest holding its end; a run still
// running, or killed, is passed over. A run directory with no ended run is an error matching
// fs.ErrNotExist.
func (d *Dir) Latest() (string, error) {
	ids, err := d.Runs()
	if err != nil {
		return "", err
	}
	for _, id := range slices.Backward(ids) {
		m, err := d.Manifest(id)
		if err != nil {
			return "", err
		}
		if !m.End.IsZero() {
			return id, nil
		}
	}
	return "", fmt.Errorf("rundir: %s holds no run that ended: %w", d.root, fs.ErrNotExist)
}

// Manifest reads the manifest of d's run id, refusing an id that isn't a run of d.
func (d *Dir) Manifest(id string) (schema.Manifest, error) {
	if err := d.checkRun(id); err != nil {
		return schema.Manifest{}, err
	}
	path := filepath.Join(d.root, runPath(id, manifestFile))
	b, err := os.ReadFile(path)
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("rundir: %w", err)
	}
	var m schema.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return schema.Manifest{}, fmt.Errorf("rundir: %s: %w", path, err)
	}
	return m, nil
}

// Record yields the lines of d's run id's record, in the order they were written, refusing an id
// that isn't a run of d with an error yielded alone. It ends on the first line that doesn't
// decode, yielding its error, which names the line. For an unended run, its manifest holding no
// end, it drops a last line with no newline, the one a killed run may have torn, and yields an
// error matching [ErrNotEnded] last, after every line, so a caller may read what the run wrote and
// still know it has not ended.
func (d *Dir) Record(id string) iter.Seq2[schema.Event, error] {
	return func(yield func(schema.Event, error) bool) {
		m, err := d.Manifest(id)
		if err != nil {
			yield(schema.Event{}, err)
			return
		}
		ended := !m.End.IsZero()
		if !d.lines(id, ended, yield) {
			return
		}
		if !ended {
			yield(schema.Event{}, fmt.Errorf("rundir: run %s: %w", id, ErrNotEnded))
		}
	}
}

func (d *Dir) lines(id string, ended bool, yield func(schema.Event, error) bool) bool {
	path := filepath.Join(d.root, runPath(id, recordFile))
	f, err := os.Open(path)
	if !ended && errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		yield(schema.Event{}, fmt.Errorf("rundir: %w", err))
		return false
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	for n := 1; ; n++ {
		line, err := r.ReadBytes('\n')
		last := errors.Is(err, io.EOF)
		switch {
		case err != nil && !last:
			yield(schema.Event{}, fmt.Errorf("rundir: %w", err))
			return false
		case last && (len(line) == 0 || !ended):
			return true
		}
		var e schema.Event
		if err := json.Unmarshal(line, &e); err != nil {
			yield(schema.Event{}, fmt.Errorf("rundir: %s: line %d: %w", path, n, err))
			return false
		}
		if !yield(e, nil) {
			return false
		}
		if last {
			return true
		}
	}
}

// Content returns the run directory's content store, dispatch's FileContent in its content
// directory, where each output's content is found by its SHA-256. On a Dir from [Open] it reads
// alone, refusing a put, and misses every content when the directory is absent.
func (d *Dir) Content() dispatch.Content { return d.content }

// ReportDir returns the report directory of d's run id, runs/<run>/report, relative to the run
// directory, with slashes, refusing an id that isn't a run of d with an error matching
// fs.ErrNotExist. It names the directory alone: it neither makes it nor looks for it, and nothing
// here writes a report. A Dir from [Open] answers it too, as does a writer for the run it is
// writing, whose manifest [Dir.Begin] wrote.
func (d *Dir) ReportDir(id string) (string, error) {
	if err := d.checkRun(id); err != nil {
		return "", err
	}
	return runsDir + "/" + id + "/" + reportDir, nil
}

// ContentPath returns the file of the content sha256 names, relative to a run directory: the
// content directory joined to dispatch.ContentFile(sha256). It returns "" for a sha256 that isn't
// 64 lowercase hex digits.
func ContentPath(sha256 string) string {
	file := dispatch.ContentFile(sha256)
	if file == "" {
		return ""
	}
	return filepath.Join(contentDir, file)
}

type readContent struct {
	c *dispatch.FileContent
}

func (r readContent) Put(_ context.Context, sha256 string, _ io.Reader) error {
	return fmt.Errorf("rundir: put %s: the run directory is opened to read", sha256)
}

func (r readContent) Open(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	if r.c != nil {
		return r.c.Open(ctx, sha256)
	}
	if dispatch.ContentFile(sha256) == "" {
		return nil, fmt.Errorf("rundir: content %q isn't a SHA-256 as lowercase hex: %w", sha256, dispatch.ErrRequest)
	}
	return nil, fmt.Errorf("rundir: content %s: %w", sha256, fs.ErrNotExist)
}

func encodeIndented(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeFile(dst string, write func(io.Writer) error) (err error) {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := errors.Join(tmp.Sync(), tmp.Close()); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
