package report

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
)

// IndexPage is the file name of a report's index page; each content's page is its SHA-256 and
// ".html".
const IndexPage = "index.html"

// tempPattern names a report's working directories beside the report directory, under
// runs/<run>/: the pages being written, and the earlier pages being removed.
const tempPattern = ".report-*"

//go:embed pages.html.tmpl
var sources embed.FS

// pages is the report's templates, parsed once: "index" and "content", each a whole page.
var pages = template.Must(template.New("").Funcs(funcs).ParseFS(sources, "pages.html.tmpl"))

// funcs are the templates' functions.
var funcs = template.FuncMap{
	"time":  func(t schema.Time) string { return t.String() },
	"took":  took,
	"bytes": byteSize,
}

// took returns how long span ran, to the millisecond.
func took(span schema.Span) string {
	return span.End.Time().Sub(span.Start.Time()).Round(time.Millisecond).String()
}

// byteSize returns n bytes in binary units, to one decimal place from a KiB up.
func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f, unit := float64(n)/1024, "KiB"
	for _, u := range []string{"MiB", "GiB", "TiB"} {
		if f < 1024 {
			break
		}
		f, unit = f/1024, u
	}
	return fmt.Sprintf("%.1f %s", f, unit)
}

// Render renders the report of the run id in the run directory d, whose root is root, into the
// run's report directory, d.ReportDir(id) under root: index.html, the index of every input, and
// one page per distinct content its inputs hold, <sha256>.html. It replaces earlier pages whole:
// it writes every page into a new directory beside the report directory, then moves the earlier
// report directory aside, moves the new one into its place, and removes the earlier one, so the
// report directory never holds two renders' pages. It first removes the working directories a
// killed render left beside the report directory, since one process writes a run directory.
//
// Every link into the run directory, to an output's content or from the index to a content's
// page, is relative, so the run directory moves or archives whole; an input's source is linked by
// its absolute path, as a file URL. The pages load nothing from elsewhere.
//
// A render reads the run's manifest and record, the content store, and the kept files, and no
// clock: the same run directory renders the same bytes, in any local zone. An unended run renders
// what its record holds, marked not ended. An id that isn't a run of d is refused with an error
// matching fs.ErrNotExist, as rundir's methods refuse it.
func Render(ctx context.Context, d *rundir.Dir, root, id string) error {
	m, err := d.Manifest(id)
	if err != nil {
		return err
	}
	var lines []schema.Event
	for e, err := range d.Record(id) {
		if errors.Is(err, rundir.ErrNotEnded) {
			break // the manifest says so too: the page marks the run not ended
		}
		if err != nil {
			return err
		}
		lines = append(lines, e)
	}
	rel, err := d.ReportDir(id)
	if err != nil {
		return err
	}
	v := build(m, lines)
	if err := dress(ctx, d, rel, v); err != nil {
		return err
	}
	files, err := render(ctx, v)
	if err != nil {
		return err
	}
	return replace(filepath.Join(root, filepath.FromSlash(rel)), files)
}

// page is one rendered page: its file name in the report directory and its bytes.
type page struct {
	name string
	body []byte
}

// render returns v's pages: the index, then each content's, in v's order.
func render(ctx context.Context, v *model) ([]page, error) {
	var out []page
	exec := func(name, tmpl string, data any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := pages.ExecuteTemplate(&buf, tmpl, data); err != nil {
			return fmt.Errorf("report: %s: %w", name, err)
		}
		out = append(out, page{name: name, body: buf.Bytes()})
		return nil
	}
	if err := exec(IndexPage, "index", v); err != nil {
		return nil, err
	}
	for _, c := range v.Contents {
		if err := exec(c.SHA256+".html", "content", contentView{Run: v, Content: c}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// contentView is what a content's page renders: the run and the content.
type contentView struct {
	Run     *model
	Content *content
}

// replace writes files into dst, a report directory, replacing whatever dst held, as [Render]
// says.
func replace(dst string, files []page) (err error) {
	parent := filepath.Dir(dst)
	stale, err := filepath.Glob(filepath.Join(parent, tempPattern))
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	for _, s := range stale {
		if err := os.RemoveAll(s); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	}
	tmp, err := os.MkdirTemp(parent, tempPattern)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp) // the render failed already; the leftover is the lesser loss
		}
	}()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmp, f.name), f.body, 0o644); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	}
	// The earlier report directory, if any, moves into a working directory of its own, so the new
	// one can take its name; a rename can't replace a directory that isn't empty.
	old, err := os.MkdirTemp(parent, tempPattern)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.Rename(dst, filepath.Join(old, filepath.Base(dst))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = os.Remove(old) // empty: nothing moved into it
		return fmt.Errorf("report: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.RemoveAll(old); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	return nil
}
