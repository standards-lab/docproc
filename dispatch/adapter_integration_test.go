//go:build linux && integration

package dispatch_test

import (
	"archive/zip"
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// builtMSG is email's built .msg, attaching a docx, a legacy xls, a TIFF, an EMF, and a .msg.
var builtMSG = filepath.Join("..", "email", "testdata", "built", "msg", "office-attachments.msg")

// allStages returns every adapter, over this machine's gs, qpdf, pdfdetach, magick, ffmpeg, and
// ffprobe, LibreOffice through the container runner in its pinned image, and the office, mail,
// and onenote mise pins (the last two through emailStage and onenoteStage), as a composition root
// wires them: the document stage reading each page's text layer, and the text stage, which runs
// no tool.
func allStages(t *testing.T) dispatch.Stages {
	t.Helper()
	gs := pinned(t, document.GhostscriptSpec())
	mg := pinned(t, picture.MagickSpec())
	lo, err := tool.NewContainer(t.Context(), "", tool.Image{Ref: picture.LibreOfficeImage}, picture.LibreOfficeSpec())
	if err != nil {
		t.Fatal(err)
	}
	of := pinned(t, office.OfficeSpec())

	render, err := document.NewGhostscript(document.Config{Ghostscript: gs})
	if err != nil {
		t.Fatal(err)
	}
	prep := document.NewPreparer(document.Config{Renderer: render})
	docs := pdfStage(t, prep, gs, inspect.Options{})
	oc, err := office.NewConverter(t.Context(), office.Config{Office: of, LibreOffice: lo, Magick: mg})
	if err != nil {
		t.Fatal(err)
	}
	offices, err := dispatch.NewOfficeStage(dispatch.OfficeConfig{Converter: oc, Version: dispatch.Versions(of, lo, mg)})
	if err != nil {
		t.Fatal(err)
	}
	pc, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: lo})
	if err != nil {
		t.Fatal(err)
	}
	pictures, err := dispatch.NewPictureStage(dispatch.PictureConfig{Converter: pc, Version: dispatch.Versions(mg, lo)})
	if err != nil {
		t.Fatal(err)
	}
	ff, fp := pinned(t, audio.FFmpegSpec()), pinned(t, audio.FFprobeSpec())
	splitter, err := audio.NewSplitter(audio.Config{FFmpeg: ff, FFprobe: fp})
	if err != nil {
		t.Fatal(err)
	}
	audios, err := dispatch.NewAudioStage(dispatch.AudioConfig{Splitter: splitter, Version: dispatch.Versions(ff, fp)})
	if err != nil {
		t.Fatal(err)
	}
	streamer, err := media.NewStreamer(media.Config{FFmpeg: ff, FFprobe: fp})
	if err != nil {
		t.Fatal(err)
	}
	videos, err := dispatch.NewVideoStage(dispatch.VideoConfig{Streamer: streamer, Version: dispatch.Versions(ff, fp)})
	if err != nil {
		t.Fatal(err)
	}
	return dispatch.Stages{
		route.Document:        docs,
		route.Office:          offices,
		route.VisionConverted: pictures,
		route.Vision:          dispatch.NewVisionStage(),
		route.Nested:          emailStage(t),
		route.OneNote:         onenoteStage(t),
		route.Audio:           audios,
		route.Video:           videos,
		route.Text:            dispatch.NewTextStage(),
	}
}

// pdfStage returns the document stage over prep, whose renderer runs gs, as a composition root
// wires it: reading each page's text layer, and with this machine's qpdf and pdfdetach, its
// embedded files held to opts's bomb thresholds.
func pdfStage(t *testing.T, prep *document.Preparer, gs *tool.Tool, opts inspect.Options) *dispatch.DocumentStage {
	t.Helper()
	qt, dt := pinned(t, document.QPDFSpec()), pinned(t, document.PDFDetachSpec())
	q, err := document.NewQPDF(document.Config{QPDF: qt})
	if err != nil {
		t.Fatal(err)
	}
	detach, err := document.NewDetacher(document.Config{PDFDetach: dt})
	if err != nil {
		t.Fatal(err)
	}
	s, err := dispatch.NewDocumentStage(dispatch.DocumentConfig{
		Preparer: prep, Version: dispatch.Versions(gs, qt, dt), Request: document.ImagesRequest{Text: true, Concurrency: 2},
		QPDF: q, PDFDetach: detach, Inspect: opts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// wantPNG fails the test unless the image output o is a PNG file.
func wantPNG(t *testing.T, o dispatch.Output) {
	t.Helper()
	b, err := os.ReadFile(o.Path)
	if err != nil || o.Kind != dispatch.ImageOutput || o.MediaType != "image/png" || !bytes.HasPrefix(b, []byte(route.PNGSignature)) {
		t.Errorf("%s: a %s %s of %d bytes, %v; want a PNG", o.Name, o.Kind, o.MediaType, len(b), err)
	}
}

// TestDocumentStageReal: the clean PDF's six pages, through gs, are each its text layer and a
// PNG, in page order, the text the page's own.
func TestDocumentStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join("..", "document", "testdata", "clean.pdf"), func(ev *dispatch.Event) {
		if o := ev.Output; o != nil && o.Kind == dispatch.ImageOutput {
			wantPNG(t, *o)
		}
	})
	wantSteps(t, events, []step{{"clean.pdf", 0, dispatch.Prepared}})
	var got []string
	for _, o := range events[0].Outputs {
		got = append(got, string(o.Kind)+" "+o.Name+" of "+o.Meta["count"])
	}
	var want []string
	for _, n := range []string{"1", "2", "3", "4", "5", "6"} {
		want = append(want, "text page "+n+" of 6", "image page "+n+" of 6")
	}
	if !slices.Equal(got, want) {
		t.Fatalf("outputs:\n got %q\nwant %q", got, want)
	}
	if text := string(events[0].Outputs[0].Value); !strings.Contains(text, "The Harbor Survey") {
		t.Errorf("page 1's text %.80q", text)
	}
}

// TestOfficeStageReal: a document's EMF, with LibreOffice and magick, is found as its PNG and
// prepared by the vision stage, as its PNGs are.
func TestOfficeStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join(officeFixtures, "media.libreoffice.docx"), nil)
	const emf = "670be6349ed84b27d6ca43d66ba6e9454c632f68de8d9b8fea78a480e90afb31"
	for _, ev := range events[:len(events)-1] {
		if ev.Part.ContentType.Route != route.Vision || ev.Outcome != dispatch.Prepared {
			t.Errorf("%s at %q: %s %s", ev.Part.Name, ev.Part.Where, ev.Part.ContentType.Route, ev.Outcome)
		}
	}
	if ev := byName(t, events, emf+".png"); ev.Part.Where != "media "+emf+".emf, as PNG" {
		t.Errorf("the EMF's PNG found at %q", ev.Part.Where)
	}
	if last := events[len(events)-1]; last.Part.Depth != 0 || last.Children != 4 {
		t.Errorf("the document: %s@%d, %d children", last.Part.Name, last.Part.Depth, last.Children)
	}
}

// TestOfficeStageRTFReal: an RTF, through LibreOffice in its pinned image to docx and then
// office, is one Markdown document converted from "rtf", holding the RTF's heading and table.
func TestOfficeStageRTFReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join("..", "office", "testdata", "rtf", "memo.rtf"), nil)
	wantSteps(t, events, []step{{"memo.rtf", 0, dispatch.Prepared}})
	if ev := events[0]; ev.Part.ContentType.Route != route.Office || ev.Part.ContentType.Kind != route.RTF {
		t.Errorf("memo.rtf: %s %s", ev.Part.ContentType.Route, ev.Part.ContentType.Kind)
	}
	md := outputNamed(t, events[0], dispatch.MarkdownOutput, "document")
	if md.Meta["converted_from"] != "rtf" {
		t.Errorf("converted from %q, want rtf", md.Meta["converted_from"])
	}
	for _, want := range []string{"# Harbor memo", "| North | 12 m |", "Next survey: Thursday."} {
		if !bytes.Contains(md.Value, []byte(want)) {
			t.Errorf("no %q in the Markdown:\n%s", want, md.Value)
		}
	}
}

// TestPictureStageReal: an SVG, a HEIC, a WebP, and an AVIF each convert to one PNG, and a TIFF
// of three pages to three, in page order, each naming its page and the count.
func TestPictureStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	for name, kind := range map[string]route.Kind{
		"shapes.svg": route.SVG, "two-images.heic": route.HEIC, "animated.webp": route.WebP, "animated.avif": route.AVIF,
	} {
		events := dispatchAll(t, d, filepath.Join("..", "picture", "testdata", name), func(ev *dispatch.Event) {
			if ev.Output != nil {
				wantPNG(t, *ev.Output)
			}
		})
		wantSteps(t, events, []step{{name, 0, dispatch.Prepared}})
		if o := events[0].Outputs; len(o) != 1 || o[0].Name != "picture" || o[0].Meta["from"] != string(kind) {
			t.Errorf("%s: outputs %+v", name, o)
		}
	}
	events := dispatchAll(t, d, filepath.Join("..", "picture", "testdata", "pages.tiff"), func(ev *dispatch.Event) {
		if ev.Output != nil {
			wantPNG(t, *ev.Output)
		}
	})
	wantSteps(t, events, []step{{"pages.tiff", 0, dispatch.Prepared}})
	var got []string
	for _, o := range events[0].Outputs {
		got = append(got, o.Name+" "+o.Meta["page"]+"/"+o.Meta["count"]+" "+o.Meta["from"])
	}
	if want := []string{"page 1 1/3 tiff", "page 2 2/3 tiff", "page 3 3/3 tiff"}; !slices.Equal(got, want) {
		t.Errorf("pages.tiff: outputs %q, want %q", got, want)
	}
}

// TestEmailStageReal: a .msg's attachments each reach their route's stage, recursively: the
// docx office's, its pictures vision's; the legacy xls office's through LibreOffice; the TIFF and
// the EMF the picture stage's; and the forwarded .msg email's again. A PDF attachment reaches the
// document stage.
func TestEmailStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, builtMSG, func(ev *dispatch.Event) {
		if ev.Part.ContentType.Route == route.VisionConverted && ev.Output != nil {
			wantPNG(t, *ev.Output)
		}
	})
	top := map[string]route.Route{
		"minutes.docx": route.Office, "budget.xls": route.Office, "scan.tif": route.VisionConverted,
		"diagram.emf": route.VisionConverted, "forwarded.msg": route.Nested,
	}
	for i, ev := range events {
		if ev.Outcome != dispatch.Prepared {
			t.Errorf("%s@%d at %q: %s %s, %s", ev.Part.Name, ev.Part.Depth, ev.Part.Where, ev.Part.ContentType.Route, ev.Outcome, ev.Reason)
		}
		if want, ok := top[ev.Part.Name]; ok && ev.Part.Depth == 1 {
			if ev.Part.ContentType.Route != want || !strings.HasPrefix(ev.Part.Where, "message, attachment ") {
				t.Errorf("%s at %q: route %s, want %s", ev.Part.Name, ev.Part.Where, ev.Part.ContentType.Route, want)
			}
			delete(top, ev.Part.Name)
		}
		// Post-order: a part's parent's event comes after it.
		if ev.Part.Depth > 0 && !slices.ContainsFunc(events[i+1:], func(p dispatched) bool { return p.Part.ID == ev.Part.Parent }) {
			t.Errorf("event %d, %s@%d, comes after its parent's", i, ev.Part.Name, ev.Part.Depth)
		}
	}
	if len(top) != 0 {
		t.Errorf("attachments never dispatched: %v", top)
	}
	if xls := byName(t, events, "budget.xls"); outputNamed(t, xls, dispatch.MarkdownOutput, "document").Meta["converted_from"] != "xls" {
		t.Error("budget.xls wasn't converted from xls")
	}

	events = dispatchAll(t, d, filepath.Join(mailFixtures(t), "msg", "tika", "testMSG_att_msg.msg"), nil)
	wantSteps(t, events, []step{
		{"smbprn.00009008.KdcPjl.pdf", 1, dispatch.Prepared},
		{"testMSG_att_msg.msg", 0, dispatch.Prepared},
	})
	if ev := events[0]; ev.Stage.Name != "document" || len(ev.Outputs) == 0 {
		t.Errorf("the PDF attachment: %v, %d outputs", ev.Stage, len(ev.Outputs))
	}
}

// TestOneNoteStageReal: a page's ink, an SVG, reaches the picture stage and becomes a PNG.
func TestOneNoteStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join(onenoteFixtures(t), "handwriting_recognition.one"), func(ev *dispatch.Event) {
		if ev.Part.ContentType.Route == route.VisionConverted && ev.Output != nil {
			wantPNG(t, *ev.Output)
		}
	})
	var ink *dispatched
	for i := range events {
		if strings.HasSuffix(events[i].Part.Name, ".svg") {
			ink = &events[i]
		}
	}
	if ink == nil || ink.Outcome != dispatch.Prepared || ink.Stage.Name != "picture" || len(ink.Outputs) != 1 {
		t.Fatalf("the ink: %+v", ink)
	}
}

// TestAdapterIdentities: each adapter names itself and its tools' version, refuses a missing
// reader or converter and an empty version, and hashes exactly the options that change what it
// prepares; the vision stage is versioned by this package.
func TestAdapterIdentities(t *testing.T) {
	prep := standinPreparer(t, gsSettings{Pages: 1})
	doc := func(req document.ImagesRequest) dispatch.Identity {
		t.Helper()
		req.Concurrency = max(req.Concurrency, 1)
		s, err := dispatch.NewDocumentStage(dispatch.DocumentConfig{Preparer: prep, Version: "gs 10.5", Request: req})
		if err != nil {
			t.Fatal(err)
		}
		return s.Identity()
	}
	base := doc(document.ImagesRequest{})
	if base.Name != "document" || base.Version != "gs 10.5" || len(base.Config) != 64 {
		t.Errorf("document identity %v", base)
	}
	if doc(document.ImagesRequest{DPI: document.DefaultDPI, Concurrency: 4, Dir: "elsewhere"}) != base {
		t.Error("the default DPI, a concurrency, or a Dir changed the document identity")
	}
	for name, req := range map[string]document.ImagesRequest{
		"dpi":        {DPI: 300},
		"text":       {Text: true},
		"max pixels": {MaxPixels: 1 << 20},
	} {
		if doc(req).Config == base.Config {
			t.Errorf("%s left the document identity's config unchanged", name)
		}
	}

	if _, err := dispatch.NewDocumentStage(dispatch.DocumentConfig{Preparer: prep, Request: document.ImagesRequest{Concurrency: 1}}); err == nil {
		t.Error("a document stage with no version was built")
	}

	officeTool := pinned(t, office.OfficeSpec())
	if got, want := dispatch.Versions(officeTool, nil), "docproc-ooxml "+officeTool.Version().String(); got != want {
		t.Errorf("Versions %q, want %q", got, want)
	}
	o := officeStage(t).Identity()
	if o.Name != "office" || o.Version != dispatch.Versions(officeTool) {
		t.Errorf("office identity %v", o)
	}
	c, err := office.NewConverter(t.Context(), office.Config{Office: officeTool})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.NewOfficeStage(dispatch.OfficeConfig{Converter: c}); err == nil {
		t.Error("an office stage with no version was built")
	}
	scaled, err := dispatch.NewOfficeStage(dispatch.OfficeConfig{Converter: c, Version: o.Version, MetafileScale: 5})
	if err != nil {
		t.Fatal(err)
	}
	if scaled.Identity().Config == o.Config {
		t.Error("a metafile scale left the office identity's config unchanged")
	}

	p := toollessPicture(t).Identity()
	if p.Name != "picture" || p.Version != "none" {
		t.Errorf("picture identity %v", p)
	}
	for prefix, id := range map[string]dispatch.Identity{"docproc-mail ": emailStage(t).Identity(), "docproc-onenote ": onenoteStage(t).Identity()} {
		if id.Config == "" || !strings.HasPrefix(id.Version, prefix) {
			t.Errorf("%s identity %v: want its tool's name and version, %s…", id.Name, id, prefix)
		}
	}
	v := dispatch.NewVisionStage().Identity()
	if v.Name != "vision" || v.Version != "1" || v != dispatch.NewVisionStage().Identity() {
		t.Errorf("vision identity %v", v)
	}
}

// TestOfficeNoProcessor: a WordPerfect document, an ODF package, and an Excel binary workbook
// have no processor yet, nor a legacy document or an RTF for a Converter with no LibreOffice;
// each reason names the kind.
func TestOfficeNoProcessor(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.Office: officeStage(t)}, dispatch.NewMemoryIndex(0))
	for name, c := range map[string]struct{ content, reason string }{
		"x.rtf":  {`{\rtf1\ansi hello}`, "x.rtf is an RTF document: no LibreOffice converter"},
		"x.wpd":  {"\xffWPC\x10\x00\x00\x00\x01\x0a", "office stage: no processor yet for a WordPerfect document"},
		"x.odt":  {zipOf(t, "mimetype", "application/vnd.oasis.opendocument.text", "content.xml", "<x/>"), "office stage: no processor yet for an ODF package"},
		"x.xlsb": {zipOf(t, "[Content_Types].xml", "<x/>"), "office stage: no processor yet for an Excel binary workbook"},
		"x.doc":  {"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1" + strings.Repeat("\x00", 504), "no LibreOffice converter"},
	} {
		ev := dispatchAll(t, d, input(t, name, c.content), nil)[0]
		if ev.Part.ContentType.Route != route.Office || ev.Outcome != dispatch.NoProcessor || !strings.Contains(ev.Reason, c.reason) {
			t.Errorf("%s: %s %s, %q; want no processor, %q", name, ev.Part.ContentType.Route, ev.Outcome, ev.Reason, c.reason)
		}
	}
}

// TestOfficeEncrypted dispatches docproc-ooxml's encrypted fixture, read in place, an OLE2
// compound file holding an EncryptedPackage stream, named .xlsx and .xlsb, through the pinned
// docproc-ooxml: each is routed office as an encrypted OOXML package, inspect flags it, and
// docproc-ooxml's refusal, its reason beginning "encrypted", ends the stage with ErrEncrypted, so
// it is encrypted. inspect's own encrypted.xlsx, a minimal compound file, is one docproc-ooxml
// refuses as malformed.
func TestOfficeEncrypted(t *testing.T) {
	stage := officeStage(t)
	content, err := os.ReadFile(tooltest.Tools(t, "ooxml/fixtures/generated/refused/encrypted.libreoffice.docx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"encrypted.xlsx", "encrypted.xlsb"} {
		// An index of its own, so the same bytes aren't seen.
		d := newDispatcher(t, dispatch.Stages{route.Office: stage}, dispatch.NewMemoryIndex(0))
		events := dispatchAll(t, d, input(t, name, string(content)), nil)
		wantSteps(t, events, []step{{name, 0, dispatch.Encrypted}})
		ev := events[0]
		if ev.Part.ContentType.Route != route.Office || !strings.Contains(ev.Part.ContentType.RouteReason, "an encrypted OOXML package") {
			t.Errorf("%s: route %q (%s), want office as an encrypted OOXML package", name, ev.Part.ContentType.Route, ev.Part.ContentType.RouteReason)
		}
		if !hasFlag(ev.Part.Flags, inspect.OOXMLEncrypted) || !errors.Is(ev.Err, dispatch.ErrEncrypted) {
			t.Errorf("%s: flags %+v, %v; want ooxml-encrypted and ErrEncrypted", name, ev.Part.Flags, ev.Err)
		}
	}
}

// TestOfficeStage runs the pinned office alone over a document holding PNGs and an EMF: its
// Markdown is the document's output, and each medium office wrote is a part of its own, found
// at its office name, the PNGs prepared by the vision stage, the EMF, with no converter, routed
// to the picture stage, which has none either. Every part's event comes before the document's.
func TestOfficeStage(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{
		route.Office: officeStage(t), route.Vision: dispatch.NewVisionStage(), route.VisionConverted: toollessPicture(t),
	}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join(officeFixtures, "media.libreoffice.docx"), nil)
	const (
		red   = "0fcca33bb0baf1a0669bd2fcdcaeb34e33d27196f7ffb5294b27aea8a9e46446"
		blue  = "10a86bd779a80e60e5d3314ec1edeab643cb454a4c7dfee671132090878d7aea"
		emf   = "670be6349ed84b27d6ca43d66ba6e9454c632f68de8d9b8fea78a480e90afb31"
		green = "df026100fd6fe38e22923c5152a33ea6f6879e38073fb47f022ed9d90dec65a8"
	)
	wantSteps(t, events, []step{
		{red + ".png", 1, dispatch.Prepared},
		{blue + ".png", 1, dispatch.Prepared},
		{emf + ".emf", 1, dispatch.NoProcessor},
		{green + ".png", 1, dispatch.Prepared},
		{"media.libreoffice.docx", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"media " + red + ".png", "media " + blue + ".png", "media " + emf + ".emf", "media " + green + ".png"}; !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if ev := byName(t, events, red+".png"); ev.Part.ContentType.Route != route.Vision || ev.Outputs[0].Kind != dispatch.ImageOutput || ev.Outputs[0].MediaType != "image/png" {
		t.Errorf("red: %s, %+v", ev.Part.ContentType.Route, ev.Outputs)
	}
	if ev := byName(t, events, emf+".emf"); ev.Part.ContentType.Route != route.VisionConverted || ev.Part.ContentType.Stored != "image/x-emf" {
		t.Errorf("emf: %s, %q", ev.Part.ContentType.Route, ev.Part.ContentType.Stored)
	}
	md := outputNamed(t, events[len(events)-1], dispatch.MarkdownOutput, "document")
	if !bytes.HasPrefix(md.Value, []byte("# Media")) || !bytes.Contains(md.Value, []byte(red+".png")) {
		t.Errorf("markdown: %.80q", md.Value)
	}
}

// TestEmailStage dispatches a .msg whose picture a .pst holds too, then the .pst, sharing one
// index. The .msg's message is a record and Markdown, its picture a part prepared by the vision
// stage; the .pst's store, folders, and messages are records, a message's Meta naming its folder's
// path, and its picture, the same content, is seen; its text and CSV attachments, which no stage
// takes, are no processor yet. Every part is found where its message says, and its event comes
// before the message's record, which comes right before its Markdown and says one follows.
func TestEmailStage(t *testing.T) {
	index := dispatch.NewMemoryIndex(0)
	d := newDispatcher(t, dispatch.Stages{route.Nested: emailStage(t), route.Vision: dispatch.NewVisionStage()}, index)
	const (
		moth = "moth.png"
		csv  = "отчёт.csv"
		note = "note-g.txt"
	)
	events := dispatchAll(t, d, filepath.Join(mailFixtures(t), "generated", "msg", "03-html-inline-image.msg"), nil)
	wantSteps(t, events, []step{{moth, 1, dispatch.Prepared}, {"03-html-inline-image.msg", 0, dispatch.Prepared}})
	if got := wheres(events); !slices.Equal(got, []string{"message, attachment 1"}) {
		t.Errorf("wheres %q", got)
	}
	msg := events[1]
	rec := outputNamed(t, msg, dispatch.RecordOutput, "message")
	body := outputNamed(t, msg, dispatch.MarkdownOutput, "message")
	if rec.Meta["subject"] == "" || body.Meta["subject"] != rec.Meta["subject"] || len(body.Value) == 0 {
		t.Errorf("message: %v, %v, %d bytes of Markdown", rec.Meta, body.Meta, len(body.Value))
	}
	recordThenMarkdown(t, msg, "message")
	outputNamed(t, msg, dispatch.RecordOutput, "store")

	// order is every event's label, in the order the loop takes them: an output's kind and name,
	// a part's own event's Where.
	var order []string
	events = dispatchAll(t, d, filepath.Join(mailFixtures(t), "generated", "pst", "mailbox.pst"), func(ev *dispatch.Event) {
		switch {
		case ev.Output != nil && ev.Part.Depth == 0:
			order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
		case ev.Output == nil && ev.Part.Depth == 1:
			order = append(order, ev.Part.Where)
		}
	})
	// A message's attachments come before its record, its record right before its Markdown,
	// which the record's Meta says follows, and the next message after both.
	for _, id := range []string{"2097220", "2097252", "2097380"} {
		at := slices.Index(order, "message "+id+", attachment 1")
		rec := slices.Index(order, "record message "+id)
		if at < 0 || rec != at+1 || order[rec+1] != "markdown message "+id ||
			(rec+2 < len(order) && strings.HasPrefix(order[rec+2], "message "+id)) {
			t.Errorf("message %s's events, in order %q: want its attachment, its record, then its Markdown", id, order)
		}
	}
	wantSteps(t, events, []step{
		{moth, 1, dispatch.Seen},
		{csv, 1, dispatch.NoProcessor},
		{note, 1, dispatch.NoProcessor},
		{"mailbox.pst", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{
		"message 2097220, attachment 1", "message 2097252, attachment 1", "message 2097380, attachment 1",
	}; !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if ev := byName(t, events, moth); ev.Recorded != dispatch.Prepared || ev.Stage.Name != "vision" {
		t.Errorf("moth.png: recorded %s by %v", ev.Recorded, ev.Stage)
	}
	pst := events[len(events)-1]
	if m := outputNamed(t, pst, dispatch.RecordOutput, "message 2097220").Meta; m["folder"] != "Top of Personal Folders/Inbox/Projects/2026" || m["id"] != "2097220" || m["markdown"] != "follows" {
		t.Errorf("message 2097220's meta %v", m)
	}
	if m := outputNamed(t, pst, dispatch.RecordOutput, "folder 32962").Meta; m["path"] != "Top of Personal Folders/Inbox/Projects/2026" {
		t.Errorf("folder 32962's meta %v", m)
	}
	recordThenMarkdown(t, pst, "message 2097156")
	if pst.Children != 3 {
		t.Errorf("the .pst's children %+v", pst.Children)
	}
}

// TestEmailResume: an input's After reaches the email stage, which reads the .pst from the
// message after it: the resumed store's event holds the whole read's store and folder records and
// the records of the messages after it, and finds the attachments of those messages alone. The
// resumed store's own entry isn't recorded, while the parts found in it are; a whole dispatch of
// it on the same index then prepares it, and a resumed one after that sees it. A .msg given an
// After fails, mail refusing --after with a single message.
func TestEmailResume(t *testing.T) {
	stage := emailStage(t)
	stages := dispatch.Stages{route.Nested: stage, route.Vision: dispatch.NewVisionStage()}
	path := filepath.Join(mailFixtures(t), "generated", "pst", "mailbox.pst")
	whole := dispatchAll(t, newDispatcher(t, stages, dispatch.NewMemoryIndex(0)), path, nil)
	wholeStore := whole[len(whole)-1]
	var ids []string
	for _, o := range wholeStore.Outputs {
		if o.Kind == dispatch.RecordOutput && strings.HasPrefix(o.Name, "message ") {
			ids = append(ids, o.Meta["id"])
		}
	}
	k := ids[len(ids)/2]
	after, err := strconv.ParseUint(k, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	// want are the whole read's outputs less the messages up to k's, and wantWheres the whole
	// read's parts found in the messages after k's.
	var want []string
	past := false
	for _, o := range wholeStore.Outputs {
		switch {
		case o.Name == "message "+k || strings.HasPrefix(o.Name, "message "+k+", "):
			past = true
		case !strings.HasPrefix(o.Name, "message ") || past:
			want = append(want, string(o.Kind)+" "+o.Name)
		}
	}
	var wantWheres []string
	for _, w := range wheres(whole) {
		id, _, _ := strings.Cut(strings.TrimPrefix(w, "message "), ",")
		if slices.Index(ids, id) > slices.Index(ids, k) {
			wantWheres = append(wantWheres, w)
		}
	}

	if len(want) >= len(wholeStore.Outputs) || len(wantWheres) == 0 || len(wantWheres) == len(wheres(whole)) {
		t.Fatalf("after %s: %d of %d outputs and %d of %d parts left; want some of each passed over, and a part left", k,
			len(want), len(wholeStore.Outputs), len(wantWheres), len(wheres(whole)))
	}

	index := dispatch.NewMemoryIndex(0)
	d := newDispatcher(t, stages, index)
	resumed := collect(t, d.Dispatch(t.Context(), dispatch.Input{Path: path, After: after}), nil)
	store := resumed[len(resumed)-1]
	if store.Outcome != dispatch.Prepared || store.Part.After != after {
		t.Fatalf("the resumed store: %s, after %d", store.Outcome, store.Part.After)
	}
	var got []string
	for _, o := range store.Outputs {
		got = append(got, string(o.Kind)+" "+o.Name)
	}
	if !slices.Equal(got, want) {
		t.Errorf("after %s, outputs:\n got %q\nwant %q", k, got, want)
	}
	if got := wheres(resumed); !slices.Equal(got, wantWheres) {
		t.Errorf("after %s, parts found %q, want %q", k, got, wantWheres)
	}
	for _, ev := range resumed[:len(resumed)-1] {
		if ev.Part.After != 0 {
			t.Errorf("%s, found below the input, carries after %d", ev.Part.Name, ev.Part.After)
		}
	}
	key := dispatch.Key{SHA256: store.Part.SHA256, Stage: stage.Identity()}
	if _, ok, err := index.Lookup(t.Context(), key); err != nil || ok {
		t.Errorf("the resumed store recorded: %t, %v", ok, err)
	}
	if again := dispatchAll(t, d, path, nil); again[len(again)-1].Outcome != dispatch.Prepared {
		t.Errorf("a whole dispatch after the resumed one: %s, want prepared", again[len(again)-1].Outcome)
	}
	for ev, err := range d.Dispatch(t.Context(), dispatch.Input{Path: path, After: after}) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Part.Depth == 0 && ev.Outcome != dispatch.Seen {
			t.Errorf("a resumed dispatch of a recorded store: %s, want seen", ev.Outcome)
		}
	}

	msg := filepath.Join(mailFixtures(t), "generated", "msg", "01-plain.msg")
	for ev, err := range d.Dispatch(t.Context(), dispatch.Input{Path: msg, After: 1}) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != dispatch.Failed || !errors.Is(ev.Err, email.ErrUsage) {
			t.Errorf("a .msg after 1: %s, %v; want failed, mail's usage error", ev.Outcome, ev.Err)
		}
	}
}

// TestEmailEmbedded: an embedded message whose content is no file is prepared inline, its record
// and Markdown named by its place and naming the message it is embedded in, while a file beside
// it, a PDF here, which no stage takes in this test, is found.
func TestEmailEmbedded(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.Nested: emailStage(t)}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join(mailFixtures(t), "msg", "tika", "testMSG_att_msg.msg"), nil)
	wantSteps(t, events, []step{
		{"smbprn.00009008.KdcPjl.pdf", 1, dispatch.NoProcessor},
		{"testMSG_att_msg.msg", 0, dispatch.Prepared},
	})
	if got := wheres(events); !slices.Equal(got, []string{"message, attachment 2"}) {
		t.Errorf("wheres %q", got)
	}
	if ev := events[0]; ev.Part.ContentType.Route != route.Document {
		t.Errorf("the PDF's route %s", ev.Part.ContentType.Route)
	}
	inline := outputNamed(t, events[1], dispatch.RecordOutput, "message, attachment 1")
	if inline.Meta["embedded_in"] != "message" || inline.Meta["subject"] != "Test Attachment" {
		t.Errorf("the embedded message's meta %v", inline.Meta)
	}
	outputNamed(t, events[1], dispatch.MarkdownOutput, "message, attachment 1")

	events = dispatchAll(t, d, filepath.Join(mailFixtures(t), "generated", "eml", "08-forward-attached-message.eml"), nil)
	wantSteps(t, events, []step{{"note-g.txt", 1, dispatch.NoProcessor}, {"08-forward-attached-message.eml", 0, dispatch.Prepared}})
	if m := outputNamed(t, events[1], dispatch.RecordOutput, "message, attachment 2").Meta; m["subject"] != "Notes on the engine (draft)" {
		t.Errorf("the forwarded message's meta %v", m)
	}
}

// TestEmailConversations dispatches mail's conversations.pst, whose Conversation History folder
// holds three conversations, a missed one, and a note. Each conversation-class item is a
// conversation record, "conversation <id>", the email package's Conversation, right before its
// transcript's Markdown of the same name, the record saying one follows; the Meta of both names
// its id, folder, topic, and whether it was missed. The note is a message record and Markdown, as
// before. Each item's Markdown is its hand truth's, in truth.json, found by its subject.
func TestEmailConversations(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.Nested: emailStage(t)}, dispatch.NewMemoryIndex(0))
	generated := filepath.Join(mailFixtures(t), "generated")
	events := dispatchAll(t, d, filepath.Join(generated, "pst", "conversations.pst"), nil)
	wantSteps(t, events, []step{{"conversations.pst", 0, dispatch.Prepared}})
	data, err := os.ReadFile(filepath.Join(generated, "truth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var truth struct {
		Items map[string]struct {
			Class    string `json:"class"`
			Subject  string `json:"subject"`
			Markdown string `json:"markdown"`
		} `json:"items"`
	}
	if err := jsonv2.Unmarshal(data, &truth); err != nil {
		t.Fatal(err)
	}
	markdownBy := map[string]string{}
	for _, item := range truth.Items {
		markdownBy[item.Subject] = item.Markdown
	}
	const folder = "Top of Personal Folders/Conversation History"
	store := events[0]
	var conversations, missed, messages int
	for i, o := range store.Outputs {
		if o.Kind != dispatch.RecordOutput {
			continue
		}
		kind, id, _ := strings.Cut(o.Name, " ")
		switch kind {
		case "conversation":
			var c email.Conversation
			if err := jsonv2.Unmarshal(o.Value, &c); err != nil {
				t.Fatalf("%s: %v", o.Name, err)
			}
			conversations++
			if c.Missed {
				missed++
			}
			want := map[string]string{
				"id": id, "folder": folder, "topic": c.Topic, "missed": strconv.FormatBool(c.Missed),
				"markdown": "follows",
			}
			if !maps.Equal(o.Meta, want) || strconv.FormatUint(c.ID, 10) != id || c.Topic == "" {
				t.Errorf("%s: meta %v, want %v", o.Name, o.Meta, want)
			}
			recordThenMarkdown(t, store, o.Name)
			md := store.Outputs[i+1]
			delete(want, "markdown")
			if string(md.Value) != c.Markdown || c.Markdown != markdownBy[c.Topic] || !maps.Equal(md.Meta, want) {
				t.Errorf("%s: Markdown %q, meta %v; the record's %q, the truth's %q", o.Name, md.Value, md.Meta, c.Markdown, markdownBy[c.Topic])
			}
		case "message":
			messages++
			recordThenMarkdown(t, store, o.Name)
			if md := store.Outputs[i+1]; o.Meta["folder"] != folder || string(md.Value) != markdownBy[o.Meta["subject"]] {
				t.Errorf("%s: meta %v, Markdown %q", o.Name, o.Meta, md.Value)
			}
		}
	}
	if conversations != 4 || missed != 1 || messages != 1 {
		t.Errorf("%d conversations, %d missed, %d messages; want 4, 1, and 1", conversations, missed, messages)
	}
}

// TestOneNoteStage dispatches a section with an image and ink, then a section holding the same
// image, sharing one index. Each page is a record, then its Markdown, beside the section and
// notebook records; the image, found on its page, is prepared by the vision stage, then seen; the
// ink, an SVG, goes to the picture stage, which has no processor without magick.
func TestOneNoteStage(t *testing.T) {
	index := dispatch.NewMemoryIndex(0)
	d := newDispatcher(t, dispatch.Stages{
		route.OneNote: onenoteStage(t), route.Vision: dispatch.NewVisionStage(), route.VisionConverted: toollessPicture(t),
	}, index)
	const (
		page  = "page 5eaf76b1-8dde-8745-a8a7-1a9865cc478a"
		jpg   = "d6d4898c203cbff35fe92e844bbf404064293314a71d1e6b24e907334e5bdff9.jpg"
		inked = "95c93a747acbbbeeb6d345faf0d17e86669bc7cdd0854155b588c58a7556d194.svg"
	)
	events := dispatchAll(t, d, filepath.Join(onenoteFixtures(t), "handwriting_recognition.one"), nil)
	var names []string
	for _, ev := range events {
		names = append(names, fmt.Sprintf("%s@%d:%s %s", filepath.Ext(ev.Part.Name), ev.Part.Depth, ev.Outcome, ev.Part.ContentType.Route))
	}
	want := []string{".jpg@1:prepared vision", ".svg@1:no processor yet vision-converted", ".one@0:prepared onenote"}
	if !slices.Equal(names, want) {
		t.Fatalf("events:\n got %q\nwant %q", names, want)
	}
	for _, w := range wheres(events) {
		if !strings.HasPrefix(w, "page ") || !strings.Contains(w, ", ") {
			t.Errorf("where %q: want the page and the medium's place", w)
		}
	}
	if ink := events[1]; ink.Part.Name != inked || ink.Part.Where != page+", media 2" || ink.Part.ContentType.Kind != route.SVG {
		t.Errorf("ink: %s at %q, %s", ink.Part.Name, ink.Part.Where, ink.Part.ContentType.Kind)
	}
	section := events[2]
	outputNamed(t, section, dispatch.RecordOutput, "notebook")
	outputNamed(t, section, dispatch.RecordOutput, "section 3ef70c2a-1787-854a-a0b4-ef192956c142")
	md := outputNamed(t, section, dispatch.MarkdownOutput, page)
	if md.Meta["title"] != "Test Page" || !bytes.HasPrefix(md.Value, []byte("# Test Page")) {
		t.Errorf("page markdown %v, %.40q", md.Meta, md.Value)
	}
	recordThenMarkdown(t, section, page)

	events = dispatchAll(t, d, filepath.Join(onenoteFixtures(t), "New Section 1.one"), nil)
	img := events[0]
	if img.Part.SHA256+".jpg" != jpg || img.Outcome != dispatch.Seen || img.Recorded != dispatch.Prepared {
		t.Errorf("the shared image: %s %s, recorded %s", img.Part.Name, img.Outcome, img.Recorded)
	}
	if last := events[len(events)-1]; last.Part.Depth != 0 || last.Outcome != dispatch.Prepared {
		t.Errorf("the section's event: %s@%d %s", last.Part.Name, last.Part.Depth, last.Outcome)
	}
}

// onenoteFixtures returns onenote.rs's sections among docproc-onenote's committed inputs, read in
// place under tools/.
func onenoteFixtures(t *testing.T) string {
	t.Helper()
	return tooltest.Tools(t, "onenote/tests/fixtures/onenote-rs")
}

// mailFixtures returns docproc-mail's committed inputs, read in place under tools/.
func mailFixtures(t *testing.T) string {
	t.Helper()
	return tooltest.Tools(t, "mail/tests/fixtures")
}

// The format packages' fixtures the adapters' tests dispatch.
var (
	officeFixtures = filepath.Join("..", "office", "testdata", "ooxml")
)

// pinned builds the tool spec declares, found on PATH through the exec runner: docproc-ooxml,
// docproc-mail, and docproc-onenote among them, built from tools/ in this checkout.
func pinned(t *testing.T, spec tool.Spec) *tool.Tool {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatalf("%s, which mise pins, run the check through mise run check: %v", spec.Program, err)
	}
	return tl
}

// officeStage returns an office stage over the pinned office alone, with no LibreOffice or
// magick.
func officeStage(t *testing.T) *dispatch.OfficeStage {
	t.Helper()
	tl := pinned(t, office.OfficeSpec())
	c, err := office.NewConverter(t.Context(), office.Config{Office: tl})
	if err != nil {
		t.Fatal(err)
	}
	s, err := dispatch.NewOfficeStage(dispatch.OfficeConfig{Converter: c, Version: dispatch.Versions(tl)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// emailStage returns an email stage over the pinned mail.
func emailStage(t *testing.T) *dispatch.EmailStage {
	t.Helper()
	tl := pinned(t, email.MailSpec())
	r, err := email.NewReader(email.Config{Mail: tl})
	if err != nil {
		t.Fatal(err)
	}
	s, err := dispatch.NewEmailStage(dispatch.EmailConfig{Reader: r, Version: dispatch.Versions(tl)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// onenoteStage returns a onenote stage over docproc-onenote on PATH.
func onenoteStage(t *testing.T) *dispatch.OneNoteStage {
	t.Helper()
	tl := pinned(t, onenote.OneNoteSpec())
	r, err := onenote.NewReader(onenote.Config{OneNote: tl})
	if err != nil {
		t.Fatal(err)
	}
	s, err := dispatch.NewOneNoteStage(dispatch.OneNoteConfig{Reader: r, Version: dispatch.Versions(tl)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// recordThenMarkdown fails the test unless ev's record named name comes right before its
// Markdown of the same name, the order the email and onenote stages yield a message or a page in.
func recordThenMarkdown(t *testing.T, ev dispatched, name string) {
	t.Helper()
	i := slices.IndexFunc(ev.Outputs, func(o dispatch.Output) bool { return o.Name == name })
	if i < 0 || i+1 >= len(ev.Outputs) || ev.Outputs[i].Kind != dispatch.RecordOutput ||
		ev.Outputs[i+1].Kind != dispatch.MarkdownOutput || ev.Outputs[i+1].Name != name {
		t.Errorf("%s: %q's outputs aren't its record, then its Markdown", ev.Part.Name, name)
	}
}

// zipOf returns a ZIP of entries, each name and content in order, stored uncompressed with its
// sizes in its local header, as an ODF package's mimetype is.
func zipOf(t *testing.T, entries ...string) string {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for i := 0; i < len(entries); i += 2 {
		content := entries[i+1]
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name: entries[i], Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte(content)),
			CompressedSize64: uint64(len(content)), UncompressedSize64: uint64(len(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
