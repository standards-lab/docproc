//go:build linux && integration

package dispatch_test

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// documentFixture returns the path of one of document's committed fixtures, under its testdata.
func documentFixture(name string) string {
	return filepath.Join("..", "document", "testdata", filepath.FromSlash(name))
}

// encryptedPDF returns a PDF made from document's clean.pdf by this machine's qpdf, encrypted with
// args, in a directory of the test's own.
func encryptedPDF(t *testing.T, name string, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	args = append(append(append([]string{documentFixture("clean.pdf"), "--encrypt"}, args...), "--"), out)
	if b, err := exec.CommandContext(t.Context(), "qpdf", args...).CombinedOutput(); err != nil {
		t.Fatalf("qpdf making %s: %v: %s", name, err, b)
	}
	return out
}

// outputNames returns ev's outputs as their kind and name, in order.
func outputNames(ev dispatched) []string {
	var names []string
	for _, o := range ev.Outputs {
		names = append(names, string(o.Kind)+" "+o.Name)
	}
	return names
}

// TestDocumentStageEncryptedReal: of PDFs encrypted from document's clean.pdf, through this
// machine's qpdf, the two that need a password are Encrypted with nothing rendered; the one
// restricted only by its permissions renders its pages as any PDF does, as does clean.pdf, which
// isn't encrypted. inspect flags /Encrypt on the three encrypted, as before.
func TestDocumentStageEncryptedReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	for _, c := range []struct {
		pdf     string
		outcome dispatch.Outcome
		flagged bool
	}{
		{encryptedPDF(t, "open-password.pdf", "--user-password=user", "--owner-password=owner", "--bits=256"), dispatch.Encrypted, true},
		{encryptedPDF(t, "open-password-aes128.pdf", "--user-password=user", "--owner-password=owner", "--bits=128", "--use-aes=y"), dispatch.Encrypted, true},
		{encryptedPDF(t, "permissions.pdf", "--user-password=", "--owner-password=owner", "--bits=256", "--extract=n", "--modify=none"), dispatch.Prepared, true},
		{documentFixture("clean.pdf"), dispatch.Prepared, false},
	} {
		name := filepath.Base(c.pdf)
		ev := dispatchAll(t, d, c.pdf, nil)[0]
		flagged := slices.ContainsFunc(ev.Part.Flags, func(f inspect.Flag) bool { return f.Check == inspect.PDFEncrypt })
		if ev.Outcome != c.outcome || flagged != c.flagged {
			t.Errorf("%s: %s, %q, /Encrypt %t; want %s, %t", name, ev.Outcome, ev.Reason, flagged, c.outcome, c.flagged)
		}
		switch {
		case c.outcome == dispatch.Encrypted && (len(ev.Outputs) != 0 || !strings.Contains(ev.Reason, "needs a password")):
			t.Errorf("%s: %d outputs, %q; want none, a password", name, len(ev.Outputs), ev.Reason)
		case c.outcome == dispatch.Prepared && !slices.Contains(outputNames(ev), "image page 6"):
			t.Errorf("%s: outputs %q, want page 6's image", name, outputNames(ev))
		}
	}
}

// TestDocumentStageFormsReal: the generated acroform.pdf yields its forms record first, its
// fields the generator's exactly, then its page; signed.pdf, a page of a signed PDF, yields its
// one signature field, signed, its reference value failing nothing, then its page; and the clean
// PDF, with no AcroForm, yields none.
func TestDocumentStageFormsReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	for name, want := range map[string]string{
		"forms/acroform.pdf": `{"fields":[` +
			`{"name":"applicant.name","type":"text","value":"Ada Lovelace"},` +
			`{"name":"applicant.city","type":"text","value":"Zürich"},` +
			`{"name":"subscribe","type":"checkbox","value":"Yes","checked":true},` +
			`{"name":"newsletter","type":"checkbox","value":"Off","checked":false},` +
			`{"name":"contact","type":"radio","value":"Phone","checked":true},` +
			`{"name":"colour","type":"choice","value":"Green","options":["Red","Green","Blue"],"selected":["Green"]},` +
			`{"name":"languages","type":"choice","options":["English","French","German"],"selected":["en","de"]}]}`,
		"forms/signed.pdf": `{"fields":[{"name":"USGPOSignature","type":"signature","signed":true}]}`,
	} {
		ev := dispatchAll(t, d, documentFixture(name), nil)[0]
		if got := outputNames(ev); ev.Outcome != dispatch.Prepared || len(got) == 0 || got[0] != "record forms" || !slices.Contains(got, "image page 1") {
			t.Errorf("%s: %s, %q, %q; want the forms record first and page 1's image", name, ev.Outcome, ev.Reason, got)
			continue
		}
		rec := ev.Outputs[0]
		if string(rec.Value) != want || rec.Meta["fields"] != strconv.Itoa(strings.Count(want, `"name"`)) {
			t.Errorf("%s: forms record %s, %v;\nwant %s", name, rec.Value, rec.Meta, want)
		}
		t.Logf("%s: %s", name, rec.Value)
	}

	ev := dispatchAll(t, d, documentFixture("clean.pdf"), nil)[0]
	if slices.Contains(outputNames(ev), "record forms") {
		t.Error("clean.pdf, with no AcroForm, has a forms record")
	}
}

// TestDocumentStageAttachmentsReal: the generated attachments.pdf's name tree file, notes.txt, and
// file attachment annotation's, data.csv, are each found "attachment <name>", written by their
// SHA-256, inspected and routed again; the Screen annotation's clip.avi isn't extracted.
func TestDocumentStageAttachmentsReal(t *testing.T) {
	d, work := newDispatcherIn(t, allStages(t))
	events := dispatchAll(t, d, documentFixture("attachments/attachments.pdf"), func(ev *dispatch.Event) {
		if ev.Output == nil && ev.Part.Depth == 1 && filepath.Base(ev.Part.Path) != ev.Part.SHA256 {
			t.Errorf("%s at %s: want it named by its SHA-256", ev.Part.Name, ev.Part.Path)
		}
	})
	if len(events) != 3 {
		t.Fatalf("%d events, want the two attachments' and the PDF's: %v", len(events), steps(events))
	}
	for i, c := range []struct{ file, content string }{
		{"notes.txt", "Embedded through the name tree.\n"},
		{"data.csv", "name,count\nharbor,3\n"},
	} {
		att := events[i]
		if att.Part.Name != c.file || att.Part.Where != "attachment "+c.file || att.Part.SHA256 != sum(c.content) || att.Part.Depth != 1 {
			t.Errorf("attachment %d: found %s %q at depth %d, SHA-256 %s", i+1, att.Part.Name, att.Part.Where, att.Part.Depth, att.Part.SHA256)
		}
		t.Logf("%s: %s by %s, %s", att.Part.Name, att.Part.ContentType.Kind, att.Part.ContentType.Route, att.Outcome)
	}
	if pdf := events[2]; pdf.Outcome != dispatch.Prepared || pdf.Children != 2 || !slices.Contains(outputNames(pdf), "image page 1") {
		t.Errorf("attachments.pdf: %s, %d children, %q", pdf.Outcome, pdf.Children, outputNames(pdf))
	}
	wantEmpty(t, work)
}

// newDispatcherIn returns a dispatcher over stages and the working directory it makes parts' in.
func newDispatcherIn(t *testing.T, stages dispatch.Stages) (*dispatch.Dispatcher, string) {
	t.Helper()
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: stages, Index: dispatch.NewMemoryIndex(0), Dir: work})
	if err != nil {
		t.Fatal(err)
	}
	return d, work
}

// attachedPDF returns a one-page PDF whose name tree embeds name holding content, FlateDecode
// compressed.
func attachedPDF(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var z bytes.Buffer
	zw, err := zlib.NewWriterLevel(&z, zlib.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(" + name + ") 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Filespec /F (" + name + ") /UF (" + name + ") /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Type /EmbeddedFile /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", z.Len(), z.Bytes()),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// TestDocumentStageAttachmentBombReal: a PDF of about 8 KiB embedding 8 MiB of zeros, through this
// machine's pdfdetach, is saved whole under no bomb rule; under a 1 MiB size and a 100:1 ratio,
// pdfdetach is stopped past them, the file is a record, its check archive-bomb and its outcome
// quarantined, and nothing of it is left.
func TestDocumentStageAttachmentBombReal(t *testing.T) {
	gs := pinned(t, document.GhostscriptSpec())
	render, err := document.NewGhostscript(document.Config{Ghostscript: gs})
	if err != nil {
		t.Fatal(err)
	}
	prep := document.NewPreparer(document.Config{Renderer: render})
	pdf := input(t, "zeros.pdf", string(attachedPDF(t, "zeros.bin", make([]byte, 8<<20))))

	d, work := newDispatcherIn(t, dispatch.Stages{route.Document: pdfStage(t, prep, gs, inspect.Options{})})
	events := dispatchAll(t, d, pdf, nil)
	if len(events) != 2 || events[0].Part.Name != "zeros.bin" || events[0].Part.Size != 8<<20 || events[0].Part.Where != "attachment zeros.bin" {
		t.Errorf("no bomb rule: %v", steps(events))
	}
	wantEmpty(t, work)

	d, work = newDispatcherIn(t, dispatch.Stages{route.Document: pdfStage(t, prep, gs, inspect.Options{BombSize: 1 << 20, BombRatio: 100})})
	events = dispatchAll(t, d, pdf, nil)
	wantSteps(t, events, []step{{"zeros.pdf", 0, dispatch.Prepared}})
	rec := outputNamed(t, events[0], dispatch.RecordOutput, "attachment zeros.bin")
	var saved struct {
		Written uint64 `json:"written"`
	}
	if err := json.Unmarshal(rec.Value, &saved); err != nil || saved.Written <= 1<<20 || saved.Written >= 8<<20 {
		t.Errorf("written %d, %v: want past 1 MiB and short of 8 MiB", saved.Written, err)
	}
	if rec.Meta["check"] != string(inspect.ArchiveBomb) || rec.Meta["outcome"] != string(dispatch.Quarantined) {
		t.Errorf("zeros.bin's record: %v", rec.Meta)
	}
	t.Logf("zeros.bin: %s", rec.Meta["evidence"])
	wantEmpty(t, work)
}
