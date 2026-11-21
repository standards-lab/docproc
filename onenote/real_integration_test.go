//go:build integration && linux

package onenote_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/tool"
)

const fixtures = "onenote/tests/fixtures"

const fileOnly = ".file-only"

func TestRealGoldens(t *testing.T) {
	r := realReader(t)
	root := tooltest.Tools(t, goldens)
	in := tooltest.Tools(t, fixtures)
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		stem := strings.TrimSuffix(path, ".jsonl")
		rel, err := filepath.Rel(root, stem)
		if err != nil {
			return err
		}
		input, only := strings.CutSuffix(rel, fileOnly)
		n++
		t.Run(rel, func(t *testing.T) {
			media := t.TempDir()
			recs, err := collect(r.Read(t.Context(), onenote.Request{Input: filepath.Join(in, input), Media: media, FileOnly: only}))
			if _, ok := errors.AsType[*cli.ExitError](err); ok {
				_ = checkRefused(t, stem, recs, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			checkReplay(t, stem, media, recs)
			for _, rec := range recs {
				if p, ok := rec.(*onenote.Page); ok {
					checkWritten(t, p)
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("no goldens under %s", root)
	}
}

func checkWritten(t *testing.T, p *onenote.Page) {
	t.Helper()
	for _, m := range p.Media {
		if m.File == "" {
			continue
		}
		content, err := os.ReadFile(m.Path)
		if err != nil {
			t.Errorf("page %s, %s: %v", p.ID, m.File, err)
			continue
		}
		got := sha256.Sum256(content)
		if hex.EncodeToString(got[:]) != m.SHA256 {
			t.Errorf("page %s, %s: its content's SHA-256 is %x", p.ID, m.File, got)
		}
		if m.Size != nil && *m.Size != int64(len(content)) {
			t.Errorf("page %s, %s: size %d, the file's %d", p.ID, m.File, *m.Size, len(content))
		}
	}
}

func realReader(t *testing.T) *onenote.Reader {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), onenote.OneNoteSpec())
	if err != nil {
		t.Fatalf("docproc-onenote, built from tools/onenote, on PATH: %v", err)
	}
	r, err := onenote.NewReader(onenote.Config{OneNote: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
