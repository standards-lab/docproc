package rundir

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
	"uuid"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir/schema"
)

// Index returns the run directory's index, dispatch's FileIndex in its index directory, which
// every run over it shares; nil on a Dir from [Open], which reads alone.
func (d *Dir) Index() dispatch.Index {
	if d.index == nil {
		return nil
	}
	return d.index
}

// checkWrite refuses a writer's call on a Dir from Open.
func (d *Dir) checkWrite(call string) error {
	if !d.write {
		return fmt.Errorf("rundir: %s: %s is opened to read", call, d.root)
	}
	return nil
}

// Quarantine keeps the file at path, a quarantined part's whose SHA-256 is sha256, as
// quarantine/<sha256>, and returns that name, relative to the run directory, with slashes, as a
// record line's "quarantine" names it. A file already kept there, by this run or an earlier one,
// is kept as it is, so a part is copied once. A sha256 that isn't 64 lowercase hex digits is
// refused, matching dispatch.ErrRequest.
func (d *Dir) Quarantine(sha256, path string) (string, error) {
	if err := d.checkWrite("quarantine"); err != nil {
		return "", err
	}
	if dispatch.ContentFile(sha256) == "" {
		return "", fmt.Errorf("rundir: quarantine %q isn't a SHA-256 as lowercase hex: %w", sha256, dispatch.ErrRequest)
	}
	rel := quarantineDir + "/" + sha256
	dst := filepath.Join(d.root, quarantineDir, sha256)
	if _, err := os.Lstat(dst); err == nil {
		return rel, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("rundir: %w", err)
	}
	in, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("rundir: %w", err)
	}
	defer func() { _ = in.Close() }() // read only: a close error loses nothing
	err = writeFile(dst, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("rundir: quarantine %s: %w", sha256, err)
	}
	return rel, nil
}

// Run is one run over a run directory, begun by [Dir.Begin]: its manifest and its record, under
// runs/<run>/. It is not safe for concurrent use.
type Run struct {
	dir      string // runs/<run>/ under the run directory's root
	manifest schema.Manifest
	file     *os.File
	enc      *json.Encoder
}

// Begin begins a run over d from m. It first removes the .work-* directories at d's root, which a
// killed run left, since one process writes a run directory. The run's id is m's Run, which must
// be a version 7 UUID in its lowercase form naming no run d holds, or a new one when m's is empty;
// its start is m's Start, or now when that is zero. Begin sets the manifest's schema, clears any
// end and error, makes the run's directory, writes its manifest, and opens its record.
func (d *Dir) Begin(m schema.Manifest) (*Run, error) {
	if err := d.checkWrite("begin"); err != nil {
		return nil, err
	}
	work, err := filepath.Glob(filepath.Join(d.root, workPattern))
	if err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	for _, w := range work {
		if err := os.RemoveAll(w); err != nil {
			return nil, fmt.Errorf("rundir: %w", err)
		}
	}
	if m.Run == "" {
		m.Run = uuid.NewV7().String()
	} else if !validID(m.Run) {
		return nil, fmt.Errorf("rundir: run id %q isn't a version 7 UUID in its lowercase form: %w", m.Run, dispatch.ErrRequest)
	}
	if m.Start.IsZero() {
		m.Start = schema.TimeOf(time.Now())
	}
	m.Schema, m.End, m.Error = schema.Version, schema.Time{}, ""
	dir := filepath.Join(d.root, runsDir, m.Run)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	// Mkdir, not MkdirAll: a run's directory is made once, so an id can't name two runs.
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("rundir: begin: %w", err)
	}
	r := &Run{dir: dir, manifest: m}
	if err := r.writeManifest(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, recordFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("rundir: %w", err)
	}
	// The encoder writes each line to the file in one write, unbuffered: a line written is on its
	// way to the disk, and a killed run loses at most the one it was writing.
	r.file, r.enc = f, json.NewEncoder(f)
	r.enc.SetEscapeHTML(false)
	return r, nil
}

// ID returns the run's id.
func (r *Run) ID() string { return r.manifest.Run }

// Write appends e to the run's record as one line, written through to the file in one write, with
// no fsync.
func (r *Run) Write(e schema.Event) error {
	if err := r.enc.Encode(e); err != nil {
		return fmt.Errorf("rundir: record %s: %w", r.manifest.Run, err)
	}
	return nil
}

// End syncs and closes the run's record, then writes its manifest again with its end, now, and,
// when err isn't nil, err's text as the error that stopped the whole run.
func (r *Run) End(err error) error {
	closed := errors.Join(r.file.Sync(), r.file.Close())
	if closed != nil {
		closed = fmt.Errorf("rundir: record %s: %w", r.manifest.Run, closed)
	}
	r.manifest.End = schema.TimeOf(time.Now())
	if err != nil {
		r.manifest.Error = err.Error()
	}
	return errors.Join(closed, r.writeManifest())
}

// writeManifest writes the run's manifest whole, replacing the one written before.
func (r *Run) writeManifest() error {
	err := writeFile(filepath.Join(r.dir, manifestFile), func(w io.Writer) error {
		return encodeIndented(w, r.manifest)
	})
	if err != nil {
		return fmt.Errorf("rundir: manifest %s: %w", path.Join(runsDir, r.manifest.Run), err)
	}
	return nil
}
