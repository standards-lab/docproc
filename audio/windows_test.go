//go:build linux

package audio_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
)

// gainProbe is ffprobe's report of speech-gain.wav: 75.23 s of 16 kHz mono 16-bit PCM.
const gainProbe = `{"streams":[` +
	`{"index":0,"codec_name":"pcm_s16le","codec_type":"audio","sample_rate":"16000","channels":1,"duration":"75.230000"}],` +
	`"format":{"duration":"75.230000"}}`

// gainDuration is speech-gain.wav's duration, its samples at 16 kHz.
const gainDuration = 75230 * time.Millisecond

// probeOf returns ffprobe's report of a source of 16 kHz mono 16-bit PCM d long, its audio the
// first stream.
func probeOf(d time.Duration) string {
	return fmt.Sprintf(`{"streams":[{"index":0,"codec_name":"pcm_s16le","codec_type":"audio","sample_rate":"16000","channels":1}],"format":{"duration":"%.6f"}}`,
		d.Seconds())
}

// windows ranges over the windows of the source at path, returning those yielded and the error
// that ended the stream, if any; a window yielded with an error fails the test.
func windows(s *audio.Splitter, ctx context.Context, path string, req audio.Request) ([]audio.WindowFile, error) {
	var out []audio.WindowFile
	for w, err := range s.Windows(ctx, path, req) {
		if err != nil {
			return out, err
		}
		out = append(out, w)
	}
	return out, nil
}

// collect is windows, failing the test on an error.
func collect(t *testing.T, s *audio.Splitter, ctx context.Context, path string, req audio.Request) []audio.WindowFile {
	t.Helper()
	ws, err := windows(s, ctx, path, req)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// checkFile checks that w's file is pcm, as WAV, named by its SHA-256 in dir.
func checkFile(t *testing.T, w audio.WindowFile, pcm []byte, dir string) {
	t.Helper()
	b, err := os.ReadFile(w.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != w.SHA256 || w.Path != filepath.Join(dir, got+".wav") {
		t.Errorf("window %d at %s, SHA-256 %s; its bytes' SHA-256 is %s, in %s", w.Index, w.Path, w.SHA256, got, dir)
	}
	if w.Samples != len(pcm)/2 || w.Duration() != time.Duration(len(pcm)/2)*time.Second/audio.SampleRate {
		t.Errorf("window %d holds %d samples, %v; want %d", w.Index, w.Samples, w.Duration(), len(pcm)/2)
	}
	checkWAV(t, b, pcm)
}

// checkWAV checks that b is pcm, 16 kHz mono 16-bit, under the canonical 44-byte WAV header.
func checkWAV(t *testing.T, b, pcm []byte) {
	t.Helper()
	header := []byte("RIFF")
	header = binary.LittleEndian.AppendUint32(header, uint32(36+len(pcm)))
	header = append(header, "WAVEfmt "...)
	for _, v := range []uint32{16, 1 | 1<<16, 16000, 32000, 2 | 16<<16} {
		header = binary.LittleEndian.AppendUint32(header, v)
	}
	header = append(header, "data"...)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(pcm)))
	if len(b) < 44 || string(b[:44]) != string(header) {
		t.Errorf("header % x, want % x", b[:min(len(b), 44)], header)
	}
	if len(b) != 44+len(pcm) || string(b[44:]) != string(pcm) {
		t.Errorf("%d bytes of samples, not the window's %d", len(b)-44, len(pcm))
	}
}

// files returns the names in dir, sorted.
func files(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// names returns the base names of ws' files, sorted.
func names(ws []audio.WindowFile) []string {
	var out []string
	for _, w := range ws {
		out = append(out, filepath.Base(w.Path))
	}
	slices.Sort(out)
	return out
}

// windowPCM returns the samples of the window w of pcm, the stream's from Sample of its start up to
// but not including its end's.
func windowPCM(pcm []byte, w audio.Window) []byte {
	return pcm[2*audio.Sample(w.Start) : 2*audio.Sample(w.End)]
}

// synthPCM returns the stand-in's synthetic source of n samples.
func synthPCM(n int) []byte {
	var out []byte
	for i := range n {
		out = binary.LittleEndian.AppendUint16(out, uint16(synthSample(i)))
	}
	return out
}

// TestWindows decodes speech-gain.wav once, the stand-in ffmpeg writing its samples in uneven
// chunks, under several requests: the windows are the incremental path's over the same samples,
// a Tracker and a Planner fed them as a stream, and PlanWindows' of the tracker's silences; each is
// its span's samples, as WAV, named by its SHA-256; and the last carries the plan, which ends at
// the decoded samples, ffprobe's duration matching them.
func TestWindows(t *testing.T) {
	t.Parallel()
	decode, err := filepath.Abs(filepath.Join("testdata", "speech-gain.wav"))
	if err != nil {
		t.Fatal(err)
	}
	pcm := samplesOf(t, "speech-gain.wav")
	for name, req := range map[string]audio.Request{
		"defaults": {},
		"settings": {Percentile: 20, FloorWindow: 10 * time.Second, Margin: 9, MinSilence: 400 * time.Millisecond, Overlap: time.Second},
		"fixed":    {Threshold: -46.2},
		"limit":    {Limit: 12 * time.Second, Overlap: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, st := standinSplitter(t)
			st.set(func(c *settings) { c.Probe, c.Decode = gainProbe, decode })
			dir := t.TempDir()
			req.Dir, req.Concurrency = dir, 2
			ws := collect(t, s, t.Context(), "speech-gain.wav", req)
			want := streamPlan(t, pcm, req, 1<<15, gainDuration)
			if len(ws) != len(want) || len(ws) < 3 {
				t.Fatalf("%d windows, want the incremental path's %d, at least 3", len(ws), len(want))
			}
			silences := feedTracker(t, pcm, req, len(pcm))
			var spans []audio.Span
			for _, x := range silences {
				spans = append(spans, x.Span)
			}
			planned, err := audio.PlanWindows(gainDuration, spans, req)
			if err != nil {
				t.Fatal(err)
			}
			for i, w := range ws {
				if w.Index != i || w.Window != want[i] {
					t.Errorf("window %d, %v; want %d, %v", w.Index, w.Window, i, want[i])
				}
				checkFile(t, w, windowPCM(pcm, w.Window), dir)
				if (i > 0) != (w.SeamBefore != audio.Span{}) || (i > 0 && w.SeamBefore != planned.Seams[i-1]) {
					t.Errorf("window %d's seam before %v", i, w.SeamBefore)
				}
				if (i < len(ws)-1) != (w.SeamAfter != audio.Span{}) || (i < len(ws)-1 && w.SeamAfter != planned.Seams[i]) {
					t.Errorf("window %d's seam after %v", i, w.SeamAfter)
				}
				if w.Threshold >= 0 || (req.Threshold != 0 && w.Threshold != req.Threshold) {
					t.Errorf("window %d's threshold %v dB", i, w.Threshold)
				}
				if (w.Plan != nil) != (i == len(ws)-1) {
					t.Errorf("window %d's plan %v", i, w.Plan)
				}
			}
			if got := files(t, dir); !slices.Equal(got, names(ws)) {
				t.Errorf("media directory %q, want the windows %q", got, names(ws))
			}
			plan := ws[len(ws)-1].Plan
			if plan == nil {
				t.Fatal("no plan")
			}
			planned.Source = audio.Source{Duration: gainDuration, Stream: 0, Codec: "pcm_s16le", SampleRate: 16000, Channels: 1}
			planned.Silences = silences
			if !reflect.DeepEqual(*plan, planned) {
				t.Errorf("plan %+v, want PlanWindows' %+v", *plan, planned)
			}
			rs := st.runs()
			if got := programs(rs); !slices.Equal(got, []string{"ffprobe", "ffmpeg"}) {
				t.Fatalf("runs %q, want ffprobe, then ffmpeg once", got)
			}
			args := strings.Join(rs[1].Args, " ")
			abs, _ := filepath.Abs("speech-gain.wav")
			for _, want := range []string{"-i " + abs + " -map 0:0", "-af aresample=16000", "-ac 1 -ar 16000 -c:a pcm_s16le", "-f s16le -"} {
				if !strings.Contains(args, want) {
					t.Errorf("ffmpeg's arguments %q, without %q", args, want)
				}
			}
		})
	}
}

// TestWindowsProbed: a plan ends at the decoded samples, whatever ffprobe estimated, with
// ffprobe's duration beside it as Probed when the two differ by more than a frame, as a truncated
// file's do; within a frame, Probed is zero.
func TestWindowsProbed(t *testing.T) {
	t.Parallel()
	decode, err := filepath.Abs(filepath.Join("testdata", "speech-gain.wav"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		probed, want time.Duration
	}{
		{90 * time.Second, 90 * time.Second},
		{60 * time.Second, 60 * time.Second},
		{gainDuration + audio.FrameLength + time.Millisecond, gainDuration + audio.FrameLength + time.Millisecond},
		{gainDuration + audio.FrameLength, 0},
		{gainDuration - audio.FrameLength, 0},
	} {
		t.Run(tt.probed.String(), func(t *testing.T) {
			t.Parallel()
			s, st := standinSplitter(t)
			st.set(func(c *settings) { c.Probe, c.Decode = probeOf(tt.probed), decode })
			ws := collect(t, s, t.Context(), "speech-gain.wav", audio.Request{Dir: t.TempDir(), Concurrency: 1})
			last := ws[len(ws)-1]
			if last.Plan == nil || last.Plan.Duration != gainDuration || last.Plan.Probed != tt.want || last.Window.End != gainDuration {
				t.Errorf("plan %+v, last window %v; want %v decoded, %v probed", last.Plan, last.Window, gainDuration, tt.want)
			}
			if rec := last.Plan.Record(); rec.Duration != gainDuration.Seconds() || rec.ProbedDuration != tt.want.Seconds() {
				t.Errorf("record's durations %v and %v", rec.Duration, rec.ProbedDuration)
			}
		})
	}
}

// TestWindowsHeld: the decode waits while Concurrency windows are held, from each one's cut until
// the loop body returns from it, so the media directory never holds more than that many while the
// consumer holds the first; and it reaches that many. The windows are the synthetic source's, each
// cut at the limit under a fixed threshold no sample is under.
func TestWindowsHeld(t *testing.T) {
	t.Parallel()
	for _, concurrency := range []int{1, 3} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			t.Parallel()
			s, st := standinSplitter(t)
			const samples = 100 * audio.SampleRate
			st.set(func(c *settings) { c.Probe, c.Synth = probeOf(100*time.Second), samples })
			dir := t.TempDir()
			req := audio.Request{Dir: dir, Concurrency: concurrency, Limit: 10 * time.Second, Overlap: time.Second, Threshold: -100}
			var ws []audio.WindowFile
			for w, err := range s.Windows(t.Context(), "synth.wav", req) {
				if err != nil {
					t.Fatal(err)
				}
				ws = append(ws, w)
				if w.Index != 0 {
					continue
				}
				// Held in the loop body: the decode cuts until concurrency windows are held, and no
				// further.
				deadline := time.Now().Add(10 * time.Second)
				for len(files(t, dir)) < concurrency && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				time.Sleep(200 * time.Millisecond)
				if n := len(files(t, dir)); n != concurrency {
					t.Errorf("%d windows held while the first is, want %d", n, concurrency)
				}
			}
			pcm := synthPCM(samples)
			if len(ws) != 11 {
				t.Fatalf("%d windows, want 11", len(ws))
			}
			for i, w := range ws {
				if i < 10 && w.Window.Cut != audio.CutLimit || w.Threshold != -100 {
					t.Errorf("window %d, %v at %v dB; want cut at the limit, at -100", i, w.Window, w.Threshold)
				}
				checkFile(t, w, windowPCM(pcm, w.Window), dir)
			}
		})
	}
}

// TestWindowsEarlyStop: breaking out of the range after the first window stops the decode, and
// the media directory keeps that window alone.
func TestWindowsEarlyStop(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	st.set(func(c *settings) { c.Probe, c.Synth, c.PaceMS = probeOf(100*time.Second), 100*audio.SampleRate, 20 })
	dir := t.TempDir()
	req := audio.Request{Dir: dir, Concurrency: 2, Limit: 10 * time.Second, Overlap: time.Second, Threshold: -100}
	start := time.Now()
	var ws []audio.WindowFile
	for w, err := range s.Windows(t.Context(), "synth.wav", req) {
		if err != nil {
			t.Fatal(err)
		}
		ws = append(ws, w)
		break
	}
	// The paced stand-in takes 20 s to write the whole source, and 2 s to write the first window:
	// the bound sits between them, wide of both on a loaded machine.
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("the loop returned after %v, not stopping the decode", d)
	}
	if got := files(t, dir); len(ws) != 1 || !slices.Equal(got, names(ws)) {
		t.Errorf("media directory %q after %d windows, want the first alone", got, len(ws))
	}
}

// TestWindowsCancel: a context that ends while the decode runs is yielded once, after the windows
// already cut, and the media directory keeps only those yielded.
func TestWindowsCancel(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	st.set(func(c *settings) { c.Probe, c.Synth, c.PaceMS = probeOf(100*time.Second), 100*audio.SampleRate, 5 })
	dir := t.TempDir()
	req := audio.Request{Dir: dir, Concurrency: 2, Limit: 10 * time.Second, Overlap: time.Second, Threshold: -100}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var ws []audio.WindowFile
	var errs []error
	for w, err := range s.Windows(ctx, "synth.wav", req) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ws = append(ws, w)
		cancel()
	}
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) || len(ws) != 1 {
		t.Errorf("%d windows and errors %v; want one window, then context.Canceled once", len(ws), errs)
	}
	if got := files(t, dir); !slices.Equal(got, names(ws)) {
		t.Errorf("media directory %q, want %q", got, names(ws))
	}
}

// TestWindowsCancelledBeforeRuns: a context ended before the stream is ranged over runs nothing.
func TestWindowsCancelledBeforeRuns(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := windows(s, ctx, "speech.flac", audio.Request{Dir: t.TempDir(), Concurrency: 1}); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v, want context.Canceled", err)
	}
	if rs := st.runs(); len(rs) != 0 {
		t.Errorf("runs %q, want none", programs(rs))
	}
}

// TestWindowsFailsMidStream: a decode that fails partway yields the windows cut before it, then
// its error, carrying ffmpeg's last line; the media directory keeps the windows yielded.
func TestWindowsFailsMidStream(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	st.set(func(c *settings) {
		c.Probe, c.Synth = probeOf(100*time.Second), 100*audio.SampleRate
		c.Fail, c.FailAfter = "[aist#0:0/flac] Error while decoding: Cannot allocate memory\n", 2*25*audio.SampleRate
	})
	dir := t.TempDir()
	req := audio.Request{Dir: dir, Concurrency: 1, Limit: 10 * time.Second, Overlap: time.Second, Threshold: -100}
	ws, err := windows(s, t.Context(), "synth.wav", req)
	if err == nil || !strings.Contains(err.Error(), "Cannot allocate memory") {
		t.Errorf("err %v, want ffmpeg's", err)
	}
	// 25 s of the source fixes the windows whose limit it passes: 0-10 s and 9-19 s.
	if len(ws) != 2 {
		t.Errorf("%d windows before the error, want 2", len(ws))
	}
	if got := files(t, dir); !slices.Equal(got, names(ws)) {
		t.Errorf("media directory %q, want %q", got, names(ws))
	}
}

// TestWindowsNoSamples: a decode that writes no sample is an error, with no window.
func TestWindowsNoSamples(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	st.set(func(c *settings) { c.Probe = probeOf(time.Second) })
	dir := t.TempDir()
	ws, err := windows(s, t.Context(), "empty.wav", audio.Request{Dir: dir, Concurrency: 1})
	if err == nil || !strings.Contains(err.Error(), "no samples") || len(ws) != 0 {
		t.Errorf("%d windows, err %v; want none and no samples", len(ws), err)
	}
}

// cutAll cuts pcm with a Cutter of req, fed in chunks next(rest) bytes long, rest the bytes left,
// returning each piece, its PCM copied, and the plan.
func cutAll(t *testing.T, pcm []byte, req audio.Request, next func(rest int) int) ([]audio.Piece, audio.Plan) {
	t.Helper()
	c, err := audio.NewCutter(req)
	if err != nil {
		t.Fatal(err)
	}
	var out []audio.Piece
	keep := func(ps []audio.Piece, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			p.PCM = slices.Clone(p.PCM)
			out = append(out, p)
		}
	}
	for rest := pcm; len(rest) > 0; {
		n := min(next(len(rest)), len(rest))
		keep(c.Feed(rest[:n]))
		rest = rest[n:]
	}
	keep(c.End())
	return out, c.Plan()
}

// TestCutterChunking: a Cutter's pieces are the same however the stream is chunked, each its
// window's samples, and its plan is PlanWindows' of the stream's length and the tracker's
// silences.
func TestCutterChunking(t *testing.T) {
	t.Parallel()
	pcm := samplesOf(t, "speech-noise.wav")
	for name, req := range map[string]audio.Request{
		"defaults": {},
		"limit":    {Limit: 8 * time.Second, Overlap: 500 * time.Millisecond},
		"fixed":    {Threshold: -40},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, plan := cutAll(t, pcm, req, func(rest int) int { return rest })
			var spans []audio.Span
			for _, s := range plan.Silences {
				spans = append(spans, s.Span)
			}
			planned, err := audio.PlanWindows(time.Duration(len(pcm)/2)*time.Second/audio.SampleRate, spans, req)
			if err != nil {
				t.Fatal(err)
			}
			planned.Silences = plan.Silences
			if !reflect.DeepEqual(plan, planned) {
				t.Errorf("plan %+v, PlanWindows' %+v", plan, planned)
			}
			if len(want) != len(plan.Windows) {
				t.Fatalf("%d pieces, %d windows", len(want), len(plan.Windows))
			}
			for i, p := range want {
				if p.Index != i || p.Window != plan.Windows[i] || !slices.Equal(p.PCM, windowPCM(pcm, p.Window)) {
					t.Errorf("piece %d, %v, %d bytes; want window %d, %v", p.Index, p.Window, len(p.PCM), i, plan.Windows[i])
				}
			}
			for _, chunk := range []int{1, 321, 4097, 1 << 16} {
				got, again := cutAll(t, pcm, req, func(int) int { return chunk })
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(again, plan) {
					t.Errorf("in chunks of %d: the pieces or the plan differ", chunk)
				}
			}
		})
	}
}

// TestCutterRefuses: a Cutter refuses its request's windowing and tracker fields, and an End after
// End, or of a stream with no whole sample.
func TestCutterRefuses(t *testing.T) {
	t.Parallel()
	for name, req := range map[string]audio.Request{
		"negative limit":     {Limit: -time.Second},
		"positive threshold": {Threshold: 1},
	} {
		if _, err := audio.NewCutter(req); !errors.Is(err, audio.ErrRequest) {
			t.Errorf("%s: %v, want ErrRequest", name, err)
		}
	}
	c, err := audio.NewCutter(audio.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Feed([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if ps, err := c.End(); err == nil || len(ps) != 0 {
		t.Errorf("End of a stream of one byte: %v, %v", ps, err)
	}
	c, _ = audio.NewCutter(audio.Request{})
	if _, err := c.Feed(make([]byte, 320)); err != nil {
		t.Fatal(err)
	}
	if ps, err := c.End(); err != nil || len(ps) != 1 || ps[0].Window.End != audio.FrameLength {
		t.Errorf("End of a frame: %v, %v", ps, err)
	}
	if _, err := c.End(); !errors.Is(err, audio.ErrRequest) {
		t.Errorf("End after End: %v, want ErrRequest", err)
	}
}

// TestPlanRecord: a plan's record holds its times in seconds, its silences with their thresholds,
// and its windows by index and cut, ProbedDuration left out when it is zero.
func TestPlanRecord(t *testing.T) {
	t.Parallel()
	plan := audio.Plan{
		Source:   audio.Source{Duration: 49820 * time.Millisecond, Stream: 1, Codec: "flac", SampleRate: 16000, Channels: 1},
		Duration: 49820 * time.Millisecond, Limit: audio.DefaultWindowLimit, Overlap: audio.DefaultOverlap,
		Windows: []audio.Window{
			{Start: 0, End: 24910 * time.Millisecond, Cut: audio.CutSilence},
			{Start: 22910 * time.Millisecond, End: 49820 * time.Millisecond, Cut: audio.CutEnd},
		},
		Seams:    []audio.Span{{Start: 22910 * time.Millisecond, End: 24910 * time.Millisecond}},
		Silences: []audio.Silence{{Start: 24410 * time.Millisecond, End: 25410 * time.Millisecond, Threshold: -52.5}},
	}
	b, err := json.Marshal(plan.Record())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"duration":49.82,"limit":30,"overlap":2,"stream":1,"codec":"flac","sample_rate":16000,"channels":1,` +
		`"silences":[{"start":24.41,"end":25.41,"threshold":-52.5}],` +
		`"windows":[{"index":0,"start":0,"end":24.91,"cut":"silence"},{"index":1,"start":22.91,"end":49.82,"cut":"end"}],` +
		`"seams":[{"start":22.91,"end":24.91}]}`
	if string(b) != want {
		t.Errorf("record %s\nwant   %s", b, want)
	}
	plan.Probed = 50 * time.Second
	if b, _ := json.Marshal(plan.Record()); !strings.Contains(string(b), `"duration":49.82,"probed_duration":50,`) {
		t.Errorf("record with a probed duration %s", b)
	}
	if b, _ := json.Marshal(audio.Plan{Duration: time.Second}.Record()); !strings.Contains(string(b), `"silences":[],"windows":[],"seams":[]`) {
		t.Errorf("an empty plan's record %s", b)
	}
}

// v090Window is one line of testdata/windows-v0.9.0.txt: a window v0.9.0 cut under
// DetectorTracker.
type v090Window struct {
	window  audio.Window
	samples int
	sha256  string
}

// v090Windows returns testdata/windows-v0.9.0.txt's windows by fixture.
func v090Windows(t *testing.T) map[string][]v090Window {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "windows-v0.9.0.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]v090Window{}
	for line := range strings.Lines(string(data)) {
		var name, cut string
		var index int
		var start, end int64
		var w v090Window
		if _, err := fmt.Sscan(line, &name, &index, &start, &end, &cut, &w.samples, &w.sha256); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		w.window = audio.Window{Start: time.Duration(start), End: time.Duration(end)}
		for _, c := range []audio.Cut{audio.CutSilence, audio.CutLimit, audio.CutEnd} {
			if c.String() == cut {
				w.window.Cut = c
			}
		}
		if index != len(out[name]) || w.window.Cut == 0 {
			t.Fatalf("%q: out of order, or an unknown cut", line)
		}
		out[name] = append(out[name], w)
	}
	return out
}

// checkV090 checks that ws are v0.9.0's windows want, byte for byte.
func checkV090(t *testing.T, ws []audio.WindowFile, want []v090Window) {
	t.Helper()
	if len(ws) != len(want) {
		t.Fatalf("%d windows, v0.9.0 cut %d", len(ws), len(want))
	}
	for i, w := range ws {
		if w.Window != want[i].window || w.Samples != want[i].samples || w.SHA256 != want[i].sha256 {
			t.Errorf("window %d, %v %v, %d samples, %s; v0.9.0's %v %v, %d, %s",
				i, w.Window, w.Window.Cut, w.Samples, w.SHA256, want[i].window, want[i].window.Cut, want[i].samples, want[i].sha256)
		}
	}
}

// TestWindowsMatchV090: one decode of each 16 kHz mono WAV fixture, which the stand-in decodes to
// its own samples as ffmpeg does, cuts v0.9.0's windows under DetectorTracker, byte for byte. The
// integration tier holds speech.flac, through the real ffmpeg, to them too.
func TestWindowsMatchV090(t *testing.T) {
	t.Parallel()
	golden := v090Windows(t)
	for _, name := range []string{"speech-gain.wav", "speech-noise.wav"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			decode, err := filepath.Abs(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			s, st := standinSplitter(t)
			st.set(func(c *settings) { c.Probe, c.Decode = gainProbe, decode })
			ws := collect(t, s, t.Context(), name, audio.Request{Dir: t.TempDir(), Concurrency: 1})
			checkV090(t, ws, golden[name])
		})
	}
}
