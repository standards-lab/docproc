package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/metadata"
)

func TestFFprobeMetadataGoldens(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		golden string
		want   metadata.Media
	}{
		{
			"speech-tagged.ffprobe.json",
			metadata.Media{
				Format: "mp3", Duration: 6.590938, BitRate: 32988,
				Tags: metadata.Properties{
					{Key: "title", Value: "Chapter 1, utterance 1"}, {Key: "artist", Value: "LibriSpeech 2277"},
					{Key: "album", Value: "dev-clean"}, {Key: "date", Value: "2015"}, {Key: "genre", Value: "Speech"},
					{Key: "comment", Value: "Zürich harbor"}, {Key: "librispeech_id", Value: "2277-149896-0000"},
				},
				Streams: []metadata.MediaStream{{Index: 0, Type: "audio", Codec: "mp3", SampleRate: 16000, Channels: 1}},
			},
		},
		{
			"speech-scenes.ffprobe.json",
			metadata.Media{
				Format: "matroska,webm", Duration: 50, BitRate: 135819,
				Tags: metadata.Properties{{Key: "encoder", Value: "Lavf"}},
				Streams: []metadata.MediaStream{
					{
						Index: 0, Type: "video", Codec: "mpeg4", Width: 320, Height: 180, FrameRate: 10,
						Tags: metadata.Properties{{Key: "ENCODER", Value: "Lavc mpeg4"}, {Key: "DURATION", Value: "00:00:50.000000000"}},
					},
					{
						Index: 1, Type: "audio", Codec: "flac", SampleRate: 16000, Channels: 1,
						Tags: metadata.Properties{{Key: "DURATION", Value: "00:00:49.820000000"}},
					},
				},
			},
		},
	} {
		t.Run(c.golden, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "..", "audio", "testdata", c.golden))
			if err != nil {
				t.Fatal(err)
			}
			report, err := cli.ParseFFprobe(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := report.Metadata(); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Metadata =\n %+v\nwant\n %+v", got, c.want)
			}
		})
	}
}

func TestFFprobeMetadataAbsent(t *testing.T) {
	t.Parallel()
	report, err := cli.ParseFFprobe([]byte(`{"streams":[` +
		`{"index":0,"codec_name":"mjpeg","codec_type":"video","width":600,"height":600,"r_frame_rate":"90000/1",` +
		`"avg_frame_rate":"0/0","disposition":{"attached_pic":1},"tags":{"comment":"Cover (front)"}},` +
		`{"index":1,"codec_type":"data","width":7,"sample_rate":"8000","channels":2,"r_frame_rate":"0/0","avg_frame_rate":"0/0"},` +
		`{"index":2,"codec_name":"h264","codec_type":"video","width":1920,"height":1080,"r_frame_rate":"60000/1001",` +
		`"avg_frame_rate":"30000/1001"},` +
		`{"index":3,"codec_name":"aac","codec_type":"audio","sample_rate":"N/A","channels":2,"width":5,"r_frame_rate":"0/0"}],` +
		`"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"N/A","bit_rate":"N/A"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := metadata.Media{
		Format: "mov,mp4,m4a,3gp,3g2,mj2",
		Streams: []metadata.MediaStream{
			{
				Index: 0, Type: "video", Codec: "mjpeg", Width: 600, Height: 600, FrameRate: 90000, AttachedPicture: true,
				Tags: metadata.Properties{{Key: "comment", Value: "Cover (front)"}},
			},
			{Index: 1, Type: "data"},
			{Index: 2, Type: "video", Codec: "h264", Width: 1920, Height: 1080, FrameRate: 30000.0 / 1001},
			{Index: 3, Type: "audio", Codec: "aac", Channels: 2},
		},
	}
	if got := report.Metadata(); !reflect.DeepEqual(got, want) {
		t.Errorf("Metadata =\n %+v\nwant\n %+v", got, want)
	}
}

func TestParseFFprobeRefuses(t *testing.T) {
	t.Parallel()
	for _, report := range []string{"", "[]", `{"format":{"tags":["not","an","object"]}}`} {
		if _, err := cli.ParseFFprobe([]byte(report)); err == nil {
			t.Errorf("ParseFFprobe(%q) returned no error", report)
		}
	}
}
