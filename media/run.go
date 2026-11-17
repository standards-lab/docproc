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
// not counted, and refuses one with neither, with ErrNoMedia, before ffmpeg runs. Then one ffmpeg
// run reads the file, allowed only the file protocol, and writes:
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
		s.run(ctx, c, st, yield)
	}
}

// framePattern is the name the image2 muxer gives each frame in the run's directory, numbered from
// 0 in the order the frames are taken.
const framePattern = "frame-%08d.png"

// args returns ffmpeg's arguments for a run over the file of c with the streams st, writing its
// frames into scratch: one input, allowed only the file protocol, and an output for each stream st
// takes, the audio's PCM to stdout and the video's frames to PNG files.
func args(c checked, st streams, scratch string) []string {
	// The audio decodes in one thread, which is cheap, and threads change no output. Bit-exact
	// decoding, so the same file gives the same samples and pixels whatever the build's CPU paths,
	// through either runner.
	a := []string{
		"-nostdin", "-hide_banner", "-nostats", "-loglevel", "info", "-threads:a", "1",
		"-flags:a", "+bitexact", "-flags:v", "+bitexact", "-protocol_whitelist", fileProtocols, "-i", c.path,
	}
	if st.audio != "" {
		// audio's Splitter decodes with these arguments, so the windows are its windows.
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
			// One encoder thread: with frame threads, the PNG encoder holds a frame until the
			// frames after it fill its threads.
			"-fps_mode", "passthrough", "-c:v", "png", "-threads:v", "1", "-flags:v", "+bitexact", "-fflags", "+bitexact",
			"-f", "image2", "-start_number", "0", "-atomic_writing", "1",
			filepath.Join(scratch, framePattern),
		)
	}
	return a
}

// videoFilter returns the run's video filter chain. select takes the first frame, each whose scene
// score is past the threshold, and each the gap or more after the last it took; settb puts the
// frames' timestamps in microseconds, so the report's pts is the frame's time exactly; metadata
// prints each taken frame's pts and scene score to stderr; scale fits the frame to the maximum
// side, never enlarging it, bit-exact, and format makes it RGB for the PNG.
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

// item is one event the run hands the stream: the event, its Path empty, and the temporary file in
// the media directory holding its window or frame.
type item struct {
	event Event
	tmp   string
}

// run reads the file of c, its streams st, through one ffmpeg run, yielding each event as it
// completes and then the run's error, if any. It returns once the run and its goroutines have
// ended, and every file it didn't yield is removed.
//
// The run's goroutine runs ffmpeg; the PCM on its stdout is cut into windows by a cutWriter, over
// audio's stream cutter, as it is written, and the frames reported on its stderr are announced to
// a frameWatcher, whose goroutine waits for each frame's PNG and writes it into the media
// directory. Each hands its items to one channel, which the stream reads; a writer that waits on
// it holds ffmpeg back, so a slow consumer paces the run.
//
// The run works in the file's directory, which the container runner mounts, and makes its
// directory of frames there, since ffmpeg reads the file by its path and the runner mounts one
// directory.
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
		// The request was checked: the cutter doesn't refuse it.
		cutter, _ := audio.NewCutter(c.audio)
		cw = &cutWriter{r: r, cutter: cutter}
	}
	fw := &frameWatcher{r: r, scratch: scratch, scene: c.scene, gap: c.gap, wake: make(chan struct{}, 1), ended: make(chan struct{})}
	var wg sync.WaitGroup
	if st.video != "" {
		wg.Go(fw.watch)
	} else {
		// No video, no frames to watch for.
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
			// ffmpeg's refusal of a protocol the run doesn't allow comes lines before its last,
			// which says only that the input failed.
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
		// Every goroutine sends its items or removes them once work has ended, and the channel
		// closes once they have all ended; an item still on its way is removed here.
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

// runner is what a run's writers share: the run's context, ended by a failure or the stream's end,
// the channel they hand items to, the media directory, and the run's first failure.
type runner struct {
	work   context.Context
	cancel context.CancelFunc
	items  chan<- item
	dir    string

	mu      sync.Mutex
	failure error
}

// send hands it to the stream, or removes its file and reports false once the run has ended.
func (r *runner) send(it item) bool {
	select {
	case r.items <- it:
		return true
	case <-r.work.Done():
		removeTemp(it.tmp)
		return false
	}
}

// fail records the run's first failure and ends the run, stopping ffmpeg.
func (r *runner) fail(err error) {
	r.note(err)
	r.cancel()
}

// note records the run's first failure once ffmpeg has exited, without ending the run, so the
// frames whose PNG is whole are still handed to the stream before its error.
func (r *runner) note(err error) {
	r.mu.Lock()
	if r.failure == nil {
		r.failure = err
	}
	r.mu.Unlock()
}

// err returns the run's first failure.
func (r *runner) err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failure
}

// publish renames the item's temporary file to its SHA-256 name in the media directory and sets
// its event's Path.
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

// removeTemp removes a temporary file, if there is one. The file is the package's own, so a
// failure to remove it leaves a hidden file and nothing else.
func removeTemp(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// cutWriter cuts the PCM ffmpeg writes to its stdout into windows as it comes, through audio's
// stream cutter, and writes each window the cutter cuts. Its writes never fail; a window it can't
// write fails the run.
type cutWriter struct {
	r      *runner
	cutter *audio.Cutter
	done   bool
}

// writer returns w as ffmpeg's stdout; nil, discarding it, when w is nil, as for a file with no
// audio.
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

// stop fails the run with err and takes nothing more.
func (w *cutWriter) stop(err error) {
	w.done = true
	w.r.fail(fmt.Errorf("media: %w", err))
}

// end ends the stream once ffmpeg has exited: the cutter's last windows. A stream with no samples
// is an error, since its audio stream gave none.
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

// send writes pieces, in order, and hands each to the stream as a window.
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

// announced is a frame ffmpeg reported on its stderr, its PNG not yet seen whole.
type announced struct {
	index  int
	reason Reason
	time   time.Duration
}

// frameReport and sceneScore match the metadata filter's report of a frame: its line of the
// frame's count and pts, in microseconds after settb, and then its line of the scene score.
var (
	frameReport = regexp.MustCompile(`^\[Parsed_metadata_\d+ @ [^\]]*\] frame:\s*\d+\s+pts:\s*(-?\d+)\s`)
	sceneScore  = regexp.MustCompile(`^\[Parsed_metadata_\d+ @ [^\]]*\] lavfi\.scene_score=(\S+)$`)
	// refusedProtocol matches ffmpeg's refusal of a protocol its -protocol_whitelist doesn't hold.
	refusedProtocol = regexp.MustCompile(`Protocol '[^']*' not on whitelist '[^']*'`)
)

// frameWatcher reads ffmpeg's stderr for the frames it takes, and waits for each one's PNG in the
// run's directory, scratch, writing it into the media directory and handing it to the stream in
// the order taken.
type frameWatcher struct {
	r       *runner
	scratch string
	scene   float64
	gap     time.Duration
	lines   cli.LineWriter

	// pts is the pts of the frame whose score line is next, -1 when none; count the frames
	// announced, and previous the last one's time; and refused ffmpeg's first refusal of a
	// protocol the run doesn't allow, such as a playlist's, empty when none. They are the stderr
	// goroutine's alone until ffmpeg has exited.
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

// stderr returns the writer ffmpeg's stderr goes to, which reads the frames' reports.
func (f *frameWatcher) stderr() *cli.LineWriter {
	f.pts = -1
	f.lines.Line = f.line
	return &f.lines
}

// line reads one line of ffmpeg's stderr: a frame's pts, or its score, which announces it; or its
// refusal of a protocol.
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
		// select took it past the threshold, which the score's six printed decimals round away.
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

// end tells the watcher ffmpeg has exited, ok when it succeeded: every frame it announced has its
// PNG, and one that hasn't is an error. After a failed run, the frames whose PNG is whole are
// still yielded, and the rest dropped.
func (f *frameWatcher) end(ok bool) {
	f.mu.Lock()
	f.ok = ok
	f.mu.Unlock()
	close(f.ended)
}

// pollInterval is how often the watcher looks for a frame's PNG while ffmpeg writes it.
const pollInterval = 10 * time.Millisecond

// watch waits for each announced frame's PNG in turn and hands it to the stream, until ffmpeg has
// exited and every frame is handed over, or the run ends.
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

// handOver writes a's PNG, b, into the media directory and hands it to the stream; false when the
// run has ended.
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

// pngSize returns a PNG's width and height, from its IHDR chunk, which follows its signature.
func pngSize(b []byte) (width, height int, err error) {
	if len(b) < 24 || string(b[:8]) != route.PNGSignature || string(b[12:16]) != "IHDR" {
		return 0, 0, errors.New("ffmpeg's frame isn't a PNG")
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])), nil
}
