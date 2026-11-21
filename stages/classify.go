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

var contentErrors = []error{
	audio.ErrNoAudio, media.ErrNoMedia,
	sevenzip.ErrNotArchive, sevenzip.ErrListing, sevenzip.ErrEntries, sevenzip.ErrContent,
}

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

type fault struct {
	content bool
	reason  string
}

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

func unitWarning(ctx context.Context, program, where string, err error, memory int64) (dispatch.Warning, error) {
	f := classify(ctx, err, memory)
	if !f.content {
		return dispatch.Warning{}, err
	}
	return dispatch.Warning{Tool: program, Where: where, What: f.reason}, nil
}

type refused struct {
	reason string
	err    error
}

func (e *refused) Error() string { return e.reason }

func (e *refused) Unwrap() []error { return []error{dispatch.ErrRefused, e.err} }

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
