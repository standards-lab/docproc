//go:build linux

package onenote_test

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
)

const goldens = "onenote/tests/golden"

func TestReplayGoldens(t *testing.T) {
	t.Parallel()
	root := tooltest.Tools(t, goldens)
	var outputs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			outputs = append(outputs, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) == 0 {
		t.Fatalf("no goldens under %s", root)
	}
	var mu sync.Mutex
	kinds := map[string]bool{}
	refusals := map[error]bool{}
	for _, jsonl := range outputs {
		rel, _ := filepath.Rel(root, jsonl)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			r, on := standinReader(t)
			on.set(func(s *onenoteSettings) { s.Replay = jsonl })
			media := t.TempDir()
			recs, err := collect(r.Read(t.Context(), onenote.Request{Input: "input", Media: media}))
			stem := strings.TrimSuffix(jsonl, ".jsonl")
			if _, ok := errors.AsType[*cli.ExitError](err); ok {
				refusal := checkRefused(t, stem, recs, err)
				mu.Lock()
				defer mu.Unlock()
				refusals[refusal] = true
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			read := checkReplay(t, stem, media, recs)
			mu.Lock()
			defer mu.Unlock()
			for _, k := range read {
				kinds[k] = true
			}
		})
	}
	t.Cleanup(func() {
		if t.Failed() {
			return
		}
		for _, k := range []string{"notebook", "section", "page", "skipped"} {
			if !kinds[k] {
				t.Errorf("no golden holds a %s line", k)
			}
		}
		for _, r := range []error{dispatch.ErrEncrypted, dispatch.ErrReadElsewhere} {
			if !refusals[r] {
				t.Errorf("no golden is a refusal classed %v", r)
			}
		}
		t.Logf("%d goldens", len(outputs))
	})
}

func checkReplay(t *testing.T, stem, media string, recs []onenote.Record) (kinds []string) {
	t.Helper()
	lines := readLines(t, stem+".jsonl")
	want := goldenWarnings(t, stem+".stderr")
	var warnings []onenote.Warning
	i := 0
	for _, rec := range recs {
		if w, ok := rec.(*onenote.Warning); ok {
			warnings = append(warnings, *w)
			continue
		}
		if i >= len(lines) {
			t.Fatalf("records past the golden's %d lines", len(lines))
		}
		var line map[string]any
		if err := json.Unmarshal(lines[i], &line); err != nil {
			t.Fatal(err)
		}
		kind, _ := line["line"].(string)
		kinds = append(kinds, kind)
		var ok bool
		switch r := rec.(type) {
		case *onenote.Notebook:
			ok = kind == "notebook" && i == 0 && r.Schema == cli.Schema
		case *onenote.SectionGroup:
			ok = kind == "section_group"
		case *onenote.Section:
			ok = kind == "section"
		case *onenote.Page:
			ok = kind == "page"
			checkFiles(t, i+1, media, r)
		case *onenote.Skipped:
			ok = kind == "skipped"
		}
		if !ok {
			t.Errorf("line %d: a %T for a %s line", i+1, rec, kind)
		}
		delete(line, "line")
		sameJSON(t, i+1, line, rec)
		i++
	}
	if i != len(lines) {
		t.Errorf("records: %d, want %d, one per line", i, len(lines))
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings %+v\nwant %+v", warnings, want)
	}
	return kinds
}

func checkRefused(t *testing.T, stem string, recs []onenote.Record, err error) error {
	t.Helper()
	if lines := readLines(t, stem+".jsonl"); len(lines) != 0 {
		t.Errorf("a refusal's golden has %d lines", len(lines))
	}
	if len(recs) != 0 {
		t.Errorf("records: got %d, want none", len(recs))
	}
	exit, _ := errors.AsType[*cli.ExitError](err)
	if exit.Code != 3 || !errors.Is(err, onenote.ErrRefused) {
		t.Errorf("exit code %d, want 3, a refusal", exit.Code)
	}
	stderr := readLines(t, stem+".stderr")
	if len(stderr) != 1 {
		t.Fatalf("stderr: %q, want one error line", stderr)
	}
	reason := strings.TrimPrefix(string(stderr[0]), errorPrefix)
	if exit.Message != reason {
		t.Errorf("message %q, want %q", exit.Message, reason)
	}
	refusal := exit.Refusal()
	if refusal != cli.RefusalClass(reason) || (refusal != dispatch.ErrEncrypted && refusal != dispatch.ErrReadElsewhere) {
		t.Errorf("reason %q classed %v", reason, refusal)
	}
	return refusal
}

func goldenWarnings(t *testing.T, path string) []onenote.Warning {
	t.Helper()
	var warnings []onenote.Warning
	for _, l := range readLines(t, path) {
		rest, ok := strings.CutPrefix(string(l), "docproc-onenote: warning: ")
		where, what, cut := strings.Cut(rest, ": ")
		if !ok || !cut {
			t.Fatalf("stderr line %q isn't a warning", l)
		}
		warnings = append(warnings, onenote.Warning{Tool: "docproc-onenote", Where: where, What: what})
	}
	return warnings
}

func checkFiles(t *testing.T, n int, media string, p *onenote.Page) {
	t.Helper()
	for i, m := range p.Media {
		switch {
		case m.File == "":
			if m.Path != "" || m.SHA256 != "" {
				t.Errorf("line %d: medium %d, with no file: path %q, SHA-256 %q", n, i+1, m.Path, m.SHA256)
			}
		case m.Path != filepath.Join(media, m.File) || m.SHA256 != m.File[:64]:
			t.Errorf("line %d: medium %q: path %q, SHA-256 %q", n, m.File, m.Path, m.SHA256)
		}
	}
}

func sameJSON(t *testing.T, n int, line map[string]any, rec onenote.Record) {
	t.Helper()
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, line) {
		t.Errorf("line %d: written back as\n%s\nnot\n%v", n, out, line)
	}
}

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		lines = append(lines, bytes.Clone(sc.Bytes()))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}
