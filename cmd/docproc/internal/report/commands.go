package report

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/docproc/rundir"
)

// Commands returns the report command. It declares no node: a report reads a run directory alone,
// so it builds nothing, reads no configuration, and looks up no tool.
func Commands() []*cli.Command {
	return []*cli.Command{command()}
}

const exitStatus = `Exit status:
  0  the report was rendered, a run that never ended among them, its pages marked not ended
  1  nothing was rendered: a run directory that doesn't exist or isn't one, a --run that isn't
     one of its runs, no --run and no run that ended, or a page that couldn't be read or written
  2  a usage error, such as no run directory or more than one
`

func command() *cli.Command {
	var id string
	cmd := &cli.Command{
		Name:     "report",
		Summary:  "Render a run's report: HTML pages in the run's directory, replacing earlier ones",
		Synopsis: "<run-dir>",
		Footer:   func(w io.Writer) { _, _ = io.WriteString(w, exitStatus) },
		Args:     cli.ExactArgs(1),
		Validate: func(inv *cli.Invocation) error {
			if inv.Changed("run") && id == "" {
				return cli.Usagef("--run names no run")
			}
			return nil
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			root := inv.Args[0]
			d, err := rundir.Open(root)
			if err != nil {
				return err
			}
			run := id
			if run == "" {
				if run, err = d.Latest(); err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return fmt.Errorf("%w; --run renders a run that hasn't ended", err)
					}
					return err
				}
			}
			return Make(ctx, inv.Stdout, d, root, run)
		},
	}
	cmd.Flags().StringVar(&id, "run", "", "the `id` of the run to render, one that never ended among them; the latest run that ended by default")
	return cmd
}
