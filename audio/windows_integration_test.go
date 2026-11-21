//go:build integration && linux

package audio_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/tool"
)

func TestSpeechWindows(t *testing.T) {
	golden := v090Windows(t)
	host := hostTools(t)
	got := map[string][]string{}
	for name, runner := range speechRunners {
		t.Run(name, func(t *testing.T) {
			s := runner(t)
			for _, fixture := range []string{"speech.flac", "speech-gain.wav", "speech-noise.wav"} {
				path, err := filepath.Abs(filepath.Join("testdata", fixture))
				if err != nil {
					t.Fatal(err)
				}
				source := decode(t, host.ffmpeg, path)
				dir := t.TempDir()
				ws := collect(t, s, t.Context(), path, audio.Request{Dir: dir, Concurrency: 2})
				for i, w := range ws {
					t.Logf("recorded: %s window %d: %v, %d samples (%.6fs), sha256 %s", fixture, i, w.Window.Span, w.Samples, w.Duration().Seconds(), w.SHA256)
					got[name] = append(got[name], w.SHA256)
					checkFormat(t, host.ffprobe, w.Path)
					b, err := os.ReadFile(w.Path)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(b[44:], windowPCM(source, w.Window)) {
						t.Errorf("%s window %d's samples aren't the source's over %v", fixture, i, w.Window.Span)
					}
				}
				checkV090(t, ws, golden[fixture])
				if files := files(t, dir); !slices.Equal(files, names(ws)) {
					t.Errorf("the media directory holds %q, want the windows' %q", files, names(ws))
				}
			}
		})
	}
	if e, c := got["exec"], got["container"]; len(got) == 2 && !slices.Equal(e, c) {
		t.Errorf("the exec runner wrote windows %q, the container runner %q", e, c)
	}
}

func TestTruncatedMP3(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "speech-truncated.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	host := hostTools(t)
	source := decode(t, host.ffmpeg, path)
	s := speechRunners["exec"](t)
	ws := collect(t, s, t.Context(), path, audio.Request{Dir: t.TempDir(), Concurrency: 1})
	plan := ws[len(ws)-1].Plan
	decoded := time.Duration(len(source)/2) * time.Second / audio.SampleRate
	t.Logf("recorded: probed %v, decoded %v, %d samples, %d windows", plan.Source.Duration, plan.Duration, len(source)/2, len(ws))
	if len(source)/2 != 94511 || plan.Duration != decoded || plan.Probed != 12*time.Second || plan.Source.Duration != 12*time.Second {
		t.Errorf("decoded %v of %d samples, probed %v; want %v of 94511, probed 12s", plan.Duration, len(source)/2, plan.Probed, decoded)
	}
	last := ws[len(ws)-1]
	if last.Window.End != decoded || !bytes.Equal(windowPCM(source, last.Window), source[len(source)-2*last.Samples:]) {
		t.Errorf("the last window %v, %d samples; want it to end at %v, the decode's last samples", last.Window, last.Samples, decoded)
	}
	if rec := plan.Record(); rec.ProbedDuration != 12 || rec.Duration != decoded.Seconds() {
		t.Errorf("record's durations %v, probed %v", rec.Duration, rec.ProbedDuration)
	}
}

func TestWindowsResample(t *testing.T) {
	host := hostTools(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "tone.m4a")
	err := host.ffmpeg.Run(t.Context(), tool.Invocation{
		Args: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=f=440:r=44100:d=40", "-ac", "2", "-c:a", "aac", path},
		Dir:  dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := speechRunners["exec"](t)
	media := filepath.Join(dir, "media")
	if err := os.Mkdir(media, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := collect(t, s, t.Context(), path, audio.Request{Dir: media, Concurrency: 2})
	src := ws[len(ws)-1].Plan.Source
	t.Logf("recorded: source %v, %s, %d Hz, %d channel(s)", src.Duration, src.Codec, src.SampleRate, src.Channels)
	if len(ws) != 2 {
		t.Fatalf("%d windows, want 2", len(ws))
	}
	for i, w := range ws {
		t.Logf("recorded: window %d: %v, %d samples", i, w.Window.Span, w.Samples)
		checkFormat(t, host.ffprobe, w.Path)
		if want := audio.Sample(w.Window.End) - audio.Sample(w.Window.Start); int64(w.Samples) != want {
			t.Errorf("window %d holds %d samples, want %d", i, w.Samples, want)
		}
	}
}

type hostFFmpeg struct {
	ffmpeg, ffprobe *tool.Tool
}

func hostTools(t *testing.T) hostFFmpeg {
	t.Helper()
	ffmpeg, err := tool.NewExec(t.Context(), audio.FFmpegSpec())
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := tool.NewExec(t.Context(), audio.FFprobeSpec())
	if err != nil {
		t.Fatal(err)
	}
	return hostFFmpeg{ffmpeg: ffmpeg, ffprobe: ffprobe}
}

func decode(t *testing.T, ffmpeg *tool.Tool, path string) []byte {
	t.Helper()
	var pcm bytes.Buffer
	err := ffmpeg.Run(t.Context(), tool.Invocation{
		Args:   []string{"-nostdin", "-loglevel", "error", "-i", path, "-ac", "1", "-ar", "16000", "-f", "s16le", "-"},
		Dir:    filepath.Dir(path),
		Stdout: &pcm,
	})
	if err != nil {
		t.Fatal(err)
	}
	return pcm.Bytes()
}

func checkFormat(t *testing.T, ffprobe *tool.Tool, path string) {
	t.Helper()
	var out bytes.Buffer
	err := ffprobe.Run(t.Context(), tool.Invocation{
		Args: []string{"-v", "error", "-print_format", "json", "-show_entries",
			"format=format_name:stream=codec_name,sample_rate,channels,sample_fmt,bits_per_sample", path},
		Dir:    filepath.Dir(path),
		Stdout: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Format struct {
			Name string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Codec      string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
			SampleFmt  string `json:"sample_fmt"`
			Bits       int    `json:"bits_per_sample"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Format.Name != "wav" || len(report.Streams) != 1 {
		t.Fatalf("%s: format %q, %d streams; want one stream of wav", path, report.Format.Name, len(report.Streams))
	}
	st := report.Streams[0]
	if st.Codec != "pcm_s16le" || st.SampleRate != strconv.Itoa(audio.SampleRate) || st.Channels != 1 || st.SampleFmt != "s16" || st.Bits != 16 {
		t.Errorf("%s: %+v, want pcm_s16le, 16000 Hz, 1 channel, s16, 16 bits", path, st)
	}
}
