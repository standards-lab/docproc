package report

import (
	"cmp"
	"html/template"
	"maps"
	"slices"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir/schema"
)

const noRoute = "-"

const errorOutcome = "error"

const rootID = "/"

type model struct {
	Manifest schema.Manifest
	Ended    bool
	Inputs   []*input
	Contents []*content
	Summary  []tally
	Total    tally
}

func (v *model) Span() schema.Span { return schema.Span{Start: v.Manifest.Start, End: v.Manifest.End} }

type tally struct {
	Route   string
	Outcome dispatch.Outcome
	Inputs  int
	Parts   int
}

type input struct {
	Path     string
	Route    string
	Outcome  dispatch.Outcome
	Recorded dispatch.Outcome
	File     *schema.File
	Time     *schema.Span
	Error    string
	Content  *content
	Source   template.URL
	roots    []*node
}

type content struct {
	SHA256 string
	Inputs []*input
	Roots  []*node
}

type node struct {
	Part     schema.Part
	Own      bool
	Event    schema.Event
	Outputs  []*output
	Warnings []warning
	Children []*node
	Kept     string
	seq      int
}

type output struct {
	schema.Output
	Seen    bool
	Show    string
	Href    string
	Text    string
	Missing bool
}

const (
	showText  = "text"
	showImage = "image"
	showAudio = "audio"
	showLink  = "link"
)

type warning struct {
	schema.Warning
	Seen bool
}

func build(m schema.Manifest, lines []schema.Event) *model {
	v := &model{Manifest: m, Ended: !m.End.IsZero()}
	byInput := map[string][]schema.Event{}
	for _, e := range lines {
		byInput[e.Input] = append(byInput[e.Input], e)
	}
	byContent := map[string]*content{}
	for _, path := range slices.Sorted(maps.Keys(byInput)) {
		in := inputOf(path, byInput[path])
		v.Inputs = append(v.Inputs, in)
		sha := ""
		if root := rootOf(in.roots); root != nil {
			sha = root.Part.SHA256
		}
		if in.Outcome == errorOutcome || dispatch.ContentFile(sha) == "" {
			continue
		}
		c := byContent[sha]
		if c == nil {
			c = &content{SHA256: sha, Roots: in.roots}
			byContent[sha] = c
		}
		c.Inputs = append(c.Inputs, in)
		in.Content = c
	}
	for _, sha := range slices.Sorted(maps.Keys(byContent)) {
		v.Contents = append(v.Contents, byContent[sha])
	}
	v.Summary, v.Total = summarize(m, v.Inputs)
	return v
}

func summarize(m schema.Manifest, inputs []*input) ([]tally, tally) {
	type key struct {
		route   string
		outcome dispatch.Outcome
	}
	counts := map[key]*tally{}
	var total tally
	count := func(route string, outcome dispatch.Outcome, isInput bool) {
		k := key{route, outcome}
		t := counts[k]
		if t == nil {
			t = &tally{Route: route, Outcome: outcome}
			counts[k] = t
		}
		t.Parts++
		total.Parts++
		if isInput {
			t.Inputs++
			total.Inputs++
		}
	}
	var walk func(nodes []*node)
	walk = func(nodes []*node) {
		for _, n := range nodes {
			if n.Own {
				count(routeOf(n.Part), n.Event.Outcome, n.Part.ID == rootID)
			}
			walk(n.Children)
		}
	}
	for _, in := range inputs {
		if in.Outcome == errorOutcome {
			count(noRoute, errorOutcome, true)
		}
		walk(in.roots)
	}
	rank := map[string]int{}
	for i, row := range m.Stages {
		if _, ok := rank[string(row.Route)]; !ok {
			rank[string(row.Route)] = i
		}
	}
	rankOf := func(route string) int {
		if i, ok := rank[route]; ok {
			return i
		}
		return len(m.Stages)
	}
	rows := make([]tally, 0, len(counts))
	for _, t := range counts {
		rows = append(rows, *t)
	}
	slices.SortFunc(rows, func(a, b tally) int {
		return cmp.Or(cmp.Compare(rankOf(a.Route), rankOf(b.Route)), cmp.Compare(a.Route, b.Route),
			cmp.Compare(a.Outcome, b.Outcome))
	})
	return rows, total
}

func rootOf(roots []*node) *node {
	for _, n := range roots {
		if n.Own && n.Part.ID == rootID {
			return n
		}
	}
	return nil
}

func inputOf(path string, lines []schema.Event) *input {
	in := &input{Path: path, Route: noRoute}
	in.roots = treeOf(lines)
	for _, e := range lines {
		switch {
		case e.Kind == schema.PartEvent && e.Part.ID == rootID:
			in.Route = routeOf(e.Part)
			in.Outcome, in.Recorded = e.Outcome, e.Recorded
			in.File, in.Time = e.File, e.Time
		case e.Kind == schema.ErrorEvent:
			in.Route, in.Outcome, in.Recorded = noRoute, errorOutcome, ""
			in.File, in.Time, in.Error = e.File, e.Time, e.Error
		}
	}
	return in
}

func treeOf(lines []schema.Event) []*node {
	byID := map[string]*node{}
	seq := 0
	get := func(p schema.Part) *node {
		n := byID[p.ID]
		if n == nil || n.Own {
			n = &node{Part: p}
			byID[p.ID] = n
		}
		seq++
		n.seq = seq
		return n
	}
	for _, e := range lines {
		switch e.Kind {
		case schema.OutputEvent:
			if e.Output == nil {
				continue
			}
			n := get(e.Part)
			n.Outputs = append(n.Outputs, &output{Output: *e.Output, Seen: e.Outcome == dispatch.Seen})
		case schema.WarningEvent:
			if e.Warning == nil {
				continue
			}
			n := get(e.Part)
			n.Warnings = append(n.Warnings, warning{Warning: *e.Warning, Seen: e.Outcome == dispatch.Seen})
		case schema.PartEvent:
			n := byID[e.Part.ID]
			if n == nil || n.Own {
				n = get(e.Part)
			} else {
				seq++
				n.seq = seq
			}
			n.Part, n.Own, n.Event = e.Part, true, e
		}
	}
	nodes := slices.SortedFunc(maps.Values(byID), func(a, b *node) int { return cmp.Compare(a.seq, b.seq) })
	var roots []*node
	for _, n := range nodes {
		parent := byID[n.Part.Parent]
		if n.Part.Parent != "" && parent != nil && parent.Own && parent != n {
			parent.Children = append(parent.Children, n)
			continue
		}
		roots = append(roots, n)
	}
	rank := func(n *node) int {
		if n.Own && n.Part.ID == rootID {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(roots, func(a, b *node) int { return cmp.Compare(rank(a), rank(b)) })
	return roots
}

func routeOf(p schema.Part) string {
	if p.ContentType.Route != "" {
		return string(p.ContentType.Route)
	}
	return noRoute
}
