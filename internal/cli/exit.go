package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
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
// code 3, whose reason names its class by [RefusalClass] matches that class too,
// dispatch.ErrEncrypted or dispatch.ErrReadElsewhere, so a stage ends with it as it is.
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

// Is reports whether target is the run's outcome's error, or its refusal's class.
func (e *ExitError) Is(target error) bool {
	if e.Signal != "" {
		return target == ErrFailed
	}
	if class := e.Refusal(); class != nil && class == target {
		return true
	}
	outcome := outcomes[e.Code]
	return outcome != nil && outcome == target
}

// Refusal returns the class of a refusal's reason by [RefusalClass]; nil for a run that wasn't
// refused, exit code 3, or died by a signal.
func (e *ExitError) Refusal() error {
	if e.Code != 3 || e.Signal != "" {
		return nil
	}
	return RefusalClass(e.Message)
}

// Unwrap returns the run's *tool.ExitError.
func (e *ExitError) Unwrap() error { return e.ExitError }

// faultSignals are the signals a tool dies by through its own fault: a bad access, an abort, a
// bus error, an arithmetic fault, or an illegal instruction. An allocation refused under the
// memory bound ends a tool by one of them, SIGABRT or SIGSEGV, never by an exit code.
var faultSignals = []string{"SIGSEGV", "SIGABRT", "SIGBUS", "SIGFPE", "SIGILL"}

// OwnFault reports whether signal, a *tool.ExitError's Signal, is one a tool dies by through its
// own fault, which a stage reads as the content's failure; any other signal, a SIGKILL or SIGTERM
// docproc didn't send among them, is the run's.
func OwnFault(signal string) bool { return slices.Contains(faultSignals, signal) }

// TableOfContents is docproc-onenote's fixed reason for a table of contents it refuses under
// --file-only, its sections being read as their own inputs.
const TableOfContents = "a table of contents; its sections are read on their own"

// RefusalClass is the one mapping of a refusal's reason, a tool's `error:` line on exit code 3,
// to dispatch's class: a reason beginning "encrypted", whichever tool gave it, is
// dispatch.ErrEncrypted, the input encrypted; docproc-onenote's [TableOfContents] is
// dispatch.ErrReadElsewhere, its sections read as their own inputs; and any other, prose for a
// person alone, is nil.
func RefusalClass(reason string) error {
	switch {
	case strings.HasPrefix(reason, "encrypted"):
		return dispatch.ErrEncrypted
	case reason == TableOfContents:
		return dispatch.ErrReadElsewhere
	}
	return nil
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
