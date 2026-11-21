//go:build integration && linux

package picture_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

var legacyFixtures = []string{
	"charts.libreoffice.doc", "cells.libreoffice.xls", "bullets.libreoffice.ppt", "lists.libreoffice.doc",
	"formulas.libreoffice.xls", "slides.libreoffice.ppt", "tables.libreoffice.doc", "merges.libreoffice.xls",
}

var legacyExports = map[string]string{
	".doc": "docx:MS Word 2007 XML",
	".xls": "xlsx:Calc MS Excel 2007 XML",
	".ppt": "pptx:Impress MS PowerPoint 2007 XML",
}

func TestRunLibreOfficeConcurrent(t *testing.T) {
	runners := map[string]func(*testing.T) *tool.Tool{
		"exec": func(t *testing.T) *tool.Tool {
			so, err := tool.NewExec(t.Context(), picture.LibreOfficeSpec())
			if err != nil {
				t.Fatal(err)
			}
			return so
		},
		"container": func(t *testing.T) *tool.Tool {
			so, err := tool.NewContainer(t.Context(), "", tool.Image{Ref: picture.LibreOfficeImage}, picture.LibreOfficeSpec())
			if err != nil {
				t.Fatal(err)
			}
			return so
		},
	}
	for name, runner := range runners {
		t.Run(name, func(t *testing.T) {
			so := runner(t)
			mg, err := tool.NewExec(t.Context(), picture.MagickSpec())
			if err != nil {
				t.Fatal(err)
			}
			c, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: so, Limits: hostLimits})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("soffice %s, %d runs at once", so.Version(), len(legacyFixtures))
			dir := t.TempDir()
			var wg sync.WaitGroup
			for _, fixture := range legacyFixtures {
				wg.Go(func() { convertLegacy(t, c, dir, fixture) })
			}
			wg.Wait()
			if left := entries(t, dir); len(left) != 0 {
				t.Errorf("working directory holds %q, want nothing", left)
			}
		})
	}
}

func convertLegacy(t *testing.T, c *picture.Converter, dir, fixture string) {
	ext := filepath.Ext(fixture)
	to := legacyExports[ext]
	out, err := c.RunLibreOffice(t.Context(), picture.LibreOfficeRun{
		Dir:       dir,
		ConvertTo: to,
		Inputs:    []picture.LibreOfficeInput{{Path: filepath.Join("..", "office", "testdata", "legacy", "generated", fixture), Name: "document" + ext}},
	})
	if err != nil {
		t.Errorf("%s: %v", fixture, err)
		return
	}
	defer func() {
		if err := out.Remove(); err != nil {
			t.Error(err)
		}
	}()
	target, _, _ := strings.Cut(to, ":")
	b, err := os.ReadFile(filepath.Join(out.Dir, "document."+target))
	if err != nil {
		t.Errorf("%s: %v (soffice: %s)", fixture, err, out.Message)
		return
	}
	if !bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		t.Errorf("%s: document.%s isn't a ZIP package", fixture, target)
	}
}
