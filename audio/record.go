package audio

// PlanRecord is the JSON of a source's plan, the record a caller keeps beside its windows, such as
// the audio stage's "plan" output: the source as ffprobe read it, the windows' limit and overlap,
// the silences the tracker found, and the windows and seams, every time in seconds. Duration is
// the samples the stream decoded to; ProbedDuration, ffprobe's, is beside it only when the two
// differ by more than a FrameLength.
type PlanRecord struct {
	// Duration is the source's decoded duration, which the windows cover, and ProbedDuration
	// ffprobe's, present only when it differs, as Plan's Probed.
	Duration       float64 `json:"duration"`
	ProbedDuration float64 `json:"probed_duration,omitzero"`
	// Limit is a window's longest, its overlaps included, and Overlap each seam's length.
	Limit   float64 `json:"limit"`
	Overlap float64 `json:"overlap"`
	// Stream, Codec, SampleRate, and Channels are the source's audio stream, as Source's.
	Stream     int    `json:"stream"`
	Codec      string `json:"codec"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	// Silences are the silences the tracker found, in order, each with the threshold in force
	// when it opened.
	Silences []SilenceRecord `json:"silences"`
	// Windows are the plan's windows, in order, each the window of its Index.
	Windows []WindowRecord `json:"windows"`
	// Seams are the ranges consecutive windows share: Seams[i] is the end of window i and the
	// start of window i+1.
	Seams []SpanRecord `json:"seams"`
}

// SpanRecord is a span of a source, in seconds from its start.
type SpanRecord struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// SilenceRecord is a silence of a [PlanRecord]: its span and its threshold, in dB relative to full
// scale.
type SilenceRecord struct {
	SpanRecord
	Threshold float64 `json:"threshold"`
}

// WindowRecord is one window of a [PlanRecord]: its index in the plan, its span, and what ended it,
// as Cut names it: silence, limit, or end.
type WindowRecord struct {
	Index int `json:"index"`
	SpanRecord
	Cut string `json:"cut"`
}

// Record returns the plan's record, its slices empty rather than nil, so each is a JSON array.
func (p Plan) Record() PlanRecord {
	out := PlanRecord{
		Duration: p.Duration.Seconds(), ProbedDuration: p.Probed.Seconds(),
		Limit: p.Limit.Seconds(), Overlap: p.Overlap.Seconds(),
		Stream: p.Source.Stream, Codec: p.Source.Codec, SampleRate: p.Source.SampleRate, Channels: p.Source.Channels,
		Silences: make([]SilenceRecord, 0, len(p.Silences)),
		Windows:  make([]WindowRecord, 0, len(p.Windows)),
		Seams:    make([]SpanRecord, 0, len(p.Seams)),
	}
	for _, s := range p.Silences {
		out.Silences = append(out.Silences, SilenceRecord{SpanRecord: s.record(), Threshold: s.Threshold})
	}
	for i, w := range p.Windows {
		out.Windows = append(out.Windows, WindowRecord{Index: i, SpanRecord: w.record(), Cut: w.Cut.String()})
	}
	for _, s := range p.Seams {
		out.Seams = append(out.Seams, s.record())
	}
	return out
}

// record returns the span in seconds.
func (s Span) record() SpanRecord { return SpanRecord{Start: s.Start.Seconds(), End: s.End.Seconds()} }
