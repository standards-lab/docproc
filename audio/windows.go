package audio

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

// SampleRate is the windows' sample rate, 16 kHz, the rate a speech model's audio input takes.
// Every window is mono 16-bit PCM at this rate, whatever the source's rate, channels, and format.
const SampleRate = 16000

const wavHeader = 44

// WindowFile is one window of a source, written to the media directory as WAV: 16 kHz,
// SampleRate, mono 16-bit PCM, its header the canonical 44 bytes, carrying no encoder's name, so
// the same window of the same source is the same bytes through either runner.
type WindowFile struct {
	// Index is the window's index in the plan's Windows, from 0.
	Index int
	// Window is the window: its span of the source, overlaps included, and what ended it.
	Window Window
	// SeamBefore is the seam the window starts in, shared with the window before it; zero for the
	// first. SeamAfter is the seam it ends in, shared with the window after it; zero for the last.
	SeamBefore Span
	SeamAfter  Span
	// Threshold is the silence threshold, in dB relative to full scale, the window was cut under,
	// as a Piece's.
	Threshold float64
	// Path is the WAV's path in the media directory, named by SHA256 and ".wav".
	Path string
	// SHA256 is the lowercase hex SHA-256 of the WAV's bytes, header included.
	SHA256 string
	// Samples is the number of samples the WAV holds, at SampleRate.
	Samples int
	// Plan is the source's whole plan, set on the last window alone, the one cut at the end,
	// CutEnd: the decode has ended by then, so the plan's Duration is the samples it gave.
	Plan *Plan
}

// Duration returns the WAV's duration: its Samples at SampleRate.
func (w WindowFile) Duration() time.Duration {
	return time.Duration(w.Samples) * time.Second / SampleRate
}

// Windows returns the stream of the windows of the source at path, cut as req says, each written
// into req.Dir as a WindowFile. The stream is lazy: nothing runs, not even the request's
// validation, until it is ranged over.
//
// Ranging over it validates path and req, an invalid one an error wrapping ErrRequest. It probes
// the source, as Probe does, refusing one with no audio stream, with ErrNoAudio, before ffmpeg
// runs. Then one ffmpeg run decodes the audio stream Probe found, resampled to SampleRate and
// downmixed to mono, as 16-bit PCM on its stdout, and a Cutter of req's fields cuts it into
// windows as it arrives: each window is cut once the stream passes its limit or ends, its samples
// the stream's from Sample of its start up to but not including its end's, and the package writes
// the WAV header and names the file by the SHA-256 of the whole. The source is decoded once,
// whatever its windows; ffmpeg writes nothing to the media directory, which the container runner
// doesn't mount. Over the committed speech fixtures, the windows are v0.9.0's under its tracker
// detector, byte for byte.
//
// Windows are yielded in plan order, each as soon as it is cut. At most req.Concurrency windows
// are held at once, from its cut until the consumer's loop body returns from it: while that many
// are held, the decode waits. The last window, cut at the end, carries the source's Plan: its
// Duration the samples the stream decoded to, which the windows cover, with ffprobe's duration
// beside it as Probed when the two differ by more than a FrameLength.
//
// A window is written to a hidden temporary file in req.Dir and renamed to its SHA-256 name just
// before it is yielded, so the media directory holds only whole windows, and only the ones the
// stream yielded: a window cut but never yielded, because the loop stopped or ctx ended first, is
// removed.
//
// A refused request, or a failed probe, yields one error with a zero WindowFile, and the stream
// ends. A decode that fails, a stream of no samples, or a window that can't be written ends the
// stream with its error, after the windows already yielded. When ctx ends, the run is stopped, and
// the stream yields ctx's error once, with a zero WindowFile, and ends. Breaking out of the range
// stops the run, and the loop returns once it has ended and its files are removed.
func (s *Splitter) Windows(ctx context.Context, path string, req Request) iter.Seq2[WindowFile, error] {
	return func(yield func(WindowFile, error) bool) {
		abs, cutter, err := req.check(path)
		if err != nil {
			yield(WindowFile{}, err)
			return
		}
		if err := ctx.Err(); err != nil {
			yield(WindowFile{}, err)
			return
		}
		src, err := s.probe(ctx, abs)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				err = cerr
			}
			yield(WindowFile{}, err)
			return
		}
		s.windows(ctx, abs, req, src, cutter, yield)
	}
}

func (req Request) check(path string) (string, *Cutter, error) {
	switch {
	case path == "":
		return "", nil, fmt.Errorf("audio: no source path: %w", ErrRequest)
	case req.Dir == "":
		return "", nil, fmt.Errorf("audio: request has no media directory: %w", ErrRequest)
	case req.Concurrency < 1:
		return "", nil, fmt.Errorf("audio: request's concurrency %d isn't 1 or more: %w", req.Concurrency, ErrRequest)
	}
	cutter, err := NewCutter(req)
	if err != nil {
		return "", nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("audio: source path %s: %w: %w", path, ErrRequest, err)
	}
	info, err := os.Stat(req.Dir)
	if err != nil {
		return "", nil, fmt.Errorf("audio: media directory: %w: %w", ErrRequest, err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("audio: media directory %s isn't a directory: %w", req.Dir, ErrRequest)
	}
	return abs, cutter, nil
}

type cut struct {
	file WindowFile
	tmp  string
}

func (s *Splitter) windows(ctx context.Context, path string, req Request, src Source, cutter *Cutter, yield func(WindowFile, error) bool) {
	work, cancel := context.WithCancel(ctx)
	cuts := make(chan cut, req.Concurrency)
	c := &cutting{work: work, cancel: cancel, cuts: cuts, slots: make(chan struct{}, req.Concurrency), dir: req.Dir, cutter: cutter}
	go func() {
		defer close(cuts)
		stderr := &cli.LineWriter{}
		err := s.ffmpeg.Run(work, tool.Invocation{Args: decodeArgs(path, src), Dir: filepath.Dir(path), Stdout: c, Stderr: stderr})
		stderr.Flush()
		switch {
		case c.failure != nil:
		case err != nil && stderr.Last() != "":
			c.failure = fmt.Errorf("audio: ffmpeg decoding %s: %w: %s", path, err, stderr.Last())
		case err != nil:
			c.failure = fmt.Errorf("audio: ffmpeg decoding %s: %w", path, err)
		default:
			pieces, err := cutter.End()
			if err != nil {
				c.failure = fmt.Errorf("audio: ffmpeg decoding %s: %w", path, err)
				return
			}
			plan := cutter.Plan()
			plan.Source = src
			if d := plan.Duration - src.Duration; d > FrameLength || d < -FrameLength {
				plan.Probed = src.Duration
			}
			c.hand(pieces, &plan)
		}
	}()
	defer func() {
		cancel()
		for w := range cuts {
			removeTemp(w.tmp)
		}
	}()
	for {
		var w cut
		var ok bool
		select {
		case w, ok = <-cuts:
		case <-ctx.Done():
		}
		if err := ctx.Err(); err != nil {
			if ok {
				removeTemp(w.tmp)
			}
			yield(WindowFile{}, err)
			return
		}
		if !ok {
			break
		}
		name := filepath.Join(req.Dir, w.file.SHA256+".wav")
		if err := os.Rename(w.tmp, name); err != nil {
			removeTemp(w.tmp)
			yield(WindowFile{}, fmt.Errorf("audio: window %d, %v: %w", w.file.Index, w.file.Window.Span, err))
			return
		}
		w.file.Path = name
		if !yield(w.file, nil) {
			return
		}
		<-c.slots
	}
	if c.failure != nil {
		yield(WindowFile{}, c.failure)
	}
}

type cutting struct {
	work    context.Context
	cancel  context.CancelFunc
	cuts    chan<- cut
	slots   chan struct{}
	dir     string
	cutter  *Cutter
	failure error
	done    bool
}

func (c *cutting) Write(p []byte) (int, error) {
	if c.done {
		return len(p), nil
	}
	pieces, err := c.cutter.Feed(p)
	if err != nil {
		c.stop(err)
		return len(p), nil
	}
	c.hand(pieces, nil)
	return len(p), nil
}

func (c *cutting) stop(err error) {
	c.done = true
	c.failure = err
	c.cancel()
}

func (c *cutting) hand(pieces []Piece, plan *Plan) {
	for _, p := range pieces {
		if c.done {
			return
		}
		select {
		case c.slots <- struct{}{}:
		case <-c.work.Done():
			c.done = true
			return
		}
		wav := WAV(p.PCM)
		sum := sha256.Sum256(wav)
		file := WindowFile{
			Index: p.Index, Window: p.Window, SeamBefore: p.SeamBefore, SeamAfter: p.SeamAfter, Threshold: p.Threshold,
			SHA256: hex.EncodeToString(sum[:]), Samples: len(p.PCM) / 2,
		}
		if p.Window.Cut == CutEnd {
			file.Plan = plan
		}
		tmp, err := cli.WriteTemp(c.dir, ".window-*.wav", wav)
		if err != nil {
			c.stop(fmt.Errorf("audio: window %d, %v: %w", p.Index, p.Window.Span, err))
			return
		}
		select {
		case c.cuts <- cut{file: file, tmp: tmp}:
		case <-c.work.Done():
			removeTemp(tmp)
			c.done = true
			return
		}
	}
}

func removeTemp(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// Sample returns the index of the sample at SampleRate nearest d, without overflow for any
// Duration: a window of span s holds the samples from Sample(s.Start) up to but not including
// Sample(s.End).
func Sample(d time.Duration) int64 {
	return int64(d/time.Second)*SampleRate + (int64(d%time.Second)*SampleRate+int64(time.Second)/2)/int64(time.Second)
}

// WAV returns pcm, mono 16-bit little-endian samples at SampleRate, as a WAV file: the canonical
// 44-byte header, a RIFF chunk holding a 16-byte PCM fmt chunk and the data chunk, then the
// samples. It is the header ffmpeg's own WAV muxer writes with -bitexact, which leaves out its
// encoder tag, and every window is written through it, so the same samples are the same bytes
// whichever package cut them.
func WAV(pcm []byte) []byte {
	const (
		formatPCM  = 1
		channels   = 1
		bits       = 16
		blockAlign = channels * bits / 8
	)
	b := make([]byte, 0, wavHeader+len(pcm))
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(wavHeader-8+len(pcm)))
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, formatPCM)
	b = binary.LittleEndian.AppendUint16(b, channels)
	b = binary.LittleEndian.AppendUint32(b, SampleRate)
	b = binary.LittleEndian.AppendUint32(b, SampleRate*blockAlign)
	b = binary.LittleEndian.AppendUint16(b, blockAlign)
	b = binary.LittleEndian.AppendUint16(b, bits)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(pcm)))
	return append(b, pcm...)
}
