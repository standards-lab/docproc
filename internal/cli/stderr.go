package cli

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
)

// Warning is a warning a tool wrote to stderr: the tool's name, where in the input, and what went
// wrong, as dispatch.Warning holds one.
//
// Interim until slice 15: internal/cli can't import dispatch while dispatch holds the stage
// adapters, which import the readers and so internal/cli. Once they move to stages, Read yields
// dispatch.Warning and this type goes.
type Warning struct {
	Tool  string
	Where string
	What  string
}

// pendingMax is how many parsed warnings a run's stderr holds for its reader before the tool's
// writes to stderr wait for the reader to take them. It bounds no output, only the buffer between
// the two pipes: the reader takes warnings whenever they arrive, between stdout's lines.
const pendingMax = 256

// stderrReader is a run's stderr, parsed as it arrives: each line a tool writes is a warning, or
// an `error:` line whose reason is the run's message, the last one written.
type stderrReader struct {
	tool string
	// notify has a value once a warning is pending, for the reader to take it.
	notify chan struct{}

	mu   sync.Mutex
	cond *sync.Cond
	// partial is a line written in part, its line feed yet to come.
	partial []byte
	pending []Warning
	message string
	// closed is set once the run has returned; a write after it, a copy os/exec left running, is
	// dropped. unbounded lets pending grow past pendingMax, once the run is being ended, and
	// discard drops every warning, once nothing will read them.
	closed, unbounded, discard bool
}

func newStderrReader(tool string) *stderrReader {
	s := &stderrReader{tool: tool, notify: make(chan struct{}, 1)}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Write parses each whole line in p, keeping a line's start until its line feed comes. It never
// fails, so the tool's stderr is read to its end; it waits while pendingMax warnings are pending.
func (s *stderrReader) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	for len(p) > 0 && !s.closed {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.partial = append(s.partial, p...)
			break
		}
		line := p[:i]
		if len(s.partial) > 0 {
			line = append(s.partial, line...)
			s.partial = s.partial[:0]
		}
		s.line(string(line))
		p = p[i+1:]
	}
	return n, nil
}

// line parses one line, s.mu held: a blank line is skipped; an `error:` line sets the message; any
// other line is a warning.
func (s *stderrReader) line(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	w, reason, isError := parseStderr(s.tool, line)
	if isError {
		s.message = reason
		return
	}
	for len(s.pending) >= pendingMax && !s.unbounded && !s.discard && !s.closed {
		s.cond.Wait()
	}
	if s.discard || s.closed {
		return
	}
	s.pending = append(s.pending, w)
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// take returns the pending warnings, in order, and lets a waiting write go on.
func (s *stderrReader) take() []Warning {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := s.pending
	s.pending = nil
	s.cond.Broadcast()
	return taken
}

// close ends the reading once the run has returned: a line still unended is parsed, and later
// writes are dropped. It returns the run's message.
func (s *stderrReader) close() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.partial) > 0 {
		s.unbounded = true
		s.line(string(s.partial))
		s.partial = nil
	}
	s.closed = true
	s.cond.Broadcast()
	return s.message
}

// unbound lets warnings pend past pendingMax, so a run being ended never waits on its reader.
func (s *stderrReader) unbound() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unbounded = true
	s.cond.Broadcast()
}

// drop discards every warning from now on, the reader having stopped.
func (s *stderrReader) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discard = true
	s.pending = nil
	s.cond.Broadcast()
}

// parseStderr parses one line of tool's stderr, as the envelope writes it: `<tool>: warning:
// <where>: <what>`, split at its first three ": " and each part unescaped, or `<tool>: error:
// <reason>`, its reason unescaped, which reports isError. A line that fits neither is a warning
// whose What is the line alone, as written.
func parseStderr(tool, line string) (w Warning, reason string, isError bool) {
	w.Tool = tool
	if rest, ok := strings.CutPrefix(line, tool+": "); ok {
		if r, ok := strings.CutPrefix(rest, "error: "); ok {
			return Warning{}, unescape(r), true
		}
		if r, ok := strings.CutPrefix(rest, "warning: "); ok {
			if where, what, ok := strings.Cut(r, ": "); ok {
				w.Where, w.What = unescape(where), unescape(what)
				return w, "", false
			}
		}
	}
	w.What = line
	return w, "", false
}

// unescape inverts the envelope's escape of a warning's where and what and a reason: a backslash
// before a character is that character, so `\\` is a backslash and, in a where, `:\ ` a colon
// followed by a space; `\n` and `\r` are a line feed and a carriage return; and `\u{…}` is the
// character of that hex code point. An escape that is none of these, a trailing backslash among
// them, is kept as written.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		switch next := s[i+1]; next {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'u':
			r, n := codePoint(s[i+2:])
			if n == 0 {
				b.WriteString(`\u`)
				break
			}
			b.WriteRune(r)
			i += n
		default:
			b.WriteByte(next)
		}
		i++
	}
	return b.String()
}

// codePoint reads `{hex}` at s's start, as the escape `\u{…}` follows its `u` with, and returns
// its character and the bytes it took; 0 bytes when s doesn't start with one of a valid code
// point.
func codePoint(s string) (rune, int) {
	if !strings.HasPrefix(s, "{") {
		return 0, 0
	}
	end := strings.IndexByte(s, '}')
	if end < 2 || end > 7 {
		return 0, 0
	}
	n, err := strconv.ParseUint(s[1:end], 16, 32)
	if err != nil || n > 0x10FFFF || n >= 0xD800 && n <= 0xDFFF {
		return 0, 0
	}
	return rune(n), end + 1
}
