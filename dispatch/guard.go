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

const (
	capName   = 255
	capWord   = 255
	capReason = 4096
	capText   = 1024
)

const hashLen = 16

func guard(s string, max int, lines bool) string {
	if len(s) <= max && clean(s, lines) {
		return s
	}
	var b strings.Builder
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

func control(r rune, lines bool) bool {
	if lines && (r == '\t' || r == '\n') {
		return false
	}
	return r < 0x20 || (r >= 0x80 && r <= 0x9f)
}

func guardPart(p Part) Part {
	p.Name = guard(p.Name, capName, false)
	p.Where = guard(p.Where, capName, false)
	p.ContentType.RouteReason = guard(p.ContentType.RouteReason, capWord, false)
	p.ContentType.Stored = guard(p.ContentType.Stored, capWord, false)
	p.Flags = guardFlags(p.Flags)
	return p
}

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

func guardIdentity(id Identity) Identity {
	return Identity{Name: guard(id.Name, capWord, false), Version: guard(id.Version, capWord, false), Config: guard(id.Config, capWord, false)}
}

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
		for _, k := range slices.Sorted(maps.Keys(c.Meta)) {
			meta[guard(k, capName, false)] = guard(c.Meta[k], capText, false)
		}
		c.Meta = meta
	}
	return &c
}

func guardWarning(w *Warning) *Warning {
	if w == nil {
		return nil
	}
	return &Warning{Tool: guard(w.Tool, capWord, false), Where: guard(w.Where, capText, false), What: guard(w.What, capText, false)}
}

func guardEvent(ev Event) Event {
	ev.Part = guardPart(ev.Part)
	ev.Stage = guardIdentity(ev.Stage)
	ev.Output = guardOutput(ev.Output)
	ev.Warning = guardWarning(ev.Warning)
	ev.Reason = guard(ev.Reason, capReason, true)
	return ev
}

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
