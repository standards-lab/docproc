//go:build integration && linux

package audio_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/standards-lab/docproc/audio"
)

// TestLevelsTracker cuts each level-change variant through both runners with the tracker at its
// defaults: the plan's silences hold every inserted gap, before the change and after it; and its
// windows, ffmpeg decoding the variant, are the incremental path's over the variant's own samples,
// read without ffmpeg, a Tracker and a Planner fed them as a stream.
func TestLevelsTracker(t *testing.T) {
	var f levelsFixture
	readTruth(t, levelsTruth, &f)
	for name, runner := range speechRunners {
		t.Run(name, func(t *testing.T) {
			s := runner(t)
			for _, v := range f.Variants {
				req := audio.Request{Dir: t.TempDir(), Concurrency: 1}
				ws := collect(t, s, t.Context(), filepath.Join("testdata", v.File), req)
				plan := *ws[len(ws)-1].Plan
				var silences []audio.Span
				for _, x := range plan.Silences {
					silences = append(silences, x.Span)
				}
				t.Logf("recorded: %s: %v", v.File, plan.Silences)
				before, after := gapsFound(t, f, silences)
				if n := len(f.Gaps) - len(f.GapsAfterChange); before != n || after != len(f.GapsAfterChange) {
					t.Errorf("%s: %d of %d gaps found before the change and %d of %d after it",
						v.File, before, n, after, len(f.GapsAfterChange))
				}
				logPlan(t, plan)
				want := streamPlan(t, samplesOf(t, v.File), req, 1<<14, plan.Duration)
				if !reflect.DeepEqual(plan.Windows, want) {
					t.Errorf("%s: windows %v, the incremental path's %v", v.File, plan.Windows, want)
				}
			}
		})
	}
}
