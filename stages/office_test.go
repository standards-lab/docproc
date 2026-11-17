//go:build linux

package stages_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

// standinOfficeStage returns an office stage over the stand-in docproc-ooxml, set to s, and the
// stand-in magick and soffice, which its documents' pictures, none of a kind converted, never run.
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

// TestOfficeStageSheets: an xlsx is its document line's Markdown, named "document", then two
// outputs for each sheet, both named "sheet/<n>", each with the sheet's name in its Meta: a
// Markdown output, its sheet line's Markdown and its rows lines', and a record of the ooxml media
// type, its rows lines as JSON Lines, each without its Markdown, each a file of the part's
// directory. Each medium the document names is found at "document, media <n>": a stored medium
// with its SHA-256 claimed and its stored type, a linked picture an unread child, no processor
// yet, named by its target. A warning is a dispatch.Warning naming docproc-ooxml.
func TestOfficeStageSheets(t *testing.T) {
	t.Parallel()
	// The stand-in's media pass through its JSON settings, so they are text.
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
	d := newDispatcher(t, dispatch.Stages{route.Office: stage, route.Vision: stages.NewVisionStage()}, dispatch.NewMemoryIndex(0))
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

// TestOfficeStageIdentity: the office stage hashes its resolved request, its metafile scale's
// default filled, and the schema; its tools' version lines are its Version.
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
