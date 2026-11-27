package metadata

import "github.com/standards-lab/docproc/dispatch"

// Media is an audio or video file's metadata record, one JSON object under
// [dispatch.MediaMediaMetadata], which the audio stage yields as a record output named "metadata"
// before its first window, and the video stage before its plan: what FFmpeg's ffprobe reads of
// the file's container and each of its streams, from the same probe that finds the streams the
// stage decodes. Every file the stage probes yields one, since ffprobe always names a container's
// format; a file ffprobe can't read, or one with no stream the stage decodes, yields none. Its
// fields:
//
//   - "format": the container's format, as ffprobe names its demuxer, such as "mp3", "flac", or
//     "matroska,webm"; always present.
//   - "duration": the container's duration, in seconds, as ffprobe estimates it from the
//     container's header or its streams; absent when ffprobe gives none.
//   - "bit_rate": the container's overall bit rate, in bits per second; absent when ffprobe gives
//     none.
//   - "tags": the container's tags, such as an ID3 tag's or a Vorbis comment's, each a string as
//     stored, in ffprobe's order, by its key as ffprobe gives it; absent when the container has
//     none.
//   - "streams": the container's streams, each a [MediaStream], in the container's order; always
//     present.
type Media struct {
	Format   string        `json:"format"`
	Duration float64       `json:"duration,omitzero"`
	BitRate  int64         `json:"bit_rate,omitzero"`
	Tags     Properties    `json:"tags,omitzero"`
	Streams  []MediaStream `json:"streams"`
}

// MediaType returns the record's media type, [dispatch.MediaMediaMetadata].
func (Media) MediaType() string { return dispatch.MediaMediaMetadata }

// MediaStream is one stream of a [Media] record, as ffprobe reads it. Its fields:
//
//   - "index": the stream's index in the container, from 0; always present.
//   - "type": the stream's type, as ffprobe names it: "audio", "video", "subtitle", "data", or
//     "attachment"; always present.
//   - "codec": the stream's codec, as ffprobe names it, such as "flac", "mp3", or "h264"; absent
//     when ffprobe names none.
//   - "sample_rate" and "channels": an audio stream's sample rate, in Hz, and its channel count;
//     each absent for a stream of another type, or when ffprobe gives none.
//   - "width" and "height": a video stream's frame size, in pixels; each absent for a stream of
//     another type, or when ffprobe gives none.
//   - "frame_rate": a video stream's average frame rate, in frames per second, else its base
//     frame rate when ffprobe gives no average; absent for a stream of another type, or when
//     ffprobe gives neither.
//   - "attached_picture": true for a video stream that is a picture attached to the file, such as
//     an audio file's cover art, rather than a video; absent otherwise.
//   - "tags": the stream's tags, such as its language or title, each a string as stored, in
//     ffprobe's order, by its key as ffprobe gives it; absent when the stream has none.
type MediaStream struct {
	Index           int        `json:"index"`
	Type            string     `json:"type"`
	Codec           string     `json:"codec,omitzero"`
	SampleRate      int        `json:"sample_rate,omitzero"`
	Channels        int        `json:"channels,omitzero"`
	Width           int        `json:"width,omitzero"`
	Height          int        `json:"height,omitzero"`
	FrameRate       float64    `json:"frame_rate,omitzero"`
	AttachedPicture bool       `json:"attached_picture,omitzero"`
	Tags            Properties `json:"tags,omitzero"`
}
