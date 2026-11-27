package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/standards-lab/docproc/dispatch"
)

// Office is an office package's metadata record, one JSON object under
// [dispatch.MediaOfficeMetadata], which the office stage yields as a record output named
// "metadata", after the document's Markdown: the package's core, extended, and custom properties,
// each in its stored order and its stored types, and the format the document was converted from.
// A package with no property part, converted from no other format, yields no record. Its fields:
//
//   - "core": the core properties, docProps/core.xml's, such as "creator", "created", and
//     "modified", each a string as stored, a date in its stored ISO 8601 text; absent when the
//     package has no core part.
//   - "extended": the extended properties, docProps/app.xml's, such as "Application" and
//     "Pages", each a number when it is all digits and a string otherwise; absent when the package
//     has no extended part.
//   - "custom": the custom properties, docProps/custom.xml's, each a string, a number, or a
//     boolean by its stored variant's type, a file time as its stored ISO 8601 text; absent when
//     the package has no custom part.
//   - "converted_from": the format the document was converted from to OOXML before it was read, a
//     legacy document's or an ODF package's extension without its dot, such as "doc" or "odt",
//     "rtf", "wpd" for a WordPerfect document, or "xlsb"; absent when the document was read as it
//     was.
//
// A part the package has with no properties in it is present as an empty object.
type Office struct {
	Core          Properties `json:"core,omitzero"`
	Extended      Properties `json:"extended,omitzero"`
	Custom        Properties `json:"custom,omitzero"`
	ConvertedFrom string     `json:"converted_from,omitzero"`
}

// MediaType returns the record's media type, [dispatch.MediaOfficeMetadata].
func (Office) MediaType() string { return dispatch.MediaOfficeMetadata }

// Properties are one property part's properties, in the order the part stores them, as a JSON
// object whose members keep that order, which a Go map would lose. Each key appears once.
type Properties []Property

// Property is one property: its key, the JSON member's name, and its value, the member's value:
// a string, a float64, or a bool, as the record types it.
type Property struct {
	Key   string
	Value any
}

// MarshalJSON writes p as a JSON object, its members in p's order.
func (p Properties) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, prop := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(prop.Key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(prop.Value)
		if err != nil {
			return nil, fmt.Errorf("metadata: property %q: %w", prop.Key, err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads a JSON object into p, one Property per member, in order.
func (p *Properties) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("metadata: properties are %v, not an object", tok)
	}
	list := Properties{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("metadata: a property key %v, not a string", tok)
		}
		prop := Property{Key: key}
		if err := dec.Decode(&prop.Value); err != nil {
			return err
		}
		list = append(list, prop)
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*p = list
	return nil
}
