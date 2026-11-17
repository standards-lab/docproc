package picture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

// LibreOfficeRun is one soffice run's conversion, such as a batch's metafiles to PNG or the
// office package's legacy document to OOXML.
type LibreOfficeRun struct {
	// Dir is the working directory, which the caller owns: the run makes a directory of its own
	// in it, libreoffice-*, which holds everything the run reads and writes. It is required.
	Dir string
	// ConvertTo is soffice's --convert-to: the target extension and, after a colon, the export
	// filter and its options. It is required.
	ConvertTo string
	// Inputs are the files the run converts, in order; at least one is required.
	Inputs []LibreOfficeInput
	// Registry, when set, is the registrymodifications.xcu the run's user profile starts with,
	// such as one setting LibreOffice's locale; "" starts it empty.
	Registry string
}

// LibreOfficeInput is one input of a LibreOfficeRun.
type LibreOfficeInput struct {
	// Path is the input's file, which is read and never changed.
	Path string
	// Name is the name soffice reads the input's copy under, whose extension tells LibreOffice
	// its format and whose stem names its output. It is a file name, no path, and no two of a
	// run's inputs may share a stem.
	Name string
}

// LibreOfficeOutput is what a LibreOfficeRun wrote: its output directory and soffice's message.
// The caller removes it with Remove when it is done with the output.
type LibreOfficeOutput struct {
	// Dir is the directory soffice wrote its outputs to, each named for its input's Name's stem
	// and the extension ConvertTo names.
	Dir string
	// Message is soffice's message, the first line of its output beginning "Error" or else its
	// last line that isn't blank, for an error the caller finds in the output: soffice exits zero
	// having written nothing for an input it can't export.
	Message string
	// work is the run's own directory, which Remove removes.
	work string
}

// Remove removes the run's directory, the output with it.
func (o *LibreOfficeOutput) Remove() error { return os.RemoveAll(o.work) }

// sofficeOutputLimit is the most of soffice's output a run keeps, for its message.
const sofficeOutputLimit = 64 << 10

// RunLibreOffice runs the Converter's soffice once, converting run's inputs as run.ConvertTo
// says: the package's one LibreOffice runner, which renders a batch's metafiles and which the
// office package's legacy conversion runs too. It makes a directory of its own in run.Dir,
// libreoffice-*, and copies each input into it under its Name, so soffice never opens the
// caller's file and finds nothing beside the copy. The run's working directory is that
// directory, which the container runner mounts alone and which the exec runner makes soffice's
// HOME and TMPDIR, so the font cache and configuration soffice writes there go with it. It holds
// soffice's user profile too, -env:UserInstallation, started with run.Registry when it is set, so
// no run shares or leaves a profile elsewhere, and the output directory.
//
// An empty Dir or ConvertTo, no inputs, and a Name that is no plain file name, or that shares
// another's stem, are errors matching ErrRequest before anything runs. A run that fails removes
// its directory, and its error carries soffice's message; when ctx ends the run, the error
// matches ctx's, as every Tool run's does.
func (c *Converter) RunLibreOffice(ctx context.Context, run LibreOfficeRun) (*LibreOfficeOutput, error) {
	dir, err := workingDir(run.Dir)
	if err != nil {
		return nil, err
	}
	if run.ConvertTo == "" {
		return nil, fmt.Errorf("picture: libreoffice: no --convert-to: %w", ErrRequest)
	}
	if len(run.Inputs) == 0 {
		return nil, fmt.Errorf("picture: libreoffice: no inputs: %w", ErrRequest)
	}
	stems := map[string]bool{}
	for _, in := range run.Inputs {
		stem := strings.TrimSuffix(in.Name, filepath.Ext(in.Name))
		switch {
		case in.Name == "" || in.Name != filepath.Base(in.Name) || in.Name == "." || in.Name == "..":
			return nil, fmt.Errorf("picture: libreoffice: input name %q isn't a file name: %w", in.Name, ErrRequest)
		case stems[stem]:
			return nil, fmt.Errorf("picture: libreoffice: two inputs' names have the stem %q: %w", stem, ErrRequest)
		}
		stems[stem] = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(dir, "libreoffice-*")
	if err != nil {
		return nil, fmt.Errorf("picture: libreoffice directory: %w", err)
	}
	o := &LibreOfficeOutput{Dir: filepath.Join(work, "out"), work: work}
	if err := c.sofficeRun(ctx, run, o); err != nil {
		return nil, errors.Join(err, o.Remove())
	}
	return o, nil
}

// sofficeRun copies run's inputs into o's directory, writes its profile's registry, and runs
// soffice there, setting o's message.
func (c *Converter) sofficeRun(ctx context.Context, run LibreOfficeRun, o *LibreOfficeOutput) error {
	in := filepath.Join(o.work, "in")
	if err := os.Mkdir(in, 0o700); err != nil {
		return fmt.Errorf("picture: libreoffice: %w", err)
	}
	var inputs []string
	for _, input := range run.Inputs {
		copied := filepath.Join(in, input.Name)
		if err := copyFile(input.Path, copied); err != nil {
			return fmt.Errorf("picture: libreoffice: %s: %w", input.Name, err)
		}
		inputs = append(inputs, copied)
	}
	profile := filepath.Join(o.work, "profile")
	if run.Registry != "" {
		if err := writeRegistry(profile, run.Registry); err != nil {
			return fmt.Errorf("picture: libreoffice profile: %w", err)
		}
	}
	out := &cli.LimitedBuffer{Limit: sofficeOutputLimit}
	args := []string{
		"-env:UserInstallation=" + (&url.URL{Scheme: "file", Path: profile}).String(),
		"--headless", "--norestore",
		"--convert-to", run.ConvertTo,
		"--outdir", o.Dir,
	}
	err := c.libreoffice.Run(ctx, tool.Invocation{
		Args:   append(args, inputs...),
		Dir:    o.work,
		Stdout: out,
		Stderr: out,
	})
	o.Message = sofficeMessage(out.String())
	if err != nil {
		return sofficeError(err, o.Message)
	}
	return nil
}

// copyFile copies the file at from to a new file at to.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

// writeRegistry writes registry as the registrymodifications.xcu of the soffice user profile at
// profile, before soffice first runs on it.
func writeRegistry(profile, registry string) error {
	user := filepath.Join(profile, "user")
	if err := os.MkdirAll(user, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(user, "registrymodifications.xcu"), []byte(registry), 0o600)
}

// sofficeError returns err as soffice's error, with soffice's message, msg, when it has one.
func sofficeError(err error, msg string) error {
	err = fmt.Errorf("picture: libreoffice: %w", err)
	if msg != "" {
		err = fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// sofficeMessage returns soffice's message in its output: the first line beginning "Error", or
// else the last line that isn't blank.
func sofficeMessage(out string) string {
	last := ""
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Error") {
			return line
		}
		if line != "" {
			last = line
		}
	}
	return last
}
