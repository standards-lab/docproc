package office

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

// OfficeMinimum is the oldest docproc-ooxml the Converter accepts, its first schema "2" release.
const OfficeMinimum = "0.1.0"

// OfficeSpec declares docproc-ooxml, minimum OfficeMinimum, for the exec runner's constructor. Its
// --version prints the version alone, a build other than a release's appending + and its commit,
// then `schema 2`, which internal/cli's version pattern requires, so a docproc-ooxml of another
// schema is refused as the Tool is built. A caller that needs another program path or a
// KillAfter period sets them on the Spec returned.
func OfficeSpec() tool.Spec {
	return tool.Spec{
		Program:        "docproc-ooxml",
		VersionArgs:    []string{"--version"},
		VersionPattern: cli.VersionPattern,
		Minimum:        OfficeMinimum,
	}
}

// Config is what a Converter is built over: the Tools the caller built.
type Config struct {
	// Office is docproc-ooxml, built from OfficeSpec through the exec runner. It is required, and
	// a container runner's is refused.
	Office *tool.Tool
	// Pictures is the picture package's Converter, over LibreOffice's soffice and ImageMagick's
	// magick. Its LibreOffice runner converts legacy documents to OOXML, and RTFs to docx, and its
	// ConvertBatch converts a document's TIFF, EMF, and WMF pictures to PNG. It is required.
	Pictures *picture.Converter
}

// Converter prepares office documents for a model through docproc-ooxml: a document in, its
// records out, streamed, with the media the document embeds. It runs no inference. Its methods are
// safe for concurrent use.
type Converter struct {
	office   *tool.Tool
	pictures *picture.Converter
}

// NewConverter builds a Converter over cfg's Office and Pictures. A nil Office or Pictures is the
// caller's mistake, and panics naming the field; an Office the container runner built is refused
// with an error matching ErrRequest, since the reader reads a part's path in place. It refuses an
// Office whose version is older than OfficeMinimum, whatever minimum the Tool's own Spec declared,
// with a *tool.VersionError wrapping tool.ErrTooOld. It runs nothing, so its context is unused:
// Pictures ran magick's format listing when it was built.
func NewConverter(_ context.Context, cfg Config) (*Converter, error) {
	if cfg.Office == nil {
		panic("office: Config.Office is nil")
	}
	if cfg.Pictures == nil {
		panic("office: Config.Pictures is nil")
	}
	if _, ok := cfg.Office.Image(); ok {
		return nil, fmt.Errorf("office: %s runs in a container; it runs on the exec runner alone: %w", cfg.Office.Name(), ErrRequest)
	}
	if err := cfg.Office.AtLeast(OfficeMinimum); err != nil {
		return nil, err
	}
	return &Converter{office: cfg.Office, pictures: cfg.Pictures}, nil
}

// Request is one document's conversion.
type Request struct {
	// Path is the document's file, which the caller owns: docproc-ooxml reads it in place, and only
	// a legacy document or an RTF is copied, into the LibreOffice runner's directory. It is
	// required.
	Path string `json:"-"`
	// Dir is the working directory, which the caller owns: the media directory is made in it and
	// left, and a legacy document's or an RTF's conversion is made and removed in it. It is
	// required; an empty Dir is refused rather than taken as the system's temporary directory.
	Dir string `json:"-"`
	// Name is the document's file name, or a path ending in it; only its extension is read, never
	// Path's. A document with the OLE2 compound-file signature whose Name has a legacy extension,
	// as route.LegacyTarget gives them, in any case, is a legacy document, converted to docx,
	// xlsx, or pptx before docproc-ooxml runs. An RTF, found by its signature, is converted to
	// docx whatever Name says. "" names no extension.
	Name string `json:"-"`
	// MetafileScale is the scale the document's EMF and WMF pictures are rendered at, a multiple
	// of their size at 96 dpi: zero is picture.DefaultMetafileScale, 3, and it may be at most
	// picture.MaxMetafileScale, 8. A negative scale, or one above picture.MaxMetafileScale, is
	// refused before anything runs, with an error matching ErrRequest.
	MetafileScale int `json:"metafile_scale"`
}

// ErrRequest is the cause of every refused request: a MetafileScale out of range, an empty Path
// or working directory, and an Office the container runner built. It is the picture package's, so
// a scale picture refuses matches it as office's refusal.
var ErrRequest = picture.ErrRequest

// The outcomes of a docproc-ooxml run that exits nonzero, each its exit code's, internal/cli's
// shared errors: the *cli.ExitError a conversion returns matches its code's.
var (
	// ErrMediaNotWritten is exit code 1: docproc-ooxml couldn't write stdout or its media.
	ErrMediaNotWritten = cli.ErrNotWritten
	// ErrUsage is exit code 2: docproc-ooxml refused its arguments, a fault in this package.
	ErrUsage = cli.ErrUsage
	// ErrRefused is exit code 3: docproc-ooxml refused the document, giving its reason.
	ErrRefused = cli.ErrRefused
	// ErrFailed is exit code 4, or a death by a signal: a fault of docproc-ooxml's own.
	ErrFailed = cli.ErrFailed
)

// ErrSchema is the error of a run whose first line isn't a schema "2" document line.
var ErrSchema = cli.ErrSchema

// Convert converts the document at req.Path as req says, and yields its records as docproc-ooxml
// writes them: the Document first, then for an xlsx each sheet's Sheet and its Rows, with each
// Warning between them. It runs docproc-ooxml once, in req.Dir, on req.Path in place, with a media
// directory of its own made there, media-*; nothing runs until the sequence is ranged over, and
// LibreOffice, docproc-ooxml, the picture batch, and every removal run inside it. Stopping the
// range ends the run.
//
// A legacy document, as Request.Name says, is converted first, through the picture package's
// LibreOffice runner in req.Dir, which copies it in with its legacy extension, to the OOXML format
// its extension names; docproc-ooxml runs on the converted document, which is removed with the
// run's directory when the run ends, and the Document's ConvertedFrom names the legacy format.
// Any other OLE2 file, such as an encrypted OOXML package, goes to
// docproc-ooxml as it is, which refuses it. An RTF, by its signature, whatever Request.Name's
// extension, is converted as a .doc is, to docx, through Writer's docx export filter;
// ConvertedFrom is "rtf".
//
// The Document's Media are resolved in the media directory by the envelope's media name rule, each
// Path the file handed onward. Before the Document is yielded, its pictures follow the picture
// standard, in one batch through the picture package: an EMF or WMF is rendered to PNG at
// req.MetafileScale through soffice, all of a document's in one soffice run, and trimmed through
// magick, and a TIFF is converted through magick; any other picture, an embedded package, and an
// OLE object are handed on as stored. Each PNG is named by the stored name's SHA-256 and .png,
// beside the stored file, which stays. A picture conversion that fails ends the sequence with its error. In each record's Markdown, each
// converted picture's stored name is swapped for its PNG's wherever it appears, which rewrites its
// links without parsing the Markdown.
//
// A MetafileScale below zero or above picture.MaxMetafileScale is an error matching ErrRequest
// before anything runs, as are an empty Path and an empty Dir. A first line that isn't a schema
// "2" document is an error matching ErrSchema. docproc-ooxml's exit codes 1 to 4, and a death by a
// signal, are a *cli.ExitError, which matches its code's error and names a refusal's class by its
// Refusal method. When ctx ends the run, the error matches ctx's, as every Tool run's does. A
// conversion that fails removes its media directory; one that succeeds, or whose range stopped,
// leaves it, the caller's.
func (c *Converter) Convert(ctx context.Context, req Request) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		stopped := false
		err := c.convert(ctx, req, func(rec Record, err error) bool {
			stopped = !yield(rec, err)
			return !stopped
		})
		// A removal that fails after the caller stopped ranging has no one to tell.
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

// convert is Convert's sequence: it yields each record and returns the conversion's error, nil
// when it succeeded or yield stopped it.
func (c *Converter) convert(ctx context.Context, req Request, yield func(Record, error) bool) (err error) {
	scale, err := picture.CheckMetafileScale(req.MetafileScale)
	if err != nil {
		return fmt.Errorf("office: %w", err)
	}
	if req.Path == "" {
		return fmt.Errorf("office: no document path: %w", ErrRequest)
	}
	if req.Dir == "" {
		return fmt.Errorf("office: no working directory: %w", ErrRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := filepath.Abs(req.Dir)
	if err != nil {
		return fmt.Errorf("office: %w", err)
	}
	path, err := filepath.Abs(req.Path)
	if err != nil {
		return fmt.Errorf("office: %w", err)
	}
	head, err := readHead(path)
	if err != nil {
		return err
	}
	from, to := legacyFormat(head, req.Name)
	if from != "" {
		converted, remove, err := c.toOOXML(ctx, dir, path, from, to)
		if err != nil {
			return err
		}
		defer func() {
			if rerr := remove(); rerr != nil {
				err = errors.Join(err, fmt.Errorf("office: removing the converted document: %w", rerr))
			}
		}()
		path = converted
	}
	media, err := os.MkdirTemp(dir, "media-*")
	if err != nil {
		return fmt.Errorf("office: media directory: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(media))
		}
	}()
	return c.read(ctx, dir, path, media, from, scale, yield)
}

// readHead returns the first legacyHeadLen bytes of the document at path, fewer for a shorter
// one.
func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("office: %w", err)
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, legacyHeadLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("office: %w", err)
	}
	return head[:n], nil
}

// read runs docproc-ooxml on the document at path, in dir, with media as its media directory, and
// yields its records: the Document once its pictures are converted at scale, with from as its
// ConvertedFrom, and every record's Markdown with the converted pictures' links rewritten. It
// returns the run's error, or a line's that didn't decode, nil when the run succeeded or yield
// stopped it.
func (c *Converter) read(ctx context.Context, dir, path, media, from string, scale int, yield func(Record, error) bool) error {
	var swaps *strings.Replacer
	for line, err := range cli.Read(ctx, cli.Run{
		Tool:  c.office,
		Args:  []string{"--media", media, "--", path},
		Dir:   dir,
		First: "document",
	}) {
		if err != nil {
			return fmt.Errorf("office: %w", err)
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
		switch r := rec.(type) {
		case *Document:
			r.ConvertedFrom = from
			if err := resolveMedia(media, r.Media); err != nil {
				return err
			}
			if err := c.convertPictures(ctx, dir, scale, r.Media); err != nil {
				return err
			}
			swaps = linkSwaps(r.Media)
			r.Markdown = rewriteLinks(r.Markdown, swaps)
		case *Sheet:
			r.Markdown = rewriteLinks(r.Markdown, swaps)
		case *Rows:
			r.Markdown = rewriteLinks(r.Markdown, swaps)
		}
		if !yield(rec, nil) {
			return nil
		}
	}
	return nil
}

// resolveMedia sets each of media's Path and SHA256 from its File, by the envelope's media name
// rule, in the media directory dir; a linked picture, with no File, has neither. A File the rule
// can't have made is an error.
func resolveMedia(dir string, media []Medium) error {
	for i, m := range media {
		if m.File == "" {
			continue
		}
		resolved, err := cli.ResolveMedium(dir, m.File)
		if err != nil {
			return fmt.Errorf("office: media %d: %w", i+1, err)
		}
		media[i].Path, media[i].SHA256 = resolved.Path, resolved.SHA256
	}
	return nil
}
