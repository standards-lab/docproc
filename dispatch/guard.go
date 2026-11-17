package dispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/standards-lab/docproc/inspect"
)

// The caps of the wire's strings, in bytes, one per field: a string past its cap is cut, as guard
// says.
const (
	// capName caps a part's name and its Where, an output's name, and a Meta key.
	capName = 255
	// capWord caps a string a stage or routing names from a vocabulary rather than the content:
	// a media type, a stored MIME type, a route's reason, a warning's tool, and an identity's
	// fields.
	capWord = 255
	// capReason caps an event's and a child's reason, the only strings that keep tab and line
	// feed.
	capReason = 4096
	// capText caps a warning's where and what, a flag's evidence, and a Meta value.
	capText = 1024
)

// hashLen is the length of what guard appends to a string it cuts: the hex of the first 8 bytes of
// the whole string's SHA-256.
const hashLen = 16

// guard returns s as the wire takes it, the one rule every string of dispatch's shapes passes
// before an event is yielded or an item appended: valid UTF-8, with no C0 or C1 control, U+0000
// among them, but tab and line feed where lines is set; each byte of invalid UTF-8 or a control
// percent-encoded, as "%00"; and, past max bytes, cut at a rune or an escape so that the hex of
// the first 8 bytes of s's SHA-256 fits after it, within max, so two long strings that share
// their head stay apart. A string the rule leaves alone is returned as is.
func guard(s string, max int, lines bool) string {
	if len(s) <= max && clean(s, lines) {
		return s
	}
	var b strings.Builder
	// ends holds the end of each rune or escape written, for a cut that splits neither.
	var ends []int
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && n == 1) || control(r, lines) {
			for _, c := range []byte(s[i : i+n]) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		} else {
			b.WriteString(s[i : i+n])
		}
		ends = append(ends, b.Len())
		i += n
	}
	out := b.String()
	if len(out) <= max {
		return out
	}
	cut := 0
	for _, e := range ends {
		if e > max-hashLen {
			break
		}
		cut = e
	}
	sum := sha256.Sum256([]byte(s))
	return out[:cut] + hex.EncodeToString(sum[:hashLen/2])
}

// clean reports whether s is valid UTF-8 holding no control guard encodes.
func clean(s string, lines bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if control(r, lines) {
			return false
		}
	}
	return true
}

// control reports whether r is a C0 or C1 control the wire refuses: every one, but tab and line
// feed where lines is set.
func control(r rune, lines bool) bool {
	if lines && (r == '\t' || r == '\n') {
		return false
	}
	return r < 0x20 || (r >= 0x80 && r <= 0x9f)
}

// guardPart returns p with every string guard's.
func guardPart(p Part) Part {
	p.Name = guard(p.Name, capName, false)
	p.Where = guard(p.Where, capName, false)
	p.ContentType.RouteReason = guard(p.ContentType.RouteReason, capWord, false)
	p.ContentType.Stored = guard(p.ContentType.Stored, capWord, false)
	p.Flags = guardFlags(p.Flags)
	return p
}

// guardFlags returns flags with each evidence guard's, a copy when one changes.
func guardFlags(flags []inspect.Flag) []inspect.Flag {
	var out []inspect.Flag
	for i, f := range flags {
		if e := guard(f.Evidence, capText, false); e != f.Evidence {
			if out == nil {
				out = append([]inspect.Flag(nil), flags...)
			}
			out[i].Evidence = e
		}
	}
	if out == nil {
		return flags
	}
	return out
}

// guardIdentity returns id with every string guard's.
func guardIdentity(id Identity) Identity {
	return Identity{Name: guard(id.Name, capWord, false), Version: guard(id.Version, capWord, false), Config: guard(id.Config, capWord, false)}
}

// guardOutput returns a copy of o with every string guard's, its Meta a copy when one changes.
func guardOutput(o *Output) *Output {
	if o == nil {
		return nil
	}
	c := *o
	c.Name = guard(c.Name, capName, false)
	c.MediaType = guard(c.MediaType, capWord, false)
	changed := false
	for k, v := range c.Meta {
		if guard(k, capName, false) != k || guard(v, capText, false) != v {
			changed = true
			break
		}
	}
	if changed {
		meta := make(map[string]string, len(c.Meta))
		// Sorted, so of two keys guard makes alike, the later one's value is kept on every run.
		for _, k := range slices.Sorted(maps.Keys(c.Meta)) {
			meta[guard(k, capName, false)] = guard(c.Meta[k], capText, false)
		}
		c.Meta = meta
	}
	return &c
}

// guardWarning returns a copy of w with every string guard's.
func guardWarning(w *Warning) *Warning {
	if w == nil {
		return nil
	}
	return &Warning{Tool: guard(w.Tool, capWord, false), Where: guard(w.Where, capText, false), What: guard(w.What, capText, false)}
}

// guardEvent returns ev with every string guard's.
func guardEvent(ev Event) Event {
	ev.Part = guardPart(ev.Part)
	ev.Stage = guardIdentity(ev.Stage)
	ev.Output = guardOutput(ev.Output)
	ev.Warning = guardWarning(ev.Warning)
	ev.Reason = guard(ev.Reason, capReason, true)
	return ev
}

// guardRecorded returns r with every string guard's.
func guardRecorded(r Recorded) Recorded {
	r.Output = guardOutput(r.Output)
	r.Warning = guardWarning(r.Warning)
	if r.Child != nil {
		c := *r.Child
		c.Part = guardPart(c.Part)
		r.Child = &c
	}
	return r
}

// guardEntry returns e with every string guard's.
func guardEntry(e Entry) Entry {
	e.Reason = guard(e.Reason, capReason, true)
	e.Flags = guardFlags(e.Flags)
	stages := make([]Identity, len(e.Stages))
	for i, id := range e.Stages {
		stages[i] = guardIdentity(id)
	}
	e.Stages = stages
	return e
}
