// Package audio prepares speech for a model's audio input: it cuts a source into windows short
// enough for the model to take whole, each ending in a silence so no cut splits a word. It provides
// pre-inference preparation only; a layer above sends the windows to a model and joins their
// transcripts. The package builds on every 64-bit platform, on the standard library and this
// module's tool package. The programs it runs are FFmpeg's ffmpeg and ffprobe, each a tool.Tool the
// caller built through either runner: FFmpegSpec and FFprobeSpec declare them, minimum 9.0, and the
// container runner runs them in FFmpegImage.
//
// A Splitter, built over a Config of the two Tools, both required, cuts a source's windows with one
// decode. Splitter.Windows probes the source through ffprobe for its first audio stream and its
// estimated duration, refusing a source with none, with ErrNoAudio, before ffmpeg runs. Then one
// ffmpeg run decodes the stream to 16 kHz, SampleRate, mono 16-bit PCM, and a Cutter, the stream
// cutter, cuts it into windows as it arrives, each written into the request's media directory as
// WAV with the canonical 44-byte header of ffmpeg's -bitexact WAV, named by the SHA-256 of its
// bytes. The same window is the same file through either runner, and over the committed speech
// fixtures the windows are v0.9.0's under its tracker detector, byte for byte. A Request names the
// media directory and how many windows are held at once, at least one; the decode waits while that
// many wait for the consumer. A loop that stops early, or a context that ends, stops the run, and
// the media directory keeps only the windows the stream yielded.
//
// A Cutter feeds the PCM to a Tracker, which finds its silences, and gives them and the stream's
// position to a Planner, which fixes each window once the stream has passed its limit or ended,
// and returns each with its samples. A Tracker frames the PCM in 10 ms frames, FrameLength, and
// takes each frame's RMS energy in dB; its threshold is a low percentile of the frames' energy
// over a rolling window, plus a margin, DefaultPercentile, the 10th, over DefaultFloorWindow, 4 s,
// plus DefaultMargin, 12 dB, by default, or a fixed threshold in their place; a run of frames under
// it at least the minimum silence long, DefaultMinSilence, 0.3 s, by default, is a Silence, which
// reports the threshold in force when it opened. It is fed PCM in chunks as the stream arrives,
// returns each silence as it closes, keeps memory bounded by its window, and finds the same
// silences however the stream is chunked. The tracker is the one silence detector: its threshold
// follows a change of level or of noise floor partway through a stream, which a threshold fixed
// for a run, as ffmpeg's silencedetect's is, can't; silencedetect stays in
// spike-ai-pipeline-processing at v0.9.0.
//
// PlanWindows, a pure function of a duration, its silences, and a Request's Limit and Overlap,
// plans the windows. They cover the source in order, each at most the Limit, DefaultWindowLimit,
// 30 s, by default, overlap included. Each window but the last ends at the middle of the latest
// silence its limit allows, or at the limit when it allows none. The next window starts Overlap
// before it, DefaultOverlap, 2 s, by default, and the range the two share is their seam. The same
// arguments give the same plan. A Planner plans a stream's windows by the same policy as it
// arrives, so a Cutter's windows are PlanWindows' of the stream's length and the tracker's
// silences.
//
// A plan ends at the samples the stream decoded to, not at ffprobe's duration, which a truncated
// or mis-sized file's header misstates: the last window carries the source's Plan, its Duration
// the decoded samples', with ffprobe's duration beside it as Probed when the two differ by more
// than a FrameLength. Plan.Record gives its JSON, a PlanRecord, for the record a caller keeps
// beside the windows.
//
// Its exported identifiers:
//
//   - [FFmpegSpec] and [FFprobeSpec] declare ffmpeg and ffprobe for either runner's constructor.
//   - [FFmpegMinimum] is the oldest ffmpeg and ffprobe the package accepts, 9.0.
//   - [FFmpegImage] is the default image ffmpeg and ffprobe run in through the container runner.
//   - [Config] is the Tools a Splitter is built over: ffmpeg and ffprobe.
//   - [Splitter] cuts a source's windows with one decode; [NewSplitter] builds one over a Config.
//   - [Splitter.Probe] reads a source's audio stream and estimated duration, a [Source].
//   - [Splitter.Windows] cuts a source's windows as a [Request] says, each a [WindowFile], the
//     last carrying the source's [Plan].
//   - [Cutter] is the stream cutter, cutting PCM into windows as it arrives, each a [Piece];
//     [NewCutter] builds one. [Cutter.Feed] gives it the stream's next bytes and [Cutter.End]
//     ends it, each returning the windows that fixes; [Cutter.Plan] returns its plan.
//   - [SampleRate] is the windows' sample rate, 16 kHz.
//   - [Sample] is the sample at SampleRate nearest a time, where a window's samples begin and end.
//   - [WAV] writes 16 kHz mono 16-bit PCM as a window's WAV, with the canonical 44-byte header.
//   - [PlanWindows] plans the windows of a duration and its silences, running nothing.
//   - [Planner] plans a stream's windows as it arrives, by PlanWindows' policy; [NewPlanner] builds
//     one.
//   - [Planner.Silence] gives it a silence found; [Planner.Open] gives it one still open,
//     [Planner.Advance] the stream's position, and [Planner.End] the stream's end, each returning
//     the windows that fixes; [Planner.Limit] and [Planner.Overlap] return the limit and the
//     overlap it plans with.
//   - [Tracker] finds a stream's silences as its PCM arrives, each a [Silence] with its
//     threshold; [NewTracker] builds one.
//   - [Tracker.Feed] gives it the stream's next bytes and [Tracker.End] ends the stream, each
//     returning the silences that closes; [Tracker.Open] reports the silence still open,
//     [Tracker.Position] how far the stream is settled, [Tracker.Duration] its length, and
//     [Tracker.Threshold] the threshold in force.
//   - [FrameLength] is the tracker's frame, 10 ms.
//   - [DefaultPercentile], [DefaultFloorWindow], and [DefaultMargin] are the tracker's defaults.
//   - [Plan] is a source's windows, each a [Window], ended as its [Cut] says, their seams, and its
//     silences; [Plan.Record] is its JSON, a [PlanRecord] of [SilenceRecord]s, [WindowRecord]s,
//     and [SpanRecord]s.
//   - [Cut] is what ends a window: [CutSilence], [CutLimit], or [CutEnd].
//   - [Span] is a range of the source: a silence, a window's, or a seam.
//   - [DefaultWindowLimit], [DefaultOverlap], and [DefaultMinSilence] are a Request's defaults.
//   - [ErrRequest] is a refused request's cause; [ErrNoAudio] a source's with no audio stream.
package audio
