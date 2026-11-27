//go:build linux

package stages_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

func standinOfficeStage(t *testing.T, s onenoteSettings) *stages.OfficeStage {
	t.Helper()
	spec := office.OfficeSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "docproc-ooxml", s)
	of, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	pc := stages.PictureConverter(standinPictureStage(t, pictureListing, 1))
	c, err := office.NewConverter(t.Context(), office.Config{Office: of, Pictures: pc})
	if err != nil {
		t.Fatal(err)
	}
	return stages.NewOfficeStage(stages.OfficeConfig{Converter: c, Version: stages.Versions(of)})
}

func TestOfficeStageSheets(t *testing.T) {
	t.Parallel()
	const stored = "a stored picture's bytes"
	sum := sha256.Sum256([]byte(stored))
	png := hex.EncodeToString(sum[:]) + ".png"
	lines := []string{
		`{"line":"document","schema":"2","format":"xlsx","markdown":"# Book\n","media":[` +
			`{"kind":"picture","file":"` + png + `","content_type":"image/png","part":"/xl/media/image1.png","sheet":1},` +
			`{"kind":"picture","target":"https://example.org/a.png","sheet":2}]}`,
		`{"line":"sheet","name":"Prices","markdown":"## Prices\n\n| A | B |\n|---|---|\n"}`,
		`{"line":"rows","markdown":"| 1 | 2 |\n","rows":[{"row":2,"cells":[{"ref":"A2","value_type":"number","value":1},{"ref":"B2","value_type":"number","value":2}]}]}`,
		`{"line":"rows","markdown":"| 3 | 4 |\n","rows":[{"row":3,"cells":[{"ref":"A3","value_type":"number","value":3}]}]}`,
		`{"line":"sheet","name":"document","markdown":"## document\n"}`,
	}
	stage := standinOfficeStage(t, onenoteSettings{
		Stdout: strings.Join(lines, "\n") + "\n",
		Stderr: "docproc-ooxml: warning: sheet 2: notes not read\n",
		Media:  map[string]string{png: stored},
	})
	d := newDispatcher(t, dispatch.Stages{route.Office: stage, route.Vision: standinVisionStage(t, pictureSettings{})}, dispatch.NewMemoryIndex(0))
	book := input(t, "book.xlsx", zipBytes(t, zent{name: "[Content_Types].xml", body: "<Types/>"}))
	var files []string
	events := dispatchAll(t, d, book, func(ev *dispatch.Event) {
		if o := ev.Output; o != nil && ev.Part.Depth == 0 && strings.HasPrefix(o.Name, "sheet/") {
			b, err := os.ReadFile(o.Path)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, string(b))
		}
	})
	wantSteps(t, events, []step{
		{png, 1, dispatch.NoProcessor},
		{"https://example.org/a.png", 1, dispatch.NoProcessor},
		{"book.xlsx", 0, dispatch.Prepared},
	})
	if got, want := wheres(events), []string{"document, media 1", "document, media 2"}; !slices.Equal(got, want) {
		t.Errorf("wheres %q, want %q", got, want)
	}
	if ev := events[0]; ev.Part.ContentType.Stored != "image/png" || ev.Part.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("the stored picture: %q, %s", ev.Part.ContentType.Stored, ev.Part.SHA256)
	}
	if ev := events[1]; ev.Part.SHA256 != "" || ev.Reason != "a linked picture, never fetched" {
		t.Errorf("the linked picture: %q, %q", ev.Part.SHA256, ev.Reason)
	}

	doc := events[2]
	var got []string
	for _, o := range doc.Outputs {
		got = append(got, string(o.Kind)+" "+o.Name+" "+o.MediaType+" "+o.Meta[dispatch.MetaSheet])
	}
	want := []string{
		"markdown document text/markdown ",
		"markdown sheet/1 text/markdown Prices", "record sheet/1 " + dispatch.MediaOOXML + " Prices",
		"markdown sheet/2 text/markdown document", "record sheet/2 " + dispatch.MediaOOXML + " document",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("outputs:\n got %q\nwant %q", got, want)
	}
	if string(doc.Outputs[0].Value) != "# Book\n" {
		t.Errorf("the document's Markdown %q", doc.Outputs[0].Value)
	}
	if files[0] != "## Prices\n\n| A | B |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n" {
		t.Errorf("sheet 1's Markdown %q", files[0])
	}
	var rows []office.Rows
	sc := bufio.NewScanner(strings.NewReader(files[1]))
	for sc.Scan() {
		if bytes.Contains(sc.Bytes(), []byte("markdown")) {
			t.Errorf("a rows line kept its Markdown: %s", sc.Bytes())
		}
		var r office.Rows
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	if len(rows) != 2 || rows[0].Rows[0].Row != 2 || rows[1].Rows[0].Cells[0].Ref != "A3" {
		t.Errorf("sheet 1's rows %+v", rows)
	}
	if files[2] != "## document\n" || files[3] != "" {
		t.Errorf("sheet 2's files %q, %q", files[2], files[3])
	}
	if want := (dispatch.Warning{Tool: "docproc-ooxml", Where: "sheet 2", What: "notes not read"}); !slices.Equal(doc.Warnings, []dispatch.Warning{want}) {
		t.Errorf("warnings %+v, want %+v", doc.Warnings, want)
	}
}

func TestOfficeStageIdentity(t *testing.T) {
	t.Parallel()
	base := standinOfficeStage(t, onenoteSettings{})
	c := stages.OfficeConverter(base)
	same := stages.NewOfficeStage(stages.OfficeConfig{Converter: c, Version: base.Identity().Version, MetafileScale: picture.DefaultMetafileScale})
	scaled := stages.NewOfficeStage(stages.OfficeConfig{Converter: c, Version: base.Identity().Version, MetafileScale: 5})
	if same.Identity() != base.Identity() || scaled.Identity().Config == base.Identity().Config {
		t.Errorf("identities %v, %v, %v", base.Identity(), same.Identity(), scaled.Identity())
	}
	if !strings.HasPrefix(base.Identity().Version, office.OfficeMinimum) {
		t.Errorf("version %q, want docproc-ooxml's version line", base.Identity().Version)
	}
}

func TestOfficeStageUnread(t *testing.T) {
	t.Parallel()
	encrypted, err := os.ReadFile(filepath.Join("..", "office", "testdata", "odf", "generated", "encrypted.odt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, content string
		outcome       dispatch.Outcome
		reason        string
	}{
		{"formula.odf", zipOf(t, "mimetype", "application/vnd.oasis.opendocument.formula", "content.xml", "<x/>"), dispatch.NoProcessor,
			"office stage: office: no processor yet for an ODF package of type application/vnd.oasis.opendocument.formula"},
		{"database.odb", zipOf(t, "mimetype", "application/vnd.oasis.opendocument.base"), dispatch.NoProcessor, "application/vnd.oasis.opendocument.base"},
		{"encrypted.odt", string(encrypted), dispatch.Encrypted, "office stage: office: an ODF package whose manifest holds encryption data: encrypted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			link := tooltest.Link(t, t.TempDir(), "docproc-ooxml", onenoteSettings{})
			spec := office.OfficeSpec()
			spec.Program = link
			of, err := tool.NewExec(t.Context(), spec)
			if err != nil {
				t.Fatal(err)
			}
			conv, err := office.NewConverter(t.Context(), office.Config{Office: of, Pictures: stages.PictureConverter(standinPictureStage(t, pictureListing, 1))})
			if err != nil {
				t.Fatal(err)
			}
			stage := stages.NewOfficeStage(stages.OfficeConfig{Converter: conv, Version: stages.Versions(of)})
			d := newDispatcher(t, dispatch.Stages{route.Office: stage}, dispatch.NewMemoryIndex(0))
			events := dispatchAll(t, d, input(t, c.name, c.content), nil)
			wantSteps(t, events, []step{{c.name, 0, c.outcome}})
			if ev := events[0]; ev.Part.ContentType.Route != route.Office || !strings.Contains(ev.Reason, c.reason) {
				t.Errorf("route %s, reason %q; want office, %q", ev.Part.ContentType.Route, ev.Reason, c.reason)
			}
			if runs := tooltest.Runs(t, link); len(runs) != 1 {
				t.Errorf("docproc-ooxml ran %d times, want its version command alone", len(runs))
			}
		})
	}
}

func TestOfficeStageMetadata(t *testing.T) {
	t.Parallel()
	const golden = "ooxml/test/Docproc.Ooxml.Tests/Expected/generated/docx/links.libreoffice.jsonl"
	lines, err := os.ReadFile(tooltest.Tools(t, golden))
	if err != nil {
		t.Fatal(err)
	}
	fixture := tooltest.Tools(t, "ooxml/fixtures/generated/docx/links.libreoffice.docx")
	content := dispatch.NewMemoryContent()
	stage := standinOfficeStage(t, onenoteSettings{Stdout: string(lines)})
	d, err := dispatch.New(dispatch.Config{Content: content, Puts: 2, Stages: dispatch.Stages{route.Office: stage}, Index: dispatch.NewMemoryIndex(0), Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	events := dispatchAll(t, d, fixture, nil)
	wantSteps(t, events, []step{{"links.libreoffice.docx", 0, dispatch.Prepared}})
	var got []string
	for _, o := range events[0].Outputs {
		got = append(got, string(o.Kind)+" "+o.Name+" "+o.MediaType)
	}
	if want := []string{"markdown document text/markdown", "record metadata " + dispatch.MediaOfficeMetadata}; !slices.Equal(got, want) {
		t.Fatalf("outputs:\n got %q\nwant %q", got, want)
	}
	meta := storedRecord[metadata.Office](t, content, events[0].Outputs[1], dispatch.MediaOfficeMetadata)
	if want := (metadata.Properties{
		{Key: "created", Value: "2013-12-23T23:15:00Z"}, {Key: "creator", Value: "Fixture Author"},
		{Key: "description", Value: "generated by python-docx"}, {Key: "language", Value: "en-US"},
		{Key: "modified", Value: "2013-12-23T23:15:00Z"}, {Key: "revision", Value: "1"}, {Key: "title", Value: "Links"},
	}); !reflect.DeepEqual(meta.Core, want) {
		t.Errorf("core %+v, want %+v", meta.Core, want)
	}
	if want := (metadata.Properties{
		{Key: "Template", Value: "Normal.dotm"}, {Key: "TotalTime", Value: 0.0},
		{Key: "Application", Value: "LibreOffice/26.8.1.1$Linux_X86_64 LibreOffice_project/680$Build-1"},
		{Key: "AppVersion", Value: "15.0000"}, {Key: "Pages", Value: 1.0}, {Key: "Words", Value: 0.0},
		{Key: "Characters", Value: 0.0}, {Key: "CharactersWithSpaces", Value: 0.0}, {Key: "Paragraphs", Value: 0.0},
	}); !reflect.DeepEqual(meta.Extended, want) {
		t.Errorf("extended %+v, want %+v", meta.Extended, want)
	}
	if want := (metadata.Properties{
		{Key: "Due", Value: "2026-02-01T00:00:00Z"}, {Key: "Project", Value: "docproc"},
		{Key: "Ratio", Value: 0.25}, {Key: "Reviewed", Value: true}, {Key: "Revision", Value: 7.0},
	}); !reflect.DeepEqual(meta.Custom, want) {
		t.Errorf("custom %+v, want %+v", meta.Custom, want)
	}
	if meta.ConvertedFrom != "" {
		t.Errorf("converted from %q, want none", meta.ConvertedFrom)
	}
}

func TestOfficeStageNoProperties(t *testing.T) {
	t.Parallel()
	stage := standinOfficeStage(t, onenoteSettings{Stdout: `{"line":"document","schema":"2","format":"docx","markdown":"# Bare\n"}` + "\n"})
	d := newDispatcher(t, dispatch.Stages{route.Office: stage}, dispatch.NewMemoryIndex(0))
	events := dispatchAll(t, d, input(t, "bare.docx", zipBytes(t, zent{name: "[Content_Types].xml", body: "<Types/>"})), nil)
	wantSteps(t, events, []step{{"bare.docx", 0, dispatch.Prepared}})
	if outs := events[0].Outputs; len(outs) != 1 || outs[0].Name != "document" {
		t.Errorf("outputs %+v, want the document's Markdown alone", outs)
	}
}
