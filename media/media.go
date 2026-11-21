package media

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/tool"
)

// DefaultSceneThreshold is the scene threshold a request's zero SceneThreshold asks for: 0.2, on
// the 0 to 1 scale of ffmpeg's select filter's scene score. On the committed fixture it takes the
// four hard cuts, scored 1, and no other frame; on Tears of Steel's 180 s clip it takes the same 30
// frames, at the same times, as scdet at its default threshold, 10 on scdet's own 0 to 100 scale,
// as any threshold from 0.15 to 0.3 does.
const DefaultSceneThreshold = 0.2

// DefaultMaxGap is the longest a request's zero MaxGap lets the stream go without a frame: 10 s.
const DefaultMaxGap = 10 * time.Second

var (
	// ErrRequest is the cause of a refused request: a field out of range, before anything runs. It
	// is audio's own, so a refusal of a request's windowing fields, which audio validates, matches
	// it as a refusal of media's own fields does.
	ErrRequest = audio.ErrRequest
	// ErrNoMedia is the cause of the error for a file ffprobe finds neither an audio stream nor a
	// video stream in, whose probe has run and ffmpeg hasn't.
	ErrNoMedia = errors.New("no audio or video stream")
)

// Config is what a Streamer is built over: the Tools the caller built, through either runner,
// typically from audio's FFmpegSpec and FFprobeSpec, on the host or in audio.FFmpegImage. Both are
// required.
type Config struct {
	// FFmpeg is FFmpeg's ffmpeg: the one run that reads a file, decodes its audio, and writes its
	// frames.
	FFmpeg *tool.Tool
	// FFprobe is FFmpeg's ffprobe: it finds a file's audio and video streams before ffmpeg runs.
	FFprobe *tool.Tool
}

// Streamer reads a file through one ffmpeg run and streams its windows and frames as they are
// complete. Its methods are safe for concurrent use.
type Streamer struct {
	ffmpeg  *tool.Tool
	ffprobe *tool.Tool
}

// NewStreamer builds a Streamer over cfg's Tools. A nil FFmpeg or FFprobe is the caller's
// mistake, and panics naming the field. It refuses either older than audio.FFmpegMinimum,
// whatever minimum the Tool's own Spec declared, with a *tool.VersionError wrapping
// tool.ErrTooOld.
func NewStreamer(cfg Config) (*Streamer, error) {
	if cfg.FFmpeg == nil {
		panic("media: Config.FFmpeg is nil")
	}
	if cfg.FFprobe == nil {
		panic("media: Config.FFprobe is nil")
	}
	for _, t := range []*tool.Tool{cfg.FFmpeg, cfg.FFprobe} {
		if err := t.AtLeast(audio.FFmpegMinimum); err != nil {
			return nil, err
		}
	}
	return &Streamer{ffmpeg: cfg.FFmpeg, ffprobe: cfg.FFprobe}, nil
}

const fileProtocols = "file"

// Request is how one file's windows and frames are taken.
type Request struct {
	// Dir is the media directory the windows and frames are written into and left, which the
	// caller owns. It is required, and must be a directory.
	Dir string

	// Limit is the longest a window may be, its overlaps included, as audio.Request's: zero is
	// audio.DefaultWindowLimit, 30 s; below zero is refused.
	Limit time.Duration
	// Overlap is how long before a window's end the next window starts, as audio.Request's: zero
	// is audio.DefaultOverlap, 2 s, and it must be under Limit/2.
	Overlap time.Duration
	// MinSilence is the shortest silence a window may end in, as audio.Request's: zero is
	// audio.DefaultMinSilence, 0.3 s; below zero is refused.
	MinSilence time.Duration
	// Threshold, when not zero, is a fixed silence threshold in dB relative to full scale, from
	// -120 to below zero, in place of the tracker's rolling one, as audio.Request's; Percentile,
	// FloorWindow, and Margin must then be zero.
	Threshold float64
	// Percentile, FloorWindow, and Margin are the tracker's rolling threshold, as audio.Request's
	// and audio.NewTracker say: zero is audio.DefaultPercentile, audio.DefaultFloorWindow, and
	// audio.DefaultMargin.
	Percentile  float64
	FloorWindow time.Duration
	Margin      float64

	// SceneThreshold is the scene score past which a frame is taken as a scene change, on the 0
	// to 1 scale of ffmpeg's select filter: zero is DefaultSceneThreshold, 0.2; it must otherwise
	// be above zero and under 1.
	SceneThreshold float64
	// MaxGap is the longest the stream goes without a frame: a frame MaxGap or more after the last
	// one taken is taken. Zero is DefaultMaxGap, 10 s; below zero is refused.
	MaxGap time.Duration
	// MaxSide, when not zero, is the largest side a frame is written at: a frame larger is scaled
	// down to fit, its aspect kept, and a smaller one is written as it is. Zero writes every frame
	// at the source's resolution; below zero is refused.
	MaxSide int
}

func (req Request) audio() audio.Request {
	return audio.Request{
		Limit: req.Limit, Overlap: req.Overlap, MinSilence: req.MinSilence, Threshold: req.Threshold,
		Percentile: req.Percentile, FloorWindow: req.FloorWindow, Margin: req.Margin,
	}
}

type checked struct {
	path    string
	dir     string
	audio   audio.Request
	scene   float64
	gap     time.Duration
	maxSide int
}

func (req Request) check(path string) (checked, error) {
	if path == "" {
		return checked{}, fmt.Errorf("media: no source path: %w", ErrRequest)
	}
	c := checked{
		dir:     req.Dir,
		audio:   req.audio(),
		scene:   cmp.Or(req.SceneThreshold, DefaultSceneThreshold),
		gap:     cmp.Or(req.MaxGap, DefaultMaxGap),
		maxSide: req.MaxSide,
	}
	if _, err := audio.NewCutter(c.audio); err != nil {
		return checked{}, fmt.Errorf("media: %w", err)
	}
	switch {
	case math.IsNaN(req.SceneThreshold) || req.SceneThreshold < 0 || req.SceneThreshold >= 1:
		return checked{}, fmt.Errorf("media: scene threshold %v isn't 0 or above it and under 1: %w", req.SceneThreshold, ErrRequest)
	case req.MaxGap < 0:
		return checked{}, fmt.Errorf("media: maximum gap %v is below zero: %w", req.MaxGap, ErrRequest)
	case req.MaxSide < 0:
		return checked{}, fmt.Errorf("media: maximum side %d is below zero: %w", req.MaxSide, ErrRequest)
	case req.Dir == "":
		return checked{}, fmt.Errorf("media: request has no media directory: %w", ErrRequest)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return checked{}, fmt.Errorf("media: source file %s: %w: %w", path, ErrRequest, err)
	}
	c.path = abs
	info, err := os.Stat(req.Dir)
	if err != nil {
		return checked{}, fmt.Errorf("media: media directory: %w: %w", ErrRequest, err)
	}
	if !info.IsDir() {
		return checked{}, fmt.Errorf("media: media directory %s isn't a directory: %w", req.Dir, ErrRequest)
	}
	return c, nil
}

type streams struct {
	audio, video string
}

func (s *Streamer) probe(ctx context.Context, path string) (streams, error) {
	var stdout bytes.Buffer
	err := s.ffprobe.Run(ctx, tool.Invocation{
		Args: []string{
			"-v", "error", "-protocol_whitelist", fileProtocols, "-print_format", "json",
			"-show_entries", "stream=index,codec_type:stream_disposition=attached_pic", path,
		},
		Dir:    filepath.Dir(path),
		Stdout: &stdout,
	})
	if err != nil {
		return streams{}, fmt.Errorf("media: probe %s: %w", path, err)
	}
	var report struct {
		Streams []struct {
			Index       int    `json:"index"`
			CodecType   string `json:"codec_type"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return streams{}, fmt.Errorf("media: probe %s: ffprobe's report: %w", path, err)
	}
	var st streams
	for _, x := range report.Streams {
		switch {
		case x.CodecType == "audio" && st.audio == "":
			st.audio = "0:" + strconv.Itoa(x.Index)
		case x.CodecType == "video" && x.Disposition.AttachedPic == 0 && st.video == "":
			st.video = "0:" + strconv.Itoa(x.Index)
		}
	}
	if st.audio == "" && st.video == "" {
		return streams{}, fmt.Errorf("media: probe %s: %w", path, ErrNoMedia)
	}
	return st, nil
}

// Event is one event of a file's stream: a window of its audio or a frame of its video. Exactly
// one of Window and Frame is set; the zero Event, which a stream yields with an error that ends it,
// has neither.
type Event struct {
	// Window is the window, for a window event.
	Window *Window
	// Frame is the frame, for a frame event.
	Frame *Frame
	// Time is the event's source time, an offset from the source's start: a window's start, its
	// Span's Start, or a frame's presentation time.
	Time time.Duration
}

// Window is one window of a source's audio, cut by audio's stream cutter and written to the media
// directory as WAV: 16 kHz, audio.SampleRate, mono 16-bit PCM, its header the canonical 44 bytes,
// through audio.WAV, so the same window is the same bytes audio's Splitter.Windows cuts.
type Window struct {
	// Index is the window's index in the stream's plan, from 0.
	Index int
	// Span is the window's span of the source, overlaps included.
	Span audio.Span
	// Cut is what ended the window: audio.CutSilence, audio.CutLimit, or audio.CutEnd, the last.
	Cut audio.Cut
	// SeamBefore is the seam the window starts in, shared with the window before it; zero for the
	// first. SeamAfter is the seam it ends in, shared with the window after it; zero for the last.
	SeamBefore audio.Span
	SeamAfter  audio.Span
	// Samples is the number of samples the WAV holds, at audio.SampleRate.
	Samples int
	// Threshold is the silence threshold, in dB relative to full scale, the window was cut under,
	// as audio.Piece's.
	Threshold float64
	// Path is the WAV's path in the media directory, named by SHA256 and ".wav".
	Path string
	// SHA256 is the lowercase hex SHA-256 of the WAV's bytes, header included.
	SHA256 string
}

// Frame is one frame of a source's video, written to the media directory as PNG.
type Frame struct {
	// Index is the frame's index among the frames the stream takes, from 0.
	Index int
	// Reason is why the frame was taken.
	Reason Reason
	// Width and Height are the PNG's, the source's or, scaled to the request's MaxSide, smaller.
	Width  int
	Height int
	// Path is the PNG's path in the media directory, named by SHA256 and ".png".
	Path string
	// SHA256 is the lowercase hex SHA-256 of the PNG's bytes.
	SHA256 string
}

// Reason is why a frame was taken.
type Reason int

const (
	// ReasonFirst is the source's first frame.
	ReasonFirst Reason = iota + 1
	// ReasonScene is a scene change: a frame whose scene score is past the request's
	// SceneThreshold.
	ReasonScene
	// ReasonInterval is a frame taken because the request's MaxGap passed with none.
	ReasonInterval
)

// String returns the reason's name: first, scene, or interval.
func (r Reason) String() string {
	switch r {
	case ReasonFirst:
		return "first"
	case ReasonScene:
		return "scene"
	case ReasonInterval:
		return "interval"
	}
	return fmt.Sprintf("Reason(%d)", int(r))
}
