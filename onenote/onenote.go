package onenote

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"path/filepath"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

// OneNoteMinimum is the oldest docproc-onenote the Reader accepts, its first schema "2" release.
const OneNoteMinimum = "0.1.0"

// OneNoteSpec declares docproc-onenote, minimum OneNoteMinimum, for the exec runner's
// constructor. Its --version prints the version alone, a build other than a release's appending +
// and its commit, then `schema 2`, which internal/cli's version pattern requires, so a
// docproc-onenote of another schema is refused as the Tool is built. A caller that needs another
// program path or a KillAfter period sets them on the Spec returned.
func OneNoteSpec() tool.Spec {
	return tool.Spec{
		Program:        "docproc-onenote",
		VersionArgs:    []string{"--version"},
		VersionPattern: cli.VersionPattern,
		Minimum:        OneNoteMinimum,
	}
}

// Config is what a Reader is built over: the Tool the caller built.
type Config struct {
	// OneNote is docproc-onenote, built from OneNoteSpec through the exec runner. It is required,
	// and a container runner's is refused.
	OneNote *tool.Tool
}

// Reader reads OneNote sections, packages, and notebooks through docproc-onenote: an input in, its
// records streamed out as docproc-onenote writes them. It runs no inference. Its methods are safe
// for concurrent use.
type Reader struct {
	onenote *tool.Tool
}

// NewReader builds a Reader over cfg's Tool. A nil OneNote is the caller's mistake, and panics
// naming the field; a OneNote the container runner built is refused with an error matching
// ErrRequest, since docproc-onenote reads the input's path in place. It refuses a OneNote whose
// version is older than OneNoteMinimum, whatever minimum the Tool's own Spec declared, with a
// *tool.VersionError wrapping tool.ErrTooOld.
func NewReader(cfg Config) (*Reader, error) {
	if cfg.OneNote == nil {
		panic("onenote: Config.OneNote is nil")
	}
	if _, ok := cfg.OneNote.Image(); ok {
		return nil, fmt.Errorf("onenote: %s runs in a container; it runs on the exec runner alone: %w", cfg.OneNote.Name(), ErrRequest)
	}
	if err := cfg.OneNote.AtLeast(OneNoteMinimum); err != nil {
		return nil, err
	}
	return &Reader{onenote: cfg.OneNote}, nil
}

// Request is one input's read.
type Request struct {
	// Input is the path of the .one section or the .onepkg package to read, or, an operator's
	// read, the .onetoc2 notebook or notebook directory, which the caller owns; docproc-onenote
	// sniffs its kind from its content, never its name, and reads it in place. Read makes it
	// absolute, so a name that begins with "-" is never taken as an option. It is required.
	Input string
	// Media is the directory docproc-onenote writes each distinct image, embedded file, and ink
	// drawing into once, named by its media name, created once the input is read; the caller owns
	// it. Each Medium's Path then names its file there. Read makes it absolute. "" writes nothing,
	// and the lines name the files all the same.
	Media string
	// FileOnly passes --file-only, under which docproc-onenote refuses a table of contents, a
	// .onetoc2 or a notebook directory, before any section is read, with its fixed reason,
	// cli.TableOfContents, since each section is read as its own input; a section and a package
	// read as they do without it.
	FileOnly bool
}

// ErrRequest is the cause of every refused request, one with no Input or whose Input or Media
// can't be made absolute, and of a OneNote the container runner built.
var ErrRequest = errors.New("invalid request")

// The outcomes of a docproc-onenote run that exits nonzero, each its exit code's, internal/cli's
// shared errors: the *cli.ExitError a read returns matches its code's.
var (
	// ErrNotWritten is exit code 1: docproc-onenote couldn't write stdout or its media.
	ErrNotWritten = cli.ErrNotWritten
	// ErrUsage is exit code 2: docproc-onenote refused its arguments, a fault in this package.
	ErrUsage = cli.ErrUsage
	// ErrRefused is exit code 3: docproc-onenote refused the input before any output, giving its
	// reason, such as a password-protected section's or, under --file-only, a table of contents'.
	ErrRefused = cli.ErrRefused
	// ErrFailed is exit code 4, or a death by a signal: a fault of docproc-onenote's own.
	ErrFailed = cli.ErrFailed
)

// ErrSchema is the error of a run whose first line isn't a schema "2" notebook line.
var ErrSchema = cli.ErrSchema

// Read reads req's input, and yields its records as docproc-onenote writes them: the Notebook
// first, then the notebook's entries depth-first in its table of contents' order, each
// SectionGroup before the entries it holds and each Section before its Pages, then a Skipped
// record for each section the read left out, with each Warning between them. A section input
// gives the notebook, its one section, and its pages. It runs docproc-onenote once, on req.Input
// in place; nothing runs until the sequence is ranged over, and stopping the range ends the run.
// docproc-onenote's stdout is decoded one line at a time as it arrives, so memory follows the
// record being read, never the notebook, and a slow consumer holds docproc-onenote back.
//
// Each Medium with a File is resolved by the envelope's media name rule, its Path in req.Media
// when it is set, and its SHA256.
//
// An empty Input, or an Input or Media that can't be made absolute, which filepath.Abs does from
// the working directory, is an error matching ErrRequest before anything runs. A first line that
// isn't a schema "2" notebook line is an error matching ErrSchema. docproc-onenote's exit codes 1
// to 4, and a death by a signal, are a *cli.ExitError, which matches its code's error and names a
// refusal's class by its Refusal method: an encrypted section's is cli.Encrypted, and a table of
// contents' under --file-only cli.ReadElsewhere. When ctx ends the run, the error matches ctx's,
// as every Tool run's does. A read that fails yields one last pair, nil and its error.
func (r *Reader) Read(ctx context.Context, req Request) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		stopped := false
		err := r.read(ctx, req, func(rec Record, err error) bool {
			stopped = !yield(rec, err)
			return !stopped
		})
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

// read is Read's sequence: it yields each record and returns the read's error, nil when it
// succeeded or yield stopped it.
func (r *Reader) read(ctx context.Context, req Request, yield func(Record, error) bool) error {
	if req.Input == "" {
		return fmt.Errorf("onenote: no input: %w", ErrRequest)
	}
	input, err := filepath.Abs(req.Input)
	if err != nil {
		return fmt.Errorf("onenote: input: %w: %w", ErrRequest, err)
	}
	var media string
	if req.Media != "" {
		if media, err = filepath.Abs(req.Media); err != nil {
			return fmt.Errorf("onenote: media: %w: %w", ErrRequest, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var args []string
	if media != "" {
		args = append(args, "--media", media)
	}
	if req.FileOnly {
		args = append(args, "--file-only")
	}
	args = append(args, input)
	for line, err := range cli.Read(ctx, cli.Run{Tool: r.onenote, Args: args, First: "notebook"}) {
		if err != nil {
			return fmt.Errorf("onenote: %w", err)
		}
		if line.Warning != nil {
			if !yield((*Warning)(line.Warning), nil) {
				return nil
			}
			continue
		}
		rec, err := decodeLine(line.Kind, line.Raw)
		if err != nil {
			return err
		}
		if p, ok := rec.(*Page); ok {
			if err := resolveMedia(media, p); err != nil {
				return err
			}
		}
		if !yield(rec, nil) {
			return nil
		}
	}
	return nil
}

// resolveMedia resolves the files of p's media by the envelope's media name rule in dir, the
// read's media directory, "" for none. A File the rule can't have made is an error, so no Path
// leaves dir.
func resolveMedia(dir string, p *Page) error {
	for i := range p.Media {
		m := &p.Media[i]
		if m.File == "" {
			continue
		}
		resolved, err := cli.ResolveMedium(dir, m.File)
		if err != nil {
			return fmt.Errorf("onenote: page %s, media %d: %w", p.ID, i+1, err)
		}
		m.Path, m.SHA256 = resolved.Path, resolved.SHA256
	}
	return nil
}
