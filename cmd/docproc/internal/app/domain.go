package app

import (
	"context"
	"os/exec"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docproc/stages"

	"github.com/standards-lab/docproc/cmd/docproc/internal/report"
	"github.com/standards-lab/docproc/cmd/docproc/internal/run"
)

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

func newStageBuilder(*graph.Scope) (run.StageBuilder, error) {
	return func(ctx context.Context, opts stages.Options) (stages.Set, error) {
		return stages.Build(ctx, exec.LookPath, opts)
	}, nil
}

func mountDomain(root *cli.Command, n *Nodes) {
	root.Add(run.Commands(n.RunConfig)...)
	root.Add(report.Commands()...)
}
