package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"iter"
	"regexp"
	"sync"
	"time"

	"github.com/standards-lab/docproc/tool"
)

// Schema is the envelope's schema, which each tool's first line carries in `schema` and its
// --version writes on its second line.
const Schema = "2"

// VersionPattern matches a tool's --version output, the version alone on its first line and
// `schema 2` on its second, capturing the version's dotted numbers, a build suffix such as
// "+8ea6b2d" left out: each reader's tool.Spec takes it, so a tool of another schema is refused
// as the Tool is built.
var VersionPattern = regexp.MustCompile(`\A(\d+(?:\.\d+)*)(?:[-+][0-9A-Za-z.+-]*)?\nschema ` + Schema + `\n?\z`)

// ErrSchema is the error of a run whose first line isn't the kind its reader reads first, or
// doesn't carry the envelope's [Schema], or of a run that exited 0 with no line at all.
var ErrSchema = errors.New("not the envelope's schema " + Schema)

// Run is one run of a tool by [Read].
type Run struct {
	// Tool is the tool, built through the exec runner; nil panics.
	Tool *tool.Tool
	// Args are its arguments.
	Args []string
	// Dir is the run's working directory, the caller's; empty is the process's own.
	Dir string
	// First is the kind of the tool's first line, such as "document".
	First string
	// Timeout and Idle are the run's own bounds, tool.Invocation's, zero for none.
	Timeout, Idle time.Duration
}

// Line is one item of a run: a line of the tool's stdout, its kind and its JSON, or a warning the
// tool wrote to stderr, with Warning set and nothing else.
type Line struct {
	// Kind is the line's `line`.
	Kind string
	// Raw is the line's JSON object, its line feed left out, the reader's to keep.
	Raw jsontext.Value
	// Warning is a warning, parsed from its stderr line, with the tool's name.
	Warning *Warning
}

// Read runs a tool with its stdout and stderr on pipes and yields what it writes, as it writes
// it: each stdout line's kind and JSON, and each stderr warning, parsed and unescaped by the
// envelope's rules, between them. The order between a warning and a line is best effort, since
// they travel on two pipes; every warning is yielded before the sequence's final error.
//
// The first line must be of run.First's kind and carry [Schema], or the read ends with an error
// matching [ErrSchema]; a line that isn't a JSON object with a `line` ends it with an error of its
// own. Either ends the run. A run that exits nonzero, or dies by a signal, ends the sequence with
// an [*ExitError] whose message is the last `error:` line's reason; a context's end, with the
// tool package's error matching it. A caller that stops ranging ends the run and waits for it.
func Read(ctx context.Context, run Run) iter.Seq2[Line, error] {
	if run.Tool == nil {
		panic("cli: Run.Tool is nil")
	}
	return func(yield func(Line, error) bool) {
		r := start(ctx, run)
		defer r.stop()
		count := 0
		for {
			for _, w := range r.stderr.take() {
				if !yield(Line{Warning: &w}, nil) {
					return
				}
			}
			var raw []byte
			var more bool
			select {
			case raw, more = <-r.lines:
			case <-r.stderr.notify:
				continue
			}
			if !more {
				break
			}
			count++
			line, err := decodeLine(r.name, run.First, count, raw)
			if err != nil {
				r.fail(err)
				break
			}
			if !yield(line, nil) {
				return
			}
		}
		err := r.finish(count)
		for _, w := range r.stderr.take() {
			if !yield(Line{Warning: &w}, nil) {
				return
			}
		}
		if err != nil {
			yield(Line{}, err)
		}
	}
}

// reading is one run of Read's in progress.
type reading struct {
	name   string
	cancel context.CancelFunc
	stderr *stderrReader
	pr     *io.PipeReader
	// lines carries stdout's lines from the scanner, closed at stdout's end.
	lines chan []byte
	// stopped is closed once the reader takes no more lines.
	stopped chan struct{}
	// done is closed once the run has returned, runErr and message then set.
	done    chan struct{}
	runErr  error
	message string
	// scanned is closed once the scanner has returned.
	scanned chan struct{}
	// failed is the read's own error, a line that didn't decode, which ended the run.
	failed error
	// ranAlready says the run had returned when the read failed.
	ranAlready bool
	stopOnce   sync.Once
}

// start starts the run and its stdout scanner.
func start(ctx context.Context, run Run) *reading {
	runCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	r := &reading{
		name:    run.Tool.Name(),
		cancel:  cancel,
		stderr:  newStderrReader(run.Tool.Name()),
		pr:      pr,
		lines:   make(chan []byte),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
		scanned: make(chan struct{}),
	}
	go func() {
		r.runErr = run.Tool.Run(runCtx, tool.Invocation{
			Args: run.Args, Dir: run.Dir, Stdout: pw, Stderr: r.stderr,
			Timeout: run.Timeout, Idle: run.Idle,
		})
		r.message = r.stderr.close()
		close(r.done)
		// stdout's end follows the run's, so the reader finds the run's error set at it.
		_ = pw.Close()
	}()
	go r.scan()
	return r
}

// scan sends each stdout line to r.lines, a last line without its line feed included, until
// stdout ends or the reader stops.
func (r *reading) scan() {
	defer close(r.scanned)
	defer close(r.lines)
	br := bufio.NewReader(r.pr)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			select {
			case r.lines <- bytes.TrimSuffix(line, []byte("\n")):
			case <-r.stopped:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// fail ends the run for err, the read's own error, keeping the warnings the run writes as it ends
// for the reader.
func (r *reading) fail(err error) {
	select {
	case <-r.done:
		r.ranAlready = true
	default:
	}
	r.failed = err
	r.stderr.unbound()
	r.end()
}

// end stops taking lines and ends the run, and waits for it and the scanner.
func (r *reading) end() {
	r.stopOnce.Do(func() {
		close(r.stopped)
		r.cancel()
		r.pr.CloseWithError(errors.New("cli: the reader stopped"))
	})
	<-r.done
	<-r.scanned
}

// stop ends a read its caller stopped ranging over, dropping what is left; after finish, it only
// releases the run's context.
func (r *reading) stop() {
	r.stderr.drop()
	r.end()
}

// finish waits for the run and returns the read's final error, nil for a run that exited 0 with
// count lines read: the read's own failure, unless the run had already failed by then; else the
// run's error, an *ExitError carrying the run's message; else ErrSchema for a run that wrote no
// line.
func (r *reading) finish(count int) error {
	r.end()
	switch {
	case r.failed != nil && (!r.ranAlready || r.runErr == nil):
		return r.failed
	case r.runErr != nil:
		return exitError(r.runErr, r.message)
	case count == 0:
		return fmt.Errorf("cli: %s: no first line: %w", r.name, ErrSchema)
	}
	return nil
}

// head is what Read decodes of every line: its kind, and its schema, which the first carries.
type head struct {
	Line   string `json:"line"`
	Schema string `json:"schema"`
}

// decodeLine decodes the n-th line of tool's stdout, raw, checking the first against first and
// Schema.
func decodeLine(tool, first string, n int, raw []byte) (Line, error) {
	var h head
	if err := json.Unmarshal(raw, &h); err != nil {
		return Line{}, fmt.Errorf("cli: %s: line %d: %w", tool, n, err)
	}
	if h.Line == "" {
		return Line{}, fmt.Errorf("cli: %s: line %d has no kind", tool, n)
	}
	if n == 1 && (h.Line != first || h.Schema != Schema) {
		return Line{}, fmt.Errorf("cli: %s: the first line is %q of schema %q, not %q of schema %q: %w",
			tool, h.Line, h.Schema, first, Schema, ErrSchema)
	}
	return Line{Kind: h.Line, Raw: jsontext.Value(raw)}, nil
}
