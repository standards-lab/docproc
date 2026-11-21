//go:build linux && integration

package stages_test

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
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

var builtMSG = filepath.Join("..", "email", "testdata", "built", "msg", "office-attachments.msg")

func allStages(t *testing.T) dispatch.Stages {
	t.Helper()
	return builtSet(t, stages.Options{Document: document.ImagesRequest{Text: true}, Concurrency: 2}).Stages
}

func builtSet(t *testing.T, opts stages.Options) stages.Set {
	t.Helper()
	set, err := stages.Build(t.Context(), exec.LookPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range set.Rows {
		if row.State == dispatch.Missing {
			t.Fatalf("route %s: %s", row.Route, row.Reason)
		}
	}
	return set
}

func pdfStage(t *testing.T, prep *document.Preparer, gs *tool.Tool, opts inspect.Options) *stages.DocumentStage {
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
	s := stages.NewDocumentStage(stages.DocumentConfig{
		Preparer: prep, Version: stages.Versions(gs, qt, dt), Request: document.ImagesRequest{Text: true, Concurrency: 2},
		QPDF: q, PDFDetach: detach, Inspect: opts,
	})
	return s
}

func wantPNG(t *testing.T, o dispatch.Output) {
	t.Helper()
	b, err := os.ReadFile(o.Path)
	if err != nil || o.Kind != dispatch.ImageOutput || o.MediaType != "image/png" || !bytes.HasPrefix(b, []byte(route.PNGSignature)) {
		t.Errorf("%s: a %s %s of %d bytes, %v; want a PNG", o.Name, o.Kind, o.MediaType, len(b), err)
	}
}

func TestDocumentStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, documentFixture(t, "clean.pdf"), func(ev *dispatch.Event) {
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

func TestOfficeStageReal(t *testing.T) {
	d := newDispatcher(t, allStages(t), dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, filepath.Join(officeFixtures, "media.libreoffice.docx"), nil)
	const emf = "670be6349ed84b27d6ca43d66ba6e9454c632f68de8d9b8fea78a480e90afb31"
	for _, ev := range events[:len(events)-1] {
		linked := strings.HasPrefix(ev.Part.Name, "https://")
		if linked && ev.Outcome != dispatch.NoProcessor ||
			!linked && (ev.Part.ContentType.Route != route.Vision || ev.Outcome != dispatch.Prepared && ev.Outcome != dispatch.Seen) {
			t.Errorf("%s at %q: %s %s", ev.Part.Name, ev.Part.Where, ev.Part.ContentType.Route, ev.Outcome)
		}
	}
	if ev := byName(t, events, emf+".png"); !strings.HasPrefix(ev.Part.Where, "document, media ") || ev.Part.ContentType.Stored != "image/png" {
		t.Errorf("the EMF's PNG found at %q, stored as %q", ev.Part.Where, ev.Part.ContentType.Stored)
	}
	if last := events[len(events)-1]; last.Part.Depth != 0 || last.Children != 8 {
		t.Errorf("the document: %s@%d, %d children", last.Part.Name, last.Part.Depth, last.Children)
	}
}

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

func TestBuiltIdentities(t *testing.T) {
	set := builtSet(t, stages.Options{})
	lines := map[string]string{}
	for _, tl := range set.Tools {
		lines[tl.Program] = tl.Version
	}
	for r, needs := range stages.Needs() {
		row := set.Rows[slices.IndexFunc(set.Rows, func(row dispatch.Row) bool { return row.Route == r })]
		var want []string
		for _, program := range needs {
			want = append(want, lines[program])
		}
		if len(want) > 0 && (row.Stage.Version != strings.Join(want, ", ") || row.Tools != row.Stage.Version) {
			t.Errorf("%s: version %q, tools %q; want %q", r, row.Stage.Version, row.Tools, strings.Join(want, ", "))
		}
		if row.Stage.Name == "" || len(row.Stage.Config) != 64 || set.Stages[r].Identity() != row.Stage {
			t.Errorf("%s: identity %v", r, row.Stage)
		}
	}
	bounded := builtSet(t, stages.Options{Timeout: time.Hour, Idle: time.Hour, Concurrency: 3})
	changed := builtSet(t, stages.Options{
		Document: document.ImagesRequest{DPI: 300}, Picture: picture.Request{Density: 300},
		Audio: audio.Request{Limit: 20 * time.Second}, Video: media.Request{MaxSide: 768},
		Inspect: inspect.Options{BombRatio: 100, BombSize: 1 << 20},
	})
	for i, row := range set.Rows {
		if bounded.Rows[i].Stage != row.Stage {
			t.Errorf("%s: a run bound changed the identity", row.Route)
		}
		switch row.Route {
		case route.Document, route.VisionConverted, route.Audio, route.Video, route.Archive:
			if changed.Rows[i].Stage.Config == row.Stage.Config {
				t.Errorf("%s: a request left the identity's config alone", row.Route)
			}
		}
	}
}

func TestOfficeNoProcessor(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.Office: testOfficeStage(t)}, dispatch.NewMemoryIndex(0))
	for name, c := range map[string]struct{ content, reason string }{
		"x.wpd":  {"\xffWPC\x10\x00\x00\x00\x01\x0a", "office stage: no processor yet for a WordPerfect document"},
		"x.odt":  {zipOf(t, "mimetype", "application/vnd.oasis.opendocument.text", "content.xml", "<x/>"), "office stage: no processor yet for an ODF package"},
		"x.xlsb": {zipOf(t, "[Content_Types].xml", "<x/>"), "office stage: no processor yet for an Excel binary workbook"},
	} {
		ev := dispatchAll(t, d, input(t, name, c.content), nil)[0]
		if ev.Part.ContentType.Route != route.Office || ev.Outcome != dispatch.NoProcessor || !strings.Contains(ev.Reason, c.reason) {
			t.Errorf("%s: %s %s, %q; want no processor, %q", name, ev.Part.ContentType.Route, ev.Outcome, ev.Reason, c.reason)
		}
	}
}

func TestOfficeEncrypted(t *testing.T) {
	stage := testOfficeStage(t)
	content, err := os.ReadFile(tooltest.Tools(t, "ooxml/fixtures/generated/refused/encrypted.libreoffice.docx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"encrypted.xlsx", "encrypted.xlsb"} {
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

func TestOfficeStage(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{
		route.Office: testOfficeStage(t), route.Vision: stages.NewVisionStage(), route.VisionConverted: testPictureStage(t),
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
		{red + ".png", 1, dispatch.Seen},
		{red + ".png", 1, dispatch.Seen},
		{emf + ".png", 1, dispatch.Prepared},
		{"https://example.invalid/linked.png", 1, dispatch.NoProcessor},
		{green + ".png", 1, dispatch.Prepared},
		{blue + ".png", 1, dispatch.Seen},
		{"media.libreoffice.docx", 0, dispatch.Prepared},
	})
	var want []string
	for n := 1; n <= 8; n++ {
		want = append(want, "document, media "+strconv.Itoa(n))
	}
	if got := wheres(events); !slices.Equal(got, want) {
		t.Errorf("wheres:\n got %q\nwant %q", got, want)
	}
	if ev := byName(t, events, red+".png"); ev.Part.ContentType.Route != route.Vision || ev.Outputs[0].Kind != dispatch.ImageOutput || ev.Outputs[0].MediaType != "image/png" {
		t.Errorf("red: %s, %+v", ev.Part.ContentType.Route, ev.Outputs)
	}
	if ev := byName(t, events, emf+".png"); ev.Part.ContentType.Route != route.Vision || ev.Part.ContentType.Stored != "image/png" {
		t.Errorf("emf: %s, %q", ev.Part.ContentType.Route, ev.Part.ContentType.Stored)
	}
	md := outputNamed(t, events[len(events)-1], dispatch.MarkdownOutput, "document")
	if !bytes.HasPrefix(md.Value, []byte("# Media")) || !bytes.Contains(md.Value, []byte(red+".png")) {
		t.Errorf("markdown: %.80q", md.Value)
	}
}

func TestEmailStage(t *testing.T) {
	index := dispatch.NewMemoryIndex(0)
	d := newDispatcher(t, dispatch.Stages{route.Nested: testEmailStage(t), route.Vision: stages.NewVisionStage()}, index)
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

	var order []string
	events = dispatchAll(t, d, filepath.Join(mailFixtures(t), "generated", "pst", "mailbox.pst"), func(ev *dispatch.Event) {
		switch {
		case ev.Output != nil && ev.Part.Depth == 0:
			order = append(order, string(ev.Output.Kind)+" "+ev.Output.Name)
		case ev.Output == nil && ev.Part.Depth == 1:
			order = append(order, ev.Part.Where)
		}
	})
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
	if m := outputNamed(t, pst, dispatch.RecordOutput, "message 2097220").Meta; m["folder"] != "Top of Personal Folders/Inbox/Projects/2026" || m["id"] != "2097220" {
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

func TestEmailResume(t *testing.T) {
	stage := testEmailStage(t)
	stages := dispatch.Stages{route.Nested: stage, route.Vision: stages.NewVisionStage()}
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
		if ev.Warning != nil {
			continue
		}
		if ev.Outcome != dispatch.Failed || !errors.Is(ev.Err, email.ErrUsage) {
			t.Errorf("a .msg after 1: %s, %v; want failed, mail's usage error", ev.Outcome, ev.Err)
		}
	}
}

func TestEmailEmbedded(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.Nested: testEmailStage(t)}, dispatch.NewMemoryIndex(0))
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

func TestEmailConversations(t *testing.T) {
	d := newDispatcher(t, dispatch.Stages{route.Nested: testEmailStage(t)}, dispatch.NewMemoryIndex(0))
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
			}
			if !maps.Equal(o.Meta, want) || strconv.FormatUint(c.ID, 10) != id || c.Topic == "" {
				t.Errorf("%s: meta %v, want %v", o.Name, o.Meta, want)
			}
			if o.MediaType != dispatch.MediaMailConversation {
				t.Errorf("%s: %s, want %s", o.Name, o.MediaType, dispatch.MediaMailConversation)
			}
			recordThenMarkdown(t, store, o.Name)
			md := store.Outputs[i+1]
			if string(md.Value) != c.Markdown || c.Markdown != markdownBy[c.Topic] || !maps.Equal(md.Meta, want) {
				t.Errorf("%s: Markdown %q, meta %v; the record's %q, the truth's %q", o.Name, md.Value, md.Meta, c.Markdown, markdownBy[c.Topic])
			}
		case "message":
			messages++
			recordThenMarkdown(t, store, o.Name)
			if md := store.Outputs[i+1]; o.Meta["folder"] != folder || string(md.Value) != markdownBy[o.Meta["subject"]] {
				t.Errorf("%s: meta %v, Markdown %q", o.Name, o.Meta, md.Value)
			}
			if o.MediaType != dispatch.MediaMail {
				t.Errorf("%s: %s, want %s", o.Name, o.MediaType, dispatch.MediaMail)
			}
		}
	}
	if conversations != 4 || missed != 1 || messages != 1 {
		t.Errorf("%d conversations, %d missed, %d messages; want 4, 1, and 1", conversations, missed, messages)
	}
}

func TestOneNoteStage(t *testing.T) {
	index := dispatch.NewMemoryIndex(0)
	d := newDispatcher(t, dispatch.Stages{
		route.OneNote: testOnenoteStage(t), route.Vision: stages.NewVisionStage(), route.VisionConverted: testPictureStage(t),
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
	want := []string{".jpg@1:prepared vision", ".svg@1:prepared vision-converted", ".one@0:prepared onenote"}
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

func onenoteFixtures(t *testing.T) string {
	t.Helper()
	return tooltest.Tools(t, "onenote/tests/fixtures/onenote-rs")
}

func mailFixtures(t *testing.T) string {
	t.Helper()
	return tooltest.Tools(t, "mail/tests/fixtures")
}

var (
	officeFixtures = filepath.Join("..", "office", "testdata", "ooxml")
)

func pinned(t *testing.T, spec tool.Spec) *tool.Tool {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatalf("%s, which mise pins, run the check through mise run check: %v", spec.Program, err)
	}
	return tl
}

func hostPictures(t *testing.T) (*picture.Converter, *tool.Tool, *tool.Tool) {
	t.Helper()
	lo, mg := pinned(t, picture.LibreOfficeSpec()), pinned(t, picture.MagickSpec())
	c, err := picture.NewConverter(t.Context(), picture.Config{Magick: mg, LibreOffice: lo})
	if err != nil {
		t.Fatal(err)
	}
	return c, lo, mg
}

func testOfficeStage(t *testing.T) *stages.OfficeStage {
	t.Helper()
	tl := pinned(t, office.OfficeSpec())
	pc, lo, mg := hostPictures(t)
	c, err := office.NewConverter(t.Context(), office.Config{Office: tl, Pictures: pc})
	if err != nil {
		t.Fatal(err)
	}
	s := stages.NewOfficeStage(stages.OfficeConfig{Converter: c, Version: stages.Versions(tl, lo, mg)})
	return s
}

func testPictureStage(t *testing.T) *stages.PictureStage {
	t.Helper()
	pc, lo, mg := hostPictures(t)
	s := stages.NewPictureStage(stages.PictureConfig{Converter: pc, Version: stages.Versions(mg, lo)})
	return s
}

func testEmailStage(t *testing.T) *stages.EmailStage {
	t.Helper()
	tl := pinned(t, email.MailSpec())
	r, err := email.NewReader(email.Config{Mail: tl})
	if err != nil {
		t.Fatal(err)
	}
	s := stages.NewEmailStage(stages.EmailConfig{Reader: r, Version: stages.Versions(tl)})
	return s
}

func testOnenoteStage(t *testing.T) *stages.OneNoteStage {
	t.Helper()
	tl := pinned(t, onenote.OneNoteSpec())
	r, err := onenote.NewReader(onenote.Config{OneNote: tl})
	if err != nil {
		t.Fatal(err)
	}
	s := stages.NewOneNoteStage(stages.OneNoteConfig{Reader: r, Version: stages.Versions(tl)})
	return s
}

func recordThenMarkdown(t *testing.T, ev dispatched, name string) {
	t.Helper()
	i := slices.IndexFunc(ev.Outputs, func(o dispatch.Output) bool { return o.Name == name })
	if i < 0 || i+1 >= len(ev.Outputs) || ev.Outputs[i].Kind != dispatch.RecordOutput ||
		ev.Outputs[i+1].Kind != dispatch.MarkdownOutput || ev.Outputs[i+1].Name != name {
		t.Errorf("%s: %q's outputs aren't its record, then its Markdown", ev.Part.Name, name)
	}
}

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
