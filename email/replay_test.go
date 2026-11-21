//go:build linux

package email_test

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/tooltest"
)

const goldens = "mail/tests/golden"

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
	filled := 0
	for _, jsonl := range outputs {
		rel, _ := filepath.Rel(root, jsonl)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			r, mail := standinReader(t)
			mail.set(func(s *mailSettings) { s.Replay = jsonl })
			media := t.TempDir()
			recs, err := collect(r.Read(t.Context(), email.Request{Input: "input", Media: media}))
			if err != nil {
				t.Fatal(err)
			}
			read, n := checkReplay(t, strings.TrimSuffix(jsonl, ".jsonl"), media, recs)
			mu.Lock()
			defer mu.Unlock()
			for _, k := range read {
				kinds[k] = true
			}
			filled += n
		})
	}
	t.Cleanup(func() {
		for _, k := range []string{"store", "folder", "message", "skipped"} {
			if !kinds[k] && !t.Failed() {
				t.Errorf("no golden holds a %s line", k)
			}
		}
		if filled == 0 && !t.Failed() {
			t.Error("no golden's sender took its SMTP address from its headers")
		}
		t.Logf("%d goldens; %d senders took their SMTP address from their headers", len(outputs), filled)
	})
}

func checkReplay(t *testing.T, stem, media string, recs []email.Record) (kinds []string, filled int) {
	t.Helper()
	lines := readLines(t, stem+".jsonl")
	want := goldenWarnings(t, stem+".stderr")
	folders := map[string]string{}
	var warnings []email.Warning
	i := 0
	for _, rec := range recs {
		if w, ok := rec.(*email.Warning); ok {
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
		switch r := rec.(type) {
		case *email.Store:
			if kind != "store" || i != 0 || r.Schema != "2" {
				t.Errorf("line %d: a Store of schema %q for a %s line", i+1, r.Schema, kind)
			}
		case *email.Folder:
			if kind != "folder" {
				t.Errorf("line %d: a Folder for a %s line", i+1, kind)
			}
			folders[strconv.FormatUint(r.ID, 10)] = strings.Join(r.Path, "/")
		case *email.Message:
			if kind != "message" {
				t.Errorf("line %d: a Message for a %s line", i+1, kind)
			}
			checkFiles(t, i+1, media, r)
		case *email.Skipped:
			if kind != "skipped" {
				t.Errorf("line %d: a Skipped for a %s line", i+1, kind)
			}
		default:
			t.Errorf("line %d: a %T for a %s line", i+1, rec, kind)
			i++
			continue
		}
		filled += sameJSON(t, i+1, line, rec)
		i++
	}
	if i != len(lines) {
		t.Errorf("records: %d, want %d, one per line", i, len(lines))
	}
	for i := range want {
		want[i].Where = rewritten(want[i].Where, folders)
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings %+v\nwant %+v", warnings, want)
	}
	return kinds, filled
}

func rewritten(where string, folders map[string]string) string {
	rest, ok := strings.CutPrefix(where, "folder ")
	if !ok {
		return where
	}
	id, tail, more := strings.Cut(rest, ",")
	path, ok := folders[id]
	if !ok {
		return where
	}
	if more {
		return "folder " + path + "," + tail
	}
	return "folder " + path
}

func goldenWarnings(t *testing.T, path string) []email.Warning {
	t.Helper()
	var warnings []email.Warning
	for _, l := range readLines(t, path) {
		rest, ok := strings.CutPrefix(string(l), "docproc-mail: warning: ")
		where, what, cut := strings.Cut(rest, ": ")
		if !ok || !cut {
			t.Fatalf("stderr line %q isn't a warning", l)
		}
		warnings = append(warnings, email.Warning{Tool: "docproc-mail", Where: where, What: what})
	}
	return warnings
}

func checkFiles(t *testing.T, n int, media string, m *email.Message) {
	t.Helper()
	for _, a := range m.Attachments {
		if a.File != "" && (a.Path != filepath.Join(media, a.File) || a.SHA256 != a.File[:64]) {
			t.Errorf("line %d: attachment %q: path %q, SHA-256 %q", n, a.File, a.Path, a.SHA256)
		}
		if a.Message != nil {
			checkFiles(t, n, media, a.Message)
		}
	}
	for _, md := range m.Media {
		if md.Path != filepath.Join(media, md.File) || md.SHA256 != md.File[:64] {
			t.Errorf("line %d: medium %q: path %q, SHA-256 %q", n, md.File, md.Path, md.SHA256)
		}
	}
}

func sameJSON(t *testing.T, n int, line map[string]any, rec email.Record) int {
	t.Helper()
	want := dropFalse(line).(map[string]any)
	delete(want, "line")
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	filled := 0
	if _, ok := rec.(*email.Message); ok {
		filled = dropAdded(t, n, got, want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("line %d: written back as\n%s\nnot\n%v", n, out, line)
	}
	return filled
}

func dropAdded(t *testing.T, n int, got, want map[string]any) int {
	t.Helper()
	filled := 0
	gotSender, _ := got["sender"].(map[string]any)
	wantSender, _ := want["sender"].(map[string]any)
	if added, ok := gotSender["smtp_address"].(string); ok && wantSender["smtp_address"] == nil {
		headers, _ := want["transport_headers"].(string)
		if !strings.Contains(strings.ToLower(headers), strings.ToLower(added)) {
			t.Errorf("line %d: the sender's SMTP address %q, which its headers don't hold", n, added)
		}
		delete(gotSender, "smtp_address")
		if len(gotSender) == 0 {
			delete(got, "sender")
		}
		filled++
	}
	gotAtts, _ := got["attachments"].([]any)
	wantAtts, _ := want["attachments"].([]any)
	for i := range min(len(gotAtts), len(wantAtts)) {
		ga, _ := gotAtts[i].(map[string]any)
		wa, _ := wantAtts[i].(map[string]any)
		gm, _ := ga["message"].(map[string]any)
		wm, _ := wa["message"].(map[string]any)
		if gm != nil && wm != nil {
			filled += dropAdded(t, n, gm, wm)
		}
	}
	return filled
}

func dropFalse(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if e == false {
				delete(x, k)
				continue
			}
			x[k] = dropFalse(e)
		}
	case []any:
		for i, e := range x {
			x[i] = dropFalse(e)
		}
	}
	return v
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
