//go:build linux

package office_test

import (
	"reflect"
	"testing"

	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/office"
)

func TestDocumentMetadata(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		doc  office.Document
		want *metadata.Office
	}{
		{
			"every part, in stored order",
			office.Document{
				Properties: office.Properties{
					Core:   office.PropertyList{{Key: "title", Value: "T"}, {Key: "creator", Value: "c"}},
					App:    office.PropertyList{{Key: "Pages", Value: 3.0}},
					Custom: office.PropertyList{{Key: "Reviewed", Value: true}, {Key: "Due", Value: "2026-01-02T00:00:00Z"}},
				},
				ConvertedFrom: "doc",
			},
			&metadata.Office{
				Core:          metadata.Properties{{Key: "title", Value: "T"}, {Key: "creator", Value: "c"}},
				Extended:      metadata.Properties{{Key: "Pages", Value: 3.0}},
				Custom:        metadata.Properties{{Key: "Reviewed", Value: true}, {Key: "Due", Value: "2026-01-02T00:00:00Z"}},
				ConvertedFrom: "doc",
			},
		},
		{
			"an empty part is present",
			office.Document{Properties: office.Properties{Custom: office.PropertyList{}}},
			&metadata.Office{Custom: metadata.Properties{}},
		},
		{"converted alone", office.Document{ConvertedFrom: "odt"}, &metadata.Office{ConvertedFrom: "odt"}},
		{"nothing to record", office.Document{Format: "docx", Markdown: "# Bare\n"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.doc.Metadata(); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Metadata() = %+v, want %+v", got, c.want)
			}
		})
	}
}
