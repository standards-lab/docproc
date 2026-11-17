// Package media reads a file's audio and video through one ffmpeg run and streams them, as they
// are complete, as the inputs a model takes: windows of its audio, cut by audio's stream cutter,
// and frames of its video, taken at its scene changes and at least every so often. It provides
// pre-inference preparation only; a layer above sends the windows and frames to a model. The
// package builds on every platform, on the standard library and this module's audio, route, and
// tool packages. The programs it runs are FFmpeg's ffmpeg and ffprobe, each a tool.Tool the caller
// built through either runner, typically from audio.FFmpegSpec and audio.FFprobeSpec, minimum 9.0,
// the container runner running them in audio.FFmpegImage.
//
// A Streamer, built over a Config of the two Tools, both required, takes a file and a Request: the
// media directory its windows and frames are written into and left, and the settings of its
// windows and frames. A field out of range is refused, with ErrRequest, before anything runs.
// The run allows ffmpeg only the file protocol, through -protocol_whitelist, so a file that is a
// playlist opens nothing but itself.
//
// Events probes the file through ffprobe for its first audio stream and its first video stream,
// an attached picture not counted, refusing a file with neither, with ErrNoMedia, before ffmpeg
// runs. Then one ffmpeg run reads the file, and the stream yields an Event for each window and
// each frame as soon as it is complete, a lazy iter.Seq2 of Event:
//
//   - The audio stream comes to the package as 16 kHz mono 16-bit PCM on ffmpeg's stdout, as
//     audio's Splitter decodes it, and audio's stream cutter, an audio.Cutter, cuts it into
//     windows as it arrives: the tracker finds its silences under the request's fixed threshold
//     or a rolling one that follows the stream's noise floor, with audio's tracker defaults, and
//     the windows are planned by audio.PlanWindows' policy, each at most the request's Limit, 30 s
//     by default, its overlap included, cut at the middle of the latest silence before its limit,
//     or at the limit. A window is cut once the stream passes its limit or ends, written as WAV
//     through audio.WAV, so a window is the same bytes audio's Splitter.Windows cuts, and its
//     event carries its index, span, cut, seams, samples, and the threshold it was cut under.
//   - The video stream's frames are taken by ffmpeg's select filter: the first frame, each whose
//     scene score is past the request's SceneThreshold, DefaultSceneThreshold, 0.2, by default,
//     and each MaxGap, DefaultMaxGap, 10 s, by default, or more after the last taken. Each is
//     written as PNG, at the source's resolution or scaled down to fit the request's MaxSide,
//     into a hidden directory of the run's own, made in the file's directory, the run's working
//     directory, which the container runner mounts, since ffmpeg reads the file by its path and
//     the runner mounts one directory. The metadata filter reports each frame's time and scene
//     score on ffmpeg's stderr, from which its reason comes: first, scene, or interval.
//
// Each event carries its source time. Windows come in plan order and frames in source-time order,
// interleaved as they complete. Each window and frame is named by its SHA-256 in the media
// directory, renamed there just before it is yielded, so the media directory holds only the
// windows and frames the stream yielded. The same file and request give the same files through
// either runner.
//
// A context that ends stops ffmpeg through either runner, and the stream yields its error once and
// ends; a loop that stops early stops the run. A run that fails ends the stream with its error,
// after the events already complete, the frames whose PNG is whole included. Either way, nothing
// the stream didn't yield is left behind, nor the run's directory of frames.
//
// Request.Plan gives the plan record of a request's settings, a VideoPlan, each default applied.
// The live sources, a reader, a URL, and a paced file, stay in spike-ai-pipeline-processing at
// v0.9.0, until a caller needs them.
//
// Its exported identifiers:
//
//   - [Config] is the Tools a Streamer is built over: ffmpeg and ffprobe.
//   - [Streamer] reads a file through one ffmpeg run; [NewStreamer] builds one over a Config.
//   - [Streamer.Events] streams a file's windows and frames, as a [Request] says, each an
//     [Event].
//   - [Window] is a window event's window of the audio; [Frame] a frame event's frame of the
//     video, taken for its [Reason]: [ReasonFirst], [ReasonScene], or [ReasonInterval].
//   - [Request.Plan] returns a request's plan record, a [VideoPlan] of its [VideoWindowing] and
//     its [VideoSampling].
//   - [DefaultSceneThreshold] and [DefaultMaxGap] are a Request's frame defaults.
//   - [ErrRequest] is a refused request's cause; [ErrNoMedia] a file's with neither an audio nor a
//     video stream.
package media
