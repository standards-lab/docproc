package media

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// Events returns the stream of the events of the file at path, as req says: one ffmpeg run reads
// it, and each window of its audio and each frame of its video it takes is yielded as an Event as
// soon as it is complete. The stream is lazy: nothing runs, not even the request's validation,
// until it is ranged over.
//
// Ranging over it validates path and req, an invalid one an error wrapping ErrRequest. It probes
// the file through ffprobe for its first audio stream and first video stream, an attached picture
// not counted, and refuses one with neither, with ErrNoMedia, before ffmpeg runs. The same probe
// reads the file's metadata record, a metadata.Media of its container and streams, which the
// stream yields first, as an Event whose Metadata is set. Then one ffmpeg run reads the file,
// allowed only the file protocol, and writes:
//
//   - its audio stream, resampled to 16 kHz, audio.SampleRate, downmixed to mono, as 16-bit PCM on
//     its stdout, as audio's Splitter decodes it. audio's stream cutter, an audio.Cutter of req's
//     windowing fields, cuts the PCM into windows as it comes: a window is cut as soon as the
//     stream passes its limit, inside a silence or not, or ends, its samples the stream's from
//     audio.Sample of its start up to but not including its end's, written through audio.WAV; a
//     window is the same bytes audio's Splitter.Windows cuts under the same fields.
//   - the frames of its video stream that select takes: the first, each whose scene score is
//     past req.SceneThreshold, and each req.MaxGap or more after the last taken. Each is scaled to
//     fit req.MaxSide, when set, converted to RGB, and written as PNG into a hidden directory the
//     run makes in the file's directory, through the image2 muxer's atomic writing, so a PNG is
//     whole once it has its name.
//   - each taken frame's time and scene score to its stderr, through the metadata filter, which
//     the stream reads as they come. A frame's reason is ReasonFirst for the first; ReasonScene
//     for one whose score is past the threshold; and ReasonInterval for the others, taken for the
//     gap. A frame both past the threshold and past the gap is a scene change.
//
// Windows are yielded in plan order and frames in source-time order, each as soon as it is
// complete: a window once cut and written, a frame once its PNG is whole. The two interleave as
// they complete. A file with no video stream yields windows only, and one with no audio stream
// frames only. Each window and frame is written to the media directory as a hidden temporary file
// and renamed to its SHA-256 name just before it is yielded, so the media directory holds only
// whole files, and only the ones the stream yielded.
//
// A refused request or a failed probe yields one error with a zero Event, and the stream ends.
// When ctx ends, ffmpeg is stopped, through either runner, and the stream yields ctx's error once,
// with a zero Event, and ends. Breaking out of the range stops the run, and the loop returns once
// it has ended. A run that fails, or a window or frame the stream can't write, ends the stream
// with its error after the events already complete. In each case the windows and frames not
// yielded are removed, and so is the run's directory of frames.
func (s *Streamer) Events(ctx context.Context, path string, req Request) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		c, err := req.check(path)
		if err != nil {
			yield(Event{}, err)
			return
		}
		if err := ctx.Err(); err != nil {
			yield(Event{}, err)
			return
		}
		st, err := s.probe(ctx, c.path)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				err = cerr
			}
			yield(Event{}, err)
			return
		}
		md := st.metadata
		if !yield(Event{Metadata: &md}, nil) {
			return
		}
		s.run(ctx, c, st, yield)
	}
}

const framePattern = "frame-%08d.png"

// See docs/api.md, "Running the tools".
func args(c checked, st streams, scratch string) []string {
	a := []string{
		"-nostdin", "-hide_banner", "-nostats", "-loglevel", "info", "-threads:a", "1",
		"-flags:a", "+bitexact", "-flags:v", "+bitexact", "-protocol_whitelist", fileProtocols, "-i", c.path,
	}
	if st.audio != "" {
		a = append(a,
			"-map", st.audio,
			"-af", fmt.Sprintf("aresample=%d", audio.SampleRate),
			"-ac", "1", "-ar", strconv.Itoa(audio.SampleRate), "-c:a", "pcm_s16le",
			"-flags:a", "+bitexact", "-fflags", "+bitexact", "-f", "s16le", "pipe:1",
		)
	}
	if st.video != "" {
		a = append(a,
			"-map", st.video,
			"-vf", videoFilter(c),
			"-fps_mode", "passthrough", "-c:v", "png", "-threads:v", "1", "-flags:v", "+bitexact", "-fflags", "+bitexact",
			"-f", "image2", "-start_number", "0", "-atomic_writing", "1",
			filepath.Join(scratch, framePattern),
		)
	}
	return a
}

func videoFilter(c checked) string {
	sel := fmt.Sprintf("select='eq(n,0)+gt(scene,%s)+gte(t-prev_selected_t,%s)'",
		strconv.FormatFloat(c.scene, 'f', -1, 64), strconv.FormatFloat(c.gap.Seconds(), 'f', -1, 64))
	scale := "scale=flags=bicubic+accurate_rnd+full_chroma_int+bitexact"
	if c.maxSide > 0 {
		side := strconv.Itoa(c.maxSide)
		scale = "scale=w='min(iw," + side + ")':h='min(ih," + side + ")':force_original_aspect_ratio=decrease:" +
			"flags=bicubic+accurate_rnd+full_chroma_int+bitexact"
	}
	return sel + ",settb=AVTB,metadata=mode=print:key=lavfi.scene_score," + scale + ",format=rgb24"
}

type item struct {
	event Event
	tmp   string
}

func (s *Streamer) run(ctx context.Context, c checked, st streams, yield func(Event, error) bool) {
	dir := filepath.Dir(c.path)
	scratch, err := os.MkdirTemp(dir, ".media-*")
	if err != nil {
		yield(Event{}, fmt.Errorf("media: the run's directory of frames: %w", err))
		return
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	work, cancel := context.WithCancel(ctx)
	items := make(chan item)
	r := &runner{work: work, cancel: cancel, items: items, dir: c.dir}
	var cw *cutWriter
	if st.audio != "" {
		cutter, _ := audio.NewCutter(c.audio)
		cw = &cutWriter{r: r, cutter: cutter}
	}
	fw := &frameWatcher{r: r, scratch: scratch, scene: c.scene, gap: c.gap, wake: make(chan struct{}, 1), ended: make(chan struct{})}
	var wg sync.WaitGroup
	if st.video != "" {
		wg.Go(fw.watch)
	} else {
		close(fw.ended)
	}
	go func() {
		defer close(items)
		err := s.ffmpeg.Run(work, tool.Invocation{Args: args(c, st, scratch), Dir: dir, Stdout: cw.writer(), Stderr: fw.stderr()})
		fw.lines.Flush()
		if err == nil && fw.lines.Err() != nil {
			err = fmt.Errorf("its report of a frame: %w", fw.lines.Err())
		}
		switch {
		case err != nil:
			if refused := fw.refused; refused != "" {
				err = fmt.Errorf("%w: %s", err, refused)
			}
			if last := fw.lines.Last(); last != "" {
				err = fmt.Errorf("%w: %s", err, last)
			}
			r.note(fmt.Errorf("media: ffmpeg reading %s: %w", c.path, err))
		case cw != nil:
			cw.end()
		}
		if st.video != "" {
			fw.end(err == nil)
		}
		wg.Wait()
	}()
	defer func() {
		cancel()
		for it := range items {
			removeTemp(it.tmp)
		}
	}()
	for {
		var it item
		var ok bool
		select {
		case it, ok = <-items:
		case <-ctx.Done():
		}
		if err := ctx.Err(); err != nil {
			if ok {
				removeTemp(it.tmp)
			}
			yield(Event{}, err)
			return
		}
		if !ok {
			break
		}
		if err := r.publish(&it); err != nil {
			yield(Event{}, err)
			return
		}
		if !yield(it.event, nil) {
			return
		}
	}
	if err := r.err(); err != nil {
		yield(Event{}, err)
	}
}

type runner struct {
	work   context.Context
	cancel context.CancelFunc
	items  chan<- item
	dir    string

	mu      sync.Mutex
	failure error
}

func (r *runner) send(it item) bool {
	select {
	case r.items <- it:
		return true
	case <-r.work.Done():
		removeTemp(it.tmp)
		return false
	}
}

func (r *runner) fail(err error) {
	r.note(err)
	r.cancel()
}

func (r *runner) note(err error) {
	r.mu.Lock()
	if r.failure == nil {
		r.failure = err
	}
	r.mu.Unlock()
}

func (r *runner) err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failure
}

func (r *runner) publish(it *item) error {
	var path *string
	var name string
	switch {
	case it.event.Window != nil:
		path, name = &it.event.Window.Path, it.event.Window.SHA256+".wav"
	case it.event.Frame != nil:
		path, name = &it.event.Frame.Path, it.event.Frame.SHA256+".png"
	}
	full := filepath.Join(r.dir, name)
	if err := os.Rename(it.tmp, full); err != nil {
		removeTemp(it.tmp)
		return fmt.Errorf("media: %s: %w", name, err)
	}
	*path = full
	return nil
}

func removeTemp(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

type cutWriter struct {
	r      *runner
	cutter *audio.Cutter
	done   bool
}

func (w *cutWriter) writer() io.Writer {
	if w == nil {
		return nil
	}
	return w
}

func (w *cutWriter) Write(p []byte) (int, error) {
	if w.done {
		return len(p), nil
	}
	pieces, err := w.cutter.Feed(p)
	if err != nil {
		w.stop(err)
		return len(p), nil
	}
	w.send(pieces)
	return len(p), nil
}

func (w *cutWriter) stop(err error) {
	w.done = true
	w.r.fail(fmt.Errorf("media: %w", err))
}

func (w *cutWriter) end() {
	if w.done {
		return
	}
	pieces, err := w.cutter.End()
	if err != nil {
		w.stop(fmt.Errorf("ffmpeg's audio stream: %w", err))
		return
	}
	w.send(pieces)
	w.done = true
}

func (w *cutWriter) send(pieces []audio.Piece) {
	for _, p := range pieces {
		if w.done {
			return
		}
		wav := audio.WAV(p.PCM)
		sum := sha256.Sum256(wav)
		ev := &Window{
			Index: p.Index, Span: p.Window.Span, Cut: p.Window.Cut, SeamBefore: p.SeamBefore, SeamAfter: p.SeamAfter,
			Samples: len(p.PCM) / 2, Threshold: p.Threshold, SHA256: hex.EncodeToString(sum[:]),
		}
		tmp, err := cli.WriteTemp(w.r.dir, ".media-*.wav", wav)
		if err != nil {
			w.stop(fmt.Errorf("window %d, %v: %w", p.Index, p.Window.Span, err))
			return
		}
		if !w.r.send(item{event: Event{Window: ev, Time: p.Window.Start}, tmp: tmp}) {
			w.done = true
			return
		}
	}
}

type announced struct {
	index  int
	reason Reason
	time   time.Duration
}

var (
	frameReport     = regexp.MustCompile(`^\[Parsed_metadata_\d+ @ [^\]]*\] frame:\s*\d+\s+pts:\s*(-?\d+)\s`)
	sceneScore      = regexp.MustCompile(`^\[Parsed_metadata_\d+ @ [^\]]*\] lavfi\.scene_score=(\S+)$`)
	refusedProtocol = regexp.MustCompile(`Protocol '[^']*' not on whitelist '[^']*'`)
)

type frameWatcher struct {
	r       *runner
	scratch string
	scene   float64
	gap     time.Duration
	lines   cli.LineWriter

	pts      int64
	count    int
	previous time.Duration
	refused  string

	mu      sync.Mutex
	pending []announced
	ok      bool
	wake    chan struct{}
	ended   chan struct{}
}

func (f *frameWatcher) stderr() *cli.LineWriter {
	f.pts = -1
	f.lines.Line = f.line
	return &f.lines
}

func (f *frameWatcher) line(s string) error {
	if m := refusedProtocol.FindString(s); m != "" && f.refused == "" {
		f.refused = m
		return nil
	}
	if m := frameReport.FindStringSubmatch(s); m != nil {
		pts, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return err
		}
		f.pts = pts
		return nil
	}
	m := sceneScore.FindStringSubmatch(s)
	if m == nil || f.pts < 0 {
		return nil
	}
	score, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return err
	}
	t := time.Duration(f.pts) * time.Microsecond
	f.pts = -1
	a := announced{index: f.count, time: t}
	switch {
	case f.count == 0:
		a.reason = ReasonFirst
	case score > f.scene:
		a.reason = ReasonScene
	case t-f.previous >= f.gap:
		a.reason = ReasonInterval
	default:
		a.reason = ReasonScene
	}
	f.count++
	f.previous = t
	f.mu.Lock()
	f.pending = append(f.pending, a)
	f.mu.Unlock()
	select {
	case f.wake <- struct{}{}:
	default:
	}
	return nil
}

func (f *frameWatcher) end(ok bool) {
	f.mu.Lock()
	f.ok = ok
	f.mu.Unlock()
	close(f.ended)
}

const pollInterval = 10 * time.Millisecond

func (f *frameWatcher) watch() {
	for {
		f.mu.Lock()
		next, have := announced{}, len(f.pending) > 0
		if have {
			next = f.pending[0]
		}
		f.mu.Unlock()
		ended := false
		select {
		case <-f.ended:
			ended = true
		default:
		}
		if !have {
			if ended {
				return
			}
			select {
			case <-f.wake:
			case <-f.ended:
			case <-f.r.work.Done():
				return
			}
			continue
		}
		path := filepath.Join(f.scratch, fmt.Sprintf(framePattern, next.index))
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			_ = os.Remove(path)
			if !f.handOver(next, b) {
				return
			}
			f.mu.Lock()
			f.pending = f.pending[1:]
			f.mu.Unlock()
			continue
		case !errors.Is(err, os.ErrNotExist):
			f.r.fail(fmt.Errorf("media: frame %d: %w", next.index, err))
			return
		case ended:
			f.mu.Lock()
			ok := f.ok
			f.mu.Unlock()
			if ok {
				f.r.fail(fmt.Errorf("media: ffmpeg wrote no PNG for frame %d, at %v", next.index, next.time))
			}
			return
		}
		select {
		case <-time.After(pollInterval):
		case <-f.ended:
		case <-f.r.work.Done():
			return
		}
	}
}

func (f *frameWatcher) handOver(a announced, b []byte) bool {
	width, height, err := pngSize(b)
	if err != nil {
		f.r.fail(fmt.Errorf("media: frame %d, at %v: %w", a.index, a.time, err))
		return false
	}
	sum := sha256.Sum256(b)
	tmp, err := cli.WriteTemp(f.r.dir, ".media-*.png", b)
	if err != nil {
		f.r.fail(fmt.Errorf("media: frame %d, at %v: %w", a.index, a.time, err))
		return false
	}
	fr := &Frame{Index: a.index, Reason: a.reason, Width: width, Height: height, SHA256: hex.EncodeToString(sum[:])}
	return f.r.send(item{event: Event{Frame: fr, Time: a.time}, tmp: tmp})
}

func pngSize(b []byte) (width, height int, err error) {
	if len(b) < 24 || string(b[:8]) != route.PNGSignature || string(b[12:16]) != "IHDR" {
		return 0, 0, errors.New("ffmpeg's frame isn't a PNG")
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])), nil
}
