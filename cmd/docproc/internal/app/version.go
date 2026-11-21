package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/docproc/rundir/schema"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:    "version",
		Summary: "Print the docproc version",
		Args:    cli.NoArgs,
		Run: func(_ context.Context, inv *cli.Invocation) error {
			_, err := fmt.Fprintln(inv.Stdout, moduleVersion())
			return err
		},
	}
}

func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}

const libraryPath = "github.com/standards-lab/docproc"

func docprocBuild() schema.Build {
	b := schema.Build{Command: moduleVersion(), Library: "(devel)"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	for _, dep := range info.Deps {
		if dep.Path != libraryPath {
			continue
		}
		switch {
		case dep.Replace == nil:
			b.Library = dep.Version
		case dep.Replace.Version != "":
			b.Library = dep.Replace.Version
		}
	}
	return b
}
