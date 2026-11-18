package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/standards-lab/go-cli-sdk"
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
