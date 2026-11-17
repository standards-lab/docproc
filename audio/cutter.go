package audio

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Piece is one window a Cutter cut from its stream: the window, its seams, the threshold it was
// cut under, and its samples.
type Piece struct {
	// Index is the window's index in the stream's plan, from 0.
	Index int
	// Window is the window: its span of the stream, overlaps included, and what ended it.
	Window Window
	// SeamBefore is the seam the window starts in, shared with the window before it; zero for the
	// first. SeamAfter is the seam it ends in, shared with the window after it; zero for the last,
	// cut at the end.
	SeamBefore Span
	SeamAfter  Span
	// Threshold is the silence threshold, in dB relative to full scale, the window was cut under:
	// the request's fixed Threshold; or, with the tracker's rolling threshold, for a window cut at
	// a silence, the one in force when that silence opened, and for one cut at the limit or the
	// end, the one its last sample's frame was decided against, under which the tracker found no
	// silence to end it in.
	Threshold float64
	// PCM is the window's samples, the stream's from Sample of its start up to but not including
	// Sample of its end, 16-bit little-endian. It is the Cutter's own, valid until its next Feed or
	// End; WAV makes it a window's file.
	PCM []byte
}

// Cutter is the stream cutter: it cuts a stream of PCM into windows as the PCM arrives, 16 kHz,
// SampleRate, mono, 16-bit little-endian samples, as ffmpeg decodes a source with one run. It feeds
// a Tracker, gives the silences it finds, the one still open once it is a silence, and the stream's
// position to a Planner, and returns each window the Planner fixes with its samples, so the stream
// is decoded once and never held whole. Its windows are PlanWindows' of the stream's decoded length
// and the tracker's silences, and each window's samples are the stream's own, so a window is the
// same bytes however the stream is chunked.
//
// It keeps the PCM from the start of the window not yet fixed on, at most a window's limit and the
// chunk being fed, and every silence and window found, for its Plan. It isn't safe for concurrent
// use.
type Cutter struct {
	tracker *Tracker
	planner *Planner
	// threshold is the request's fixed threshold, zero for a rolling one.
	threshold float64

	// pcm holds the stream's bytes from base, a sample index, on; received is the bytes received.
	pcm      []byte
	base     int64
	received int64
	// thresholds holds the threshold in force after each whole frame from the frame base is in on,
	// the one the tracker decided it against: a window cut at the limit names its last frame's.
	thresholds []float64
	// silences are every silence the tracker closed, in order; open is its open silence, when
	// isOpen, as the planner was last given it.
	silences []Silence
	open     Silence
	isOpen   bool
	windows  []Window
	ended    bool
	failed   error
}

// NewCutter returns a Cutter that cuts as req says: its Limit and Overlap, as PlanWindows reads
// them, and its tracker fields, MinSilence and either Threshold or Percentile, FloorWindow, and
// Margin, as NewTracker reads them. It reads no other field. A field either refuses is an error
// wrapping ErrRequest.
func NewCutter(req Request) (*Cutter, error) {
	planner, err := NewPlanner(req)
	if err != nil {
		return nil, err
	}
	tracker, err := NewTracker(req)
	if err != nil {
		return nil, err
	}
	return &Cutter{tracker: tracker, planner: planner, threshold: req.Threshold}, nil
}

// Feed gives the cutter the stream's next bytes, any number of them, a sample's two bytes split
// across calls or not, and returns the windows they fix, in order: each window whose limit the
// stream has passed, inside a silence or not. A Feed after End, or after an error, returns
// nothing. An error is the planner's refusal of what the tracker found, which no stream causes; it
// ends the cutting.
func (c *Cutter) Feed(p []byte) ([]Piece, error) {
	if c.ended || c.failed != nil {
		return nil, c.failed
	}
	c.pcm = append(c.pcm, p...)
	// The tracker is fed a frame at a time, so the threshold read after each whole frame is the
	// one it decided that frame against.
	for rest := p; len(rest) > 0; {
		n := int(min(int64(len(rest)), frameBytes-c.received%frameBytes))
		found := c.tracker.Feed(rest[:n])
		c.received += int64(n)
		rest = rest[n:]
		if c.received%frameBytes == 0 {
			c.thresholds = append(c.thresholds, c.tracker.Threshold())
		}
		if err := c.give(found); err != nil {
			return nil, c.fail(err)
		}
	}
	// A silence open past a window's limit ends the window as it will once closed, so the planner
	// is given it open, and fixes the window as the stream passes the limit inside it.
	var windows []Window
	var err error
	if c.open, c.isOpen = c.tracker.Open(); c.isOpen {
		windows, err = c.planner.Open(c.open.Span)
	} else {
		windows, err = c.planner.Advance(c.tracker.Position())
	}
	if err != nil {
		return nil, c.fail(err)
	}
	return c.cut(windows), nil
}

// End ends the stream: the tracker's last silences and the planner's last windows, in order, the
// last cut at the end, CutEnd, at the samples the stream held. A stream of no whole sample is an
// error, as is an End after End.
func (c *Cutter) End() ([]Piece, error) {
	switch {
	case c.failed != nil:
		return nil, c.failed
	case c.ended:
		return nil, fmt.Errorf("audio: the stream's end after its end: %w", ErrRequest)
	}
	c.ended = true
	last := c.tracker.End()
	c.isOpen = false
	if err := c.give(last); err != nil {
		return nil, c.fail(err)
	}
	if c.tracker.Duration() == 0 {
		return nil, c.fail(errors.New("no samples"))
	}
	windows, err := c.planner.End(c.tracker.Duration())
	if err != nil {
		return nil, c.fail(err)
	}
	return c.cut(windows), nil
}

// Plan returns the stream's plan once End has returned without error: its decoded Duration, the
// Limit and Overlap, every window and seam, and every silence the tracker found. Before then, it
// is the plan so far, its Duration zero.
func (c *Cutter) Plan() Plan {
	plan := Plan{
		Limit: c.planner.Limit(), Overlap: c.planner.Overlap(),
		Windows: slices.Clone(c.windows), Silences: slices.Clone(c.silences),
	}
	if c.ended && c.failed == nil {
		plan.Duration = c.tracker.Duration()
	}
	for i, w := range plan.Windows[min(1, len(plan.Windows)):] {
		plan.Seams = append(plan.Seams, Span{w.Start, plan.Windows[i].End})
	}
	return plan
}

// fail ends the cutting with err, as the planner's.
func (c *Cutter) fail(err error) error {
	c.failed = fmt.Errorf("audio: planning the windows: %w", err)
	return c.failed
}

// give gives the planner silences, keeping them.
func (c *Cutter) give(silences []Silence) error {
	c.silences = append(c.silences, silences...)
	for _, s := range silences {
		if err := c.planner.Silence(s.Span); err != nil {
			return err
		}
	}
	return nil
}

// frameBytes is the bytes of the tracker's frame, FrameLength at SampleRate.
const frameBytes = 2 * int64(frameSamples)

// cut returns windows as pieces, in order, dropping the PCM and thresholds behind each next
// window's start. A piece's PCM is the cutter's buffer, valid until the next Feed or End.
func (c *Cutter) cut(windows []Window) []Piece {
	var out []Piece
	overlap := c.planner.Overlap()
	for _, win := range windows {
		first, end := Sample(win.Start), Sample(win.End)
		p := Piece{
			Index: len(c.windows), Window: win, Threshold: c.cutThreshold(win),
			PCM: c.pcm[2*(first-c.base) : 2*(end-c.base) : 2*(end-c.base)],
		}
		if p.Index > 0 {
			p.SeamBefore = Span{win.Start, win.Start + overlap}
		}
		if win.Cut != CutEnd {
			p.SeamAfter = Span{win.End - overlap, win.End}
		}
		out = append(out, p)
		c.windows = append(c.windows, win)
	}
	// The pieces' PCM stays where it is until the next Feed or End, which drops what the next
	// window's start leaves behind before appending.
	if n := len(out); n > 0 && out[n-1].Window.Cut != CutEnd {
		c.drop(out[n-1].Window.End - overlap)
	}
	return out
}

// cutThreshold returns the threshold win was cut under: the fixed threshold; or, with a rolling
// one, for a window cut at a silence, the threshold of the silence whose middle, taken up to the
// window's limit as the planner takes it, is the window's end, the open one's when it is still
// open; for one cut at the end, the tracker's last, which End decided the last frame against; and
// for one cut at the limit, the one its last sample's frame was decided against.
func (c *Cutter) cutThreshold(win Window) float64 {
	switch {
	case c.threshold != 0:
		return c.threshold
	case win.Cut == CutEnd:
		return c.tracker.Threshold()
	case win.Cut == CutLimit:
		// The planner fixes a window at its limit only once the tracker has decided past it, so
		// its last sample's frame is whole.
		frame := (Sample(win.End)-1)/int64(frameSamples) - c.base/int64(frameSamples)
		if frame >= 0 && frame < int64(len(c.thresholds)) {
			return c.thresholds[frame]
		}
		return c.tracker.Threshold()
	}
	limit := win.Start + c.planner.Limit()
	candidates := c.silences
	if c.isOpen {
		candidates = append(slices.Clip(candidates), c.open)
	}
	for _, s := range slices.Backward(candidates) {
		if s.Start < win.Start {
			break
		}
		if s.Start >= limit {
			continue
		}
		if e := min(s.End, limit); s.Start+(e-s.Start)/2 == win.End {
			return s.Threshold
		}
	}
	return 0
}

// drop forgets the PCM and thresholds before start, the next window's.
func (c *Cutter) drop(start time.Duration) {
	first := Sample(start)
	// The pieces cut from the PCM are the caller's until the next Feed or End, so the PCM kept is
	// copied to a buffer of its own rather than moved down over them.
	c.pcm = append([]byte(nil), c.pcm[2*(first-c.base):]...)
	if n := min(first/int64(frameSamples)-c.base/int64(frameSamples), int64(len(c.thresholds))); n > 0 {
		c.thresholds = append(c.thresholds[:0], c.thresholds[n:]...)
	}
	c.base = first
}
