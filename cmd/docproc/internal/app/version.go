package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/docproc/rundir/schema"
)

// versionCommand returns the version command, which prints the main module's version,
// github.com/standards-lab/docproc/cmd/docproc's. It declares no nodes, so it builds nothing,
// reads no configuration, and looks up no tool.
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

// moduleVersion returns the main module's version from the build info, such as v0.1.0 for a
// binary go install built at that tag, or "(devel)" when the binary carries none, as under go
// test.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}

// libraryPath is the docproc library's module path, which the build info names among the
// command's dependencies.
const libraryPath = "github.com/standards-lab/docproc"

// docprocBuild returns what writes a run, a run manifest's "docproc": the main module's version,
// as moduleVersion gives it, and the version of the docproc library it was built with, from the
// build info: the version its replacement names when it is replaced, "(devel)" for a replacement
// by a directory, which names none, or when the build info names no library at all.
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
