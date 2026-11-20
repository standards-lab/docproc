package stages

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/tool"
)

// contentErrors are the errors a format package ends with when the content itself can't be read:
// a source with no stream to read, an archive 7-Zip can't open or whose listing doesn't parse, or
// an item that doesn't match its listing.
var contentErrors = []error{
	audio.ErrNoAudio, media.ErrNoMedia,
	sevenzip.ErrNotArchive, sevenzip.ErrListing, sevenzip.ErrEntries, sevenzip.ErrContent,
}

// runCodes returns the exit codes of program that are the run's, not the content's: the common
// envelope's 1, its output not written, and 2, a usage error, for docproc-ooxml, docproc-mail, and
// docproc-onenote; 7-Zip's 7, a command line error, and 255, stopped by the user, its 8, out of
// memory, being the content's, as an allocation refused under the bound is; LibreOffice's 77,
// EXITHELPER_FATAL_ERROR, which soffice exits with only from a fatal error at its start (no disk
// space, no write access, a broken profile or installation), never because of a document; and
// none for any other tool, whose every nonzero exit is its reading of the content, as gs's 1 and
// qpdf's 2 are.
func runCodes(program string) []int {
	switch {
	case strings.HasPrefix(program, "docproc-"):
		return []int{1, 2}
	case program == "7zz":
		return []int{7, 255}
	case program == "soffice":
		return []int{77}
	}
	return nil
}

// fault is a failure as [classify] reads it: whose it is, and the reason that says it.
type fault struct {
	// content is the content's failure, recorded: a part's fails it with dispatch.ErrRefused, and
	// a unit's is a warning. Otherwise it is the run's, never recorded, so a later dispatch tries
	// again.
	content bool
	// reason is the failure's text, a death by a signal under a memory bound naming the bound.
	reason string
}

// classify returns err's class, a unit's failure or a part's, by the package doc's table, from
// the facts tool reports and memory, the memory bound every tool ran under, zero for none:
//
//   - the content's, recorded: a tool that exited nonzero by its own code, 3 and 4 included; one
//     that died by its own fault, a signal cli.OwnFault names, an allocation refused under the bound
//     among them, its reason naming the signal and the bound, as in "killed by SIGABRT under a
//     256 MiB memory bound"; one that passed its own time bound (TimedOut); output that didn't
//     parse; and content a format package refuses as itself, contentErrors;
//   - the run's, retried: the context ended; a SIGKILL or SIGTERM docproc didn't send, or any
//     other signal; a program that didn't start; an exit runCodes names, a docproc tool's 1, its
//     output not written, or 2, a usage error, or soffice's 77, a fatal error at its start; and
//     any other error, on docproc's side of the pipe.
//
// dispatch.ErrEncrypted and dispatch.ErrReadElsewhere are neither: a stage returns them as they
// are, and partErr passes them through.
func classify(ctx context.Context, err error, memory int64) fault {
	f := fault{reason: err.Error()}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return f
	}
	if exit, ok := errors.AsType[*tool.ExitError](err); ok {
		switch {
		case exit.TimedOut:
			f.content = true
		case exit.Signal != "":
			f.content = cli.OwnFault(exit.Signal)
			if f.content && memory > 0 {
				f.reason = fmt.Sprintf("%s: killed by %s under a %s memory bound", exit.Program, exit.Signal, bytesOf(memory))
				if exit.Message != "" {
					f.reason += ": " + exit.Message
				}
			}
		default:
			f.content = !slices.Contains(runCodes(exit.Program), exit.Code)
		}
		return f
	}
	var syntax *jsontext.SyntacticError
	var semantic *jsonv2.SemanticError
	f.content = errors.As(err, &syntax) || errors.As(err, &semantic) ||
		slices.ContainsFunc(contentErrors, func(c error) bool { return errors.Is(err, c) })
	return f
}

// partErr returns err, a stage's whole run's failure, as the stage ends with it: one of the
// content's class wraps dispatch.ErrRefused, its reason classify's, so the part fails recorded;
// one of the run's is returned as it is, so the part fails unrecorded. An error that already names
// its outcome, dispatch.ErrEncrypted, ErrReadElsewhere, ErrNoProcessor, or ErrRefused, is
// returned as it is.
func partErr(ctx context.Context, err error, memory int64) error {
	for _, named := range []error{dispatch.ErrEncrypted, dispatch.ErrReadElsewhere, dispatch.ErrNoProcessor, dispatch.ErrRefused} {
		if errors.Is(err, named) {
			return err
		}
	}
	f := classify(ctx, err, memory)
	if !f.content {
		return err
	}
	return &refused{reason: f.reason, err: err}
}

// unitWarning returns err, the failure of a unit of a part, named where, read by program, as the
// stage yields it: a failure of the content's class is a warning naming the unit, and the part
// goes on with what it has; one of the run's is the error that ends the stage.
func unitWarning(ctx context.Context, program, where string, err error, memory int64) (dispatch.Warning, error) {
	f := classify(ctx, err, memory)
	if !f.content {
		return dispatch.Warning{}, err
	}
	return dispatch.Warning{Tool: program, Where: where, What: f.reason}, nil
}

// refused is a part's failure of the content's class: its reason, classify's, and the error it
// classifies, matching dispatch.ErrRefused as well.
type refused struct {
	reason string
	err    error
}

func (e *refused) Error() string { return e.reason }

// Unwrap returns dispatch.ErrRefused and the error classified.
func (e *refused) Unwrap() []error { return []error{dispatch.ErrRefused, e.err} }

// bytesOf returns n bytes in the largest binary unit that holds it whole, as in "256 MiB".
func bytesOf(n int64) string {
	for _, u := range []struct {
		shift int
		name  string
	}{{40, "TiB"}, {30, "GiB"}, {20, "MiB"}, {10, "KiB"}} {
		if n >= 1<<u.shift && n%(1<<u.shift) == 0 {
			return strconv.FormatInt(n>>u.shift, 10) + " " + u.name
		}
	}
	return strconv.FormatInt(n, 10) + " bytes"
}
