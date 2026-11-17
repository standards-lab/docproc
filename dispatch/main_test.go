//go:build linux

package dispatch_test

import (
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
)

// TestMain runs the tests, or, through a stand-in's link, the stand-in: ffmpeg and ffprobe for the
// audio and video stages, magick for the picture stage, docproc-onenote for the onenote stage, and
// gs for the document stage.
func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"gs":              gsStandin,
		"ffmpeg":          ffmpegStandin,
		"ffprobe":         ffprobeStandin,
		"magick":          pictureStandin,
		"soffice":         sofficeStandin,
		"docproc-onenote": onenoteStandin,
	})
}

// wantPanic fails the test unless build panics with want, a constructor's naming of the nil field
// that is the caller's mistake.
func wantPanic(t *testing.T, want string, build func()) {
	t.Helper()
	defer func() {
		t.Helper()
		if got := recover(); got != want {
			t.Errorf("panic %v, want %q", got, want)
		}
	}()
	build()
}
