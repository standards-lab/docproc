//go:build linux

package stages_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/onenote"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"gs":              gsStandin,
		"qpdf":            qpdfStandin,
		"pdfdetach":       pdfdetachStandin,
		"pdftotext":       pdftotextStandin,
		"ffmpeg":          ffmpegStandin,
		"ffprobe":         ffprobeStandin,
		"magick":          pictureStandin,
		"soffice":         sofficeStandin,
		"7zz":             sevenZipStandin,
		"docproc-onenote": envelopeStandin(onenote.OneNoteMinimum),
		"docproc-ooxml":   envelopeStandin(office.OfficeMinimum),
	})
}

func qpdfStandin(r tooltest.Run) int {
	if len(r.Args) == 1 && r.Args[0] == "--version" {
		fmt.Println("qpdf version 11.9.0")
		return 0
	}
	fmt.Fprintf(os.Stderr, "qpdf stand-in: unexpected arguments %q\n", r.Args)
	return 2
}

func pdfdetachStandin(r tooltest.Run) int {
	if len(r.Args) == 1 && r.Args[0] == "-v" {
		fmt.Fprintln(os.Stderr, "pdfdetach version 26.10.0")
		return 99
	}
	fmt.Fprintf(os.Stderr, "pdfdetach stand-in: unexpected arguments %q\n", r.Args)
	return 2
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
