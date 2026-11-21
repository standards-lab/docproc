//go:build linux

package office_test

import (
	"bufio"
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/office"
)

const expected = "ooxml/test/Docproc.Ooxml.Tests/Expected"

func TestReplayExpected(t *testing.T) {
	t.Parallel()
	root := tooltest.Tools(t, expected)
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
		t.Fatalf("no expected outputs under %s", root)
	}
	var mu sync.Mutex
	kinds := map[string]bool{}
	for _, jsonl := range outputs {
		rel, _ := filepath.Rel(root, jsonl)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			c, ins := standinConverter(t)
			ins.set(replayKey, jsonl)
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "PK\x03\x04"), Dir: t.TempDir()})
			stem := strings.TrimSuffix(jsonl, ".jsonl")
			if _, statErr := os.Stat(stem + ".exit"); statErr == nil {
				checkRefused(t, stem, recs, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			read := checkRead(t, stem, recs)
			mu.Lock()
			defer mu.Unlock()
			for _, k := range read {
				kinds[k] = true
			}
		})
	}
	t.Cleanup(func() {
		for _, k := range []string{"document", "sheet", "rows"} {
			if !kinds[k] && !t.Failed() {
				t.Errorf("no expected output holds a %s line", k)
			}
		}
	})
}

func checkRead(t *testing.T, stem string, recs []office.Record) []string {
	t.Helper()
	var warnings []office.Warning
	recs = slices.DeleteFunc(recs, func(r office.Record) bool {
		w, ok := r.(*office.Warning)
		if ok {
			warnings = append(warnings, *w)
		}
		return ok
	})
	lines := readLines(t, stem+".jsonl")
	if len(recs) != len(lines) {
		t.Fatalf("records: got %d, want %d, one per line", len(recs), len(lines))
	}
	var kinds []string
	var wantMarkdown, gotMarkdown strings.Builder
	for i, line := range lines {
		var head struct {
			Line     string `json:"line"`
			Markdown string `json:"markdown"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, head.Line)
		wantMarkdown.WriteString(head.Markdown)
		switch r := recs[i].(type) {
		case *office.Document:
			if head.Line != "document" || i != 0 {
				t.Errorf("line %d: a Document for a %s line", i+1, head.Line)
			}
			gotMarkdown.WriteString(r.Markdown)
			checkPropertyOrder(t, line, r.Properties)
		case *office.Sheet:
			if head.Line != "sheet" {
				t.Errorf("line %d: a Sheet for a %s line", i+1, head.Line)
			}
			gotMarkdown.WriteString(r.Markdown)
		case *office.Rows:
			if head.Line != "rows" {
				t.Errorf("line %d: a Rows for a %s line", i+1, head.Line)
			}
			gotMarkdown.WriteString(r.Markdown)
		default:
			t.Errorf("line %d: a %T for a %s line", i+1, recs[i], head.Line)
			continue
		}
		sameJSON(t, i+1, line, recs[i])
	}
	if gotMarkdown.String() != wantMarkdown.String() {
		t.Errorf("Markdown: the records' differs from the lines'")
	}
	var want []office.Warning
	for _, l := range readLines(t, stem+".stderr") {
		where, what, ok := strings.Cut(strings.TrimPrefix(string(l), "docproc-ooxml: warning: "), ": ")
		if !ok {
			t.Fatalf("stderr line %q isn't a warning", l)
		}
		want = append(want, office.Warning{Tool: "docproc-ooxml", Where: where, What: what})
	}
	if !slices.Equal(warnings, want) {
		t.Errorf("warnings: got %+v, want %+v", warnings, want)
	}
	return kinds
}

func sameJSON(t *testing.T, n int, line []byte, rec office.Record) {
	t.Helper()
	var want, got map[string]any
	if err := json.Unmarshal(line, &want); err != nil {
		t.Fatal(err)
	}
	delete(want, "line")
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("line %d: written back as\n%s\nnot\n%s", n, out, line)
	}
}

func checkPropertyOrder(t *testing.T, line []byte, props office.Properties) {
	t.Helper()
	var parts struct {
		Properties map[string]jsontext.Value `json:"properties"`
	}
	if err := json.Unmarshal(line, &parts); err != nil {
		t.Fatal(err)
	}
	for name, list := range map[string]office.PropertyList{"core": props.Core, "app": props.App, "custom": props.Custom} {
		var got []string
		for _, p := range list {
			got = append(got, p.Key)
		}
		if want := objectKeys(t, parts.Properties[name]); !slices.Equal(got, want) {
			t.Errorf("%s properties: got %q, want %q", name, got, want)
		}
	}
}

func objectKeys(t *testing.T, obj jsontext.Value) []string {
	t.Helper()
	if len(obj) == 0 {
		return nil
	}
	dec := jsontext.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.ReadToken(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for dec.PeekKind() != '}' {
		key, err := dec.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key.String())
		if err := dec.SkipValue(); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func checkRefused(t *testing.T, stem string, recs []office.Record, err error) {
	t.Helper()
	if len(recs) != 0 {
		t.Errorf("records: got %d, want none", len(recs))
	}
	exit, ok := errors.AsType[*cli.ExitError](err)
	if !ok {
		t.Fatalf("got %v, want a *cli.ExitError", err)
	}
	code, rerr := os.ReadFile(stem + ".exit")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if want := strings.TrimSpace(string(code)); want != "3" || exit.Code != 3 || !errors.Is(err, office.ErrRefused) {
		t.Errorf("exit code %d, want %s, a refusal", exit.Code, want)
	}
	stderr := readLines(t, stem+".stderr")
	if len(stderr) != 1 {
		t.Fatalf("stderr: %q, want one error line", stderr)
	}
	reason := strings.TrimPrefix(string(stderr[0]), "docproc-ooxml: error: ")
	if exit.Message != reason {
		t.Errorf("message %q, want %q", exit.Message, reason)
	}
	if want := cli.RefusalClass(reason); exit.Refusal() != want {
		t.Errorf("refusal %v, want %v", exit.Refusal(), want)
	}
	if strings.HasPrefix(reason, "encrypted") != (exit.Refusal() == dispatch.ErrEncrypted) {
		t.Errorf("reason %q classed %v", reason, exit.Refusal())
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
