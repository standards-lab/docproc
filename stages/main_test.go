//go:build linux

package stages_test

import (
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/onenote"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"gs":              gsStandin,
		"ffmpeg":          ffmpegStandin,
		"ffprobe":         ffprobeStandin,
		"magick":          pictureStandin,
		"soffice":         sofficeStandin,
		"docproc-onenote": envelopeStandin(onenote.OneNoteMinimum),
		"docproc-ooxml":   envelopeStandin(office.OfficeMinimum),
	})
}

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
