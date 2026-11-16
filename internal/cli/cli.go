package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// The outcomes of a CLI run that exits nonzero, each its exit code's.
var (
	// ErrNotWritten is exit code 1: the CLI couldn't write its output, its media, or an
	// attachment's content.
	ErrNotWritten = errors.New("not written")
	// ErrUsage is exit code 2: the CLI refused its arguments, a fault in the adapter.
	ErrUsage = errors.New("usage error")
	// ErrRefused is exit code 3: the CLI refused the input, giving its reason.
	ErrRefused = errors.New("refused")
	// ErrFailed is exit code 4: a fault of the CLI's own.
	ErrFailed = errors.New("failed")
)

// outcomes maps the exit codes to their errors.
var outcomes = map[int]error{
	1: ErrNotWritten,
	2: ErrUsage,
	3: ErrRefused,
	4: ErrFailed,
}

// Outcome returns the error of exit code code, ErrNotWritten, ErrUsage, ErrRefused, or
// ErrFailed; nil for a code outside 1 to 4.
func Outcome(code int) error { return outcomes[code] }

// Is reports whether target is the error of exit code code; false for a code outside 1 to 4.
func Is(code int, target error) bool {
	outcome := outcomes[code]
	return outcome != nil && outcome == target
}

// ExitText returns the text of an adapter's ExitError: who, the adapter and its CLI as the
// adapter names them, such as "office" or "email: mail"; then the code's outcome, or the code
// itself outside 1 to 4; then message, the CLI's, if any.
func ExitText(who string, code int, message string) string {
	msg := fmt.Sprintf("%s: exit code %d", who, code)
	if outcome := outcomes[code]; outcome != nil {
		msg = who + ": " + outcome.Error()
	}
	if message != "" {
		msg += ": " + message
	}
	return msg
}

// LimitedBuffer is a writer that keeps the first Limit bytes written to it and discards the
// rest, for a tool's output that is read only in part, such as a CLI's stderr; its writes never
// fail.
type LimitedBuffer struct {
	strings.Builder
	// Limit is the most it keeps.
	Limit int
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	if room := b.Limit - b.Len(); room > 0 {
		b.Builder.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// Spool copies r into a new file in dir, the working directory the caller owns, named spool-*
// and ext, so a tool can read it by path. It returns the file's path and a function that removes
// the file, which the caller runs when its call ends. An empty dir is refused rather than taken
// as the system's temporary directory, and a failed copy removes what it wrote. Its errors begin
// "spool: ", for the caller to prefix with its package.
func Spool(dir string, r io.Reader, ext string) (string, func() error, error) {
	if dir == "" {
		return "", nil, errors.New("spool: no working directory")
	}
	f, err := os.CreateTemp(dir, "spool-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("spool: %w", err)
	}
	path := f.Name()
	remove := func() error { return os.Remove(path) }
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("spool: %w", err), remove())
	}
	return path, remove, nil
}
