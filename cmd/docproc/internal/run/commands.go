package run

import (
	"context"
	"io"
	"runtime"
	"slices"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/docproc/stages"
)

// StageBuilder builds the stages a run dispatches to under opts, probing the tools they need:
// stages.Build over the host's tools in production, and over stand-ins in a test.
type StageBuilder func(ctx context.Context, opts stages.Options) (stages.Set, error)

// Config is what the run command runs under: the run's bounds, in the library's shapes, the
// stage builder, and the docproc build that writes the run.
type Config struct {
	// Stages are the options the stages are built under; the command adds the routes --exclude
	// names to their Exclude.
	Stages stages.Options
	// Limits are each dispatch's caps, dispatch.Config.Limits.
	Limits dispatch.Limits
	// Release bounds a claim's release once a dispatch's context has ended,
	// dispatch.Config.Release.
	Release time.Duration
	// Build builds the stages; it is required.
	Build StageBuilder
	// Docproc is what writes the run, the manifest's "docproc".
	Docproc schema.Build
}

// Commands returns the run command, over the configuration cfg's node holds, which it declares.
func Commands(cfg *graph.Node[Config]) []*cli.Command {
	return []*cli.Command{command(cfg)}
}

// exitStatus is the run command's help footer: what each exit code means.
const exitStatus = `Exit status:
  0  the run ended: every input was dispatched, though some of its parts failed or an input
     stopped with an error before its own event; the summary's last line counts both, a
     failure replayed from the index among the failed parts; with --report, its pages were
     rendered too
  1  the run stopped, or never began: an argument that is neither a file nor a directory,
     arguments that yield no input, a run directory that can't be written, a failure of the
     index or the content store, or a cancellation; a run that began ends its manifest with
     the error. With --report, a run stopped by a failure of the index or the content store
     still renders its pages, and a canceled run renders none; a run that ended and whose
     pages couldn't be rendered exits 1 too, and docproc report renders them again
  2  a usage error, such as an unknown route to --exclude or --workers under 1
`

// command returns the run command: docproc run <path>... --out <dir>.
func command(cfg *graph.Node[Config]) *cli.Command {
	var (
		out      string
		exclude  []string
		workers  int
		render   bool
		excluded []route.Route
	)
	cmd := &cli.Command{
		Name:     "run",
		Summary:  "Run docproc's stages over each file, and each regular file under each directory",
		Synopsis: "<path>...",
		Footer:   func(w io.Writer) { _, _ = io.WriteString(w, exitStatus) },
		Args: func(args []string) error {
			if len(args) == 0 {
				return cli.Usagef("requires at least one path")
			}
			return nil
		},
		Validate: func(*cli.Invocation) error {
			if out == "" {
				return cli.Usagef("--out names no directory")
			}
			if workers < 1 {
				return cli.Usagef("--workers %d is under 1", workers)
			}
			routes, err := stages.ParseRoutes(exclude)
			if err != nil {
				return cli.Usagef("--exclude: %w", err)
			}
			excluded = routes
			return nil
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			// The inputs are collected before any tool is probed, so an argument that yields
			// none fails the run at once.
			inputs, err := collect(inv.Args)
			if err != nil {
				return err
			}
			c := inv.Get(cfg)
			c.Stages.Exclude = slices.Concat(c.Stages.Exclude, excluded)
			r := &runner{cfg: c, w: inv.Stdout, out: out, args: inv.Args, inputs: inputs, workers: workers, report: render}
			return r.run(ctx)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&out, "out", "", "the run `dir`ectory the run writes in, made when it doesn't exist")
	flags.Var(cli.StringsValue(&exclude, nil), "exclude", "leave out the stage of `route`, whose parts then have no processor yet")
	flags.IntVar(&workers, "workers", runtime.NumCPU(), "how many inputs are dispatched at once, at least 1, with no ceiling; this host's CPUs by default")
	flags.BoolVar(&render, "report", false, "once the summary is printed, render the run's report and print its index's path, as docproc report does: after a failure of the index or the content store too, and never after a cancellation")
	cmd.Require("out")
	return cmd.Use(cfg)
}
