package media

import (
	"cmp"
	"fmt"
	"time"

	"github.com/standards-lab/docproc/audio"
)

// VideoPlan is the JSON of a video's plan record, the settings its windows are planned and its
// frames sampled under, each default applied, every time in seconds, such as the video stage's
// "plan" output. Events plans the windows as the audio arrives, so the plan record holds no
// windows: each window carries its own span and seams.
type VideoPlan struct {
	Windows VideoWindowing `json:"windows"`
	Frames  VideoSampling  `json:"frames"`
}

// VideoWindowing is how a video's audio is cut into windows, as a Request's windowing fields and
// audio's stream cutter say: each at most Limit long, overlap included, cut in the latest silence
// of at least MinSilence before its limit, a silence being audio below the threshold.
type VideoWindowing struct {
	// Detector is the silence detector, always "tracker", audio.Tracker, the one detector.
	Detector string `json:"detector"`
	// Limit is a window's longest, and Overlap each seam's length.
	Limit   float64 `json:"limit"`
	Overlap float64 `json:"overlap"`
	// MinSilence is the shortest silence a window may end in.
	MinSilence float64 `json:"min_silence"`
	// Threshold is a fixed silence threshold, in dB, or zero for the tracker's rolling one, which
	// Percentile, FloorWindow, and Margin set, each zero with a fixed threshold.
	Threshold   float64 `json:"threshold"`
	Percentile  float64 `json:"percentile"`
	FloorWindow float64 `json:"floor_window"`
	Margin      float64 `json:"margin"`
}

// VideoSampling is how a video's frames are taken, as a Request says: the first, each whose scene
// score is past SceneThreshold, and each MaxGap or more after the last taken, each scaled to fit
// MaxSide, or at the source's size when it is zero.
type VideoSampling struct {
	SceneThreshold float64 `json:"scene_threshold"`
	MaxGap         float64 `json:"max_gap"`
	MaxSide        int     `json:"max_side"`
}

// Plan returns the plan record of req's settings, each default applied. It validates the windowing
// fields, as audio's Cutter does, a refused one an error wrapping ErrRequest; it reads neither Dir
// nor the frame fields' ranges, which Events checks.
func (req Request) Plan() (VideoPlan, error) {
	areq := req.audio()
	planner, err := audio.NewPlanner(areq)
	if err != nil {
		return VideoPlan{}, fmt.Errorf("media: %w", err)
	}
	if _, err := audio.NewTracker(areq); err != nil {
		return VideoPlan{}, fmt.Errorf("media: %w", err)
	}
	// A fixed threshold takes the place of the rolling one, whose fields are then zero.
	percentile, floorWindow, margin := 0.0, time.Duration(0), 0.0
	if req.Threshold == 0 {
		percentile = cmp.Or(req.Percentile, audio.DefaultPercentile)
		floorWindow = cmp.Or(req.FloorWindow, audio.DefaultFloorWindow)
		margin = cmp.Or(req.Margin, audio.DefaultMargin)
	}
	return VideoPlan{
		Windows: VideoWindowing{
			Detector: "tracker", Limit: planner.Limit().Seconds(), Overlap: planner.Overlap().Seconds(),
			MinSilence: cmp.Or(req.MinSilence, audio.DefaultMinSilence).Seconds(), Threshold: req.Threshold,
			Percentile: percentile, FloorWindow: floorWindow.Seconds(), Margin: margin,
		},
		Frames: VideoSampling{
			SceneThreshold: cmp.Or(req.SceneThreshold, DefaultSceneThreshold),
			MaxGap:         cmp.Or(req.MaxGap, DefaultMaxGap).Seconds(),
			MaxSide:        req.MaxSide,
		},
	}, nil
}
