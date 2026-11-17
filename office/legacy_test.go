//go:build linux

package office_test

import (
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/tool"
)

// ole2 begins an OLE2 compound file, as a legacy document and an encrypted OOXML package do.
const ole2 = "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"

// rtf begins an RTF document.
const rtf = `{\rtf1\ansi `

// TestConvertLegacy: a document with the OLE2 signature whose name has a legacy extension, in
// any case, is copied by the picture package's LibreOffice runner into a libreoffice-* directory
// of its own in the working directory, as "document" with that extension, lowercased, and
// converted by soffice to its OOXML format, there, with a fresh profile and its output in that
// directory; docproc-ooxml then runs once, in the working directory, on the converted document. The Document records the legacy
// format, and only the media directory is left. An RTF, by its signature, whatever its name says,
// is copied as .rtf and converted to docx by Writer's filter, as a .doc is, and recorded as "rtf".
func TestConvertLegacy(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	for _, tc := range []struct {
		desc, head, name, from, to, filter string
	}{
		{"report.doc", ole2, "report.doc", "doc", "docx", "MS Word 2007 XML"},
		{"letter.DOT", ole2, "letter.DOT", "dot", "docx", "MS Word 2007 XML"},
		{"budget.xls", ole2, "/somewhere/else/budget.xls", "xls", "xlsx", "Calc MS Excel 2007 XML"},
		{"budget.xlt", ole2, "budget.xlt", "xlt", "xlsx", "Calc MS Excel 2007 XML"},
		{"deck.ppt", ole2, "deck.ppt", "ppt", "pptx", "Impress MS PowerPoint 2007 XML"},
		{"deck.Pps", ole2, "deck.Pps", "pps", "pptx", "Impress MS PowerPoint 2007 XML"},
		{"deck.pot", ole2, "deck.pot", "pot", "pptx", "Impress MS PowerPoint 2007 XML"},
		{"memo.rtf", rtf, "memo.rtf", "rtf", "docx", "MS Word 2007 XML"},
		{"MEMO.RTF", rtf, "MEMO.RTF", "rtf", "docx", "MS Word 2007 XML"},
		{"rtf named .doc", rtf, "memo.doc", "rtf", "docx", "MS Word 2007 XML"},
		{"rtf named .xls", rtf, "memo.xls", "rtf", "docx", "MS Word 2007 XML"},
		{"rtf with no name", rtf, "", "rtf", "docx", "MS Word 2007 XML"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			dir := t.TempDir()
			before := len(ins.runs())
			doc := tc.head + "the " + tc.from + " document"
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", doc), Dir: dir, Name: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			if got := document(t, recs).ConvertedFrom; got != tc.from {
				t.Errorf("converted from: got %q, want %q", got, tc.from)
			}
			all := ins.runs()[before:]
			if len(all) != 2 || all[0].Program != "soffice" || all[1].Program != "docproc-ooxml" {
				t.Fatalf("runs: got %+v, want soffice, then docproc-ooxml", all)
			}
			ins, of := all[0], all[1]
			work := regexp.QuoteMeta(dir) + `/libreoffice-[^/]+`
			if !regexp.MustCompile(`^`+work+`$`).MatchString(ins.Dir) || of.Dir != dir {
				t.Errorf("working directories: soffice %s, docproc-ooxml %s, want the run's own and %s", ins.Dir, of.Dir, dir)
			}
			if ins.Input != doc {
				t.Errorf("soffice's input: got %q, want %q", ins.Input, doc)
			}
			if ins.Registry != "" {
				t.Errorf("soffice's profile started with %q, want a fresh one", ins.Registry)
			}
			copied := work + `/in/document\.` + tc.from
			want := []string{
				`-env:UserInstallation=file://` + work + `/profile`,
				`--headless`, `--norestore`,
				`--convert-to`, regexp.QuoteMeta(tc.to + ":" + tc.filter),
				`--outdir`, work + `/out`,
				copied,
			}
			if !matchArgs(ins.Args, want) {
				t.Errorf("soffice's arguments: got %q, want %q", ins.Args, want)
			}
			outdir := ins.Args[slices.Index(ins.Args, "--outdir")+1]
			if input := of.Args[len(of.Args)-1]; input != filepath.Join(outdir, "document."+tc.to) {
				t.Errorf("docproc-ooxml's input: got %s, want soffice's output %s", input, filepath.Join(outdir, "document."+tc.to))
			}
			if of.Input != convertedPrefix+doc {
				t.Errorf("docproc-ooxml's input: got %q, want soffice's output", of.Input)
			}
			if got := entries(t, dir); len(got) != 1 || !strings.HasPrefix(got[0], "media-") {
				t.Errorf("working directory holds %q, want the media directory alone", got)
			}
		})
	}
}

// matchArgs reports whether each of args matches the anchored pattern of want at its index.
func matchArgs(args, want []string) bool {
	if len(args) != len(want) {
		return false
	}
	for i, p := range want {
		if !regexp.MustCompile(`^` + p + `$`).MatchString(args[i]) {
			return false
		}
	}
	return true
}

// TestConvertNotLegacy: an OLE2 file whose name has no legacy extension, such as an encrypted
// OOXML package, a legacy name on a file without the signature, and an .rtf without RTF's
// signature, go to docproc-ooxml as they are, read in place, and soffice never runs;
// docproc-ooxml refusing the OLE2 file is its refusal.
func TestConvertNotLegacy(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	for _, tc := range []struct{ desc, doc, name string }{
		{"encrypted docx", ole2 + "an encrypted package", "secret.docx"},
		{"no name", ole2 + "a doc", ""},
		{"no extension", ole2 + "a doc", "report"},
		{"no signature", "PK\x03\x04 a docx", "renamed.doc"},
		{"no rtf signature", "PK\x03\x04 a docx", "renamed.rtf"},
		{"short", "\xd0\xcf", "short.doc"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			before := len(ins.runs())
			path := writeDocument(t, "", tc.doc)
			recs, err := collect(t, c, office.Request{Path: path, Dir: t.TempDir(), Name: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			if got := document(t, recs).ConvertedFrom; got != "" {
				t.Errorf("converted from %q", got)
			}
			all := ins.runs()[before:]
			if len(all) != 1 || all[0].Program != "docproc-ooxml" || all[0].Input != tc.doc {
				t.Fatalf("runs: got %+v, want docproc-ooxml alone on the document", all)
			}
			if input := all[0].Args[len(all[0].Args)-1]; input != path {
				t.Errorf("input: got %s, want the document in place, %s", input, path)
			}
		})
	}

	ins.set(exitKey, "3")
	ins.set(stderrKey, "docproc-ooxml: error: encrypted package; decrypt it first\n")
	_, err := collect(t, c, office.Request{Path: writeDocument(t, "", ole2+"an encrypted package"), Dir: t.TempDir(), Name: "secret.docx"})
	if !errors.Is(err, office.ErrRefused) {
		t.Errorf("docproc-ooxml's refusal: got %v, want ErrRefused", err)
	}
}

// TestConvertLegacyFails: soffice exiting nonzero, or exiting zero having written nothing, fails
// the conversion with soffice's own error line, and docproc-ooxml never runs; docproc-ooxml
// failing on the converted document is its error. Each leaves nothing in the working directory.
func TestConvertLegacyFails(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	noise := "(process:22): dconf-CRITICAL **: unable to create directory '/.cache/dconf'\nFontconfig error: No writable cache directories\n"
	for _, tc := range []struct {
		desc     string
		env      map[string]string
		programs []string
		check    func(error) bool
		want     string
	}{
		{"soffice exits 1",
			map[string]string{sofficeExitKey: "1", sofficeOutputKey: noise + "Error: Please verify input parameters... (SfxBaseModel::impl_store failed)\n"},
			[]string{"soffice"},
			func(err error) bool { _, ok := errors.AsType[*tool.ExitError](err); return ok },
			"Error: Please verify input parameters... (SfxBaseModel::impl_store failed)"},
		{"soffice writes nothing",
			map[string]string{sofficeNothingKey: "1", sofficeOutputKey: noise},
			[]string{"soffice"},
			func(err error) bool { return err != nil },
			"office: libreoffice: soffice wrote no docx: Fontconfig error: No writable cache directories"},
		{"docproc-ooxml refuses",
			map[string]string{exitKey: "3", stderrKey: "docproc-ooxml: error: not a package\n", mediaKey: mediaName("a", ".png")},
			[]string{"soffice", "docproc-ooxml"},
			func(err error) bool { return errors.Is(err, office.ErrRefused) },
			"docproc-ooxml: refused: not a package"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			for k, v := range tc.env {
				ins.setFor(t, k, v)
			}
			dir := t.TempDir()
			before := len(ins.runs())
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", ole2+"a doc"), Dir: dir, Name: "a.doc"})
			if len(recs) != 0 || !tc.check(err) {
				t.Fatalf("got %d records, %v", len(recs), err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q doesn't say %q", err, tc.want)
			}
			var programs []string
			for _, r := range ins.runs()[before:] {
				programs = append(programs, r.Program)
			}
			if !slices.Equal(programs, tc.programs) {
				t.Errorf("runs: got %q, want %q", programs, tc.programs)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
		})
	}
}
