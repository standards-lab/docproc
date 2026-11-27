package metadata

import (
	"time"

	"github.com/standards-lab/docproc/dispatch"
)

// PDF is a PDF's metadata record, one JSON object under [dispatch.MediaPDFMetadata], which the
// document stage yields as a record output named "metadata", before the forms record and the
// pages: what poppler's pdfinfo reads of the PDF's information dictionary and its XMP metadata
// stream, and its PDF version. A PDF with neither an information dictionary holding a string nor
// an XMP packet yields no record. Its fields:
//
//   - "version": the PDF version its header or catalog states, such as "1.7"; absent when pdfinfo
//     prints none.
//   - "info": the information dictionary, a [PDFInfo]; absent when the PDF has none, or none of
//     its values is a string.
//   - "xmp": the XMP metadata stream, an [XMP]; absent when the PDF's catalog has none.
type PDF struct {
	Version string   `json:"version,omitzero"`
	Info    *PDFInfo `json:"info,omitzero"`
	XMP     *XMP     `json:"xmp,omitzero"`
}

// MediaType returns the record's media type, [dispatch.MediaPDFMetadata].
func (PDF) MediaType() string { return dispatch.MediaPDFMetadata }

// PDFInfo is a PDF's information dictionary as pdfinfo -isodates -custom prints it: each string
// value decoded from PDFDocEncoding or UTF-16 to UTF-8, a character pdfinfo can't print as "?".
// pdfinfo prints no value that isn't a string, Trapped among them, which is a name, so none is
// here. Each standard key is typed; every other key is kept in "custom". Its fields:
//
//   - "title", "author", "subject", "keywords", "creator", and "producer": the standard keys
//     Title, Author, Subject, Keywords, Creator, and Producer, each its text as stored; each
//     absent when the dictionary lacks it or its text is empty.
//   - "creation_date" and "mod_date": CreationDate and ModDate, each in UTC, a date with no zone
//     read as UTC; each absent when the dictionary lacks it or its value isn't a date.
//   - "custom": every other string key, and a CreationDate or ModDate whose value isn't a date,
//     each a string as stored, in pdfinfo's order, by key; absent when there is none.
type PDFInfo struct {
	Title        string     `json:"title,omitzero"`
	Author       string     `json:"author,omitzero"`
	Subject      string     `json:"subject,omitzero"`
	Keywords     string     `json:"keywords,omitzero"`
	Creator      string     `json:"creator,omitzero"`
	Producer     string     `json:"producer,omitzero"`
	CreationDate time.Time  `json:"creation_date,omitzero"`
	ModDate      time.Time  `json:"mod_date,omitzero"`
	Custom       Properties `json:"custom,omitzero"`
}

// XMP is a PDF's XMP metadata stream: its common Dublin Core and XMP properties typed, from every
// rdf:Description of the packet, as element or attribute, and the packet itself. A packet that
// isn't XML holds its packet alone. Its fields:
//
//   - "title" and "description": dc:title and dc:description, each its x-default alternative, or
//     its first when none is x-default.
//   - "creators", "subjects", and "languages": dc:creator, dc:subject, and dc:language, each list
//     in its stored order.
//   - "create_date" and "modify_date": xmp:CreateDate and xmp:ModifyDate, each in UTC, a date
//     with no zone read as UTC, one of a year or a month as its first instant.
//   - "creator_tool": xmp:CreatorTool.
//   - "producer" and "keywords": pdf:Producer and pdf:Keywords.
//   - "document_id": xmpMM:DocumentID.
//   - "packet": the metadata stream as pdfinfo -meta prints it, any bytes that aren't UTF-8
//     replaced by U+FFFD; always present.
//
// Each field but "packet" is absent when the packet lacks its property, the property is empty, or,
// for a date, its value isn't a date.
type XMP struct {
	Title       string    `json:"title,omitzero"`
	Creators    []string  `json:"creators,omitzero"`
	Description string    `json:"description,omitzero"`
	Subjects    []string  `json:"subjects,omitzero"`
	Languages   []string  `json:"languages,omitzero"`
	CreateDate  time.Time `json:"create_date,omitzero"`
	ModifyDate  time.Time `json:"modify_date,omitzero"`
	CreatorTool string    `json:"creator_tool,omitzero"`
	Producer    string    `json:"producer,omitzero"`
	Keywords    string    `json:"keywords,omitzero"`
	DocumentID  string    `json:"document_id,omitzero"`
	Packet      string    `json:"packet"`
}
