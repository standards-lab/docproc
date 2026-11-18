package report

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/standards-lab/docproc/rundir"
)

// Make renders the report of the run id in the run directory d, whose root is root, as [Render]
// does, then prints its index page's path to w, absolute, on a line of its own: what docproc
// report prints, and docproc run --report after its summary.
func Make(ctx context.Context, w io.Writer, d *rundir.Dir, root, id string) error {
	if err := Render(ctx, d, root, id); err != nil {
		return err
	}
	rel, err := d.ReportDir(id)
	if err != nil {
		return err
	}
	index, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel), IndexPage))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, index)
	return err
}
