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

// These tests run the real docproc-onenote, built from tools/onenote in this checkout and found on
// PATH, which the unit tier doesn't provide, so they run in the integration tier.

// fixtures is docproc-onenote's committed inputs, read in place under tools/.
const fixtures = "onenote/tests/fixtures"

// fileOnly ends the stem of a golden of a table of contents read under --file-only.
const fileOnly = ".file-only"

// TestRealGoldens runs the real docproc-onenote over the input of every committed golden, with a
// media directory, and holds its records to the golden as TestReplayGoldens does: one record per
// line, each written back as its line, and the golden's warnings; a refused input, the encrypted
// section and the table of contents under --file-only, a *cli.ExitError of exit code 3 classed by
// its reason. Each medium's file is in the media directory, its content's SHA-256 its name's and
// its size the record's.
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
				checkRefused(t, stem, recs, err)
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

// checkWritten checks that each of p's media with a file was written at its Path, its content's
// SHA-256 its SHA256 and its size the record's.
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

// realReader builds a Reader over the real docproc-onenote on PATH, through the exec runner.
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
