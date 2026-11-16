//go:build integration && linux

package office_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/tool"
)

// fixtures are the OOXML fixtures in testdata/ooxml, copied from office v0.1.0, as
// testdata/README.md lists them.
var fixtures = []string{
	"media.libreoffice.docx",
	"notes.libreoffice.docx",
	"word-with-attachments.docx",
	"drawings.libreoffice.xlsx",
	"with-drawing.xlsx",
	"slide-objects.libreoffice.pptx",
}

// mediaLink matches a medium's name, the lowercase hex SHA-256 and an extension, as
// docproc-ooxml's Markdown links it.
var mediaLink = regexp.MustCompile(`\]\(([0-9a-f]{64}\.[A-Za-z0-9]+)[) ]`)

// realConverter builds a Converter over the real docproc-ooxml, built from tools/ooxml and found
// on PATH through the exec runner.
func realConverter(t *testing.T) *office.Converter {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), office.OfficeSpec())
	if err != nil {
		t.Fatalf("docproc-ooxml, built from tools/ooxml, on PATH: %v", err)
	}
	c, err := office.NewConverter(t.Context(), office.Config{Office: tl})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// converted is one conversion of a fixture: its Document, its records' Markdown joined, its
// warnings, and its working directory.
type converted struct {
	doc      *office.Document
	recs     []office.Record
	markdown string
	warnings []office.Warning
	dir      string
}

// convertPath converts the document at path, its name its base, in a working directory of its
// own, failing the test unless it succeeds with a Document first and leaves the media directory
// alone in the working directory.
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

// TestReal runs the real docproc-ooxml over the fixtures. Each yields a schema "2" Document in the
// fixture's format first, and no record of a kind the package doesn't know. Each medium with a
// file is resolved in the media directory, its SHA-256 its name's, handed on as stored without
// converters; the media names are exactly the Markdown's links to media; a linked picture has a
// target and no file. Together the fixtures hold a sheet, rows, and pictures of every kind the
// picture standard converts or hands on.
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
				files[m.File] = true
				extensions[strings.ToLower(filepath.Ext(m.File))] = true
				if m.Converted || filepath.Base(m.Path) != m.File {
					t.Errorf("%s: handed on as %q, converted %t; want as stored", m.File, m.Path, m.Converted)
				}
				data, err := os.ReadFile(m.Path)
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

// TestRealRefused: the real docproc-ooxml refuses a document that isn't an OOXML package with exit
// code 3, and the error carries its reason without the document's path.
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
