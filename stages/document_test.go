//go:build linux

package stages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

// fakeQPDF is a qpdfReader that answers as it is told: whether the PDF needs a password, or
// lockErr, and its form, or formErr; it counts the forms it is asked for.
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

// fakeDetach is a pdfDetacher over files, each attachment's stored name and content, in
// order, numbered from 1; a content of "fail" fails its save, and listErr fails the listing.
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
	// Written in 32 KiB pieces, as a pipe delivers them, so a refused write stops the save.
	_, err := io.Copy(w, struct{ io.Reader }{strings.NewReader(content)})
	return err
}

// qpdfFailed is qpdf's failure to read the PDF, which is the content's: exit 2, with its message.
func qpdfFailed(message string) error {
	return fmt.Errorf("document: qpdf: %w", &tool.ExitError{Program: "qpdf", Code: 2, Message: message})
}

// formsDocumentStage returns a document stage over three pages of their own, reading their text,
// given qpdf and pdfdetach as q and d, and the bomb rule of opts.
func formsDocumentStage(t *testing.T, q stages.QPDFReader, d stages.PDFDetacher, opts inspect.Options) *stages.DocumentStage {
	t.Helper()
	return stages.NewDocumentStage(stages.DocumentConfig{
		Preparer: standinPreparer(t, gsSettings{Pages: 3}), QPDF: q, PDFDetach: d, Version: "gs 10.08.0",
		Request: document.ImagesRequest{Text: true, Concurrency: 1}, Inspect: opts,
	})
}

// documentOrder dispatches pdf through s and returns the dispatch's events, each with its
// outputs, and every event's place in order: a part's output by its name, a found part by its
// Where, and a warning by its tool and Where.
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

// pagesOrder are the outputs of formsDocumentStage's three pages, in order.
var pagesOrder = []string{"text page 1", "image page 1", "text page 2", "image page 2", "text page 3", "image page 3"}

// TestDocumentStagePassword: a PDF qpdf says needs a password raises qpdf's finding as its own
// flag, pdf-encrypt, through Item.Flag, and is Encrypted, its reason saying so, with no output
// and no form read; one encrypted only by its permissions, which opens without a password, has
// its pages prepared as any other.
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

// TestDocumentStageForms: a PDF whose AcroForm has fields yields its forms record first, the
// fields as qpdf's form gives them, as JSON, and their count in its Meta, then its pages; a PDF
// whose form has no fields yields none; qpdf failing to read the form, or to answer the password
// check, as the content's is a warning at "form" naming qpdf, and the pages are prepared, the part
// prepared; and a failure of the run's class, outside qpdf's own exit, fails the part unrecorded.
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
	if string(rec.Value) != want || rec.MediaType != "application/json" || rec.Meta["fields"] != "3" {
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

// TestDocumentStageAttachments: each embedded file is a part written under its SHA-256 inside
// the working directory, never its stored name, a traversing one among them, named by its stored
// name, found "attachment <name>", and routed by its content, after the forms record and before
// the pages; a file that doesn't save is an unread child, failed, its reason why; a listing that
// fails is a warning at "attachments" naming pdfdetach; and nothing is left once the dispatch is
// done.
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

// pngOf returns a 1x1 PNG.
func pngOf(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestDocumentStageAttachmentBomb: an embedded file of 8 MiB of zeros from a PDF of a few bytes
// passes the ratio at once; under a 1 MiB bomb size, its saving stops just past it, its partial
// file is removed, and it is an unread child, quarantined, flagged archive-bomb, unsafe, with the
// bytes written as evidence; the PDF's pages are prepared.
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

// TestDocumentIdentity: the identity hashes the request resolved, its DPI's default filled, so a
// zero DPI and the default are one identity, and the bomb rule; concurrency and the request's Dir
// are out of it; and the tools' version lines are its Version.
func TestDocumentIdentity(t *testing.T) {
	prep := standinPreparer(t, gsSettings{Pages: 1})
	id := func(req document.ImagesRequest, opts inspect.Options) dispatch.Identity {
		t.Helper()
		return stages.NewDocumentStage(stages.DocumentConfig{
			Preparer: prep, QPDF: &fakeQPDF{}, PDFDetach: &fakeDetach{}, Version: "gs 10.5 (2025)", Request: req, Inspect: opts,
		}).Identity()
	}
	base := id(document.ImagesRequest{Concurrency: 1}, inspect.Options{})
	if base.Version != "gs 10.5 (2025)" || base.Name != "document" {
		t.Errorf("identity %v", base)
	}
	if id(document.ImagesRequest{DPI: document.DefaultDPI, Concurrency: 4, Dir: "/elsewhere"}, inspect.Options{}) != base {
		t.Error("the default DPI, concurrency, or Dir changed the identity")
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
