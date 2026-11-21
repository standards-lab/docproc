//go:build integration && linux

package office_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

var fixtures = []string{
	"media.libreoffice.docx",
	"notes.libreoffice.docx",
	"word-with-attachments.docx",
	"drawings.libreoffice.xlsx",
	"with-drawing.xlsx",
	"slide-objects.libreoffice.pptx",
}

var mediaLink = regexp.MustCompile(`\]\(([0-9a-f]{64}\.[A-Za-z0-9]+)[) ]`)

var (
	hostLimits    = picture.Limits{Area: 256 << 20, Width: 32768, Height: 32768, Memory: 1 << 30, Disk: 4 << 30}
	hostLimitArgs = []string{"-limit", "area", "268435456", "-limit", "width", "32768", "-limit", "height", "32768",
		"-limit", "memory", "1073741824", "-limit", "disk", "4294967296"}
)

func recordingMagick(t *testing.T) (tool.Spec, func() int) {
	t.Helper()
	magick, err := exec.LookPath("magick")
	if err != nil {
		t.Fatalf("magick, a host prerequisite: %v", err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "runs.log")
	script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do printf '%%s\\0' \"$a\"; done >> '%s'\necho >> '%s'\nexec '%s' \"$@\"\n", log, log, magick)
	path := filepath.Join(dir, "magick")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := picture.MagickSpec()
	spec.Program = path
	return spec, func() int {
		t.Helper()
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for line := range strings.Lines(string(b)) {
			args := strings.Split(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\x00"), "\x00")
			if len(args) == 1 && args[0] == "-version" {
				continue
			}
			n++
			if len(args) < len(hostLimitArgs) || !slices.Equal(args[:len(hostLimitArgs)], hostLimitArgs) {
				t.Errorf("magick ran with %q, not beginning with the configured limits", args)
			}
		}
		return n
	}
}

func realConverter(t *testing.T) *office.Converter {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), office.OfficeSpec())
	if err != nil {
		t.Fatalf("docproc-ooxml, built from tools/ooxml, on PATH: %v", err)
	}
	lo, err := tool.NewExec(t.Context(), picture.LibreOfficeSpec())
	if err != nil {
		t.Fatalf("soffice, a host prerequisite: %v", err)
	}
	spec, limited := recordingMagick(t)
	mg, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatalf("magick, a host prerequisite: %v", err)
	}
	t.Logf("soffice %s and magick %s on the host", lo.Version(), mg.Version())
	pc, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: lo, Limits: hostLimits})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { t.Logf("recorded: %d magick runs, each beginning with the configured limits", limited()) })
	c, err := office.NewConverter(t.Context(), office.Config{Office: tl, Pictures: pc})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type converted struct {
	doc      *office.Document
	recs     []office.Record
	markdown string
	warnings []office.Warning
	dir      string
}

func convertPath(t *testing.T, c *office.Converter, path string) converted {
	t.Helper()
	out := converted{dir: t.TempDir()}
	var md strings.Builder
	for rec, err := range c.Convert(t.Context(), office.Request{Path: path, Dir: out.dir, Name: filepath.Base(path)}) {
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		switch r := rec.(type) {
		case *office.Document:
			out.doc = r
			md.WriteString(r.Markdown)
		case *office.Sheet:
			md.WriteString(r.Markdown)
		case *office.Rows:
			md.WriteString(r.Markdown)
		case *office.Warning:
			out.warnings = append(out.warnings, *r)
			continue
		}
		out.recs = append(out.recs, rec)
	}
	if out.doc == nil {
		t.Fatalf("%s: no Document", filepath.Base(path))
	}
	out.markdown = md.String()
	if got := entries(t, out.dir); len(got) != 1 || !strings.HasPrefix(got[0], "media-") {
		t.Errorf("%s: working directory holds %q, want the media directory alone", filepath.Base(path), got)
	}
	return out
}

func TestReal(t *testing.T) {
	c := realConverter(t)
	extensions := map[string]bool{}
	kinds := map[string]bool{}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			res := convertPath(t, c, filepath.Join("testdata", "ooxml", name))
			doc := res.doc
			if doc.Schema != cli.Schema || doc.Format != strings.TrimPrefix(filepath.Ext(name), ".") {
				t.Errorf("schema %q, format %q", doc.Schema, doc.Format)
			}
			for _, rec := range res.recs {
				switch r := rec.(type) {
				case *office.UnknownRecord:
					t.Errorf("unknown record kind %s", r.Kind)
				case *office.Sheet:
					kinds["sheet"] = true
				case *office.Rows:
					kinds["rows"] = true
				}
			}
			files := map[string]bool{}
			for _, m := range doc.Media {
				if m.File == "" {
					if m.Target == "" || m.Path != "" {
						t.Errorf("medium with neither file nor target: %+v", m)
					}
					continue
				}
				ext := strings.ToLower(filepath.Ext(m.File))
				extensions[ext] = true
				want := m.File
				if ext == ".emf" || ext == ".wmf" || ext == ".tif" || ext == ".tiff" {
					want = m.File[:64] + ".png"
				}
				files[want] = true
				if m.Converted != (want != m.File) || filepath.Base(m.Path) != want {
					t.Errorf("%s: handed on as %q, converted %t; want %s", m.File, m.Path, m.Converted, want)
				}
				data, err := os.ReadFile(filepath.Join(filepath.Dir(m.Path), m.File))
				if err != nil {
					t.Fatal(err)
				}
				if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != m.SHA256 || m.SHA256 != m.File[:64] {
					t.Errorf("%s: its SHA-256 is %x, claimed %s", m.File, sum, m.SHA256)
				}
			}
			linked := map[string]bool{}
			for _, m := range mediaLink.FindAllStringSubmatch(res.markdown, -1) {
				linked[m[1]] = true
			}
			if got, want := slices.Sorted(maps.Keys(linked)), slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
				t.Errorf("Markdown links %q, media %q", got, want)
			}
		})
	}
	for _, kind := range []string{"sheet", "rows"} {
		if !kinds[kind] {
			t.Errorf("no fixture holds a %s line", kind)
		}
	}
	for _, ext := range []string{".png", ".jpeg", ".emf", ".wmf"} {
		if !extensions[ext] {
			t.Errorf("no fixture holds a %s picture", ext)
		}
	}
}

func TestRealRefused(t *testing.T) {
	c := realConverter(t)
	dir := t.TempDir()
	path := writeDocument(t, "", "plain text, not a package")
	var err error
	for _, err = range c.Convert(t.Context(), office.Request{Path: path, Dir: dir}) {
	}
	exit, ok := errors.AsType[*cli.ExitError](err)
	if !errors.Is(err, office.ErrRefused) || !ok {
		t.Fatalf("got %v, want a refusal", err)
	}
	if exit.Message == "" || strings.Contains(exit.Message, path) {
		t.Errorf("message %q: want docproc-ooxml's reason alone", exit.Message)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("working directory holds %q, want nothing", got)
	}
}
