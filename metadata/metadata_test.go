package metadata_test

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/metadata"
)

func TestOfficeJSON(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		record metadata.Office
		want   string
	}{
		{
			"every field",
			metadata.Office{
				Core:     metadata.Properties{{Key: "title", Value: "Links"}, {Key: "created", Value: "2013-12-23T23:15:00Z"}},
				Extended: metadata.Properties{{Key: "Pages", Value: 1.0}, {Key: "AppVersion", Value: "15.0000"}},
				Custom: metadata.Properties{
					{Key: "Reviewed", Value: true}, {Key: "Ratio", Value: 0.25}, {Key: "Project", Value: "docproc"},
					{Key: "Due", Value: "2026-02-01T00:00:00Z"},
				},
				ConvertedFrom: "doc",
			},
			`{"core":{"title":"Links","created":"2013-12-23T23:15:00Z"},` +
				`"extended":{"Pages":1,"AppVersion":"15.0000"},` +
				`"custom":{"Reviewed":true,"Ratio":0.25,"Project":"docproc","Due":"2026-02-01T00:00:00Z"},` +
				`"converted_from":"doc"}`,
		},
		{"an empty part", metadata.Office{Custom: metadata.Properties{}}, `{"custom":{}}`},
		{"converted alone", metadata.Office{ConvertedFrom: "odt"}, `{"converted_from":"odt"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for name, marshal := range map[string]func(any) ([]byte, error){
				"encoding/json":    json.Marshal,
				"encoding/json/v2": func(v any) ([]byte, error) { return jsonv2.Marshal(v) },
			} {
				got, err := marshal(c.record)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != c.want {
					t.Errorf("%s: Marshal =\n %s\nwant\n %s", name, got, c.want)
				}
			}
			var back, backV2 metadata.Office
			if err := json.Unmarshal([]byte(c.want), &back); err != nil {
				t.Fatal(err)
			}
			if err := jsonv2.Unmarshal([]byte(c.want), &backV2); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, c.record) || !reflect.DeepEqual(backV2, c.record) {
				t.Errorf("Unmarshal =\n %+v\n %+v\nwant\n %+v", back, backV2, c.record)
			}
		})
	}
}

func TestOfficeMediaType(t *testing.T) {
	t.Parallel()
	if got := (metadata.Office{}).MediaType(); got != dispatch.MediaOfficeMetadata {
		t.Errorf("media type %q", got)
	}
}

func TestPropertiesRefused(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`[]`, `"core"`, `{"a":}`} {
		var p metadata.Properties
		if err := json.Unmarshal([]byte(in), &p); err == nil {
			t.Errorf("%s: no error, read %+v", in, p)
		}
	}
}
