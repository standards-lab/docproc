//go:build linux

package audio_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/tool"
)

// flacProbe is ffprobe's report of a 49.82 s FLAC whose audio is its second stream, after a cover
// picture.
const flacProbe = `{"streams":[` +
	`{"index":0,"codec_name":"mjpeg","codec_type":"video"},` +
	`{"index":1,"codec_name":"flac","codec_type":"audio","sample_rate":"16000","channels":1,"duration":"49.820000"}],` +
	`"format":{"duration":"49.820000"}}`

// TestNewSplitterRefuses: a nil Tool panics naming its field, and a Tool older than the minimum,
// whatever its Spec's, is refused.
func TestNewSplitterRefuses(t *testing.T) {
	t.Parallel()
	ffmpeg, ffprobe := installStandins(t, "9.0.2").tools()
	for name, tt := range map[string]struct {
		cfg  audio.Config
		want string
	}{
		"no ffmpeg":  {audio.Config{FFprobe: ffprobe}, "audio: Config.FFmpeg is nil"},
		"no ffprobe": {audio.Config{FFmpeg: ffmpeg}, "audio: Config.FFprobe is nil"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != tt.want {
					t.Errorf("panic %v, want %q", r, tt.want)
				}
			}()
			_, _ = audio.NewSplitter(tt.cfg)
		})
	}
	old, oldProbe := installStandins(t, "8.1").tools()
	for name, cfg := range map[string]audio.Config{
		"ffmpeg 8.1":  {FFmpeg: old, FFprobe: ffprobe},
		"ffprobe 8.1": {FFmpeg: ffmpeg, FFprobe: oldProbe},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := audio.NewSplitter(cfg)
			var verr *tool.VersionError
			if !errors.As(err, &verr) || !errors.Is(err, tool.ErrTooOld) {
				t.Fatalf("err %v, want a *tool.VersionError wrapping ErrTooOld", err)
			}
			if verr.Found != "8.1" || verr.Minimum != audio.FFmpegMinimum {
				t.Errorf("found %s, minimum %s; want 8.1 and %s", verr.Found, verr.Minimum, audio.FFmpegMinimum)
			}
		})
	}
}

func TestSpecsMatchTheirVersions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		spec       tool.Spec
		host, wrap string
	}{
		{audio.FFmpegSpec(), "ffmpeg version n9.0.2 Copyright", "ffmpeg version 9.0.2 Copyright"},
		{audio.FFprobeSpec(), "ffprobe version n9.0.2 Copyright", "ffprobe version 9.0.2 Copyright"},
	} {
		for _, out := range []string{tt.host, tt.wrap} {
			if m := tt.spec.VersionPattern.FindStringSubmatch(out); m == nil || m[1] != "9.0.2" {
				t.Errorf("%s's pattern on %q: %q, want 9.0.2", tt.spec.Program, out, m)
			}
		}
	}
}

// TestSpecsRefuseOld builds the stand-ins through the exec runner from the Specs as the package
// returns them: 9.0.2 passes, and the runner itself refuses 8.1, below the Specs' minimum.
func TestSpecsRefuseOld(t *testing.T) {
	t.Parallel()
	for version, ok := range map[string]bool{"9.0.2": true, "8.1": false} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			st := installStandins(t, version)
			for _, spec := range []tool.Spec{audio.FFmpegSpec(), audio.FFprobeSpec()} {
				tl, err := tool.NewExec(t.Context(), st.spec(spec))
				switch {
				case ok && (err != nil || tl.Version().String() != version):
					t.Errorf("NewExec(%s) over %s = %v, %v; want %s", spec.Program, version, tl, err, version)
				case !ok && !errors.Is(err, tool.ErrTooOld):
					t.Errorf("NewExec(%s) over %s returned %v, want ErrTooOld", spec.Program, version, err)
				}
			}
		})
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	st.set(func(c *settings) { c.Probe = flacProbe })
	src, err := s.Probe(t.Context(), "speech.flac")
	if err != nil {
		t.Fatal(err)
	}
	want := audio.Source{Duration: 49820 * time.Millisecond, Stream: 1, Codec: "flac", SampleRate: 16000, Channels: 1}
	if src != want {
		t.Errorf("Probe = %+v, want %+v", src, want)
	}
	rs := st.runs()
	if len(rs) != 1 || rs[0].Program != "ffprobe" {
		t.Fatalf("runs %+v, want one of ffprobe", rs)
	}
	abs, err := filepath.Abs("speech.flac")
	if err != nil {
		t.Fatal(err)
	}
	if args := rs[0].Args; args[len(args)-1] != abs {
		t.Errorf("ffprobe read %q, want the absolute path %s", args[len(args)-1], abs)
	}
}

// TestWindowsRefusesNoAudioBeforeFFmpeg: a source ffprobe finds no audio stream in, or no
// duration for, is refused with ErrNoAudio, and ffmpeg never runs.
func TestWindowsRefusesNoAudioBeforeFFmpeg(t *testing.T) {
	t.Parallel()
	for name, report := range map[string]string{
		"no audio stream": `{"streams":[{"index":0,"codec_name":"h264","codec_type":"video"}],"format":{"duration":"5.0"}}`,
		"no streams":      `{"streams":[],"format":{}}`,
		"no duration":     `{"streams":[{"index":0,"codec_name":"flac","codec_type":"audio","sample_rate":"16000","channels":1}],"format":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, st := standinSplitter(t)
			st.set(func(c *settings) { c.Probe = report })
			dir := t.TempDir()
			ws, err := windows(s, t.Context(), "video.mp4", audio.Request{Dir: dir, Concurrency: 1})
			if !errors.Is(err, audio.ErrNoAudio) || len(ws) != 0 {
				t.Errorf("%d windows, err %v; want none and ErrNoAudio", len(ws), err)
			}
			if got := programs(st.runs()); !slices.Equal(got, []string{"ffprobe"}) {
				t.Errorf("runs %q, want ffprobe alone", got)
			}
		})
	}
}

// TestRequestRefusedBeforeRuns: each field out of range is refused with ErrRequest before
// anything runs.
func TestRequestRefusedBeforeRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ok := audio.Request{Dir: dir, Concurrency: 1}
	with := func(change func(*audio.Request)) audio.Request {
		req := ok
		change(&req)
		return req
	}
	for name, tt := range map[string]struct {
		path string
		req  audio.Request
	}{
		"no path":                   {"", ok},
		"no media directory":        {"a.flac", with(func(r *audio.Request) { r.Dir = "" })},
		"a missing directory":       {"a.flac", with(func(r *audio.Request) { r.Dir = filepath.Join(dir, "missing") })},
		"a file for a directory":    {"a.flac", with(func(r *audio.Request) { r.Dir = file })},
		"zero concurrency":          {"a.flac", with(func(r *audio.Request) { r.Concurrency = 0 })},
		"negative concurrency":      {"a.flac", with(func(r *audio.Request) { r.Concurrency = -1 })},
		"negative limit":            {"a.flac", with(func(r *audio.Request) { r.Limit = -time.Second })},
		"overlap of half the limit": {"a.flac", with(func(r *audio.Request) { r.Limit, r.Overlap = 20*time.Second, 10*time.Second })},
		"negative minimum":          {"a.flac", with(func(r *audio.Request) { r.MinSilence = -time.Second })},
		"positive threshold":        {"a.flac", with(func(r *audio.Request) { r.Threshold = 3 })},
		"a threshold and a margin":  {"a.flac", with(func(r *audio.Request) { r.Threshold, r.Margin = -40, 6 })},
		"a short window":            {"a.flac", with(func(r *audio.Request) { r.FloorWindow = time.Millisecond })},
		"the 100th percentile":      {"a.flac", with(func(r *audio.Request) { r.Percentile = 100 })},
		"a negative margin":         {"a.flac", with(func(r *audio.Request) { r.Margin = -1 })},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, st := standinSplitter(t)
			if _, err := windows(s, t.Context(), tt.path, tt.req); !errors.Is(err, audio.ErrRequest) {
				t.Errorf("err %v, want ErrRequest", err)
			}
			if rs := st.runs(); len(rs) != 0 {
				t.Errorf("runs %q, want none", programs(rs))
			}
		})
	}
}

// TestWindowsFFmpegFails: a decode that fails before any sample is a *tool.ExitError carrying
// ffmpeg's last line, with no window and nothing left in the media directory.
func TestWindowsFFmpegFails(t *testing.T) {
	t.Parallel()
	s, st := standinSplitter(t)
	st.set(func(c *settings) {
		c.Probe, c.Fail = flacProbe, "noise\n[in#0] Error opening input: Invalid data found when processing input\n"
	})
	dir := t.TempDir()
	ws, err := windows(s, t.Context(), "speech.flac", audio.Request{Dir: dir, Concurrency: 1})
	var exit *tool.ExitError
	if !errors.As(err, &exit) || exit.Code != 1 || len(ws) != 0 {
		t.Fatalf("%d windows, err %v; want none and a *tool.ExitError with code 1", len(ws), err)
	}
	if !strings.Contains(err.Error(), "Invalid data found") {
		t.Errorf("err %q doesn't carry ffmpeg's last line", err)
	}
	if got := files(t, dir); len(got) != 0 {
		t.Errorf("the media directory holds %q", got)
	}
}

// programs returns the runs' programs, in order.
func programs(rs []run) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Program)
	}
	return out
}
