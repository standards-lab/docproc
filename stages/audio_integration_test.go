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
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

// speechFixture is the audio package's speech fixture: 49.82 s of LibriSpeech speech, its plan
// two windows cut in the inserted gap at 24.41 s.
var speechFixture = filepath.Join("..", "audio", "testdata", "speech.flac")

// audioRunners build ffmpeg and ffprobe through one runner each: the host's through the exec
// runner, and audio.FFmpegImage's through the container runner, which pulls the image first when
// it isn't present.
var audioRunners = map[string]func(t *testing.T, spec tool.Spec) (*tool.Tool, error){
	"exec": func(t *testing.T, spec tool.Spec) (*tool.Tool, error) { return tool.NewExec(t.Context(), spec) },
	"container": func(t *testing.T, spec tool.Spec) (*tool.Tool, error) {
		return tool.NewContainer(t.Context(), "", tool.Image{Ref: audio.FFmpegImage}, spec)
	},
}

// TestAudioStageReal dispatches the speech fixture through the audio stage over each runner's
// ffmpeg and ffprobe, and checks that each window comes in plan order, a WAV of its Meta's
// samples, named by its SHA-256 and holding v0.9.0's bytes, and that the plan record comes last,
// the Splitter's own plan of it; and that the runners prepare the same windows.
func TestAudioStageReal(t *testing.T) {
	// v0.9.0's windows of the fixture under DetectorTracker, audio/testdata/windows-v0.9.0.txt's.
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
			if len(outs) != 1+len(plan.Windows) || outs[last].Name != "plan" {
				t.Fatalf("%d outputs, want the plan's %d windows and the plan", len(outs), len(plan.Windows))
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
				o := outs[i]
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
