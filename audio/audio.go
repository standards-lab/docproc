package audio

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/tool"
)

// DefaultMinSilence is the shortest silence a request's zero MinSilence asks for: 0.3 s, about a
// pause between words, which a cut there doesn't split.
const DefaultMinSilence = 300 * time.Millisecond

var (
	// ErrRequest is the cause of a refused request: a field out of range, before anything runs.
	ErrRequest = errors.New("invalid request")
	// ErrNoAudio is the cause of the error for a source ffprobe finds no audio stream in. Its
	// probe has run; ffmpeg hasn't.
	ErrNoAudio = errors.New("no audio stream")
)

// Config is what a Splitter is built over: the Tools the caller built, through either runner.
// Both are required.
type Config struct {
	// FFmpeg is FFmpeg's ffmpeg, typically built from FFmpegSpec, on the host or in FFmpegImage.
	// It decodes the source's audio stream, once, into the PCM the windows are cut from.
	FFmpeg *tool.Tool
	// FFprobe is FFmpeg's ffprobe, typically built from FFprobeSpec, on the host or in
	// FFmpegImage. It reads the source's audio stream and its estimated duration.
	FFprobe *tool.Tool
}

// Splitter cuts a source's audio into windows: it probes the source, decodes its audio stream
// once, and cuts the windows from the stream with a Cutter as it arrives. Its methods are safe for
// concurrent use.
type Splitter struct {
	ffmpeg  *tool.Tool
	ffprobe *tool.Tool
}

// NewSplitter builds a Splitter over cfg's Tools. A nil FFmpeg or FFprobe is the caller's mistake,
// and panics naming the field. It refuses either older than FFmpegMinimum, whatever minimum the
// Tool's own Spec declared, with a *tool.VersionError wrapping tool.ErrTooOld.
func NewSplitter(cfg Config) (*Splitter, error) {
	if cfg.FFmpeg == nil {
		panic("audio: Config.FFmpeg is nil")
	}
	if cfg.FFprobe == nil {
		panic("audio: Config.FFprobe is nil")
	}
	for _, t := range []*tool.Tool{cfg.FFmpeg, cfg.FFprobe} {
		if err := t.AtLeast(FFmpegMinimum); err != nil {
			return nil, err
		}
	}
	return &Splitter{ffmpeg: cfg.FFmpeg, ffprobe: cfg.FFprobe}, nil
}

// Request is how a source's windows are cut: where they are written and how many are held, the
// windows' limit and overlap, and the tracker's silences. A Cutter and a Planner read its
// windowing fields alone, and a Tracker its tracker fields alone.
type Request struct {
	// Dir is the media directory the windows are written into and left, which the caller owns. It
	// is required by Splitter.Windows, and must be a directory; an empty Dir is refused rather than
	// taken as the system's temporary directory.
	Dir string `json:"-"`
	// Concurrency is how many windows are held at once, each from its cut until the consumer
	// takes it: the decode waits while that many wait for the consumer. Splitter.Windows requires
	// at least 1, and refuses anything less.
	Concurrency int `json:"-"`
	// Limit is the longest a window may be, its overlaps included: zero is DefaultWindowLimit,
	// 30 s; below zero is refused.
	Limit time.Duration `json:"limit"`
	// Overlap is how long before a window's end the next window starts: zero is DefaultOverlap,
	// 2 s, and it must be under Limit/2, so a window cut at the limit still has audio of its own
	// between its two seams.
	Overlap time.Duration `json:"overlap"`
	// MinSilence is the shortest silence a window may end in: zero is DefaultMinSilence, 0.3 s.
	// Below zero is refused.
	MinSilence time.Duration `json:"min_silence"`
	// Threshold, when not zero, is a fixed silence threshold in dB relative to full scale, such
	// as -40, in place of the tracker's rolling one. It must be from -120 dB to below zero, and
	// Percentile, FloorWindow, and Margin zero with it.
	Threshold float64 `json:"threshold"`
	// Percentile is the tracker's percentile of frame energy, as a Tracker says: zero is
	// DefaultPercentile, the 10th; it must be under 100.
	Percentile float64 `json:"percentile"`
	// FloorWindow is the tracker's rolling window, the frames the percentile is taken over: zero
	// is DefaultFloorWindow, 4 s; it must otherwise be from 1 s to 10 minutes. The tracker keeps
	// each of its frames' energy, 2 bytes a frame, 120,000 bytes at 10 minutes.
	FloorWindow time.Duration `json:"floor_window"`
	// Margin is how far above the percentile, in dB, the tracker's threshold is: zero is
	// DefaultMargin, 12 dB; below zero is refused.
	Margin float64 `json:"margin"`
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// Source is what ffprobe reports of a source: its duration and its first audio stream.
type Source struct {
	// Duration is the source's duration as ffprobe reports it, the container's, or its audio
	// stream's when the container gives none: an estimate, which a truncated or mis-sized file's
	// samples may fall short of or pass. A plan's windows end at the samples the stream decodes
	// to, its Duration, this one beside it as its Probed when the two differ.
	Duration time.Duration
	// Stream is the audio stream's index in the container, which ffmpeg's runs map.
	Stream int
	// Codec is the stream's codec name, as ffprobe gives it, such as flac or pcm_s16le.
	Codec string
	// SampleRate is the stream's sample rate, in Hz.
	SampleRate int
	// Channels is the stream's channel count.
	Channels int
	// Metadata is the source's metadata record, from the same probe: its container's format,
	// duration, bit rate, and tags, and each of its streams.
	Metadata metadata.Media
}

// Probe reads the source at path through ffprobe: its duration and its first audio stream's index,
// codec, sample rate, and channels, and, from the same run, its metadata record. A source with no
// audio stream is an error wrapping ErrNoAudio, as is one ffprobe reports no duration for; a source
// ffprobe can't read is its *tool.ExitError, wrapped.
func (s *Splitter) Probe(ctx context.Context, path string) (Source, error) {
	if path == "" {
		return Source{}, fmt.Errorf("audio: no source path: %w", ErrRequest)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Source{}, fmt.Errorf("audio: source path %s: %w: %w", path, ErrRequest, err)
	}
	return s.probe(ctx, abs)
}

func (s *Splitter) probe(ctx context.Context, path string) (Source, error) {
	var stdout bytes.Buffer
	err := s.ffprobe.Run(ctx, tool.Invocation{
		Args: []string{
			"-v", "error", "-print_format", "json",
			"-show_entries", cli.FFprobeEntries,
			path,
		},
		Dir:    filepath.Dir(path),
		Stdout: &stdout,
	})
	if err != nil {
		return Source{}, fmt.Errorf("audio: probe %s: %w", path, err)
	}
	report, err := cli.ParseFFprobe(stdout.Bytes())
	if err != nil {
		return Source{}, fmt.Errorf("audio: probe %s: ffprobe's report: %w", path, err)
	}
	for _, st := range report.Streams {
		if st.CodecType != "audio" {
			continue
		}
		src := Source{Stream: st.Index, Codec: st.CodecName, Channels: st.Channels, Metadata: report.Metadata()}
		if src.SampleRate, err = strconv.Atoi(st.SampleRate); err != nil {
			return Source{}, fmt.Errorf("audio: probe %s: stream %d's sample rate %q: %w", path, st.Index, st.SampleRate, err)
		}
		d := cmp.Or(report.Format.Duration, st.Duration)
		if d == "" || d == "N/A" {
			return Source{}, fmt.Errorf("audio: probe %s: no duration: %w", path, ErrNoAudio)
		}
		if src.Duration, err = seconds(d); err != nil {
			return Source{}, fmt.Errorf("audio: probe %s: duration: %w", path, err)
		}
		if src.Duration <= 0 {
			return Source{}, fmt.Errorf("audio: probe %s: duration %v: %w", path, src.Duration, ErrNoAudio)
		}
		return src, nil
	}
	return Source{}, fmt.Errorf("audio: probe %s: %w", path, ErrNoAudio)
}

func seconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if !finite(f) {
		return 0, fmt.Errorf("%q seconds isn't finite", s)
	}
	return time.Duration(math.Round(f * float64(time.Second))), nil
}

// See docs/api.md, "Running the tools".
func decodeArgs(path string, src Source) []string {
	return []string{
		"-nostdin", "-hide_banner", "-nostats", "-loglevel", "error",
		"-flags:a", "+bitexact", "-i", path, "-map", "0:" + strconv.Itoa(src.Stream),
		"-af", fmt.Sprintf("aresample=%d", SampleRate),
		"-ac", "1", "-ar", strconv.Itoa(SampleRate), "-c:a", "pcm_s16le",
		"-flags:a", "+bitexact", "-fflags", "+bitexact", "-f", "s16le", "-",
	}
}
