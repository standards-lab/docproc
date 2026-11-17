package onenote

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
)

// Record is one item of a read, by its kind: *Notebook, *SectionGroup, *Section, *Page, or
// *Skipped for docproc-onenote's lines of those kinds, *Warning for a warning it wrote to stderr,
// and *UnknownRecord for a line of a kind schema "2" adds later, since the contract grows by
// addition.
//
// Each line's type mirrors its line: a field the line leaves out is the zero value, which means
// none, as the contract means it. Each marshals back, through encoding/json/v2, to its line's
// fields less "line", but for the fields the Reader adds, which it marks as its own and never
// marshals.
type Record interface {
	record()
}

// Notebook is the notebook line, the first of every read.
type Notebook struct {
	// Schema is the envelope's schema, cli's "2" for every Notebook a read yields.
	Schema string `json:"schema,omitzero"`
	// Input is the input's kind, sniffed from its content: "section", "notebook" (a .onetoc2 or
	// a directory), or "package".
	Input string `json:"input,omitzero"`
	// Name is a section's file stem; a notebook's directory name, for a .onetoc2 input too; a
	// package's file stem.
	Name string `json:"name,omitzero"`
	// ID is a notebook's or package's: its .onetoc2's FileIdentity, a lowercase hyphenated GUID.
	// A section input has none.
	ID string `json:"id,omitzero"`
	// Color is a notebook's or package's colour, "#rrggbb".
	Color string `json:"color,omitzero"`
}

// SectionGroup is a section group line, which comes before the entries the group holds.
type SectionGroup struct {
	// ID is the FileIdentity of the group's own .onetoc2.
	ID string `json:"id,omitzero"`
	// Parent is the id of the group it is in; a group at the notebook's root has none.
	Parent string `json:"parent,omitzero"`
	// Path is the names from the notebook's root down to this group, its own last.
	Path []string `json:"path,omitzero"`
}

// Section is a section line, which comes before its pages' lines.
type Section struct {
	// ID is the section's FileIdentity.
	ID string `json:"id,omitzero"`
	// Parent is the id of the section group it is in; a section at the notebook's root, or a
	// section input, has none.
	Parent string `json:"parent,omitzero"`
	// Path is the names from the notebook's root down to this section, its display name last.
	Path []string `json:"path,omitzero"`
	// Color is its colour, "#rrggbb"; none when it has none.
	Color string `json:"color,omitzero"`
}

// Page is a page line, one page at its latest revision, in its section's page series' order.
type Page struct {
	// ID is the page's GUID, lowercase and hyphenated.
	ID string `json:"id,omitzero"`
	// Section is the id of its section.
	Section string `json:"section,omitzero"`
	// Level is its depth in its section's page tree: 1 for a page, 2 for its subpage, and so on.
	Level int `json:"level,omitzero"`
	// Title is its title text; an untitled page's first line, as OneNote's page list shows it.
	Title string `json:"title,omitzero"`
	// CreationTime is UTC, to the second; none when the stored time couldn't be read, which
	// docproc-onenote warns of. ModificationTime is UTC, to the second, and always written: a page
	// with no stored modification time gives its creation time.
	CreationTime     time.Time `json:"creation_time,omitzero"`
	ModificationTime time.Time `json:"modification_time,omitzero"`
	// Author is the author, as stored.
	Author string `json:"author,omitzero"`
	// Markdown is the page as Markdown, docproc-onenote's text layer: its text, lists, tables,
	// math, note tags, and recognised ink words, its images, embedded files, and ink drawings
	// linked by their media names; "" when nothing on it renders.
	Markdown string `json:"markdown,omitzero"`
	// Media are its images, embedded files, and ink drawings, in the order docproc-onenote's
	// reader visits them, which differs from the Markdown's by design.
	Media []Medium `json:"media,omitzero"`
	// InkAlternatives are the recognised ink words that have other guesses, a label the Markdown
	// leaves out; the words themselves are in the Markdown.
	InkAlternatives []InkAlternative `json:"ink_alternatives,omitzero"`
}

// MediumKind is what a Medium is: an image, an embedded file, or an ink drawing.
type MediumKind string

// The kinds of Medium, as docproc-onenote writes them.
const (
	// ImageMedium is an image.
	ImageMedium MediumKind = "image"
	// EmbeddedFileMedium is an embedded file, a recording among them.
	EmbeddedFileMedium MediumKind = "embedded_file"
	// InkMedium is an ink drawing, an SVG: the page's own ink, a paragraph's handwriting, or an
	// ink block.
	InkMedium MediumKind = "ink"
)

// Medium is one of a page's images, embedded files, and ink drawings.
type Medium struct {
	// Kind is what it is.
	Kind MediumKind `json:"kind,omitzero"`
	// File is the media name of its content's file: the lowercase hex SHA-256 of the bytes, plus
	// "." and the stored extension when that is 1 to 8 ASCII letters and digits, "svg" for ink.
	// "" for a record whose data is missing or invalid, which docproc-onenote warns of.
	File string `json:"file,omitzero"`
	// Filename is the name an image was inserted from or a file attached under, as stored; never
	// a path to use. Ink has none.
	Filename string `json:"filename,omitzero"`
	// Size is its content's size in bytes; nil for ink, and for a record without a File.
	Size *int64 `json:"size,omitzero"`
	// Extension is an image's stored extension.
	Extension string `json:"extension,omitzero"`
	// Type is an embedded file's: "audio" or "video" for a recording.
	Type string `json:"type,omitzero"`
	// Path is the content's file in the read's media directory, which docproc-onenote writes each
	// distinct content into before the page's line; "" for a read without one, and for a medium
	// with no File. It is the Reader's, never marshalled.
	Path string `json:"-"`
	// SHA256 is the content's SHA-256, lowercase hex, as its File claims it; "" with no File. It is
	// the Reader's, never marshalled.
	SHA256 string `json:"-"`
}

// InkAlternative is one recognised ink word's other guesses.
type InkAlternative struct {
	// Word is the word's index, from 0, among the page's recognised words over the recognition's
	// lines; written even when zero.
	Word int `json:"word"`
	// Alternatives are its other guesses, best first.
	Alternatives []string `json:"alternatives,omitzero"`
}

// Skipped is a skipped line: a section inside a notebook that docproc-onenote couldn't read, after
// the notebook's entries in the order the parser reported them.
type Skipped struct {
	// Kind is "section".
	Kind string `json:"kind,omitzero"`
	// Name is its name as the notebook's table of contents gives it, a section's file name.
	Name string `json:"name,omitzero"`
	// Reason is why, in the parser's words, or an encrypted section's reason, beginning
	// "encrypted".
	Reason string `json:"reason,omitzero"`
	// Encrypted is whether it is a password-protected section.
	Encrypted bool `json:"encrypted,omitzero"`
}

// Warning is a warning docproc-onenote wrote to stderr, what it skipped or read with a loss: its
// tool's name, where in the input, a section by its path from the notebook's root followed by its
// page and the record on it, "/OneNote_RecycleBin" for the recycle bin, or "/" for the notebook,
// and what. A read with warnings still succeeds.
type Warning dispatch.Warning

// UnknownRecord is a line of a kind the package doesn't know, kept with its kind and its JSON as
// docproc-onenote wrote it, so a later addition to the contract is never an error.
type UnknownRecord struct {
	Kind string
	Raw  jsontext.Value
}

func (*Notebook) record()      {}
func (*SectionGroup) record()  {}
func (*Section) record()       {}
func (*Page) record()          {}
func (*Skipped) record()       {}
func (*Warning) record()       {}
func (*UnknownRecord) record() {}

// decodeLine decodes one of docproc-onenote's lines, of kind kind, into its Record, every time
// UTC. The raw JSON is the reader's to keep, as cli.Read yields it, so an UnknownRecord holds it as
// it is.
func decodeLine(kind string, raw jsontext.Value) (Record, error) {
	var rec Record
	switch kind {
	case "notebook":
		rec = new(Notebook)
	case "section_group":
		rec = new(SectionGroup)
	case "section":
		rec = new(Section)
	case "page":
		rec = new(Page)
	case "skipped":
		rec = new(Skipped)
	default:
		return &UnknownRecord{Kind: kind, Raw: raw}, nil
	}
	if err := json.Unmarshal(raw, rec, cli.UTC); err != nil {
		return nil, fmt.Errorf("onenote: decoding a %s line: %w", kind, err)
	}
	return rec, nil
}
