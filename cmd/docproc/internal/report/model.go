package report

import (
	"cmp"
	"maps"
	"slices"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir/schema"
)

// noRoute is how the report names the route of an input that has none, as the terminal does.
const noRoute = "-"

// errorOutcome is the outcome the report gives an input whose dispatch stopped with an error
// before its own event, as the terminal does.
const errorOutcome = "error"

// rootID is the ID of an input's own part, dispatch.Part's "/".
const rootID = "/"

// model is a run as the report shows it, built from its manifest and record alone.
type model struct {
	Manifest schema.Manifest
	// Ended reports whether the manifest holds the run's end; an unended run's record is what it
	// wrote before it was killed, or what it has written so far.
	Ended bool
	// Inputs are every input the record names, sorted by path, an input stopped before its own
	// event among them.
	Inputs []*input
	// Contents are the distinct contents of the inputs that reached their own event, sorted by
	// SHA-256: one page each.
	Contents []*content
}

// input is one input of the run: the path its record lines name, and what its own line, or its
// error line, says of it.
type input struct {
	Path string
	// Route is its own part's route, noRoute for none or for an input stopped with an error.
	Route string
	// Outcome is its own part's outcome, or errorOutcome; Recorded, a seen part's recorded one.
	Outcome  dispatch.Outcome
	Recorded dispatch.Outcome
	// File and Time are its own line's, or its error line's; either may be absent.
	File *schema.File
	Time *schema.Span
	// Error is the error that stopped its dispatch before its own event; empty when none did.
	Error string
	// Content is the page of the content it holds, nil for an input stopped before its own event,
	// which has no SHA-256.
	Content *content
	// roots are its part tree.
	roots []*node
}

// content is one content of the run, by its SHA-256, and the inputs holding it.
type content struct {
	SHA256 string
	// Inputs are the inputs holding it, sorted by path.
	Inputs []*input
	// Roots are its part tree: its own part, and any part whose parent never had its own event.
	// It is the first input's, by path; every input holding the content found the same parts.
	Roots []*node
}

// node is one part of an input's tree: the part, its own line, the outputs and warnings its lines
// gave, and the parts found in it.
type node struct {
	Part schema.Part
	// Own reports whether the part had its own line; a part with outputs or warnings but none is
	// shown as the lines name it, with no outcome.
	Own bool
	// Event is the part's own line, the zero Event when it had none.
	Event    schema.Event
	Outputs  []*output
	Warnings []warning
	Children []*node
	// seq orders the parts by their own lines, or, for a part with none, by its first line.
	seq int
}

// output is one output line of a part.
type output struct {
	schema.Output
	// Seen reports that the index recorded the output earlier and this run replayed it.
	Seen bool
}

// warning is one warning line of a part.
type warning struct {
	schema.Warning
	Seen bool
}

// build returns the model of the run m describes, whose record is lines, in the order they were
// written. It reads nothing but its arguments.
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
		if in.Outcome == errorOutcome || sha == "" {
			continue
		}
		c := byContent[sha]
		if c == nil {
			// The first input by path gives the content's tree.
			c = &content{SHA256: sha, Roots: in.roots}
			byContent[sha] = c
		}
		c.Inputs = append(c.Inputs, in)
		in.Content = c
	}
	for _, sha := range slices.Sorted(maps.Keys(byContent)) {
		v.Contents = append(v.Contents, byContent[sha])
	}
	return v
}

// rootOf returns the input's own part among roots, nil when it had no own line.
func rootOf(roots []*node) *node {
	for _, n := range roots {
		if n.Own && n.Part.ID == rootID {
			return n
		}
	}
	return nil
}

// inputOf returns the input at path from its record lines, in their order.
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

// treeOf returns an input's part tree from its record lines, in their order, by the parts' IDs:
// each part's outputs and warnings gather under its ID until its own line, and each part is a
// child of the part its parent ID names, when that part had its own line, and at the top of the
// tree otherwise. A part's lines repeated after its own line, as a replay whose subtree the index
// no longer held whole prepares it again, start it afresh, so the part shows its last lines once.
// The parts keep the order of their own lines, which is the order the stages finished them, but
// the input's own part, which heads the tree.
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
	// The input's own part heads the tree, before any part whose parent had no line of its own.
	rank := func(n *node) int {
		if n.Own && n.Part.ID == rootID {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(roots, func(a, b *node) int { return cmp.Compare(rank(a), rank(b)) })
	return roots
}

// routeOf returns p's route as the report names it, noRoute for none.
func routeOf(p schema.Part) string {
	if p.ContentType.Route != "" {
		return string(p.ContentType.Route)
	}
	return noRoute
}
