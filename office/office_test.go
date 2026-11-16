//go:build linux

package office_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/tool"
)

// dockerStandin is a stand-in docker CLI enough to build a container Tool: its daemon's version,
// every image present, and a run that prints the stand-in docproc-ooxml's version.
func dockerStandin(args []string) int {
	switch {
	case len(args) > 0 && args[0] == "version":
		fmt.Println("27.0.0")
	case len(args) > 0 && args[0] == "run":
		fmt.Printf("%s\nschema 2\n", setting(versionKey))
	}
	return 0
}

// TestNewConverter: the constructor takes a docproc-ooxml at or past 0.1.0, a development build's
// version included, panics naming Config.Office given none, and refuses an older one, whatever
// minimum the Tool's own Spec declared; the spec's own minimum refuses an older one at the Tool's
// construction, and its version pattern one with no schema "2" line. A docproc-ooxml the
// container runner built is refused with ErrRequest.
func TestNewConverter(t *testing.T) {
	t.Parallel()
	func() {
		defer func() {
			if r := recover(); r != "office: Config.Office is nil" {
				t.Errorf("NewConverter with no office: recovered %v, want the panic naming Config.Office", r)
			}
		}()
		_, _ = office.NewConverter(t.Context(), office.Config{})
		t.Error("NewConverter with no office returned")
	}()

	ins := installStandin(t, "0.1.0+b9bb1a2")
	tl, err := tool.NewExec(t.Context(), ins.spec(office.OfficeSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if got := tl.Version().String(); got != "0.1.0" {
		t.Errorf("version: got %s, want 0.1.0", got)
	}
	if _, err := office.NewConverter(t.Context(), office.Config{Office: tl}); err != nil {
		t.Errorf("NewConverter over 0.1.0+b9bb1a2: %v", err)
	}

	ins = installStandin(t, "0.0.9")
	if _, err := tool.NewExec(t.Context(), ins.spec(office.OfficeSpec())); !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewExec over 0.0.9: got %v, want ErrTooOld", err)
	}
	lax := ins.spec(office.OfficeSpec())
	lax.Minimum = "0.0.1"
	old, err := tool.NewExec(t.Context(), lax)
	if err != nil {
		t.Fatal(err)
	}
	_, err = office.NewConverter(t.Context(), office.Config{Office: old})
	var verr *tool.VersionError
	if !errors.As(err, &verr) || !errors.Is(err, tool.ErrTooOld) {
		t.Fatalf("NewConverter over 0.0.9: got %v, want a VersionError wrapping ErrTooOld", err)
	}
	if verr.Found != "0.0.9" || verr.Minimum != office.OfficeMinimum {
		t.Errorf("VersionError: found %s, minimum %s", verr.Found, verr.Minimum)
	}

	ins = installStandin(t, "0.1.0")
	ins.set(schemaKey, "1")
	if _, err := tool.NewExec(t.Context(), ins.spec(office.OfficeSpec())); !errors.Is(err, tool.ErrNoVersion) {
		t.Errorf("NewExec over a schema 1 docproc-ooxml: got %v, want ErrNoVersion", err)
	}

	ins = installStandin(t, "0.1.0")
	docker := tooltest.Link(t, t.TempDir(), "docker", ins.settings)
	image := tool.Image{Ref: "example.invalid/ooxml:1@sha256:" + strings.Repeat("0", 64)}
	boxed, err := tool.NewContainer(t.Context(), docker, image, ins.spec(office.OfficeSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := office.NewConverter(t.Context(), office.Config{Office: boxed}); !errors.Is(err, office.ErrRequest) {
		t.Errorf("NewConverter over a container's docproc-ooxml: got %v, want ErrRequest", err)
	}
}

// TestConvertRun: a conversion runs docproc-ooxml once in the request's working directory, on the
// document's path in place, with a media directory of its own made there, nothing copied, and
// yields the document line as the Document, leaving the media directory alone in the working
// directory. Nothing runs until the sequence is ranged over.
func TestConvertRun(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	dir := t.TempDir()
	path := writeDocument(t, "report.docx", "PK\x03\x04 the document")
	seq := c.Convert(t.Context(), office.Request{Path: path, Dir: dir})
	if n := len(ins.runs()); n != 0 {
		t.Fatalf("docproc-ooxml ran %d times before the range", n)
	}
	var recs []office.Record
	for rec, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, rec)
	}
	doc := document(t, recs)
	if len(recs) != 1 || doc.Schema != "2" || doc.Format != "docx" || doc.ConvertedFrom != "" {
		t.Errorf("records: got %d, the document %+v", len(recs), doc)
	}
	all := ins.runs()
	if len(all) != 1 {
		t.Fatalf("runs: got %d, want 1", len(all))
	}
	run := all[0]
	if run.Dir != dir || run.Input != "PK\x03\x04 the document" {
		t.Errorf("working directory %s, input %q", run.Dir, run.Input)
	}
	got := entries(t, dir)
	if len(got) != 1 || !strings.HasPrefix(got[0], "media-") {
		t.Fatalf("working directory holds %q, want the media directory alone", got)
	}
	if want := []string{"--media", filepath.Join(dir, got[0]), "--", path}; !slices.Equal(run.Args, want) {
		t.Errorf("arguments: got %q, want %q", run.Args, want)
	}
}

// TestConvertRecords: each line is decoded into its record, in order, warnings among them: an
// xlsx's document, its sheets and their rows, typed cells, a closing rows line with Markdown
// alone, and a line of a kind the package doesn't know, kept with its JSON. Properties keep their
// stored order, and write it back.
func TestConvertRecords(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	lines := []string{
		`{"line":"document","schema":"2","format":"xlsx","properties":{"core":{"title":"T","creator":"c"},"app":{"Pages":3,"Application":"Calc"},"custom":{"Reviewed":true,"Due":"2026-01-02T00:00:00Z"}}}`,
		`{"line":"sheet","name":"Data","markdown":"# Data\n\n| A |\n| --- |\n"}`,
		`{"line":"rows","markdown":"| 1 |\n","rows":[{"row":1,"cells":[{"ref":"A1","value_type":"number","value":1},{"ref":"B1","formula":"A1*2","shared_from":"B2","col_span":2,"row_span":3},{"ref":"C1","value_type":"boolean","value":false},{"ref":"D1","value_type":"text","value":""}]}]}`,
		`{"line":"rows","markdown":"\n![chart](x)\n"}`,
		`{"line":"sheet","name":"Hidden","hidden":true}`,
		`{"line":"outline","levels":[1,2]}`,
	}
	ins.set(stdoutKey, strings.Join(lines, "\n")+"\n")
	ins.set(stderrKey, "docproc-ooxml: warning: sheet 1: notes not read\n")
	recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "x"), Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var warnings []office.Warning
	for _, rec := range recs {
		kinds = append(kinds, fmt.Sprintf("%T", rec))
		if w, ok := rec.(*office.Warning); ok {
			warnings = append(warnings, *w)
		}
	}
	kinds = slices.DeleteFunc(kinds, func(k string) bool { return k == "*office.Warning" })
	if want := []string{"*office.Document", "*office.Sheet", "*office.Rows", "*office.Rows", "*office.Sheet", "*office.UnknownRecord"}; !slices.Equal(kinds, want) {
		t.Errorf("records: got %q, want %q", kinds, want)
	}
	if want := []office.Warning{{Tool: "docproc-ooxml", Where: "sheet 1", What: "notes not read"}}; !slices.Equal(warnings, want) {
		t.Errorf("warnings: got %+v, want %+v", warnings, want)
	}
	recs = slices.DeleteFunc(recs, func(r office.Record) bool { _, ok := r.(*office.Warning); return ok })

	doc := document(t, recs)
	want := office.Properties{
		Core:   office.PropertyList{{Key: "title", Value: "T"}, {Key: "creator", Value: "c"}},
		App:    office.PropertyList{{Key: "Pages", Value: float64(3)}, {Key: "Application", Value: "Calc"}},
		Custom: office.PropertyList{{Key: "Reviewed", Value: true}, {Key: "Due", Value: "2026-01-02T00:00:00Z"}},
	}
	if fmt.Sprint(doc.Properties) != fmt.Sprint(want) {
		t.Errorf("properties: got %+v, want %+v", doc.Properties, want)
	}
	out, err := json.Marshal(doc.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"core":{"title":"T","creator":"c"},"app":{"Pages":3,"Application":"Calc"},"custom":{"Reviewed":true,"Due":"2026-01-02T00:00:00Z"}}`; string(out) != want {
		t.Errorf("properties written back: got %s, want %s", out, want)
	}

	if s := recs[1].(*office.Sheet); s.Name != "Data" || s.Hidden || s.Markdown != "# Data\n\n| A |\n| --- |\n" {
		t.Errorf("sheet: %+v", s)
	}
	rows := recs[2].(*office.Rows)
	wantRows := []office.Row{{Row: 1, Cells: []office.Cell{
		{Ref: "A1", ValueType: office.ValueNumber, Value: float64(1)},
		{Ref: "B1", Formula: "A1*2", SharedFrom: "B2", ColSpan: 2, RowSpan: 3},
		{Ref: "C1", ValueType: office.ValueBoolean, Value: false},
		{Ref: "D1", ValueType: office.ValueText, Value: ""},
	}}}
	if rows.Markdown != "| 1 |\n" || fmt.Sprint(rows.Rows) != fmt.Sprint(wantRows) {
		t.Errorf("rows: got %+v, want %+v", rows, wantRows)
	}
	if closing := recs[3].(*office.Rows); closing.Rows != nil || closing.Markdown != "\n![chart](x)\n" {
		t.Errorf("closing rows: %+v", closing)
	}
	if s := recs[4].(*office.Sheet); s.Name != "Hidden" || !s.Hidden {
		t.Errorf("hidden sheet: %+v", s)
	}
	if u := recs[5].(*office.UnknownRecord); u.Kind != "outline" || string(u.Raw) != lines[5] {
		t.Errorf("unknown record: %+v", u)
	}
}

// TestConvertDecodeErrors: a first line of another kind or schema is ErrSchema, and a line of a
// known kind that doesn't decode ends the sequence with its error, after the records before it,
// and removes the media directory.
func TestConvertDecodeErrors(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	for _, first := range []string{`{"line":"document","schema":"1","format":"docx"}`, `{"line":"sheet","schema":"2"}`} {
		ins.set(stdoutKey, first+"\n")
		dir := t.TempDir()
		if _, err := collect(t, c, office.Request{Path: writeDocument(t, "", "x"), Dir: dir}); !errors.Is(err, office.ErrSchema) {
			t.Errorf("%s: got %v, want ErrSchema", first, err)
		}
		if got := entries(t, dir); len(got) != 0 {
			t.Errorf("%s: working directory holds %q, want nothing", first, got)
		}
	}
	ins.set(stdoutKey, standinDocument+`{"line":"rows","rows":{"reshaped":true}}`+"\n")
	dir := t.TempDir()
	recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "x"), Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "decoding a rows line") || len(recs) != 1 {
		t.Errorf("got %d records, %v; want the document, then the rows line's error", len(recs), err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("working directory holds %q, want nothing", got)
	}
}

// TestConvertExits: docproc-ooxml's exit codes 1 to 4 are each a *cli.ExitError matching its own
// error and no other's, carrying the last error line's reason; a refusal whose reason begins
// "encrypted" is classed Encrypted, any other Unclassed. A failed conversion leaves nothing in the
// working directory, and its warnings come before its error.
func TestConvertExits(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	all := []error{office.ErrMediaNotWritten, office.ErrUsage, office.ErrRefused, office.ErrFailed}
	for _, tc := range []struct {
		code    string
		want    error
		message string
		refusal cli.Refusal
	}{
		{"1", office.ErrMediaNotWritten, "writing stdout: Broken pipe", cli.Unclassed},
		{"2", office.ErrUsage, "unknown option --bogus", cli.Unclassed},
		{"3", office.ErrRefused, "encrypted package (an OLE compound file holding an EncryptedPackage stream); decrypt it first", cli.Encrypted},
		{"3", office.ErrRefused, "an OLE compound file, such as a legacy binary document (doc, xls, ppt); convert it to docx, xlsx, or pptx first", cli.Unclassed},
		{"4", office.ErrFailed, "an unexpected System.NullReferenceException", cli.Unclassed},
	} {
		t.Run(tc.code+" "+tc.message[:9], func(t *testing.T) {
			ins.setFor(t, exitKey, tc.code)
			ins.setFor(t, stderrKey, "docproc-ooxml: warning: section 1: equations not read\ndocproc-ooxml: error: "+tc.message+"\n")
			ins.setFor(t, mediaKey, mediaName("a", ".png"))
			dir := t.TempDir()
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: dir})
			if len(recs) != 1 {
				t.Errorf("records: got %d, want the warning alone", len(recs))
			} else if w, ok := recs[0].(*office.Warning); !ok || w.What != "equations not read" {
				t.Errorf("record: got %+v, want the warning", recs[0])
			}
			for _, e := range all {
				if is := errors.Is(err, e); is != (e == tc.want) {
					t.Errorf("errors.Is(%v, %v): got %t", err, e, is)
				}
			}
			exit, ok := errors.AsType[*cli.ExitError](err)
			if !ok {
				t.Fatalf("got %v, want a *cli.ExitError", err)
			}
			if exit.Message != tc.message || exit.Program != "docproc-ooxml" {
				t.Errorf("program %s, message %q; want docproc-ooxml, %q", exit.Program, exit.Message, tc.message)
			}
			if got := exit.Refusal(); got != tc.refusal {
				t.Errorf("refusal: got %v, want %v", got, tc.refusal)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
		})
	}
}

// TestConvertCancel: a conversion whose context ends while docproc-ooxml runs ends with an error
// matching the context's, never a *cli.ExitError, and leaves nothing in the working directory.
func TestConvertCancel(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	ins.set(sleepKey, "1")
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The stand-in logs its run before it sleeps, so the log's existence says it is running. The
	// poll ends with ctx, so it never outlives the test when docproc-ooxml never runs.
	log := ins.log
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(log); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	path := writeDocument(t, "", "doc")
	var err error
	for _, err = range c.Convert(ctx, office.Request{Path: path, Dir: dir}) {
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	if _, ok := errors.AsType[*cli.ExitError](err); ok {
		t.Errorf("got a *cli.ExitError: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("working directory holds %q, want nothing", got)
	}
	for _, err = range c.Convert(ctx, office.Request{Path: path, Dir: dir}) {
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Convert on an ended context: got %v, want context.Canceled", err)
	}
}

// TestConvertStop: a caller that stops ranging after the Document ends the run, and the media
// directory stays, the caller's.
func TestConvertStop(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	ins.set(stdoutKey, standinDocument+`{"line":"sheet","name":"A"}`+"\n")
	dir := t.TempDir()
	for rec, err := range c.Convert(t.Context(), office.Request{Path: writeDocument(t, "", "doc"), Dir: dir}) {
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := rec.(*office.Document); !ok {
			t.Fatalf("got %T first", rec)
		}
		break
	}
	if got := entries(t, dir); len(got) != 1 || !strings.HasPrefix(got[0], "media-") {
		t.Errorf("working directory holds %q, want the media directory", got)
	}
}

// TestConvertRefuses: a request with no path, no working directory, or a scale out of range is
// refused with ErrRequest before docproc-ooxml runs, and a path that isn't there is an error.
func TestConvertRefuses(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	path := writeDocument(t, "", "doc")
	for name, req := range map[string]office.Request{
		"no path":      {Dir: t.TempDir()},
		"no directory": {Path: path},
		"scale":        {Path: path, Dir: t.TempDir(), MetafileScale: -1},
	} {
		if _, err := collect(t, c, req); !errors.Is(err, office.ErrRequest) {
			t.Errorf("%s: got %v, want ErrRequest", name, err)
		}
	}
	if _, err := collect(t, c, office.Request{Path: filepath.Join(t.TempDir(), "absent"), Dir: t.TempDir()}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an absent path: got %v, want os.ErrNotExist", err)
	}
	if n := len(ins.runs()); n != 0 {
		t.Errorf("docproc-ooxml ran %d times, want none", n)
	}
}

// TestConvertMedia: each medium with a file is resolved in the media directory, its Path the file
// as stored and its SHA256 its name's, without converters; a linked picture has neither, and an
// embedded package and an OLE object keep their kinds and ProgIDs. A name the envelope's rule
// can't have made fails the conversion.
func TestConvertMedia(t *testing.T) {
	t.Parallel()
	c, ins := standinConverter(t)
	png, emf, bin := mediaName("a", ".png"), mediaName("b", ".emf"), mediaName("c", ".bin")
	ins.set(mediaKey, strings.Join([]string{png, emf, bin}, ","))
	ins.set(stdoutKey, `{"line":"document","schema":"2","format":"docx","media":[`+
		`{"kind":"picture","file":"`+png+`","content_type":"image/png","part":"/word/media/image1.png","section":0},`+
		`{"kind":"picture","target":"https://example.invalid/a.png","section":1},`+
		`{"kind":"picture","file":"`+emf+`","content_type":"image/x-emf","part":"/word/media/image2.emf","section":0},`+
		`{"kind":"ole","file":"`+bin+`","part":"/word/embeddings/oleObject1.bin","prog_id":"Excel.Sheet.8","section":0}]}`+"\n")
	recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	doc := document(t, recs)
	if len(doc.Media) != 4 {
		t.Fatalf("media: got %+v", doc.Media)
	}
	dir := filepath.Dir(doc.Media[0].Path)
	if !strings.HasPrefix(filepath.Base(dir), "media-") {
		t.Errorf("media directory %s", dir)
	}
	for i, want := range []struct {
		kind office.MediumKind
		file string
	}{{office.Picture, png}, {office.Picture, ""}, {office.Picture, emf}, {office.OLE, bin}} {
		m := doc.Media[i]
		path, sum := "", ""
		if want.file != "" {
			path, sum = filepath.Join(dir, want.file), want.file[:64]
		}
		if m.Kind != want.kind || m.File != want.file || m.Path != path || m.SHA256 != sum || m.Converted {
			t.Errorf("medium %d: got %+v, want %s, %s, %s", i, m, want.kind, path, sum)
		}
	}
	if doc.Media[1].Target != "https://example.invalid/a.png" || *doc.Media[1].Section != 1 || doc.Media[3].ProgID != "Excel.Sheet.8" {
		t.Errorf("media: %+v", doc.Media)
	}

	ins.set(stdoutKey, `{"line":"document","schema":"2","format":"docx","media":[{"kind":"picture","file":"../outside.png"}]}`+"\n")
	if _, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "isn't a media name") {
		t.Errorf("a name outside the rule: got %v", err)
	}
}
