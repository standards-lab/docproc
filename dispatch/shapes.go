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
//   - the Meta keys, every key a stage of package stages sets, [MetaPage] and the rest, and the
//     record outputs' media types, [MediaAudioPlan] and the rest.
//
// The content store's layout, [ContentFile], is stated stable apart from it.
//
// One change was made under "1" that wasn't an addition: the text stage's [MetaEncoding], once a
// combined value such as "windows-1252 (fallback)" or "utf-16le (byte order mark)", became the
// charset alone, with how it was chosen moved to [MetaEncodingBy].
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

// The Meta keys of an [Output]'s Meta: every key a stage of package stages sets is declared here,
// each value a decimal string, a time in seconds as strconv's shortest decimal of them, a name, or a
// message's Message-IDs as stored.
const (
	// MetaPage is a page's number, from 1: the document stage's text and image of a PDF's page,
	// and the picture stage's PNG of a TIFF's page.
	MetaPage = "page"
	// MetaCount is how many there are of what the output is one of: a PDF's pages on the document
	// stage's page outputs, and a TIFF's pages on the picture stage's. No window carries it: a
	// part's windows are cut as it decodes, and its plan record, last, counts them.
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
	// MetaDPI is the DPI a page was rendered at: the document stage's image of a PDF's page, at
	// its request's DPI, or lower for a page past its request's MaxPixels.
	MetaDPI = "dpi"
	// MetaFields is how many fields a PDF's form has: the document stage's forms record.
	MetaFields = "fields"

	// MetaStart is the start of a window's span of the source, in seconds: the audio and video
	// stages' audio outputs.
	MetaStart = "start"
	// MetaEnd is the end of a window's span of the source, in seconds: the audio and video
	// stages' audio outputs.
	MetaEnd = "end"
	// MetaCut is what ended a window, "silence", "limit", or "end": the audio and video stages'
	// audio outputs.
	MetaCut = "cut"
	// MetaSamples is how many 16 kHz samples a window holds: the audio and video stages' audio
	// outputs.
	MetaSamples = "samples"
	// MetaThreshold is the silence threshold a window was cut under, in dB: the audio and video
	// stages' audio outputs.
	MetaThreshold = "threshold"
	// MetaSeamBefore is the seam a window shares with the window before it, its span of the
	// source as "<start>-<end>" in seconds: the audio and video stages' audio outputs, every
	// window's but the first.
	MetaSeamBefore = "seam_before"
	// MetaSeamAfter is the seam a window shares with the window after it, its span of the source
	// as "<start>-<end>" in seconds: the audio and video stages' audio outputs, every window's but
	// the last.
	MetaSeamAfter = "seam_after"

	// MetaTime is a frame's source time, in seconds: the video stage's image outputs.
	MetaTime = "time"
	// MetaReason is why a frame was taken, "first", "scene", or "interval": the video stage's
	// image outputs.
	MetaReason = "reason"
	// MetaWidth is a frame's width in pixels: the video stage's image outputs.
	MetaWidth = "width"
	// MetaHeight is a frame's height in pixels: the video stage's image outputs.
	MetaHeight = "height"

	// MetaKind is the kind of picture a part is, a route.Kind: the vision stage's image output,
	// the part passed through.
	MetaKind = "kind"
	// MetaFrom is the kind of picture an image was converted from, a route.Kind: the picture
	// stage's image outputs.
	MetaFrom = "from"
	// MetaEncoding is the charset a text was decoded from, in lowercase: "utf-8", "utf-16le",
	// "utf-16be", "utf-32le", "utf-32be", or "windows-1252": the text stage's text output.
	MetaEncoding = "encoding"
	// MetaEncodingBy is how the text stage chose a text's [MetaEncoding] when the text alone
	// didn't settle it: "bom" for a byte order mark, "fallback" for windows-1252 taken because
	// the text isn't UTF-8. A text that is valid UTF-8 with no mark carries none.
	MetaEncodingBy = "encoding_by"
	// MetaConvertedFrom is the format an office document was converted from to OOXML, such as
	// "doc", "odt", or "rtf": the office stage's document Markdown.
	MetaConvertedFrom = "converted_from"

	// MetaID is a record's id in its store, as its tool's line gives it: the email stage's
	// folders, messages, and conversations, and the onenote stage's notebook, section groups,
	// sections, and pages.
	MetaID = "id"
	// MetaPath is a folder's or a section's path in its store, its names joined by "/": the email
	// stage's folders and the onenote stage's section groups and sections.
	MetaPath = "path"
	// MetaFormat is a mail store's format, as docproc-mail names it: the email stage's store
	// record.
	MetaFormat = "format"
	// MetaFolder is the path of the folder a message is in, its names joined by "/": the email
	// stage's messages and conversations.
	MetaFolder = "folder"
	// MetaSubject is a message's subject: the email stage's messages.
	MetaSubject = "subject"
	// MetaMessageID is a message's Message-ID, when it has one: the email stage's messages.
	MetaMessageID = "message_id"
	// MetaInReplyTo is a message's In-Reply-To, the Message-ID of the message it replies to, as
	// stored, when it has one: the email stage's messages.
	MetaInReplyTo = "in_reply_to"
	// MetaReferences is a message's References, the Message-IDs of its thread separated by spaces,
	// as stored, when it has them: the email stage's messages.
	MetaReferences = "references"
	// MetaTopic is a conversation's topic: the email stage's conversations.
	MetaTopic = "topic"
	// MetaMissed is whether a conversation was missed, "true" or "false": the email stage's
	// conversations.
	MetaMissed = "missed"
	// MetaEmbeddedIn is the name of the message an embedded message or conversation is embedded
	// in: the email stage's embedded messages and conversations.
	MetaEmbeddedIn = "embedded_in"
	// MetaName is a notebook's name: the onenote stage's notebook record.
	MetaName = "name"
	// MetaSection is the id of the section a page is in: the onenote stage's pages.
	MetaSection = "section"
	// MetaTitle is a page's title: the onenote stage's pages.
	MetaTitle = "title"
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
	// MediaForm is the document stage's forms record, one JSON value: a PDF's AcroForm, its
	// fields and their values, under version 1 of its shape.
	MediaForm = "application/vnd.docproc.form+json; version=1"
	// MediaMailConversation is the email stage's record of a conversation, one JSON value: a
	// Skype for Business conversation or missed conversation as email's Message.Conversation views
	// a docproc-mail message line, under version 1 of its shape.
	MediaMailConversation = "application/vnd.docproc.mail-conversation+json; version=1"
	// MediaOfficeMetadata is the office stage's metadata record, one JSON value: an office
	// package's core, extended, and custom properties and the format it was converted from, as
	// package metadata's Office declares it, under version 1 of its shape.
	MediaOfficeMetadata = "application/vnd.docproc.office-metadata+json; version=1"
	// MediaPDFMetadata is the document stage's metadata record, one JSON value: a PDF's
	// information dictionary, its XMP metadata, and its PDF version, as package metadata's PDF
	// declares it, under version 1 of its shape.
	MediaPDFMetadata = "application/vnd.docproc.pdf-metadata+json; version=1"
)
