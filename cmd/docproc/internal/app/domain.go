package app

import (
	"context"
	"os/exec"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docproc/stages"

	"github.com/standards-lab/docproc/cmd/docproc/internal/run"
)

// defineDomain defines the domain nodes on g into n: the services docproc's commands are built
// on, each constructed from the docproc library's packages over the infrastructure nodes it Uses.
// The stage builder builds docproc's stages over the host's tools when a run calls it, and the
// run config is what the run command runs under: the bounds and the stage builder.
func defineDomain(g *graph.Graph, n *Nodes) {
	n.StageBuilder = g.Define("stage builder", newStageBuilder)
	n.RunConfig = g.Define("run config", func(s *graph.Scope) (run.Config, error) {
		b := s.Use(n.Bounds)
		return run.Config{
			Stages:  b.Stages,
			Limits:  b.Limits,
			Release: b.Release,
			Build:   s.Use(n.StageBuilder),
			Docproc: docprocBuild(),
		}, nil
	})
}

// newStageBuilder returns the production stage builder: stages.Build over the tools on the host's
// PATH, found through exec.LookPath. Constructing it probes nothing; each tool is probed when a
// run calls it, once the run has collected its inputs.
func newStageBuilder(*graph.Scope) (run.StageBuilder, error) {
	return func(ctx context.Context, opts stages.Options) (stages.Set, error) {
		return stages.Build(ctx, exec.LookPath, opts)
	}, nil
}

// mountDomain mounts docproc's commands at root, each from its command package under internal/,
// over the nodes it passes, each command declaring with Use the nodes it reads, so a run builds
// only what its command needs: the run command over the run config.
func mountDomain(root *cli.Command, n *Nodes) {
	root.Add(run.Commands(n.RunConfig)...)
}
