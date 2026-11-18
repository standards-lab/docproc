package schema

import (
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
)

// The shapes schema "2" takes from dispatch and inspect at dispatch.Version "1", aliased so none
// is defined twice: a break in any of them is a break in the schema.
type (
	// Part is one part of an input, the input itself among them, as dispatch.Part marshals it.
	Part = dispatch.Part
	// ContentType is what routing read of a part, dispatch.ContentType.
	ContentType = dispatch.ContentType
	// Stage is a stage's identity, dispatch.Identity: a part's outputs are prepared again
	// whenever any of it changes.
	Stage = dispatch.Identity
	// Output is one output a stage prepared, dispatch.Output, its content in the run directory's
	// content store under its "sha256".
	Output = dispatch.Output
	// Warning says a unit of a part read badly or not at all, dispatch.Warning.
	Warning = dispatch.Warning
	// Flag is one finding of a part's inspection, inspect.Flag.
	Flag = inspect.Flag
)

// The kinds of a record line, its "event" field. A reader may tell the first three apart by which
// of "output" and "warning" is set.
const (
	// PartEvent is a line for a part's own event: its outcome, once every output, warning, and
	// part found in it has its line.
	PartEvent = "part"
	// OutputEvent is a line for one output a part's stage prepared, or, for a part the index has
	// seen, one output it recorded.
	OutputEvent = "output"
	// WarningEvent is a line for one warning a part's stage gave, or, for a part the index has
	// seen, one warning it recorded.
	WarningEvent = "warning"
	// ErrorEvent is a line for an input whose dispatch stopped with an error before its own
	// event, such as an input that can't be read.
	ErrorEvent = "error"
)

// Event is one line of a run's record, runs/<run>/record.jsonl, a JSON object per line in the
// order the events arrived: an input's lines keep dispatch's order, each part's outputs,
// warnings, and found parts before its own line, and different inputs' lines interleave. Its
// fields:
//
//   - "event": the line's kind, [PartEvent], [OutputEvent], [WarningEvent], or [ErrorEvent]
//   - "input": the input the line belongs to, its path, absolute, cleaned: a path argument, or a
//     file under one
//   - dispatch.Event's fields, inline, as it marshals them: "part", "stage", "output",
//     "warning", "after", "outcome", "recorded", "reason", "outputs", and "children", each
//     absent when zero; an "error" line has no "part"
//   - "file": the input's [File]; on the input's own "part" line, and on an "error" line when
//     the file could still be read; optional
//   - "quarantine": the kept file of a quarantined part, quarantine/<sha256>, relative to the run
//     directory, whenever it exists, whether this line's run kept it or an earlier one did; the
//     line's part carries the flags; optional
//   - "time": the input's [Span], on the input's own "part" line and on an "error" line;
//     optional
//   - "error": on an "error" line, the error that stopped the input's dispatch; dispatch.Event's
//     Err never marshals, so its text is here
type Event struct {
	Kind  string `json:"event"`
	Input string `json:"input"`
	dispatch.Event
	File       *File  `json:"file,omitempty"`
	Quarantine string `json:"quarantine,omitzero"`
	Time       *Span  `json:"time,omitempty"`
	Error      string `json:"error,omitzero"`
}

// Span is when an input's dispatch ran: "start", its first event, and "end", its last, each a
// [Time].
type Span struct {
	Start Time `json:"start"`
	End   Time `json:"end"`
}

// Manifest is a run's manifest, runs/<run>/manifest.json, one JSON object, written as the run
// starts and again as it ends:
//
//   - "schema": the schema's version, [Version], so a manifest read alone names it
//   - "run": the run's id, a version 7 UUID, which sorts by when the run started
//   - "docproc": the [Build] that wrote it
//   - "start": when the run started, a [Time]
//   - "end": when it ended, a [Time]; optional, absent while it runs, or when it never ended
//   - "workers": how many inputs were dispatched at once
//   - "inputs": the path arguments, files and directories, each absolute, cleaned
//   - "collected": how many inputs the path arguments named
//   - "tools": each tool the run tried, dispatch.Tool
//   - "stages": each route's line of the stage table, dispatch.Row, in the table's order
//   - "error": the error that stopped the whole run; optional, absent when none did
//
// The run's record is at the layout's path, runs/<run>/record.jsonl, which the manifest doesn't
// repeat.
type Manifest struct {
	Schema    string          `json:"schema"`
	Run       string          `json:"run"`
	Docproc   Build           `json:"docproc"`
	Start     Time            `json:"start"`
	End       Time            `json:"end,omitzero"`
	Workers   int             `json:"workers"`
	Inputs    []string        `json:"inputs"`
	Collected int             `json:"collected"`
	Tools     []dispatch.Tool `json:"tools"`
	Stages    []dispatch.Row  `json:"stages"`
	Error     string          `json:"error,omitzero"`
}

// Build is what wrote a run, from the command's build info:
//
//   - "command": the version of the command's module, such as "v0.1.0", or "(devel)" for a
//     build from a checkout
//   - "library": the version of the docproc module it was built with, as the command's build
//     info names it
type Build struct {
	Command string `json:"command"`
	Library string `json:"library"`
}
