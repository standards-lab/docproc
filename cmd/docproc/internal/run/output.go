package run

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/docproc/stages"
)

// errorOutcome is the outcome the terminal and the summary give an input whose dispatch stopped
// with an error before its own event.
const errorOutcome = "error"

// errorRunes is how many runes of an error the terminal prints, before its ellipsis: enough for
// the whole first line of every error the spike's runs over ~/docs recorded, and no more than a
// few terminal rows.
const errorRunes = 400

// writeTable writes set as two tables to w: each tool Build tried, with its version, the minimum
// a stage takes, and where it was found, or why it wasn't built; then each route's stage, its
// name and its tools' versions, or none, when wired, or its state and why.
func writeTable(w io.Writer, set stages.Set) error {
	tb := newTable(w)
	tb.row("TOOL", "VERSION", "MINIMUM", "FROM")
	for _, t := range set.Tools {
		version, from := t.Version, t.From
		if t.Error != "" {
			version, from = "-", t.Error
		}
		tb.row(t.Program, version, t.Minimum, from)
	}
	tb.row()
	tb.row("ROUTE", "STAGE", "STATE", "TOOLS")
	for _, r := range set.Rows {
		name, detail := "-", r.Reason
		if r.State == dispatch.Wired {
			name, detail = r.Stage.Name, r.Tools
			if detail == "" {
				detail = "none"
			}
		}
		tb.row(string(r.Route), name, string(r.State), detail)
	}
	tb.row()
	return tb.flush()
}

// table is a table the terminal prints, its columns aligned two spaces apart.
type table struct {
	tw  *tabwriter.Writer
	err error // the first write's failure
}

// newTable returns a table printing to w.
func newTable(w io.Writer) *table {
	return &table{tw: tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)}
}

// row adds a row of cells; no cells add an empty line.
func (t *table) row(cells ...string) {
	if t.err == nil {
		_, t.err = io.WriteString(t.tw, strings.Join(cells, "\t")+"\n")
	}
}

// flush prints the table. tabwriter buffers every cell until then, so a write to the terminal
// fails only here; a row's failure is kept all the same.
func (t *table) flush() error { return errors.Join(t.err, t.tw.Flush()) }

// tallyKey is one cell of the closing summary: a route and an outcome.
type tallyKey struct {
	route   string
	outcome string
}

// terminal is what a run prints as it goes: one line per input, as its last event arrives, and
// the closing summary, from the counts it keeps.
type terminal struct {
	w       io.Writer
	tally   map[tallyKey]int // inputs by route and outcome
	failed  int              // parts failed or replayed with a recorded failure, at any depth
	errored int              // inputs stopped before their own event
}

// input prints the line of an input whose own event, ev, has arrived: its path, route, outcome,
// with its error as brief prints it when it failed, its outputs and children, and its time.
func (t *terminal) input(path string, ev dispatch.Event, span schema.Span) error {
	outcome := string(ev.Outcome)
	if ev.Err != nil {
		outcome += ": " + brief(ev.Err.Error())
	}
	t.tally[tallyKey{routeName(ev.Part.ContentType.Route), string(ev.Outcome)}]++
	_, err := fmt.Fprintf(t.w, "%s  %s  %s  outputs %d  children %d  %s\n",
		path, routeName(ev.Part.ContentType.Route), outcome, ev.Outputs, ev.Children, took(span))
	return err
}

// stopped prints the line of an input whose dispatch the error stop ended before its own event.
func (t *terminal) stopped(path string, stop error, span schema.Span) error {
	t.tally[tallyKey{routeName(""), errorOutcome}]++
	t.errored++
	_, err := fmt.Fprintf(t.w, "%s  -  %s: %s  %s\n", path, errorOutcome, brief(stop.Error()), took(span))
	return err
}

// summary writes the closing summary: how many inputs came to each route and outcome, in the
// stage table's route order, then the total, the run's id, and, last, how many parts failed and
// how many inputs stopped with an error, which leave the run's exit status 0.
func (t *terminal) summary(id string) error {
	keys := slices.SortedFunc(maps.Keys(t.tally), func(a, b tallyKey) int {
		if c := routeRank(a.route) - routeRank(b.route); c != 0 {
			return c
		}
		return strings.Compare(a.outcome, b.outcome)
	})
	tb := newTable(t.w)
	tb.row()
	tb.row("ROUTE", "OUTCOME", "INPUTS")
	total := 0
	for _, k := range keys {
		tb.row(k.route, k.outcome, fmt.Sprint(t.tally[k]))
		total += t.tally[k]
	}
	tb.row("total", "", fmt.Sprint(total))
	tb.row()
	tb.row("run " + id)
	tb.row(fmt.Sprintf("failed parts %d  error inputs %d", t.failed, t.errored))
	return tb.flush()
}

// took returns span's length, to the millisecond, as the terminal writes it.
func took(span schema.Span) time.Duration {
	return span.End.Time().Sub(span.Start.Time()).Round(time.Millisecond)
}

// brief returns an error's text as the terminal prints it, one printable line: its first line,
// with each rune that isn't printable, a control byte, a byte that isn't UTF-8, or a format rune,
// replaced by U+FFFD, and cut at errorRunes runes. An ellipsis says more of it is in the record,
// which holds it all: a tool's error can run to several lines.
func brief(text string) string {
	first, _, more := strings.Cut(strings.TrimSpace(text), "\n")
	var b strings.Builder
	n := 0
	for _, c := range first {
		if n == errorRunes {
			more = true
			break
		}
		if !unicode.IsPrint(c) {
			c = utf8.RuneError
		}
		b.WriteRune(c)
		n++
	}
	if more {
		b.WriteString("…")
	}
	return b.String()
}

// routeName returns rt as the terminal names it, "-" for none.
func routeName(rt route.Route) string {
	if rt == "" {
		return "-"
	}
	return string(rt)
}

// routeRank returns where name, a route as routeName gives it, sorts in the summary: in the
// stage table's order, and any other route, "-" among them, after.
func routeRank(name string) int {
	if i := slices.Index(stages.Routes(), route.Route(name)); i >= 0 {
		return i
	}
	return len(stages.Routes())
}
