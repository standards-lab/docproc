package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/standards-lab/docproc/tool"
)

// ExitError is a tool's run through [Read] that exited nonzero or died by a signal: the
// *tool.ExitError that reports the run's facts, its Program, Code, Signal, and TimedOut, with its
// Message the run's message, the reason of the last `error:` line the tool wrote to stderr, as
// the envelope unescapes it; empty when it wrote none. It unwraps to the *tool.ExitError, so
// errors.As finds the facts.
//
// Exit codes 1 to 4 match their errors, [ErrNotWritten], [ErrUsage], [ErrRefused], and
// [ErrFailed]; a death by a signal matches ErrFailed; any other code matches none. A refusal, exit
// code 3, names its class by [ExitError.Refusal].
type ExitError struct {
	*tool.ExitError
}

// Error names the program, the outcome, a signal by its name, and the message, as in
// "docproc-ooxml: refused: encrypted package; decrypt it first" or "docproc-mail: failed: killed
// by SIGSEGV".
func (e *ExitError) Error() string {
	outcome := fmt.Sprintf("exit code %d", e.Code)
	switch {
	case e.Signal != "":
		outcome = ErrFailed.Error() + ": killed by " + e.Signal
	case outcomes[e.Code] != nil:
		outcome = outcomes[e.Code].Error()
	}
	if e.TimedOut {
		outcome = "timed out, " + outcome
	}
	msg := e.Program + ": " + outcome
	if s := strings.TrimSpace(e.Message); s != "" {
		msg += ": " + s
	}
	return msg
}

// Is reports whether target is the run's outcome's error.
func (e *ExitError) Is(target error) bool {
	if e.Signal != "" {
		return target == ErrFailed
	}
	outcome := outcomes[e.Code]
	return outcome != nil && outcome == target
}

// Refusal returns the class of a refusal's reason by [RefusalClass]; [Unclassed] for a run that
// wasn't refused, exit code 3, or died by a signal.
func (e *ExitError) Refusal() Refusal {
	if e.Code != 3 || e.Signal != "" {
		return Unclassed
	}
	return RefusalClass(e.Message)
}

// Unwrap returns the run's *tool.ExitError.
func (e *ExitError) Unwrap() error { return e.ExitError }

// TableOfContents is docproc-onenote's fixed reason for a table of contents it refuses under
// --file-only, its sections being read as their own inputs.
const TableOfContents = "a table of contents; its sections are read on their own"

// Refusal is the class a refusal's reason names. The stage that reads the tool maps it to
// dispatch's class: Encrypted to dispatch.ErrEncrypted, ReadElsewhere to dispatch.ErrReadElsewhere.
//
// Interim until slice 15: internal/cli can't import dispatch while dispatch holds the stage
// adapters, which import the readers and so internal/cli. Once they move to stages, RefusalClass
// returns dispatch's errors and this type goes.
type Refusal int

const (
	// Unclassed is a reason that is prose for a person alone, and docproc reads nothing from it.
	Unclassed Refusal = iota
	// Encrypted is a reason beginning "encrypted", whichever tool gave it: the input is encrypted.
	Encrypted
	// ReadElsewhere is docproc-onenote's [TableOfContents]: its sections are read as their own.
	ReadElsewhere
)

// RefusalClass is the one mapping of a refusal's reason, a tool's `error:` line on exit code 3,
// to its class: a reason beginning "encrypted" is [Encrypted], docproc-onenote's
// [TableOfContents] is [ReadElsewhere], and any other is [Unclassed].
func RefusalClass(reason string) Refusal {
	switch {
	case strings.HasPrefix(reason, "encrypted"):
		return Encrypted
	case reason == TableOfContents:
		return ReadElsewhere
	}
	return Unclassed
}

// exitError returns err, a run's error, as the envelope reader reports it: a *tool.ExitError
// becomes an [*ExitError] whose Message is message; any other error is returned as it is.
func exitError(err error, message string) error {
	exit, ok := errors.AsType[*tool.ExitError](err)
	if !ok {
		return err
	}
	exit.Message = message
	return &ExitError{ExitError: exit}
}
