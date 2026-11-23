package audio

import (
	"regexp"

	"github.com/standards-lab/docproc/tool"
)

// FFmpegMinimum is the oldest FFmpeg the package accepts, for ffmpeg and ffprobe alike, which
// FFmpeg releases together: 9.0.
const FFmpegMinimum = "9.0"

// FFmpegImage is the default image ffmpeg and ffprobe run in through the container runner:
// linuxserver/ffmpeg's CLI image, pinned by its exact tag and its multi-arch index digest. Its own
// entrypoint is an init that changes users, which the runner's lockdown refuses; the runner makes
// the Spec's program the entrypoint in its place, so tool.Image{Ref: FFmpegImage} is the whole of
// it. A caller may run another image, or the tools on the host through the exec runner.
const FFmpegImage = "linuxserver/ffmpeg:9.0.2-cli-ls90@sha256:" +
	"66fa8e2351fb78f9dad2c59c946be2658edcc529339320da3f2cf788312368af"

// FFmpegSpec declares FFmpeg's ffmpeg, minimum 9.0, for either runner's constructor. ffmpeg
// -version prints a line such as "ffmpeg version n9.0.2 ..." on a host whose build is tagged from
// git, and "ffmpeg version 9.0.2 ..." in a release build such as FFmpegImage's; the pattern skips
// the n. A caller that needs another program path or a KillAfter period sets them on the Spec
// returned.
func FFmpegSpec() tool.Spec {
	return tool.Spec{
		Program:        "ffmpeg",
		VersionArgs:    []string{"-version"},
		VersionPattern: regexp.MustCompile(`ffmpeg version n?(\d+(?:\.\d+)*)`),
		Minimum:        FFmpegMinimum,
	}
}

// FFprobeSpec declares FFmpeg's ffprobe, minimum 9.0, for either runner's constructor; its
// version prints as ffmpeg's does, "ffprobe version n9.0.2 ..." or "ffprobe version 9.0.2 ...".
// A caller that needs another program path or a KillAfter period sets them on the Spec returned.
func FFprobeSpec() tool.Spec {
	return tool.Spec{
		Program:        "ffprobe",
		VersionArgs:    []string{"-version"},
		VersionPattern: regexp.MustCompile(`ffprobe version n?(\d+(?:\.\d+)*)`),
		Minimum:        FFmpegMinimum,
	}
}
