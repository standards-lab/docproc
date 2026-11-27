//go:build linux

package stages_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/route"
)

var (
	taggedMP3    = filepath.Join("..", "audio", "testdata", "speech-tagged.mp3")
	scenesMKV    = filepath.Join("..", "audio", "testdata", "speech-scenes.mkv")
	taggedRecord = metadata.Media{
		Format: "mp3", Duration: 6.590938, BitRate: 32988,
		Tags: metadata.Properties{
			{Key: "title", Value: "Chapter 1, utterance 1"}, {Key: "artist", Value: "LibriSpeech 2277"},
			{Key: "album", Value: "dev-clean"}, {Key: "date", Value: "2015"}, {Key: "genre", Value: "Speech"},
			{Key: "comment", Value: "Zürich harbor"}, {Key: "librispeech_id", Value: "2277-149896-0000"},
		},
		Streams: []metadata.MediaStream{{Index: 0, Type: "audio", Codec: "mp3", SampleRate: 16000, Channels: 1}},
	}
	scenesRecord = metadata.Media{
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
	}
)

func ffprobeGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "audio", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func dispatchMediaMetadata(t *testing.T, r route.Route, stage dispatch.Stage, fixture string) (metadata.Media, []string) {
	t.Helper()
	content := dispatch.NewMemoryContent()
	d, err := dispatch.New(dispatch.Config{
		Content: content, Puts: 2, Index: dispatch.NewMemoryIndex(0), Dir: t.TempDir(), Stages: dispatch.Stages{r: stage},
	})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	events := dispatchAll(t, d, fixture, func(ev *dispatch.Event) {
		if ev.Output != nil {
			order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
		}
	})
	wantSteps(t, events, []step{{filepath.Base(fixture), 0, dispatch.Prepared}})
	ev := events[0]
	if ev.Part.ContentType.Route != r || len(ev.Warnings) != 0 || len(ev.Outputs) == 0 {
		t.Fatalf("route %s, warnings %+v, %d outputs", ev.Part.ContentType.Route, ev.Warnings, len(ev.Outputs))
	}
	o := ev.Outputs[0]
	if o.Kind != dispatch.RecordOutput || o.Name != "metadata" || len(o.Meta) != 0 {
		t.Fatalf("the first output %s %s, Meta %v; want the metadata record", o.Kind, o.Name, o.Meta)
	}
	return storedRecord[metadata.Media](t, content, o, dispatch.MediaMediaMetadata), order
}

func TestAudioStageMediaMetadata(t *testing.T) {
	t.Parallel()
	stage, _ := testAudioStage(t, ffprobeGolden(t, "speech-tagged.ffprobe.json"))
	got, order := dispatchMediaMetadata(t, route.Audio, stage, taggedMP3)
	want := []string{"record metadata", "audio window 0", "audio window 1", "audio window 2", "record plan"}
	if !slices.Equal(order, want) {
		t.Errorf("order %q, want %q", order, want)
	}
	if !reflect.DeepEqual(got, taggedRecord) {
		t.Errorf("record\n %+v\nwant\n %+v", got, taggedRecord)
	}
}

func TestVideoStageMediaMetadata(t *testing.T) {
	t.Parallel()
	stage, _ := testVideoStage(t, ffprobeGolden(t, "speech-scenes.ffprobe.json"), media.Request{})
	got, order := dispatchMediaMetadata(t, route.Video, stage, scenesMKV)
	if len(order) < 2 || order[0] != "record metadata" || order[1] != "record plan" {
		t.Errorf("order %q, want the metadata record, then the plan", order)
	}
	if !reflect.DeepEqual(got, scenesRecord) {
		t.Errorf("record\n %+v\nwant\n %+v", got, scenesRecord)
	}
}
