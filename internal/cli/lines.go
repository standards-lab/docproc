package cli

import (
	"bytes"
	"os"
)

// MaxLine is the longest line a LineWriter passes whole; a longer one is cut there.
const MaxLine = 4 << 10

// LineWriter splits what is written to it into lines, at a carriage return or a line feed,
// passing each to Line, trimmed of surrounding space, and keeping the last that isn't empty, for
// a program's stderr read as it arrives, such as FFmpeg's. Once Line returns an error, it keeps the
// error and passes nothing more. A nil Line passes nothing, keeping the last line alone. Its
// memory is one line, at most MaxLine; its writes never fail.
type LineWriter struct {
	// Line is passed each line that isn't empty, without its line break.
	Line func(string) error

	partial []byte
	last    string
	err     error
}

func (w *LineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexAny(p, "\r\n")
		if i < 0 {
			w.partial = append(w.partial, p[:min(len(p), MaxLine-len(w.partial))]...)
			break
		}
		w.partial = append(w.partial, p[:min(i, MaxLine-len(w.partial))]...)
		w.emit()
		p = p[i+1:]
	}
	return n, nil
}

// Flush passes the line left without a line break, if any, once the writing has ended.
func (w *LineWriter) Flush() {
	if len(w.partial) > 0 {
		w.emit()
	}
}

// Last returns the last line that wasn't empty, "" when there was none.
func (w *LineWriter) Last() string { return w.last }

// Err returns the first error Line returned, nil when there was none.
func (w *LineWriter) Err() error { return w.err }

// emit passes the partial line to Line and empties it.
func (w *LineWriter) emit() {
	s := string(bytes.TrimSpace(w.partial))
	w.partial = w.partial[:0]
	if s == "" {
		return
	}
	w.last = s
	if w.err == nil && w.Line != nil {
		w.err = w.Line(s)
	}
}

// WriteTemp writes b to a new hidden temporary file in dir, the directory the caller owns, named
// by pattern as os.CreateTemp names it, such as ".window-*.wav", and returns its path, for a file
// the caller renames into place once it is whole. A failed write removes the file.
func WriteTemp(dir, pattern string, b []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
