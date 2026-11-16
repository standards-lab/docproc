package dispatch

import "github.com/standards-lab/docproc/route"

// Version is the version of dispatch's wire, "1", grown by addition only: a field, a value, a key,
// or a media type may be added under it, and none changed or removed. It versions:
//
//   - the shapes a dispatch yields, as JSON on encoding/json: [Event], its [Part] and the part's
//     [ContentType], its stage's [Identity], its [Output], and its [Warning];
//   - the vocabularies those shapes carry, route's Kind and Route and inspect's Check, Class, and
//     Flag, which change what a shape holds without a field changing;
//   - the index's shapes, which a provider stores and a replay reads back: [Key], [Entry],
//     [Recorded], and [Child], with the [Outcome]s they hold;
//   - the stage table a host reports: [Tool], [Row], and [State];
//   - the Meta keys a consumer reads, [MetaPage] and the rest, and the record outputs' media
//     types, [MediaAudioPlan] and the rest.
//
// The content store's layout, [ContentFile], is stated stable apart from it.
const Version = "1"

// ContentType is what routing read of a part: the kind its signature names, its route and why, and
// the MIME type it was stored with, if any. It never holds a host's own sniffing of an input.
type ContentType struct {
	Kind        route.Kind  `json:"kind,omitzero"`
	Route       route.Route `json:"route,omitzero"`
	RouteReason string      `json:"route_reason,omitzero"`
	Stored      string      `json:"stored,omitzero"`
}

// Warning says a unit of a part read badly or not at all: the tool that read it, or the stage
// when the warning is the stage's own, where in the part, such as a page, a window, or the form,
// and what went wrong.
type Warning struct {
	Tool  string `json:"tool"`
	Where string `json:"where"`
	What  string `json:"what"`
}

// Tool is a tool a host's wiring tried, one line of the stage table's tools: its program, the
// minimum version a stage takes, where it was found, as a path or an image, its whole version
// line, and, when it can't be used, why. Err is local, beside its text in Error.
type Tool struct {
	Program string `json:"program"`
	Minimum string `json:"minimum"`
	From    string `json:"from,omitzero"`
	Version string `json:"version,omitzero"`
	Error   string `json:"error,omitzero"`
	Err     error  `json:"-"`
}

// State is a route's state in the stage table.
type State string

const (
	// Wired is a route whose stage is wired.
	Wired State = "wired"
	// Missing is a route whose stage lacks a tool it requires: its parts have no processor yet.
	Missing State = "missing"
	// Excluded is a route the host excluded: its parts have no processor yet.
	Excluded State = "excluded"
)

// Row is one route's line of the stage table a host reports: its route, its state, the wired
// stage's identity, its tools and their versions, and, for a route not wired, why.
type Row struct {
	Route  route.Route `json:"route"`
	State  State       `json:"state"`
	Stage  Identity    `json:"stage,omitzero"`
	Tools  string      `json:"tools,omitzero"`
	Reason string      `json:"reason,omitzero"`
}

// The Meta keys a consumer reads from an [Output]'s Meta, each a decimal string or a name. A stage
// may set others, which no consumer should read.
const (
	// MetaPage is a page's number, from 1: the document stage's text and image of a PDF's page,
	// and the picture stage's PNG of a TIFF's page.
	MetaPage = "page"
	// MetaCount is how many there are of what the output is one of: a PDF's pages on the document
	// stage's page outputs, a TIFF's pages on the picture stage's, and a part's windows on the
	// audio stage's windows.
	MetaCount = "count"
	// MetaWindow is a window's index, from 0, in its plan's order: the audio and video stages'
	// audio outputs.
	MetaWindow = "window"
	// MetaFrame is a frame's index, from 0, in its sampling's order: the video stage's image
	// outputs.
	MetaFrame = "frame"
	// MetaSheet is a sheet's name: the office stage's two outputs of a workbook's sheet, its
	// Markdown and its rows.
	MetaSheet = "sheet"
)

// The media types of the record outputs, each naming the record's shape and its version, so a
// reader checks what it decodes.
const (
	// MediaAudioPlan is the audio stage's plan record, one JSON value: the part's windows, its
	// silences, and its seams, under version 1 of its shape.
	MediaAudioPlan = "application/vnd.docproc.audio-plan+json; version=1"
	// MediaVideoPlan is the video stage's plan record, one JSON value: the settings its windows
	// are planned and its frames sampled under, under version 1 of its shape.
	MediaVideoPlan = "application/vnd.docproc.video-plan+json; version=1"
	// MediaOOXML is the office stage's record of docproc-ooxml's lines, as JSON Lines: a sheet's
	// rows lines, each without its markdown, under the tool's schema "2".
	MediaOOXML = "application/vnd.docproc.ooxml+jsonl; schema=2"
	// MediaMail is the email stage's record of docproc-mail's lines, as JSON Lines, a record
	// holding one line, under the tool's schema "2".
	MediaMail = "application/vnd.docproc.mail+jsonl; schema=2"
	// MediaOneNote is the onenote stage's record of docproc-onenote's lines, as JSON Lines, a
	// record holding one line, under the tool's schema "2".
	MediaOneNote = "application/vnd.docproc.onenote+jsonl; schema=2"
)
