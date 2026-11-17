package email

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

// MailMinimum is the oldest docproc-mail the Reader accepts, its first schema "2" release, which
// resumes a read with --after too.
const MailMinimum = "0.1.0"

// MailSpec declares docproc-mail, minimum MailMinimum, for the exec runner's constructor. Its
// --version prints the version alone, a build other than a release's appending + and its commit,
// then `schema 2`, which internal/cli's version pattern requires, so a docproc-mail of another
// schema is refused as the Tool is built. A caller that needs another program path or a
// KillAfter period sets them on the Spec returned.
func MailSpec() tool.Spec {
	return tool.Spec{
		Program:        "docproc-mail",
		VersionArgs:    []string{"--version"},
		VersionPattern: cli.VersionPattern,
		Minimum:        MailMinimum,
	}
}

// Config is what a Reader is built over: the Tool the caller built.
type Config struct {
	// Mail is docproc-mail, built from MailSpec through the exec runner. It is required, and a
	// container runner's is refused.
	Mail *tool.Tool
}

// Reader reads mail stores and messages through docproc-mail: an input in, its records streamed
// out as docproc-mail writes them. It runs no inference. Its methods are safe for concurrent use.
type Reader struct {
	mail *tool.Tool
}

// NewReader builds a Reader over cfg's Tool. A nil Mail is the caller's mistake, and panics naming
// the field; a Mail the container runner built is refused with an error matching ErrRequest, since
// docproc-mail reads the input's path in place. It refuses a Mail whose version is older than
// MailMinimum, whatever minimum the Tool's own Spec declared, with a *tool.VersionError wrapping
// tool.ErrTooOld.
func NewReader(cfg Config) (*Reader, error) {
	if cfg.Mail == nil {
		panic("email: Config.Mail is nil")
	}
	if _, ok := cfg.Mail.Image(); ok {
		return nil, fmt.Errorf("email: %s runs in a container; it runs on the exec runner alone: %w", cfg.Mail.Name(), ErrRequest)
	}
	if err := cfg.Mail.AtLeast(MailMinimum); err != nil {
		return nil, err
	}
	return &Reader{mail: cfg.Mail}, nil
}

// Request is one input's read.
type Request struct {
	// Input is the path of the .pst, .msg, .eml, or mbox to read, which the caller owns;
	// docproc-mail sniffs its format from its content, never its name, and reads it in place. Read
	// makes it absolute, so a name that begins with "-" is never taken as an option. It is
	// required.
	Input string `json:"-"`
	// Media is the directory docproc-mail writes each distinct attachment content, and each data:
	// image the Markdown links, into once, named by its media name, created if missing; the caller
	// owns it. Each Attachment's and Medium's Path then names its file there. Read makes it
	// absolute. "" writes nothing, and the lines name the files all the same.
	Media string `json:"-"`
	// After, when not zero, resumes the read of a .pst or an mbox after the message whose
	// Message.ID it is: in a .pst its node id, in an mbox its 1-based ordinal. The resumed read is
	// the tail of the whole read, as docproc-mail writes it byte for byte: the store, every folder,
	// and every folder's skipped record, then only the messages after this one, with their skipped
	// records. It is an id from 1 to 4,294,967,295, docproc-mail's range, and Read refuses one past
	// it. docproc-mail refuses After with a .msg or an .eml, a single message, as a usage error, a
	// *cli.ExitError matching ErrUsage, and an id that names no message as a refusal, matching
	// ErrRefused.
	After uint64 `json:"-"`
}

// ErrRequest is the cause of every refused request: one with no Input, whose Input or Media can't
// be made absolute, or whose After is past docproc-mail's range; and of a Mail the container
// runner built.
var ErrRequest = errors.New("invalid request")

// The outcomes of a docproc-mail run that exits nonzero, each its exit code's, internal/cli's
// shared errors: the *cli.ExitError a read returns matches its code's.
var (
	// ErrNotWritten is exit code 1: docproc-mail couldn't write stdout or its media.
	ErrNotWritten = cli.ErrNotWritten
	// ErrUsage is exit code 2: docproc-mail refused its arguments, a fault in this package, or
	// After given a single message.
	ErrUsage = cli.ErrUsage
	// ErrRefused is exit code 3: docproc-mail refused the input before any output, giving its
	// reason, such as an .ost's.
	ErrRefused = cli.ErrRefused
	// ErrFailed is exit code 4, or a death by a signal: a fault of docproc-mail's own.
	ErrFailed = cli.ErrFailed
)

// ErrSchema is the error of a run whose first line isn't a schema "2" store line.
var ErrSchema = cli.ErrSchema

// Read reads req's input, and yields its records as docproc-mail writes them: the Store first;
// then, for a .pst, each Folder before its messages and they before its subfolders; for a .msg or
// an .eml, its Message, with any embedded message nested under its attachment record; for an
// mbox, each Message in the file's order; with a Skipped record wherever docproc-mail couldn't
// read one, and each Warning between them. It runs docproc-mail once, on req.Input in place;
// nothing runs until the sequence is ranged over, and stopping the range ends the run.
// docproc-mail's stdout is decoded one line at a time as it arrives, so memory follows the record
// being read, never the store, and a slow consumer holds docproc-mail back.
//
// Each Attachment and Medium with a File is resolved by the envelope's media name rule, its Path
// in req.Media when it is set, and its SHA256. A sender whose line leaves its SMTP address out
// takes it from the message's transport headers' From. A Warning whose Where begins with a .pst
// folder's node id names the folder by its path instead, its display names joined by "/": it is
// yielded after the folder's line, held until that line comes, as is each warning after it, so
// their order is kept; one whose folder's line never comes is yielded, its id kept, as the read
// ends.
//
// An empty Input, an Input or Media that can't be made absolute, which filepath.Abs does from the
// working directory, and an After past 4,294,967,295 are an error matching ErrRequest before
// anything runs. A first line that isn't a schema "2" store line is an error matching ErrSchema.
// docproc-mail's exit codes 1 to 4, and a death by a signal, are a *cli.ExitError, which matches
// its code's error and names a refusal's class by its Refusal method. When ctx ends the run, the
// error matches ctx's, as every Tool run's does. A read that fails yields one last pair, nil and
// its error.
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
		return fmt.Errorf("email: no input: %w", ErrRequest)
	}
	if req.After > math.MaxUint32 {
		return fmt.Errorf("email: after %d, past docproc-mail's last id, %d: %w", req.After, uint64(math.MaxUint32), ErrRequest)
	}
	input, err := filepath.Abs(req.Input)
	if err != nil {
		return fmt.Errorf("email: input: %w: %w", ErrRequest, err)
	}
	var media string
	if req.Media != "" {
		if media, err = filepath.Abs(req.Media); err != nil {
			return fmt.Errorf("email: media: %w: %w", ErrRequest, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var args []string
	if media != "" {
		args = append(args, "--media", media)
	}
	if req.After != 0 {
		args = append(args, "--after", strconv.FormatUint(req.After, 10))
	}
	args = append(args, input)
	w := warnings{folders: map[uint64]string{}, yield: yield}
	for line, err := range cli.Read(ctx, cli.Run{Tool: r.mail, Args: args, First: "store"}) {
		if err != nil {
			if !w.flush() {
				return nil
			}
			return fmt.Errorf("email: %w", err)
		}
		if line.Warning != nil {
			if !w.add(Warning(*line.Warning)) {
				return nil
			}
			continue
		}
		rec, err := decodeLine(line.Kind, line.Raw)
		if err != nil {
			if !w.flush() {
				return nil
			}
			return err
		}
		if m, ok := rec.(*Message); ok {
			if err := resolveMessage(media, m); err != nil {
				if !w.flush() {
					return nil
				}
				return err
			}
		}
		if !yield(rec, nil) {
			return nil
		}
		if f, ok := rec.(*Folder); ok {
			w.folders[f.ID] = strings.Join(f.Path, "/")
			if !w.release() {
				return nil
			}
		}
	}
	w.flush()
	return nil
}

// warnings are a read's warnings on their way to its caller, each naming a .pst folder by its
// path. docproc-mail writes a folder's line before reading what the folder holds, but writes a
// warning about the folder's own properties before it, and its stdout is buffered while its stderr
// isn't, so a warning naming a folder by its node id can come before the folder's line. Such a
// warning is held until that line comes, and each warning after it with it, so their order is
// kept; whatever a read leaves held at its end is yielded then, its id kept.
type warnings struct {
	// folders are the paths of the folders read so far, by id, display names joined by "/".
	folders map[uint64]string
	held    []Warning
	yield   func(Record, error) bool
}

// add yields w, rewritten, unless it names a folder whose line hasn't come, or a warning is held
// before it; then it holds it. It returns false once yield has stopped the read.
func (ws *warnings) add(w Warning) bool {
	ws.held = append(ws.held, w)
	return ws.release()
}

// release yields the held warnings, in order, up to the first that names a folder whose line
// hasn't come. It returns false once yield has stopped the read.
func (ws *warnings) release() bool {
	for len(ws.held) > 0 {
		where, ok := folderPath(ws.held[0].Where, ws.folders)
		if !ok {
			return true
		}
		w := ws.held[0]
		w.Where = where
		ws.held = ws.held[1:]
		if !ws.yield(&w, nil) {
			return false
		}
	}
	ws.held = nil
	return true
}

// flush yields every held warning, in order, each folder named by its path where its line has
// come. It returns false once yield has stopped the read.
func (ws *warnings) flush() bool {
	for _, w := range ws.held {
		w.Where, _ = folderPath(w.Where, ws.folders)
		if !ws.yield(&w, nil) {
			return false
		}
	}
	ws.held = nil
	return true
}

// folderPath returns where, a warning's place, with its leading "folder <id>" naming the folder
// by its path from folders, and true: "folder 32898, message 2097188" becomes "folder Top of
// Personal Folders/Inbox, message 2097188". A where that names a folder folders doesn't hold is
// returned as it is, with false; any other where as it is, with true.
func folderPath(where string, folders map[uint64]string) (string, bool) {
	rest, ok := strings.CutPrefix(where, "folder ")
	if !ok {
		return where, true
	}
	digits, tail, more := strings.Cut(rest, ",")
	id, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return where, true
	}
	path, ok := folders[id]
	switch {
	case !ok:
		return where, false
	case more:
		return "folder " + path + "," + tail, true
	}
	return "folder " + path, true
}

// resolveMessage resolves the files of m's attachments and media, and its nested messages', by the
// envelope's media name rule in dir, the read's media directory, "" for none. A File the rule
// can't have made is an error, so no Path leaves dir.
func resolveMessage(dir string, m *Message) error {
	for i := range m.Attachments {
		a := &m.Attachments[i]
		if a.File != "" {
			resolved, err := cli.ResolveMedium(dir, a.File)
			if err != nil {
				return fmt.Errorf("email: attachment %d: %w", i+1, err)
			}
			a.Path, a.SHA256 = resolved.Path, resolved.SHA256
		}
		if a.Message != nil {
			if err := resolveMessage(dir, a.Message); err != nil {
				return err
			}
		}
	}
	for i := range m.Media {
		md := &m.Media[i]
		resolved, err := cli.ResolveMedium(dir, md.File)
		if err != nil {
			return fmt.Errorf("email: media %d: %w", i+1, err)
		}
		md.Path, md.SHA256 = resolved.Path, resolved.SHA256
	}
	return nil
}
