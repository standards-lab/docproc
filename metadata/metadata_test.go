package metadata_test

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"
	"time"

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

func TestPDFJSON(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 3, 15, 4, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name   string
		record metadata.PDF
		want   string
	}{
		{
			"every field",
			metadata.PDF{
				Version: "1.7",
				Info: &metadata.PDFInfo{
					Title: "T", Author: "A", Subject: "S", Keywords: "k1, k2", Creator: "C", Producer: "P",
					CreationDate: created, ModDate: created.Add(time.Hour),
					Custom: metadata.Properties{{Key: "Project", Value: "docproc"}, {Key: "CreationDate", Value: "garbage"}},
				},
				XMP: &metadata.XMP{
					Title: "XT", Creators: []string{"A", "B"}, Description: "D", Subjects: []string{"s"},
					Languages: []string{"en"}, CreateDate: created, ModifyDate: created.Add(time.Minute),
					CreatorTool: "CT", Producer: "XP", Keywords: "xk", DocumentID: "uuid:1", Packet: "<x:xmpmeta/>",
				},
			},
			`{"version":"1.7","info":{"title":"T","author":"A","subject":"S","keywords":"k1, k2","creator":"C",` +
				`"producer":"P","creation_date":"2026-03-15T04:00:00Z","mod_date":"2026-03-15T05:00:00Z",` +
				`"custom":{"Project":"docproc","CreationDate":"garbage"}},` +
				`"xmp":{"title":"XT","creators":["A","B"],"description":"D","subjects":["s"],"languages":["en"],` +
				`"create_date":"2026-03-15T04:00:00Z","modify_date":"2026-03-15T04:01:00Z","creator_tool":"CT",` +
				`"producer":"XP","keywords":"xk","document_id":"uuid:1","packet":"\u003cx:xmpmeta/\u003e"}}`,
		},
		{"info alone", metadata.PDF{Version: "1.4", Info: &metadata.PDFInfo{Title: "T"}}, `{"version":"1.4","info":{"title":"T"}}`},
		{"a packet alone", metadata.PDF{XMP: &metadata.XMP{Packet: ""}}, `{"xmp":{"packet":""}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(c.record)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("Marshal =\n %s\nwant\n %s", got, c.want)
			}
			gotV2, err := jsonv2.Marshal(c.record)
			if err != nil {
				t.Fatal(err)
			}
			var fromV2, back, backV2 metadata.PDF
			if err := json.Unmarshal(gotV2, &fromV2); err != nil || !reflect.DeepEqual(fromV2, c.record) {
				t.Errorf("encoding/json/v2: Marshal = %s, which reads back as %+v, %v", gotV2, fromV2, err)
			}
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

func TestPDFMediaType(t *testing.T) {
	t.Parallel()
	if got := (metadata.PDF{}).MediaType(); got != dispatch.MediaPDFMetadata {
		t.Errorf("media type %q", got)
	}
}

func TestMediaJSON(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		record metadata.Media
		want   string
	}{
		{
			"every field",
			metadata.Media{
				Format: "matroska,webm", Duration: 50.5, BitRate: 135819,
				Tags: metadata.Properties{{Key: "title", Value: "Zürich"}, {Key: "encoder", Value: "Lavf"}},
				Streams: []metadata.MediaStream{
					{
						Index: 0, Type: "video", Codec: "h264", Width: 1920, Height: 1080, FrameRate: 25,
						Tags: metadata.Properties{{Key: "language", Value: "und"}},
					},
					{Index: 1, Type: "video", Codec: "mjpeg", Width: 600, Height: 600, FrameRate: 90000, AttachedPicture: true},
					{Index: 2, Type: "audio", Codec: "flac", SampleRate: 16000, Channels: 1},
					{Index: 3, Type: "data"},
				},
			},
			`{"format":"matroska,webm","duration":50.5,"bit_rate":135819,"tags":{"title":"Zürich","encoder":"Lavf"},` +
				`"streams":[{"index":0,"type":"video","codec":"h264","width":1920,"height":1080,"frame_rate":25,` +
				`"tags":{"language":"und"}},` +
				`{"index":1,"type":"video","codec":"mjpeg","width":600,"height":600,"frame_rate":90000,"attached_picture":true},` +
				`{"index":2,"type":"audio","codec":"flac","sample_rate":16000,"channels":1},` +
				`{"index":3,"type":"data"}]}`,
		},
		{
			"the container alone",
			metadata.Media{Format: "mp3", Streams: []metadata.MediaStream{{Index: 0, Type: "audio"}}},
			`{"format":"mp3","streams":[{"index":0,"type":"audio"}]}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(c.record)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("Marshal =\n %s\nwant\n %s", got, c.want)
			}
			gotV2, err := jsonv2.Marshal(c.record)
			if err != nil {
				t.Fatal(err)
			}
			var fromV2, back, backV2 metadata.Media
			if err := json.Unmarshal(gotV2, &fromV2); err != nil || !reflect.DeepEqual(fromV2, c.record) {
				t.Errorf("encoding/json/v2: Marshal = %s, which reads back as %+v, %v", gotV2, fromV2, err)
			}
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

func TestMediaMediaType(t *testing.T) {
	t.Parallel()
	if got := (metadata.Media{}).MediaType(); got != dispatch.MediaMediaMetadata {
		t.Errorf("media type %q", got)
	}
}
