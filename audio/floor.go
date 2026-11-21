package audio

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// FrameLength is the tracker's frame, 10 ms, 160 samples at SampleRate: it measures each frame's
// energy and decides each frame silent or not.
const FrameLength = 10 * time.Millisecond

const frameSamples = SampleRate * int(FrameLength) / int(time.Second)

// The tracker's defaults, chosen by measurement: TestFindingsFloor, in the findings tier that stays
// in spike-ai-pipeline-processing at v0.9.0, measured a grid of settings over the level-change
// fixtures and the corpus's long sources, unchanged and with a gain step and a noise floor added at
// 10 minutes. These are the settings that find every inserted gap of both fixtures, before their
// change and after it, by the middle of a band of margins that do, and cut no more windows at the
// limit than silencedetect at its default does on the long sources unchanged, nor more under either
// change.
const (
	// DefaultPercentile is the percentile of frame energy a request's zero Percentile asks for:
	// the 10th, the quietest tenth of the frames, which in speech is its pauses.
	DefaultPercentile = 10.0
	// DefaultFloorWindow is the rolling window a request's zero FloorWindow asks for: 4 s, the
	// frames whose energy the percentile is taken over, short enough that the floor follows a
	// change within a few seconds, long enough to hold a pause.
	DefaultFloorWindow = 4 * time.Second
	// DefaultMargin is how far above the percentile, in dB, a request's zero Margin puts the
	// threshold: 12 dB.
	DefaultMargin = 12.0
)

const maxFloorWindow = 10 * time.Minute

// See docs/api.md, "Running the tools".
const (
	floorCB     = -12000
	bins        = -floorCB + 1
	fenwickSize = 1 << 14
)

// Silence is a silence the Tracker found: its span of the stream and the threshold in force when
// it opened.
type Silence struct {
	Span
	// Threshold is the threshold, in dB relative to full scale, in force at the silence's first
	// frame: the rolling percentile and the margin then, or the request's fixed Threshold.
	Threshold float64
}

type tracker struct {
	minSilence   time.Duration
	fixed        bool
	thresholdCB  int
	percentile   float64
	windowFrames int
	marginCB     int
}

func (req Request) checkTracker() (tracker, error) {
	tr := tracker{minSilence: cmp.Or(req.MinSilence, DefaultMinSilence)}
	tuned := req.Percentile != 0 || req.FloorWindow != 0 || req.Margin != 0
	switch {
	case req.MinSilence < 0:
		return tracker{}, fmt.Errorf("audio: minimum silence %v is below zero: %w", req.MinSilence, ErrRequest)
	case !finite(req.Threshold) || req.Threshold > 0 || req.Threshold < floorCB/100:
		return tracker{}, fmt.Errorf("audio: threshold %v dB isn't from %d to below zero: %w", req.Threshold, floorCB/100, ErrRequest)
	case req.Threshold != 0 && tuned:
		return tracker{}, fmt.Errorf("audio: a fixed threshold of %v dB and a rolling one's settings: %w", req.Threshold, ErrRequest)
	case req.Threshold != 0:
		tr.fixed, tr.thresholdCB = true, centibels(req.Threshold)
		return tr, nil
	case !finite(req.Percentile) || req.Percentile < 0 || req.Percentile >= 100:
		return tracker{}, fmt.Errorf("audio: percentile %v isn't from 0 to under 100: %w", req.Percentile, ErrRequest)
	case req.FloorWindow != 0 && (req.FloorWindow < warmFrames*FrameLength || req.FloorWindow > maxFloorWindow):
		return tracker{}, fmt.Errorf("audio: rolling window %v isn't 0 or from %v to %v: %w",
			req.FloorWindow, warmFrames*FrameLength, maxFloorWindow, ErrRequest)
	case !finite(req.Margin) || req.Margin < 0:
		return tracker{}, fmt.Errorf("audio: margin %v dB isn't zero or more: %w", req.Margin, ErrRequest)
	}
	tr.percentile = cmp.Or(req.Percentile, DefaultPercentile)
	tr.windowFrames = int(cmp.Or(req.FloorWindow, DefaultFloorWindow) / FrameLength)
	tr.marginCB = centibels(cmp.Or(req.Margin, DefaultMargin))
	return tr, nil
}

func centibels(db float64) int { return int(math.Round(db * 100)) }

const warmFrames = 100

// Tracker finds the silences of a stream of PCM as it arrives: 16 kHz, SampleRate, mono, 16-bit
// little-endian samples, the windows' format. It measures each 10 ms frame's energy, its RMS in
// dB relative to full scale, and decides it silent when the energy is below the threshold in force:
// the request's fixed Threshold, or the Percentile of the energy of the frames in the rolling
// FloorWindow up to and including the frame, plus the Margin. A run of silent frames at least
// MinSilence long is a Silence. Because the threshold follows the stream's quietest frames, it
// follows a change of level or of noise floor, which a threshold fixed for the stream can't.
//
// The first 100 frames, 1 s, wait for one another: each is decided against the percentile
// of all of them, so the stream's start is decided as the rest is. After them, each frame is
// decided as it arrives.
//
// A Tracker's memory is bounded by its rolling window, whatever the stream's length: it keeps each
// frame of the window's energy and a fixed histogram of them, and less than a frame of samples. It
// reads nothing but its samples, and frames them by their place in the stream, so the same
// samples give the same silences however they are chunked. It isn't safe for concurrent use.
type Tracker struct {
	settings        tracker
	energyCB        []int16
	head            int
	fenwick         []int32
	pending         []byte
	frames, decided int
	samples         int64
	run             int
	runThresholdCB  int
	ended           bool
}

// NewTracker returns a Tracker that finds silences as req says: its MinSilence, the shortest
// silence; and either its fixed Threshold or its rolling threshold's Percentile, FloorWindow, and
// Margin. It reads no other field.
//
// A MinSilence below zero; a Threshold above zero or below -120 dB; a Threshold with any of
// Percentile, FloorWindow, or Margin; a Percentile below zero or 100 or more; a FloorWindow not
// zero and under 1 s or over 10 minutes; or a Margin below zero, is an error wrapping ErrRequest.
func NewTracker(req Request) (*Tracker, error) {
	settings, err := req.checkTracker()
	if err != nil {
		return nil, err
	}
	return newTracker(settings), nil
}

func newTracker(settings tracker) *Tracker {
	t := &Tracker{settings: settings, run: -1, pending: make([]byte, 0, 2*frameSamples)}
	if !settings.fixed {
		t.energyCB = make([]int16, 0, settings.windowFrames)
		t.fenwick = make([]int32, fenwickSize+1)
	}
	return t
}

// Feed gives the tracker the next bytes of the stream, any number of them, a sample's two bytes
// split across calls or not, and returns the silences they close, in order. A silence closes at
// the first frame after it that isn't silent; one still open when the stream ends is closed by
// End. Feed after End returns nothing.
func (t *Tracker) Feed(pcm []byte) []Silence {
	if t.ended {
		return nil
	}
	var out []Silence
	if len(t.pending) > 0 {
		n := min(len(pcm), cap(t.pending)-len(t.pending))
		t.pending = append(t.pending, pcm[:n]...)
		pcm = pcm[n:]
		if len(t.pending) < cap(t.pending) {
			return nil
		}
		out = t.frame(t.pending, out)
		t.pending = t.pending[:0]
	}
	for len(pcm) >= 2*frameSamples {
		out = t.frame(pcm[:2*frameSamples], out)
		pcm = pcm[2*frameSamples:]
	}
	t.pending = append(t.pending, pcm...)
	return out
}

// End ends the stream: it measures the samples of the last frame, if they are fewer than a
// frame's, decides any frames still warming, and returns the silences that closes, in order, a run
// of silent frames open at the end closed at the stream's end if it is MinSilence long. A trailing
// byte that isn't a whole sample is dropped. After End, the tracker takes nothing more, and its
// Position is the stream's Duration.
func (t *Tracker) End() []Silence {
	if t.ended {
		return nil
	}
	var out []Silence
	if len(t.pending) >= 2 {
		out = t.frame(t.pending[:len(t.pending)/2*2], out)
	}
	t.pending = nil
	out = t.decide(out)
	t.ended = true
	if t.run >= 0 {
		out = t.close(t.Duration(), out)
	}
	return out
}

// Duration returns the length of the stream the tracker has been given, its whole samples at
// SampleRate.
func (t *Tracker) Duration() time.Duration {
	return time.Duration(t.samples) * time.Second / SampleRate
}

// Position returns how far the stream is settled: every silence that begins before it has been
// returned. It is the start of the open run of silent frames, which may yet be a silence, or the
// end of the frames decided, whichever is earlier; after End, the stream's Duration. An
// incremental Planner's Advance takes it, unless Open reports a silence, which the Planner's Open
// takes in its place.
func (t *Tracker) Position() time.Duration {
	if t.ended {
		return t.Duration()
	}
	if t.run >= 0 {
		return frameTime(t.run)
	}
	return frameTime(t.decided)
}

// Open returns the silence open now, ok true, once the open run of silent frames is MinSilence
// long: from its first frame to the end of the frames decided, with the threshold in force at its
// first frame. It is a silence however it ends, since it can only grow, and Feed or End returns it
// once it closes, its End then as late or later. A run shorter than MinSilence may yet end too
// short to be one, so it isn't reported; nor is any after End. Position stays at the run's start
// until it closes; the decided frames reach Open's End.
func (t *Tracker) Open() (Silence, bool) {
	if t.ended || t.run < 0 {
		return Silence{}, false
	}
	s := Span{frameTime(t.run), frameTime(t.decided)}
	if s.Duration() < t.settings.minSilence {
		return Silence{}, false
	}
	return Silence{Span: s, Threshold: float64(t.runThresholdCB) / 100}, true
}

// Threshold returns the threshold in force now, in dB relative to full scale: the request's fixed
// Threshold, or the rolling one, the Percentile of the energy of the frames in the FloorWindow up
// to the newest plus the Margin. Once the first second has passed, it is the threshold the newest
// whole frame was decided against, so a caller that feeds the stream a frame at a time reads each
// frame's; before then it is the threshold the warming frames would be decided against were the
// stream to end now, which End decides them against. After End, it is the threshold the last
// frame was decided against. Before any frame, the rolling threshold is -120 dB plus the Margin.
func (t *Tracker) Threshold() float64 {
	if t.settings.fixed {
		return float64(t.settings.thresholdCB) / 100
	}
	return float64(t.thresholdCB()) / 100
}

func frameTime(i int) time.Duration { return time.Duration(i) * FrameLength }

func (t *Tracker) frame(pcm []byte, out []Silence) []Silence {
	n := len(pcm) / 2
	var sum int64
	for i := range n {
		s := int64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
		sum += s * s
	}
	t.samples += int64(n)
	e := floorCB
	if sum > 0 {
		db := 10 * math.Log10(float64(sum)/(float64(n)*32768*32768))
		e = min(max(centibels(db), floorCB), 0)
	}
	t.frames++
	if t.settings.fixed {
		return t.decideFrame(e < t.settings.thresholdCB, t.settings.thresholdCB, out)
	}
	if len(t.energyCB) < t.settings.windowFrames {
		t.energyCB = append(t.energyCB, int16(e))
	} else {
		t.add(int(t.energyCB[t.head]), -1)
		t.energyCB[t.head] = int16(e)
		t.head = (t.head + 1) % len(t.energyCB)
	}
	t.add(e, 1)
	if t.frames < warmFrames {
		return out
	}
	return t.decide(out)
}

func (t *Tracker) decide(out []Silence) []Silence {
	if t.settings.fixed || t.decided == t.frames {
		return out
	}
	threshold := t.thresholdCB()
	for t.decided < t.frames {
		back := t.frames - t.decided
		e := int(t.energyCB[(t.head+len(t.energyCB)-back)%len(t.energyCB)])
		out = t.decideFrame(e < threshold, threshold, out)
	}
	return out
}

func (t *Tracker) decideFrame(silent bool, threshold int, out []Silence) []Silence {
	i := t.decided
	t.decided++
	switch {
	case silent && t.run < 0:
		t.run, t.runThresholdCB = i, threshold
	case !silent && t.run >= 0:
		out = t.close(frameTime(i), out)
	}
	return out
}

func (t *Tracker) close(end time.Duration, out []Silence) []Silence {
	start := frameTime(t.run)
	if end-start >= t.settings.minSilence {
		out = append(out, Silence{Span: Span{start, end}, Threshold: float64(t.runThresholdCB) / 100})
	}
	t.run = -1
	return out
}

func (t *Tracker) thresholdCB() int {
	n := len(t.energyCB)
	k := min(int(float64(n)*t.settings.percentile/100), n-1)
	pos, rest := 0, int32(k)
	for step := fenwickSize; step > 0; step >>= 1 {
		if next := pos + step; next <= fenwickSize && t.fenwick[next] <= rest {
			pos, rest = next, rest-t.fenwick[next]
		}
	}
	return pos + floorCB + t.settings.marginCB
}

func (t *Tracker) add(e, delta int) {
	for i := e - floorCB + 1; i <= fenwickSize; i += i & -i {
		t.fenwick[i] += int32(delta)
	}
}
