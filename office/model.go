package office

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/standards-lab/docproc/dispatch"
)

// Record is one item of a conversion, by its kind: *Document, *Sheet, or *Rows for docproc-ooxml's
// lines of those kinds, *Warning for a warning it wrote to stderr, and *UnknownRecord for a line
// of a kind schema "2" adds later, since the contract grows by addition.
type Record interface {
	record()
}

// Document is docproc-ooxml's `document` line, the first of every conversion: the format, the
// package's properties, the Markdown rendering (an xlsx's holding no sheet's), and every medium the
// document names. A field the line leaves out is the zero value, which means none.
type Document struct {
	// Schema is the envelope's schema, cli's "2" for every Document a conversion yields.
	Schema string `json:"schema"`
	// Format is the OOXML format read: "docx", "xlsx", or "pptx". For a legacy document or an RTF
	// it is the format LibreOffice converted it to.
	Format string `json:"format"`
	// Properties are the package's core, extended, and custom properties, each in its stored
	// order.
	Properties Properties `json:"properties,omitzero"`
	// Markdown is the document's Markdown rendering; an xlsx's sheets are their own lines'. Each
	// converted picture's link names its PNG.
	Markdown string `json:"markdown,omitempty"`
	// Media are the pictures, embedded packages, and OLE objects the document names, in document
	// order; one file may be named more than once.
	Media []Medium `json:"media,omitempty"`
	// ConvertedFrom is the format a document was converted from to OOXML before docproc-ooxml ran:
	// a legacy document's extension without its dot, such as "doc"; an ODF package's extension
	// for its mimetype's type, such as "odt" or "otg"; "rtf", "wpd" for a WordPerfect document, or
	// "xlsb"; "" when the document went to docproc-ooxml as it was. It is the Converter's, never
	// the line's.
	ConvertedFrom string `json:"-"`
}

// Properties are a package's three property parts, each a list of its properties in stored
// order; nil for a part the package doesn't have.
type Properties struct {
	// Core are the core properties, such as creator and modified, each a string as stored.
	Core PropertyList `json:"core,omitempty"`
	// App are the extended properties, such as Application and Pages, each a float64 when it is
	// all digits and a string otherwise.
	App PropertyList `json:"app,omitempty"`
	// Custom are the custom properties, each a string, a float64, or a bool by its variant's
	// type, a file time as its stored ISO 8601 text.
	Custom PropertyList `json:"custom,omitempty"`
}

// PropertyList is a property part's properties, in the order the part stores them, which a JSON
// object read into a map would lose. docproc-ooxml keeps a repeated key's first value alone, with
// a warning, so each Key appears once.
type PropertyList []Property

// Property is one property: its key and its value, a string, a float64, or a bool, as the line
// types it.
type Property struct {
	Key   string
	Value any
}

// UnmarshalJSONFrom reads a JSON object into l, one Property per member, in order.
func (l *PropertyList) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '{' {
		return fmt.Errorf("properties are %s, not an object", tok.Kind())
	}
	list := PropertyList{}
	for dec.PeekKind() != '}' {
		key, err := dec.ReadToken()
		if err != nil {
			return err
		}
		p := Property{Key: key.String()}
		if err := json.UnmarshalDecode(dec, &p.Value); err != nil {
			return err
		}
		list = append(list, p)
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	*l = list
	return nil
}

// MarshalJSONTo writes l as a JSON object, its members in l's order.
func (l PropertyList) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, p := range l {
		if err := enc.WriteToken(jsontext.String(p.Key)); err != nil {
			return err
		}
		if err := json.MarshalEncode(enc, p.Value); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// MediumKind is what a medium is.
type MediumKind string

// The kinds of media.
const (
	// Picture is a picture, embedded as a file or linked by its target.
	Picture MediumKind = "picture"
	// Embedded is an embedded package, as stored.
	Embedded MediumKind = "embedded"
	// OLE is an OLE object's part as stored, its compound file not unwrapped.
	OLE MediumKind = "ole"
)

// Medium is one of a document's media: a picture, an embedded package, or an OLE object, with
// where the document places it. A file docproc-ooxml wrote is named by File, the envelope's media
// name, and resolved to Path; a linked picture has its Target instead, never fetched, and no file.
type Medium struct {
	Kind MediumKind `json:"kind"`
	// File is the medium's name in the media directory: the lowercase hex SHA-256 of its bytes and
	// its stored extension.
	File string `json:"file,omitempty"`
	// ContentType is the content type the package stores the medium under.
	ContentType string `json:"content_type,omitempty"`
	// Part is the package part the medium is stored in.
	Part string `json:"part,omitempty"`
	// Target is a linked picture's URL.
	Target string `json:"target,omitempty"`
	// ProgID is an embedded package's or OLE object's ProgID, where it has one.
	ProgID string `json:"prog_id,omitempty"`
	// Section is the index from 0 of the docx section that shows the medium, Sheet the ordinal
	// from 1 of the xlsx sheet's Sheet record, and Slide the index from 0 of the pptx slide; nil
	// where the document places it in none.
	Section *int `json:"section,omitzero"`
	Sheet   *int `json:"sheet,omitzero"`
	Slide   *int `json:"slide,omitzero"`
	// Path is the file handed onward, an absolute path in the conversion's media directory: the
	// converted picture's PNG when Converted, else the file as stored; "" for a linked picture.
	Path string `json:"-"`
	// SHA256 is the stored file's SHA-256, lowercase hex, as its File claims it; "" for a linked
	// picture.
	SHA256 string `json:"-"`
	// Converted is whether Path is a PNG converted from the stored picture, which stays beside it.
	Converted bool `json:"-"`
}

// Sheet is docproc-ooxml's `sheet` line: an xlsx sheet's header, in workbook order, before its
// Rows records.
type Sheet struct {
	// Name is the sheet's name.
	Name string `json:"name"`
	// Hidden is whether the sheet is hidden.
	Hidden bool `json:"hidden,omitzero"`
	// Markdown is the sheet's heading and its table's header and delimiter rows.
	Markdown string `json:"markdown,omitempty"`
}

// Rows is docproc-ooxml's `rows` line: the current sheet's next rows, in order, with their
// Markdown table rows, within 256 KiB or one row. A sheet's closing Rows record may carry its
// charts' and pictures' Markdown alone, with no Rows.
type Rows struct {
	// Markdown is the rows' Markdown table rows, never the header row, or the sheet's drawings'
	// Markdown. Each converted picture's link names its PNG.
	Markdown string `json:"markdown,omitempty"`
	// Rows are the rows, the first first.
	Rows []Row `json:"rows,omitempty"`
}

// Row is a sheet row: its number from 1 and its cells. A position a merged cell covers has no
// cell of its own.
type Row struct {
	Row   int    `json:"row"`
	Cells []Cell `json:"cells,omitempty"`
}

// Cell is a sheet cell and its typed value.
type Cell struct {
	// Ref is the cell's reference, such as B2.
	Ref string `json:"ref"`
	// ValueType is the type of the cell's value, "" when it has none.
	ValueType ValueType `json:"value_type,omitempty"`
	// Value is the cell's value, as its ValueType says: a float64 for ValueNumber, a bool for
	// ValueBoolean, and a string for the rest (a date in ISO 8601). nil when the cell has none,
	// as a formula the file caches no result for has none.
	Value any `json:"value,omitzero"`
	// Formula is the cell's formula as stored, without the leading =.
	Formula string `json:"formula,omitempty"`
	// SharedFrom is, for a cell of a shared formula other than its anchor, the anchor's reference.
	SharedFrom string `json:"shared_from,omitempty"`
	// ColSpan and RowSpan are the columns and rows a merged cell spans; zero for one.
	ColSpan int `json:"col_span,omitzero"`
	RowSpan int `json:"row_span,omitzero"`
}

// ValueType is the type of a sheet cell's value.
type ValueType string

// A sheet cell's value types.
const (
	// ValueNumber is a number, as stored, in no number format.
	ValueNumber ValueType = "number"
	// ValueText is text, rich text flattened.
	ValueText ValueType = "text"
	// ValueBoolean is TRUE or FALSE.
	ValueBoolean ValueType = "boolean"
	// ValueError is an error, by its code, such as #DIV/0!.
	ValueError ValueType = "error"
	// ValueDate is a number in a date or time format, in ISO 8601.
	ValueDate ValueType = "date"
)

// Warning is a warning docproc-ooxml wrote to stderr, what it skipped and read the rest without:
// its tool's name, where in the document, such as "sheet 2", and what. A conversion with warnings
// still succeeds.
type Warning dispatch.Warning

// UnknownRecord is a line of a kind the package doesn't know, kept with its kind and its JSON as
// docproc-ooxml wrote it, so a later addition to the contract is never an error.
type UnknownRecord struct {
	Kind string
	Raw  jsontext.Value
}

func (*Document) record()      {}
func (*Sheet) record()         {}
func (*Rows) record()          {}
func (*Warning) record()       {}
func (*UnknownRecord) record() {}

func decodeLine(kind string, raw jsontext.Value) (Record, error) {
	var rec Record
	switch kind {
	case "document":
		rec = new(Document)
	case "sheet":
		rec = new(Sheet)
	case "rows":
		rec = new(Rows)
	default:
		return &UnknownRecord{Kind: kind, Raw: raw}, nil
	}
	if err := json.Unmarshal(raw, rec); err != nil {
		return nil, fmt.Errorf("office: decoding a %s line: %w", kind, err)
	}
	return rec, nil
}
