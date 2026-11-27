//go:build linux && integration

package stages_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

var speechFixture = filepath.Join("..", "audio", "testdata", "speech.flac")

var audioRunners = map[string]func(t *testing.T, spec tool.Spec) (*tool.Tool, error){
	"exec": func(t *testing.T, spec tool.Spec) (*tool.Tool, error) { return tool.NewExec(t.Context(), spec) },
	"container": func(t *testing.T, spec tool.Spec) (*tool.Tool, error) {
		return tool.NewContainer(t.Context(), "", tool.Image{Ref: audio.FFmpegImage}, spec)
	},
}

func TestAudioStageReal(t *testing.T) {
	v090 := []string{
		"e142ddb2f6d968dc33d2858f20ba456718b0335be22f3be7eafba3bd97a8cbd5.wav",
		"b5f802b375656c7fd2d06b6bc76c6da71b2b67ddab2f72d0ad12f2dd22293af3.wav",
	}
	sums := map[string][]string{}
	for name, build := range audioRunners {
		t.Run(name, func(t *testing.T) {
			ffmpeg, err := build(t, audio.FFmpegSpec())
			if err != nil {
				t.Fatal(err)
			}
			ffprobe, err := build(t, audio.FFprobeSpec())
			if err != nil {
				t.Fatal(err)
			}
			s, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
			if err != nil {
				t.Fatal(err)
			}
			stage := stages.NewAudioStage(stages.AudioConfig{
				Splitter: s, Version: stages.Versions(ffmpeg, ffprobe), Request: audio.Request{Concurrency: 2},
			})
			path, err := filepath.Abs(speechFixture)
			if err != nil {
				t.Fatal(err)
			}
			var plan *audio.Plan
			for w, err := range s.Windows(t.Context(), path, audio.Request{Dir: t.TempDir(), Concurrency: 1}) {
				if err != nil {
					t.Fatal(err)
				}
				plan = w.Plan
			}

			d := newDispatcher(t, dispatch.Stages{route.Audio: stage}, dispatch.NewMemoryIndex(0))
			events := dispatchAll(t, d, path, func(ev *dispatch.Event) {
				if o := ev.Output; o != nil && o.Kind == dispatch.AudioOutput {
					checkWAV(t, *o)
					t.Logf("recorded: %s %s, %s, Meta %v", o.Name, o.MediaType, filepath.Base(o.Path), o.Meta)
				}
			})
			wantSteps(t, events, []step{{"speech.flac", 0, dispatch.Prepared}})
			outs := events[0].Outputs
			last := len(outs) - 1
			if len(outs) != 2+len(plan.Windows) || outs[0].Name != "metadata" || outs[last].Name != "plan" {
				t.Fatalf("%d outputs, want the metadata record, the plan's %d windows, and the plan", len(outs), len(plan.Windows))
			}
			t.Logf("recorded: metadata %s", outs[0].Value)
			var md metadata.Media
			if err := json.Unmarshal(outs[0].Value, &md); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(md, plan.Source.Metadata) || md.Format != "flac" || md.Duration != 49.82 || md.BitRate <= 0 ||
				len(md.Streams) != 1 || md.Streams[0].Codec != "flac" || md.Streams[0].SampleRate != 16000 {
				t.Errorf("the metadata record %+v, want the probe's %+v, a 49.82 s FLAC", md, plan.Source.Metadata)
			}
			t.Logf("recorded: plan %s", outs[last].Value)
			var p audio.PlanRecord
			if err := json.Unmarshal(outs[last].Value, &p); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p, plan.Record()) {
				t.Fatalf("the plan record %+v isn't the Splitter's plan %+v", p, plan.Record())
			}
			for i, w := range plan.Windows {
				o := outs[i+1]
				if o.Kind != dispatch.AudioOutput || o.Name != "window "+strconv.Itoa(i) || o.Meta["end"] != strconv.FormatFloat(w.End.Seconds(), 'f', -1, 64) {
					t.Errorf("output %d is %s %s ending at %s, want window %d, %v", i, o.Kind, o.Name, o.Meta["end"], i, w.Span)
				}
				sums[name] = append(sums[name], filepath.Base(o.Path))
			}
			if !slices.Equal(sums[name], v090) {
				t.Errorf("windows %q, v0.9.0's %q", sums[name], v090)
			}
		})
	}
	if e, c := sums["exec"], sums["container"]; len(sums) == 2 && !slices.Equal(e, c) {
		t.Errorf("the exec runner prepared windows %q, the container runner %q", e, c)
	}
	if _, err := os.Stat(speechFixture); err != nil {
		t.Errorf("the dispatcher removed the input: %v", err)
	}
}

func TestAudioStageMetadataReal(t *testing.T) {
	for name, build := range audioRunners {
		t.Run(name, func(t *testing.T) {
			ffmpeg, err := build(t, audio.FFmpegSpec())
			if err != nil {
				t.Fatal(err)
			}
			ffprobe, err := build(t, audio.FFprobeSpec())
			if err != nil {
				t.Fatal(err)
			}
			s, err := audio.NewSplitter(audio.Config{FFmpeg: ffmpeg, FFprobe: ffprobe})
			if err != nil {
				t.Fatal(err)
			}
			stage := stages.NewAudioStage(stages.AudioConfig{
				Splitter: s, Version: stages.Versions(ffmpeg, ffprobe), Request: audio.Request{Concurrency: 2},
			})
			path, err := filepath.Abs(taggedMP3)
			if err != nil {
				t.Fatal(err)
			}
			got, order := dispatchMediaMetadata(t, route.Audio, stage, path)
			if want := []string{"record metadata", "audio window 0", "record plan"}; !slices.Equal(order, want) {
				t.Errorf("order %q, want %q", order, want)
			}
			if !reflect.DeepEqual(got, taggedRecord) {
				t.Errorf("record\n %+v\nwant\n %+v", got, taggedRecord)
			}
		})
	}
}
