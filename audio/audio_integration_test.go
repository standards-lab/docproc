//go:build integration && linux

package audio_test

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
)

// speechGaps returns the fixture's inserted gaps, in order, and its duration.
func speechGaps() (gaps []audio.Span, duration time.Duration) {
	return speechLayout(speechUtterances, 1)
}

// TestSpeech cuts the speech fixture's windows through both runners, one decode each, and checks
// that the runners agree on the plan, that every inserted gap lies within a silence the tracker
// found, that the plan's one cut lies in the last gap before 30 s, and that a second cut's plan
// is the first.
func TestSpeech(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "speech.flac"))
	if err != nil {
		t.Fatal(err)
	}
	gaps, duration := speechGaps()
	got := map[string]audio.Plan{}
	for name, runner := range speechRunners {
		t.Run(name, func(t *testing.T) {
			s := runner(t)
			req := audio.Request{Dir: t.TempDir(), Concurrency: 2}
			ws := collect(t, s, t.Context(), path, req)
			plan := *ws[len(ws)-1].Plan
			got[name] = plan
			src := plan.Source
			t.Logf("recorded: source %v, stream %d, %s, %d Hz, %d channel(s)", src.Duration, src.Stream, src.Codec, src.SampleRate, src.Channels)
			want := audio.Source{Duration: duration, Stream: 0, Codec: "flac", SampleRate: 16000, Channels: 1}
			if src != want || plan.Duration != duration || plan.Probed != 0 {
				t.Errorf("source %+v, decoded %v, probed %v; want %+v, %v decoded, none probed", src, plan.Duration, plan.Probed, want, duration)
			}
			var silences []audio.Span
			for i, s := range plan.Silences {
				t.Logf("recorded: silence %d: %v (%.3fs) at %.2f dB", i, s.Span, s.Duration().Seconds(), s.Threshold)
				silences = append(silences, s.Span)
			}
			for _, g := range gaps {
				if !within(g, silences) {
					t.Errorf("the inserted gap %v is in no silence found", g)
				}
			}
			logPlan(t, plan)
			if len(plan.Windows) != 2 {
				t.Fatalf("%d windows, want 2", len(plan.Windows))
			}
			if cut := plan.Windows[0]; cut.Cut != audio.CutSilence || !within(audio.Span{Start: cut.End, End: cut.End}, []audio.Span{gaps[3]}) {
				t.Errorf("the first window %v, cut %v; want it cut at a silence in the gap %v", cut.Span, cut.Cut, gaps[3])
			}
			again := collect(t, s, t.Context(), path, audio.Request{Dir: t.TempDir(), Concurrency: 1})
			if !reflect.DeepEqual(plan, *again[len(again)-1].Plan) {
				t.Errorf("cut twice: %+v, then %+v", plan, *again[len(again)-1].Plan)
			}
		})
	}
	if e, c := got["exec"], got["container"]; len(got) == 2 && !reflect.DeepEqual(e, c) {
		t.Errorf("the exec runner planned %+v, the container runner %+v", e, c)
	}
}

// TestSpeechFixedThreshold cuts the fixture at a fixed threshold through the exec runner: every
// inserted gap, digital silence, is below -60 dB.
func TestSpeechFixedThreshold(t *testing.T) {
	s := speechRunners["exec"](t)
	gaps, _ := speechGaps()
	req := audio.Request{Dir: t.TempDir(), Concurrency: 1, Threshold: -60, MinSilence: time.Second / 2}
	ws := collect(t, s, t.Context(), filepath.Join("testdata", "speech.flac"), req)
	var silences []audio.Span
	for _, x := range ws[len(ws)-1].Plan.Silences {
		silences = append(silences, x.Span)
		if x.Threshold != -60 {
			t.Errorf("silence %v at %v dB, want -60", x.Span, x.Threshold)
		}
	}
	t.Logf("recorded: threshold -60 dB: %v", silences)
	for _, g := range gaps {
		if !within(g, silences) {
			t.Errorf("the inserted gap %v is in no silence found", g)
		}
	}
}

// logPlan logs plan's windows and seams.
func logPlan(t *testing.T, plan audio.Plan) {
	t.Helper()
	t.Logf("recorded: plan of %v, limit %v, overlap %v: %d windows", plan.Duration, plan.Limit, plan.Overlap, len(plan.Windows))
	for i, w := range plan.Windows {
		t.Logf("recorded: window %d: %v (%.3fs), cut at %v", i, w.Span, w.Duration().Seconds(), w.Cut)
	}
	for i, s := range plan.Seams {
		t.Logf("recorded: seam %d: %v", i, s)
	}
}
