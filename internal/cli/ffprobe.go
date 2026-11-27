package cli

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/metadata"
)

// FFprobeEntries is the -show_entries audio's and media's ffprobe runs ask for: the container's
// format name, duration, bit rate, and tags, and each stream's index, type, codec, sample rate,
// channels, frame size, frame rates, duration, tags, and attached-picture disposition.
const FFprobeEntries = "format=format_name,duration,bit_rate:format_tags" +
	":stream=index,codec_type,codec_name,sample_rate,channels,width,height,avg_frame_rate,r_frame_rate,duration" +
	":stream_tags:stream_disposition=attached_pic"

// FFprobeReport is ffprobe's JSON report of a file under FFprobeEntries, each value as ffprobe
// prints it.
type FFprobeReport struct {
	Format struct {
		FormatName string              `json:"format_name"`
		Duration   string              `json:"duration"`
		BitRate    string              `json:"bit_rate"`
		Tags       metadata.Properties `json:"tags"`
	} `json:"format"`
	Streams []FFprobeStream `json:"streams"`
}

// FFprobeStream is one stream of an FFprobeReport.
type FFprobeStream struct {
	Index        int                 `json:"index"`
	CodecType    string              `json:"codec_type"`
	CodecName    string              `json:"codec_name"`
	SampleRate   string              `json:"sample_rate"`
	Channels     int                 `json:"channels"`
	Width        int                 `json:"width"`
	Height       int                 `json:"height"`
	AvgFrameRate string              `json:"avg_frame_rate"`
	RFrameRate   string              `json:"r_frame_rate"`
	Duration     string              `json:"duration"`
	Tags         metadata.Properties `json:"tags"`
	Disposition  struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

// ParseFFprobe decodes ffprobe's JSON report.
func ParseFFprobe(data []byte) (FFprobeReport, error) {
	var r FFprobeReport
	err := json.Unmarshal(data, &r)
	return r, err
}

// Metadata returns the report as a metadata.Media record: a value ffprobe gives as "N/A", or not
// as a number, is absent, and a stream's sample rate and channels are kept for an audio stream
// alone, its frame size and rate for a video stream alone.
func (r FFprobeReport) Metadata() metadata.Media {
	m := metadata.Media{
		Format: r.Format.FormatName, Duration: positive(r.Format.Duration), Tags: r.Format.Tags,
		Streams: make([]metadata.MediaStream, 0, len(r.Streams)),
	}
	if n, err := strconv.ParseInt(r.Format.BitRate, 10, 64); err == nil && n > 0 {
		m.BitRate = n
	}
	for _, st := range r.Streams {
		s := metadata.MediaStream{Index: st.Index, Type: st.CodecType, Codec: st.CodecName, Tags: st.Tags}
		switch st.CodecType {
		case "audio":
			if n, err := strconv.Atoi(st.SampleRate); err == nil && n > 0 {
				s.SampleRate = n
			}
			s.Channels = max(st.Channels, 0)
		case "video":
			s.Width, s.Height = max(st.Width, 0), max(st.Height, 0)
			s.FrameRate = rational(st.AvgFrameRate)
			if s.FrameRate == 0 {
				s.FrameRate = rational(st.RFrameRate)
			}
			s.AttachedPicture = st.Disposition.AttachedPic != 0
		}
		m.Streams = append(m.Streams, s)
	}
	return m
}

func positive(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0
	}
	return f
}

func rational(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		return 0
	}
	n, d := positive(num), positive(den)
	if n == 0 || d == 0 {
		return 0
	}
	return n / d
}
