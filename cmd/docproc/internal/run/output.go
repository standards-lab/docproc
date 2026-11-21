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

const errorOutcome = "error"

const errorRunes = 400

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

type table struct {
	tw  *tabwriter.Writer
	err error
}

func newTable(w io.Writer) *table {
	return &table{tw: tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)}
}

func (t *table) row(cells ...string) {
	if t.err == nil {
		_, t.err = io.WriteString(t.tw, strings.Join(cells, "\t")+"\n")
	}
}

func (t *table) flush() error { return errors.Join(t.err, t.tw.Flush()) }

type tallyKey struct {
	route   string
	outcome string
}

type terminal struct {
	w       io.Writer
	tally   map[tallyKey]int
	failed  int
	errored int
}

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

func (t *terminal) stopped(path string, stop error, span schema.Span) error {
	t.tally[tallyKey{routeName(""), errorOutcome}]++
	t.errored++
	_, err := fmt.Fprintf(t.w, "%s  -  %s: %s  %s\n", path, errorOutcome, brief(stop.Error()), took(span))
	return err
}

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

func took(span schema.Span) time.Duration {
	return span.End.Time().Sub(span.Start.Time()).Round(time.Millisecond)
}

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

func routeName(rt route.Route) string {
	if rt == "" {
		return "-"
	}
	return string(rt)
}

func routeRank(name string) int {
	if i := slices.Index(stages.Routes(), route.Route(name)); i >= 0 {
		return i
	}
	return len(stages.Routes())
}
