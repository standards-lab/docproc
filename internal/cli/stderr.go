package cli

import (
	"bytes"
	"strconv"
	"strings"
	"sync"

	"github.com/standards-lab/docproc/dispatch"
)

const pendingMax = 256

type stderrReader struct {
	tool   string
	notify chan struct{}

	mu                         sync.Mutex
	cond                       *sync.Cond
	partial                    []byte
	pending                    []dispatch.Warning
	message                    string
	closed, unbounded, discard bool
}

func newStderrReader(tool string) *stderrReader {
	s := &stderrReader{tool: tool, notify: make(chan struct{}, 1)}
	s.cond = sync.NewCond(&s.mu)
	return s
}

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

func (s *stderrReader) take() []dispatch.Warning {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := s.pending
	s.pending = nil
	s.cond.Broadcast()
	return taken
}

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

func (s *stderrReader) unbound() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unbounded = true
	s.cond.Broadcast()
}

func (s *stderrReader) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discard = true
	s.pending = nil
	s.cond.Broadcast()
}

func parseStderr(tool, line string) (w dispatch.Warning, reason string, isError bool) {
	w.Tool = tool
	if rest, ok := strings.CutPrefix(line, tool+": "); ok {
		if r, ok := strings.CutPrefix(rest, "error: "); ok {
			return dispatch.Warning{}, unescape(r), true
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
