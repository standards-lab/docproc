//go:build linux

package stages_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

type fakeQPDF struct {
	locked  bool
	lockErr error
	form    document.Form
	formErr error
	forms   atomic.Int32
}

func (q *fakeQPDF) RequiresPassword(context.Context, string) (bool, error) {
	return q.locked, q.lockErr
}

func (q *fakeQPDF) Form(context.Context, string) (document.Form, error) {
	q.forms.Add(1)
	return q.form, q.formErr
}

type fakeInfo struct {
	rec   metadata.PDF
	err   error
	reads atomic.Int32
}

func (f *fakeInfo) Metadata(context.Context, string) (metadata.PDF, error) {
	f.reads.Add(1)
	return f.rec, f.err
}

type fakeDetach struct {
	files   []string
	listErr error
}

func (d *fakeDetach) List(context.Context, string) ([]document.Attachment, error) {
	if d.listErr != nil {
		return nil, d.listErr
	}
	var list []document.Attachment
	for i := 0; i < len(d.files); i += 2 {
		list = append(list, document.Attachment{Number: i/2 + 1, Name: d.files[i]})
	}
	return list, nil
}

func (d *fakeDetach) Save(_ context.Context, _ string, number int, w io.Writer) error {
	content := d.files[(number-1)*2+1]
	if content == "fail" {
		return errors.New("told to fail")
	}
	_, err := io.Copy(w, struct{ io.Reader }{strings.NewReader(content)})
	return err
}

func qpdfFailed(message string) error {
	return fmt.Errorf("document: qpdf: %w", &tool.ExitError{Program: "qpdf", Code: 2, Message: message})
}

func formsDocumentStage(t *testing.T, q stages.QPDFReader, d stages.PDFDetacher, opts inspect.Options) *stages.DocumentStage {
	t.Helper()
	return stages.NewDocumentStage(stages.DocumentConfig{
		Preparer: standinPreparer(t, gsSettings{Pages: 3}), QPDF: q, PDFDetach: d, PDFInfo: &fakeInfo{}, Version: "gs 10.08.0",
		Request: document.ImagesRequest{Text: true, Concurrency: 1}, Inspect: opts,
	})
}

func documentOrder(t *testing.T, s *stages.DocumentStage, pdf string) ([]dispatched, []string) {
	t.Helper()
	d := newDispatcher(t, dispatch.Stages{route.Document: s}, dispatch.NewMemoryIndex(0))
	var order []string
	events := dispatchAll(t, d, pdf, func(ev *dispatch.Event) {
		switch {
		case ev.Warning != nil:
			order = append(order, "warning "+ev.Warning.Tool+" "+ev.Warning.Where)
		case ev.Output != nil && ev.Part.Depth == 0:
			order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
		case ev.Output == nil && ev.Part.Depth > 0:
			order = append(order, "part "+ev.Part.Where)
		}
	})
	return events, order
}

var pagesOrder = []string{"text page 1", "image page 1", "text page 2", "image page 2", "text page 3", "image page 3"}

func TestDocumentStagePassword(t *testing.T) {
	pdf := input(t, "locked.pdf", "%PDF-1.7\n%%EOF\n")
	q := &fakeQPDF{locked: true}
	events, _ := documentOrder(t, formsDocumentStage(t, q, &fakeDetach{files: []string{"a.txt", "a"}}, inspect.Options{}), pdf)
	wantSteps(t, events, []step{{"locked.pdf", 0, dispatch.Encrypted}})
	ev := events[0]
	if len(ev.Outputs) != 0 || !strings.Contains(ev.Reason, "needs a password") || !errors.Is(ev.Err, dispatch.ErrEncrypted) {
		t.Errorf("locked.pdf: %d outputs, %q, %v", len(ev.Outputs), ev.Reason, ev.Err)
	}
	if !slices.ContainsFunc(ev.Part.Flags, func(f inspect.Flag) bool {
		return f.Check == inspect.PDFEncrypt && f.Class == inspect.Active && strings.HasPrefix(f.Evidence, "qpdf: ")
	}) {
		t.Errorf("locked.pdf's flags %+v: want qpdf's pdf-encrypt", ev.Part.Flags)
	}
	if n := q.forms.Load(); n != 0 {
		t.Errorf("%d forms read of a PDF that needs a password", n)
	}

	_, order := documentOrder(t, formsDocumentStage(t, &fakeQPDF{}, &fakeDetach{}, inspect.Options{}), input(t, "permissions.pdf", "%PDF-1.7\n%%EOF\n"))
	if !slices.Equal(order, pagesOrder) {
		t.Errorf("permissions only:\n got %q\nwant %q", order, pagesOrder)
	}
}

func TestDocumentStageForms(t *testing.T) {
	form := document.Form{Fields: []document.FormField{
		{Name: "applicant.name", Type: document.TextField, Value: "Ada Lovelace"},
		{Name: "subscribe", Type: document.CheckboxField, Value: "Yes", Checked: new(true)},
		{
			Name: "colour", Type: document.ChoiceField, Value: "Green",
			Options: []string{"Red", "Green"}, Selected: []string{"Green"},
		},
	}}
	pdf := input(t, "form.pdf", "%PDF-1.7\n%%EOF\n")
	events, order := documentOrder(t, formsDocumentStage(t, &fakeQPDF{form: form}, &fakeDetach{}, inspect.Options{}), pdf)
	wantSteps(t, events, []step{{"form.pdf", 0, dispatch.Prepared}})
	if want := append([]string{"record forms"}, pagesOrder...); !slices.Equal(order, want) {
		t.Errorf("order:\n got %q\nwant %q", order, want)
	}
	rec := events[0].Outputs[0]
	want := `{"fields":[` +
		`{"name":"applicant.name","type":"text","value":"Ada Lovelace"},` +
		`{"name":"subscribe","type":"checkbox","value":"Yes","checked":true},` +
		`{"name":"colour","type":"choice","value":"Green","options":["Red","Green"],"selected":["Green"]}]}`
	if string(rec.Value) != want || rec.MediaType != dispatch.MediaForm || rec.Meta["fields"] != "3" {
		t.Errorf("forms record %s, %s, %v;\nwant %s", rec.Value, rec.MediaType, rec.Meta, want)
	}
	var back document.Form
	if err := json.Unmarshal(rec.Value, &back); err != nil || len(back.Fields) != 3 {
		t.Errorf("the record doesn't read back as a Form: %v", err)
	}

	_, order = documentOrder(t, formsDocumentStage(t, &fakeQPDF{form: document.Form{Fields: []document.FormField{}}}, &fakeDetach{}, inspect.Options{}), pdf)
	if !slices.Equal(order, pagesOrder) {
		t.Errorf("no fields:\n got %q\nwant %q", order, pagesOrder)
	}

	for name, q := range map[string]*fakeQPDF{
		"form":     {form: form, formErr: qpdfFailed("told to fail the form")},
		"password": {form: form, lockErr: qpdfFailed("told to fail the password check")},
	} {
		events, order := documentOrder(t, formsDocumentStage(t, q, &fakeDetach{}, inspect.Options{}), pdf)
		if ev := events[0]; ev.Outcome != dispatch.Prepared {
			t.Errorf("%s failing: %s, %q", name, ev.Outcome, ev.Reason)
		}
		if want := append([]string{"warning qpdf form"}, pagesOrder...); !slices.Equal(order, want) {
			t.Errorf("%s failing:\n got %q\nwant %q", name, order, want)
		}
	}

	events, _ = documentOrder(t, formsDocumentStage(t, &fakeQPDF{formErr: errors.New("a pipe broke on docproc's side")}, &fakeDetach{}, inspect.Options{}), pdf)
	if ev := events[0]; ev.Outcome != dispatch.Failed || errors.Is(ev.Err, dispatch.ErrRefused) {
		t.Errorf("the run's failure: %s, %v; want failed, unrecorded", ev.Outcome, ev.Err)
	}
}

func TestDocumentStageAttachments(t *testing.T) {
	png := pngOf(t)
	detach := &fakeDetach{files: []string{
		"KSBASE.WQ2", "a Quattro Pro file's bytes\n",
		"../../evil.png", png,
		"broken.bin", "fail",
	}}
	q := &fakeQPDF{form: document.Form{Fields: []document.FormField{{Name: "f", Type: document.TextField, Value: "v"}}}}
	s := formsDocumentStage(t, q, detach, inspect.Options{})
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages: dispatch.Stages{route.Document: s, route.Vision: stages.NewVisionStage()}, Index: dispatch.NewMemoryIndex(0), Dir: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	events := dispatchAll(t, d, input(t, "attached.pdf", "%PDF-1.7\n%%EOF\n"), func(ev *dispatch.Event) {
		switch {
		case ev.Output != nil && ev.Part.Depth == 0:
			order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
		case ev.Output == nil && ev.Warning == nil && ev.Part.Depth > 0:
			order = append(order, "part "+ev.Part.Where)
			if ev.Part.Path != "" && (filepath.Base(ev.Part.Path) != ev.Part.SHA256 || !inside(work, ev.Part.Path)) {
				t.Errorf("%s at %s: want its SHA-256 inside the working directory", ev.Part.Name, ev.Part.Path)
			}
		}
	})
	wantSteps(t, events, []step{
		{"KSBASE.WQ2", 1, dispatch.NoProcessor},
		{"../../evil.png", 1, dispatch.Prepared},
		{"broken.bin", 1, dispatch.Failed},
		{"attached.pdf", 0, dispatch.Prepared},
	})
	want := append([]string{
		"record forms", "part attachment KSBASE.WQ2", "part attachment ../../evil.png", "part attachment broken.bin",
	}, pagesOrder...)
	if !slices.Equal(order, want) {
		t.Errorf("order:\n got %q\nwant %q", order, want)
	}
	if ev := events[0]; ev.Part.SHA256 != sum("a Quattro Pro file's bytes\n") {
		t.Errorf("KSBASE.WQ2: SHA-256 %s", ev.Part.SHA256)
	}
	if ev := events[1]; ev.Part.ContentType.Route != route.Vision || ev.Part.SHA256 != sum(png) {
		t.Errorf("../../evil.png: %s, %s", ev.Part.ContentType.Route, ev.Part.SHA256)
	}
	if ev := events[2]; ev.Part.SHA256 != "" || !strings.Contains(ev.Reason, "doesn't save: told to fail") {
		t.Errorf("broken.bin: %q, %q", ev.Part.SHA256, ev.Reason)
	}
	wantEmpty(t, work)

	detach.listErr = &tool.ExitError{Program: "pdfdetach", Code: 1, Message: "told to fail the listing"}
	events, order = documentOrder(t, formsDocumentStage(t, &fakeQPDF{}, detach, inspect.Options{}), input(t, "unlisted.pdf", "%PDF-1.7\n%%EOF\n"))
	if want := append([]string{"warning pdfdetach attachments"}, pagesOrder...); events[0].Outcome != dispatch.Prepared || !slices.Equal(order, want) {
		t.Errorf("a failed listing: %s, %q, %q", events[0].Outcome, events[0].Reason, order)
	}
}

func pngOf(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestDocumentStageAttachmentBomb(t *testing.T) {
	zeros := string(make([]byte, 8<<20))
	s := formsDocumentStage(t, &fakeQPDF{}, &fakeDetach{files: []string{"zeros.bin", zeros}}, inspect.Options{BombRatio: 1000, BombSize: 1 << 20})
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: dispatch.Stages{route.Document: s}, Index: dispatch.NewMemoryIndex(0), Dir: work})
	if err != nil {
		t.Fatal(err)
	}
	events := dispatchAll(t, d, input(t, "bomb.pdf", "%PDF-1.7\n%%EOF\n"), nil)
	wantSteps(t, events, []step{{"zeros.bin", 1, dispatch.Quarantined}, {"bomb.pdf", 0, dispatch.Prepared}})
	child := events[0]
	if len(child.Part.Flags) != 1 || child.Part.Flags[0].Check != inspect.ArchiveBomb || child.Part.Flags[0].Class != inspect.Unsafe ||
		!strings.Contains(child.Part.Flags[0].Evidence, "from a 15-byte PDF") {
		t.Errorf("zeros.bin's flags: %+v", child.Part.Flags)
	}
	if len(events[1].Outputs) != 6 {
		t.Errorf("%d outputs, want six pages'", len(events[1].Outputs))
	}
	wantEmpty(t, work)
}

func TestDocumentStageAttachmentBytesCap(t *testing.T) {
	const mib = 1 << 20
	pdf := "%PDF-1.7\n%%EOF\n"
	s := formsDocumentStage(t, &fakeQPDF{}, &fakeDetach{files: []string{
		"a.bin", strings.Repeat("a", mib), "b.bin", strings.Repeat("b", mib),
	}}, inspect.Options{})
	work := t.TempDir()
	limit := int64(len(pdf)) + mib + mib/2
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2, Stages: dispatch.Stages{route.Document: s},
		Index: dispatch.NewMemoryIndex(0), Dir: work, Limits: dispatch.Limits{Bytes: limit},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := dispatchAll(t, d, input(t, "two.pdf", pdf), func(ev *dispatch.Event) {
		if ev.Part.Name != "b.bin" {
			return
		}
		if left, err := filepath.Glob(filepath.Join(work, "*", "entry-*")); err != nil || len(left) > 0 {
			t.Errorf("partial files at b.bin's event: %q, %v", left, err)
		}
	})
	wantSteps(t, events, []step{{"a.bin", 1, dispatch.NoProcessor}, {"b.bin", 1, dispatch.Limited}, {"two.pdf", 0, dispatch.Prepared}})
	wantLimited(t, events[1], "attachment b.bin", limit)
	if len(events[2].Outputs) != 6 {
		t.Errorf("%d outputs, want six pages'", len(events[2].Outputs))
	}
	wantEmpty(t, work)
}

func TestDocumentIdentity(t *testing.T) {
	prep := standinPreparer(t, gsSettings{Pages: 1})
	id := func(req document.ImagesRequest, opts inspect.Options) dispatch.Identity {
		t.Helper()
		return stages.NewDocumentStage(stages.DocumentConfig{
			Preparer: prep, QPDF: &fakeQPDF{}, PDFDetach: &fakeDetach{}, PDFInfo: &fakeInfo{}, Version: "gs 10.5 (2025)", Request: req, Inspect: opts,
		}).Identity()
	}
	base := id(document.ImagesRequest{Concurrency: 1}, inspect.Options{})
	if base.Version != "gs 10.5 (2025)" || base.Name != "document" {
		t.Errorf("identity %v", base)
	}
	if id(document.ImagesRequest{DPI: document.DefaultDPI, Concurrency: 4, PagesPerProcess: 3, Dir: "/elsewhere"}, inspect.Options{}) != base {
		t.Error("the default DPI, concurrency, PagesPerProcess, or Dir changed the identity")
	}
	for name, other := range map[string]dispatch.Identity{
		"dpi":        id(document.ImagesRequest{DPI: 300, Concurrency: 1}, inspect.Options{}),
		"text":       id(document.ImagesRequest{Text: true, Concurrency: 1}, inspect.Options{}),
		"max pixels": id(document.ImagesRequest{MaxPixels: 1 << 20, Concurrency: 1}, inspect.Options{}),
		"bomb rule":  id(document.ImagesRequest{Concurrency: 1}, inspect.Options{BombRatio: 1, BombSize: 1}),
	} {
		if other == base {
			t.Errorf("%s left the identity alone", name)
		}
	}
}

type pdfinfoSettings struct {
	Info   map[string]string `json:"info,omitzero"`
	Exit   int               `json:"exit,omitzero"`
	Stderr string            `json:"stderr,omitzero"`
}

func pdfinfoStandin(r tooltest.Run) int {
	var settings pdfinfoSettings
	if err := r.Settings(&settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(r.Args) == 1 && r.Args[0] == "-v" {
		fmt.Fprintln(os.Stderr, "pdfinfo version 26.10.0")
		return 0
	}
	if settings.Exit != 0 {
		fmt.Fprint(os.Stderr, settings.Stderr)
		return settings.Exit
	}
	mode := "summary"
	switch {
	case slices.Contains(r.Args, "-custom"):
		mode = "custom"
	case slices.Contains(r.Args, "-meta"):
		mode = "meta"
	}
	if _, err := os.Stdout.WriteString(settings.Info[mode]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

var pdfinfoGoldens = filepath.Join("..", "document", "testdata", "info")

func standinInfo(t *testing.T, s pdfinfoSettings) *document.InfoReader {
	t.Helper()
	spec := document.PDFInfoSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "pdfinfo", s)
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := document.NewInfoReader(document.Config{PDFInfo: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func goldenInfo(t *testing.T, name string) pdfinfoSettings {
	t.Helper()
	s := pdfinfoSettings{Info: map[string]string{}}
	for mode, ext := range map[string]string{"custom": "custom.txt", "meta": "meta.xml", "summary": "summary.txt"} {
		b, err := os.ReadFile(filepath.Join(pdfinfoGoldens, name+"."+ext))
		if err != nil {
			t.Fatal(err)
		}
		s.Info[mode] = string(b)
	}
	return s
}

func metadataStage(t *testing.T, q stages.QPDFReader, info stages.PDFInfoReader) *stages.DocumentStage {
	t.Helper()
	return stages.NewDocumentStage(stages.DocumentConfig{
		Preparer: standinPreparer(t, gsSettings{Pages: 3}), QPDF: q, PDFDetach: &fakeDetach{}, PDFInfo: info,
		Version: "gs 10.08.0", Request: document.ImagesRequest{Text: true, Concurrency: 1},
	})
}

func storedPDFMetadata(t *testing.T, content dispatch.Content, o dispatch.Output) metadata.PDF {
	t.Helper()
	wire, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"kind":"record","name":"metadata","media_type":"` + dispatch.MediaPDFMetadata + `","sha256":"` + o.SHA256 + `"}`; string(wire) != want {
		t.Errorf("the record's wire %s, want %s", wire, want)
	}
	r, err := content.Open(t.Context(), o.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(stored); hex.EncodeToString(sum[:]) != o.SHA256 {
		t.Errorf("the stored record's SHA-256 isn't the output's %s", o.SHA256)
	}
	var rec metadata.PDF
	if err := json.Unmarshal(stored, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func utcTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return at.UTC()
}

func TestDocumentStageMetadata(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		fixture, golden string
		want            func(packet string) metadata.PDF
	}{
		{
			filepath.Join("..", "document", "testdata", "info", "info.pdf"), "info",
			func(packet string) metadata.PDF {
				return metadata.PDF{
					Version: "1.6",
					Info: &metadata.PDFInfo{
						Title: "Harbor survey, Zürich", Author: "Ada Lovelace", Subject: "Soundings", Keywords: "harbor, survey",
						Creator: "generate.go", Producer: "docproc fixtures",
						CreationDate: utcTime(t, "2026-03-15T04:00:00Z"), ModDate: utcTime(t, "2026-04-01T12:00:00Z"),
						Custom: metadata.Properties{{Key: "Project", Value: "docproc"}, {Key: "Reviewed", Value: "D:20260402"}},
					},
					XMP: &metadata.XMP{
						Title: "Harbor survey", Creators: []string{"Ada Lovelace", "Charles Babbage"},
						Description: "Soundings of the inner harbor, Zürich edition", Subjects: []string{"harbor", "survey"},
						Languages: []string{"en", "de"}, CreateDate: utcTime(t, "2026-03-15T04:00:00Z"),
						ModifyDate: utcTime(t, "2026-04-01T12:00:00Z"), CreatorTool: "generate.go",
						Producer: "docproc fixtures", Keywords: "harbor, survey",
						DocumentID: "uuid:6f1c2a3e-4b5d-4e6f-8a9b-0c1d2e3f4a5b", Packet: packet,
					},
				}
			},
		},
		{
			filepath.Join("..", "document", "testdata", "clean.pdf"), "clean",
			func(packet string) metadata.PDF {
				return metadata.PDF{
					Version: "1.7",
					Info:    &metadata.PDFInfo{Title: "document fixtures", Creator: "Typst 0.15.1"},
					XMP: &metadata.XMP{
						Title: "document fixtures", Languages: []string{"en"}, CreatorTool: "Typst 0.15.1",
						DocumentID: "eT6gjrd4sCliyxkT8QXOGA==", Packet: packet,
					},
				}
			},
		},
	} {
		t.Run(c.golden, func(t *testing.T) {
			t.Parallel()
			golden := goldenInfo(t, c.golden)
			content := dispatch.NewMemoryContent()
			d, err := dispatch.New(dispatch.Config{
				Content: content, Puts: 2, Index: dispatch.NewMemoryIndex(0), Dir: t.TempDir(),
				Stages: dispatch.Stages{route.Document: metadataStage(t, &fakeQPDF{}, standinInfo(t, golden))},
			})
			if err != nil {
				t.Fatal(err)
			}
			var order []string
			events := dispatchAll(t, d, c.fixture, func(ev *dispatch.Event) {
				if ev.Output != nil {
					order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
				}
			})
			wantSteps(t, events, []step{{filepath.Base(c.fixture), 0, dispatch.Prepared}})
			if want := append([]string{"record metadata"}, pagesOrder...); !slices.Equal(order, want) {
				t.Errorf("order:\n got %q\nwant %q", order, want)
			}
			o := events[0].Outputs[0]
			if o.MediaType != dispatch.MediaPDFMetadata || len(events[0].Warnings) != 0 {
				t.Errorf("media type %q, warnings %+v", o.MediaType, events[0].Warnings)
			}
			got := storedPDFMetadata(t, content, o)
			if want := c.want(golden.Info["meta"]); !reflect.DeepEqual(got, want) {
				t.Errorf("record\n %+v\nwant\n %+v", got, want)
			}
			if got.XMP == nil || !strings.HasPrefix(got.XMP.Packet, "<?xpacket begin=") {
				t.Errorf("the raw packet isn't kept: %+v", got.XMP)
			}
		})
	}
}

func TestDocumentStageMetadataUnread(t *testing.T) {
	t.Parallel()
	pdf := input(t, "x.pdf", "%PDF-1.7\n%%EOF\n")

	info := &fakeInfo{}
	events, order := documentOrder(t, metadataStage(t, &fakeQPDF{locked: true}, info), pdf)
	wantSteps(t, events, []step{{"x.pdf", 0, dispatch.Encrypted}})
	want := dispatch.Warning{Tool: "qpdf", Where: "metadata", What: "the PDF needs a password, so its metadata isn't read"}
	if !slices.Equal(events[0].Warnings, []dispatch.Warning{want}) || !slices.Equal(order, []string{"warning qpdf metadata"}) {
		t.Errorf("a PDF that needs a password: warnings %+v, order %q", events[0].Warnings, order)
	}
	if !hasFlag(events[0].Part.Flags, inspect.PDFEncrypt) || !errors.Is(events[0].Err, dispatch.ErrEncrypted) || info.reads.Load() != 0 {
		t.Errorf("a PDF that needs a password: flags %+v, %v, %d reads", events[0].Part.Flags, events[0].Err, info.reads.Load())
	}

	failing := standinInfo(t, pdfinfoSettings{Exit: 1, Stderr: "Syntax Error: Couldn't find trailer dictionary\n"})
	events, order = documentOrder(t, metadataStage(t, &fakeQPDF{}, failing), pdf)
	wantSteps(t, events, []step{{"x.pdf", 0, dispatch.Prepared}})
	if want := append([]string{"warning pdfinfo metadata"}, pagesOrder...); !slices.Equal(order, want) {
		t.Errorf("pdfinfo failing:\n got %q\nwant %q", order, want)
	}
	if w := events[0].Warnings; len(w) != 1 || !strings.Contains(w[0].What, "Couldn't find trailer dictionary") {
		t.Errorf("pdfinfo failing: warnings %+v", w)
	}

	unparsed := standinInfo(t, pdfinfoSettings{Info: map[string]string{"custom": "Title:           T\n", "meta": "<x:xmpmeta>"}})
	events, order = documentOrder(t, metadataStage(t, &fakeQPDF{}, unparsed), pdf)
	if want := append([]string{"record metadata", "warning pdfinfo metadata"}, pagesOrder...); !slices.Equal(order, want) || events[0].Outcome != dispatch.Prepared {
		t.Errorf("an XMP packet that doesn't parse: %s,\n got %q\nwant %q", events[0].Outcome, order, want)
	}
	var rec metadata.PDF
	if err := json.Unmarshal(events[0].Outputs[0].Value, &rec); err != nil || rec.XMP == nil || rec.XMP.Packet != "<x:xmpmeta>" || rec.Info.Title != "T" {
		t.Errorf("an XMP packet that doesn't parse: record %+v, %v", rec, err)
	}

	_, order = documentOrder(t, metadataStage(t, &fakeQPDF{}, standinInfo(t, pdfinfoSettings{Info: map[string]string{"summary": "PDF version:     1.4\n"}})), pdf)
	if !slices.Equal(order, pagesOrder) {
		t.Errorf("no metadata:\n got %q\nwant %q", order, pagesOrder)
	}
}
