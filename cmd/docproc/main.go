// Command docproc runs docproc's stages over a set of inputs. Its main derives the signal context
// and makes one call into the composition root, internal/app. It sits at its module's root, so
// go install github.com/standards-lab/docproc/cmd/docproc@<version> installs it.
package main

import (
	"os"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := process.SignalContext()
	defer stop()
	return app.New(cli.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}).Run(ctx, os.Args[1:])
}
