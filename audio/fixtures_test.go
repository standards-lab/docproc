//go:build linux

package audio_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
)

const (
	scenesTruth = "speech-scenes.truth.json"
	levelsTruth = "speech-levels.truth.json"
)

var fixtureRow = regexp.MustCompile("^\\| `([^`]+)` \\|.*\\| (\\d+) \\| `([0-9a-f]{64})` \\|$")

func TestFixtureSums(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("testdata", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{"README.md": true}
	for line := range strings.Lines(string(readme)) {
		m := fixtureRow.FindStringSubmatch(strings.TrimRight(line, "\n"))
		if m == nil {
			continue
		}
		name, size, sum := m[1], m[2], m[3]
		listed[name] = true
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Error(err)
			continue
		}
		if got := strconv.Itoa(len(data)); got != size {
			t.Errorf("%s: %s bytes, the README records %s", name, got, size)
		}
		if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: SHA-256 %x, the README records %s", name, got, sum)
		}
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !listed[e.Name()] {
			t.Errorf("%s is in testdata and not in its README's table", e.Name())
		}
	}
	for _, name := range []string{"speech.flac", "speech-scenes.mkv", scenesTruth, "speech-gain.wav", "speech-noise.wav", levelsTruth} {
		if !listed[name] {
			t.Errorf("the README's table doesn't list %s", name)
		}
	}
}

type gapTruth struct {
	StartMS int `json:"start_ms"`
	EndMS   int `json:"end_ms"`
}

func (g gapTruth) span() audio.Span {
	return audio.Span{Start: ms(g.StartMS), End: ms(g.EndMS)}
}

type frameTruth struct {
	Reason string `json:"reason"`
	TimeMS int    `json:"time_ms"`
}

type scenesFixture struct {
	File  string `json:"file"`
	Video struct {
		Codec      string `json:"codec"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		FrameRate  int    `json:"frame_rate"`
		DurationMS int    `json:"duration_ms"`
		Generated  bool   `json:"generated"`
		Segments   []struct {
			StartMS int    `json:"start_ms"`
			EndMS   int    `json:"end_ms"`
			Pattern string `json:"pattern"`
		} `json:"segments"`
		CutsMS   []int        `json:"cuts_ms"`
		MaxGapMS int          `json:"max_gap_ms"`
		Frames   []frameTruth `json:"frames"`
	} `json:"video"`
	Audio struct {
		Source     string     `json:"source"`
		Codec      string     `json:"codec"`
		SampleRate int        `json:"sample_rate"`
		DurationMS int        `json:"duration_ms"`
		Gaps       []gapTruth `json:"gaps"`
		Transcript []string   `json:"transcript"`
	} `json:"audio"`
}

type levelsFixture struct {
	Source            string     `json:"source"`
	SampleRate        int        `json:"sample_rate"`
	DurationMS        int        `json:"duration_ms"`
	AttenuationDB     float64    `json:"attenuation_db"`
	Floor             noise      `json:"floor"`
	Gaps              []gapTruth `json:"gaps"`
	Transcript        []string   `json:"transcript"`
	ChangeMS          int        `json:"change_ms"`
	FirstMeanVolumeDB float64    `json:"first_mean_volume_db"`
	FixedThresholdDB  float64    `json:"fixed_threshold_db"`
	MinSilenceMS      int        `json:"min_silence_ms"`
	GapsAfterChange   []gapTruth `json:"gaps_after_change"`
	Variants          []struct {
		File   string  `json:"file"`
		Change string  `json:"change"`
		GainDB float64 `json:"gain_db,omitzero"`
		Noise  *noise  `json:"noise,omitempty"`
	} `json:"variants"`
}

type noise struct {
	Color     string  `json:"color"`
	Amplitude float64 `json:"amplitude"`
	Seed      int     `json:"seed"`
}

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func readTruth(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(data, v, jsonv2.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func speechTranscript(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "speech.truth.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestScenesTruth(t *testing.T) {
	var f scenesFixture
	readTruth(t, scenesTruth, &f)
	v := f.Video
	if f.File != "speech-scenes.mkv" || v.Codec != "mpeg4" || v.Width != 320 || v.Height != 180 || v.FrameRate != 10 || !v.Generated {
		t.Errorf("file %s, video %s %dx%d at %d fps, generated %t", f.File, v.Codec, v.Width, v.Height, v.FrameRate, v.Generated)
	}
	var starts []int
	end := 0
	for _, s := range v.Segments {
		if s.StartMS != end || s.EndMS <= s.StartMS {
			t.Errorf("segment %s from %d to %d ms, after one ending at %d", s.Pattern, s.StartMS, s.EndMS, end)
		}
		starts, end = append(starts, s.StartMS), s.EndMS
	}
	if end != v.DurationMS {
		t.Errorf("segments end at %d ms, the video at %d", end, v.DurationMS)
	}
	if len(starts) < 2 || !slices.Equal(starts[1:], v.CutsMS) {
		t.Errorf("cuts %v ms, segments starting at %v", v.CutsMS, starts)
	}

	var want []frameTruth
	step := 1000 / v.FrameRate
	last := 0
	for at := 0; at < v.DurationMS; at += step {
		switch {
		case at == 0:
			want = append(want, frameTruth{"first", at})
		case slices.Contains(v.CutsMS, at):
			want = append(want, frameTruth{"scene", at})
		case at-last >= v.MaxGapMS:
			want = append(want, frameTruth{"interval", at})
		default:
			continue
		}
		last = at
	}
	if v.MaxGapMS != 10000 || !slices.Equal(v.Frames, want) {
		t.Errorf("frames %v at a gap of %d ms, want %v at 10000", v.Frames, v.MaxGapMS, want)
	}
	reasons := map[string]int{}
	for _, fr := range v.Frames {
		reasons[fr.Reason]++
	}
	if reasons["first"] != 1 || reasons["scene"] == 0 || reasons["interval"] == 0 {
		t.Errorf("frames by reason %v: the fixture exercises every rule", reasons)
	}

	a := f.Audio
	gaps, duration := speechLayout(speechUtterances, 1)
	if a.Source != "speech.flac" || a.Codec != "flac" || a.SampleRate != 16000 || ms(a.DurationMS) != duration {
		t.Errorf("audio %s, %s at %d Hz, %d ms; want speech.flac, flac at 16000 Hz, %v", a.Source, a.Codec, a.SampleRate, a.DurationMS, duration)
	}
	if got := spans(a.Gaps); !slices.Equal(got, gaps) {
		t.Errorf("audio gaps %v, want %v", got, gaps)
	}
	if !slices.Equal(a.Transcript, speechTranscript(t)) {
		t.Errorf("audio transcript %q isn't speech.truth.txt's", a.Transcript)
	}
}

var (
	speechUtterances = []time.Duration{6590, 7145, 4835, 2840, 6590, 7145, 4835, 2840}
	speechGap        = time.Second
)

func speechLayout(utterances []time.Duration, rounds int) (gaps []audio.Span, duration time.Duration) {
	for r := range rounds {
		for i, d := range utterances {
			duration += d * time.Millisecond
			if r < rounds-1 || i < len(utterances)-1 {
				gaps = append(gaps, audio.Span{Start: duration, End: duration + speechGap})
				duration += speechGap
			}
		}
	}
	return gaps, duration
}

func spans(gaps []gapTruth) []audio.Span {
	out := make([]audio.Span, len(gaps))
	for i, g := range gaps {
		out[i] = g.span()
	}
	return out
}

func TestLevelsTruth(t *testing.T) {
	var f levelsFixture
	readTruth(t, levelsTruth, &f)
	gaps, duration := speechLayout(speechUtterances[:4], 3)
	if f.Source != "speech.flac" || f.SampleRate != 16000 || ms(f.DurationMS) != duration {
		t.Errorf("source %s at %d Hz, %d ms; want speech.flac at 16000 Hz, %v", f.Source, f.SampleRate, f.DurationMS, duration)
	}
	if got := spans(f.Gaps); !slices.Equal(got, gaps) {
		t.Errorf("gaps %v, want %v", got, gaps)
	}
	round := speechTranscript(t)[:4]
	if want := slices.Concat(round, round, round); !slices.Equal(f.Transcript, want) {
		t.Errorf("transcript %q, want %q", f.Transcript, want)
	}
	if f.ChangeMS < 30000 || f.ChangeMS >= f.DurationMS {
		t.Errorf("the change at %d ms, before 30 s or past the end", f.ChangeMS)
	}
	var after []gapTruth
	for _, g := range f.Gaps {
		if g.StartMS >= f.ChangeMS {
			after = append(after, g)
		}
	}
	if len(after) == 0 || !slices.Equal(f.GapsAfterChange, after) {
		t.Errorf("gaps after the change %v, want %v, at least one", f.GapsAfterChange, after)
	}
	if want := f.FirstMeanVolumeDB - 15; f.FixedThresholdDB != want || f.MinSilenceMS != int(audio.DefaultMinSilence/time.Millisecond) {
		t.Errorf("threshold %v dB, minimum silence %d ms; want %v dB, %v", f.FixedThresholdDB, f.MinSilenceMS, want, audio.DefaultMinSilence)
	}
	changes := map[string]bool{}
	for _, v := range f.Variants {
		changes[v.Change] = true
		switch {
		case v.Change == "gain" && (v.GainDB != 12 || v.Noise != nil):
			t.Errorf("%s: a gain of %v dB, noise %+v; want 12 dB and no noise", v.File, v.GainDB, v.Noise)
		case v.Change == "noise" && (v.GainDB != 0 || v.Noise == nil || v.Noise.Color != "pink"):
			t.Errorf("%s: a gain of %v dB, noise %+v; want pink noise and no gain", v.File, v.GainDB, v.Noise)
		case v.Change != "gain" && v.Change != "noise":
			t.Errorf("%s: change %q", v.File, v.Change)
		}
		data, err := os.ReadFile(filepath.Join("testdata", v.File))
		if err != nil {
			t.Error(err)
			continue
		}
		samples := f.DurationMS * f.SampleRate / 1000
		if err := canonicalWAV(data, f.SampleRate, samples); err != nil {
			t.Errorf("%s: %v", v.File, err)
		}
	}
	if len(f.Variants) != 2 || !changes["gain"] || !changes["noise"] {
		t.Errorf("variants %+v, want a gain and a noise", f.Variants)
	}
}

func canonicalWAV(data []byte, rate, samples int) error {
	if len(data) < 44 {
		return errors.New("shorter than its header")
	}
	le := binary.LittleEndian
	switch {
	case !bytes.Equal(data[0:4], []byte("RIFF")) || !bytes.Equal(data[8:16], []byte("WAVEfmt ")) || !bytes.Equal(data[36:40], []byte("data")):
		return errors.New("not a canonical RIFF, fmt, and data header")
	case int(le.Uint32(data[4:8])) != len(data)-8:
		return errors.New("its RIFF size isn't its length")
	case le.Uint32(data[16:20]) != 16 || le.Uint16(data[20:22]) != 1 || le.Uint16(data[22:24]) != 1:
		return errors.New("not mono PCM in a 16-byte fmt chunk")
	case int(le.Uint32(data[24:28])) != rate || int(le.Uint32(data[28:32])) != 2*rate || le.Uint16(data[32:34]) != 2 || le.Uint16(data[34:36]) != 16:
		return fmt.Errorf("not 16-bit at %d Hz", rate)
	case int(le.Uint32(data[40:44])) != 2*samples || len(data) != 44+2*samples:
		return fmt.Errorf("not %d samples", samples)
	}
	return nil
}

func within(s audio.Span, silences []audio.Span) bool {
	for _, x := range silences {
		if x.Start <= s.Start+audio.FrameLength && s.End <= x.End+audio.FrameLength {
			return true
		}
	}
	return false
}
