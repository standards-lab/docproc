package stages

import (
	"bufio"
	"cmp"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

// officeConfig is what an [officeStage] is built over: its Converter, the version of the tools
// behind it, the scale metafiles are rendered at, and the memory bound the tools run under, which
// a death by a signal's reason names.
type officeConfig struct {
	// Converter converts the documents; it is required.
	Converter *office.Converter
	// Version names the tools behind the Converter, docproc-ooxml's, LibreOffice's, and magick's
	// version lines, as [versions] gives them; it is required.
	Version string
	// MetafileScale is the scale a document's EMF and WMF pictures are rendered at, as
	// office.Request's, zero its default; part of the stage's identity.
	MetafileScale int
	Memory        int64
}

// officeStage prepares an office document, route.Office's parts, through office's Converter,
// which reads the part's file in place. It yields the document line's Markdown as a
// [dispatch.MarkdownOutput] named "document", its Meta naming the format a legacy document or an
// RTF was converted from, "rtf" for an RTF; an xlsx's holds no sheet's. Each sheet of an xlsx then
// yields two outputs, both named "sheet/<n>", n the sheet's ordinal from 1, as a medium's Sheet
// names it, each with the sheet's name in its Meta under [dispatch.MetaSheet]: a Markdown output,
// its sheet line's Markdown followed by its rows lines', and a [dispatch.RecordOutput] of
// [dispatch.MediaOOXML], its rows lines as JSON Lines, each without its Markdown, so each sheet's
// text exists once in the content store. A sheet's outputs are written to their files in the part's
// directory as its lines stream, so nothing holds a whole sheet.
//
// The one media rule, [stash.item], then turns each medium the document names into its item at
// "document, media <n>", in the document's order: a picture converted to PNG is found as its PNG,
// "image/png"; any other picture, an embedded package, or an OLE object as stored, typed by its
// stored content type and its SHA-256 claimed by its media name, so its own stage reads it; and a
// linked picture, with no file, is an unread child, no processor yet. docproc-ooxml's warnings are
// [dispatch.Warning]s, each with its tool's name.
//
// An RTF is office's to convert through LibreOffice to docx, as a .doc is. The stage has no
// processor yet for an ODF package, a WordPerfect document, or an Excel binary workbook: each ends
// with dispatch.ErrNoProcessor, its reason naming the kind. A refusal internal/cli classes as
// encrypted ends it with dispatch.ErrEncrypted, and any other failure is classified once, by
// [partErr].
type officeStage struct {
	converter *office.Converter
	scale     int
	memory    int64
	id        dispatch.Identity
}

// newOfficeStage builds an officeStage over cfg, panicking naming Converter when it is nil, the
// wiring's mistake.
func newOfficeStage(cfg officeConfig) *officeStage {
	if cfg.Converter == nil {
		panic("stages: officeConfig.Converter is nil")
	}
	scale := cmp.Or(cfg.MetafileScale, picture.DefaultMetafileScale)
	return &officeStage{converter: cfg.Converter, scale: scale, memory: cfg.Memory, id: identity("office", cfg.Version, struct {
		Schema  string         `json:"schema"`
		Request office.Request `json:"request"`
	}{cli.Schema, office.Request{MetafileScale: scale}})}
}

// Identity returns the stage's identity: "office", its tools' version lines, and its request's
// and schema's hash.
func (s *officeStage) Identity() dispatch.Identity { return s.id }

// officeUnread names the office kinds the stage has no processor for yet, each with its article.
var officeUnread = map[route.Kind]string{
	route.ODF:         "an ODF package",
	route.WordPerfect: "a WordPerfect document",
}

// Prepare converts the document at p.Path, as the type's doc says.
func (s *officeStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		what, ok := officeUnread[p.ContentType.Kind]
		if !ok && p.ContentType.Kind == route.ZIP && strings.EqualFold(filepath.Ext(p.Name), ".xlsb") {
			what, ok = "an Excel binary workbook", true
		}
		if ok {
			yield(dispatch.Item{}, fmt.Errorf("office stage: %w for %s", dispatch.ErrNoProcessor, what))
			return
		}
		w := officeWalk{dir: dir, yield: yield}
		defer w.close()
		var doc *office.Document
		for rec, err := range s.converter.Convert(ctx, office.Request{Path: p.Path, Dir: dir, Name: p.Name, MetafileScale: s.scale}) {
			if err == nil {
				doc, err = w.record(rec, doc)
			}
			if err != nil {
				if !errors.Is(err, errStopped) {
					yield(dispatch.Item{}, partErr(ctx, fmt.Errorf("office stage: %w", err), s.memory))
				}
				return
			}
		}
		if err := w.flush(); err != nil {
			if !errors.Is(err, errStopped) {
				yield(dispatch.Item{}, err)
			}
			return
		}
		if doc == nil {
			yield(dispatch.Item{}, errors.New("office stage: the conversion yielded no document"))
			return
		}
		st := stash{dir: dir}
		for i, m := range doc.Media {
			md := medium{
				path: m.Path, sum: m.SHA256, name: cmp.Or(m.File, m.Target, string(m.Kind)), mime: m.ContentType,
				none: dispatch.NoProcessor, why: "a linked picture, never fetched",
			}
			if m.Converted {
				md.sum, md.mime, md.name = "", "image/png", filepath.Base(m.Path)
			}
			item, err := st.item(md, "document, media "+strconv.Itoa(i+1))
			if err != nil {
				yield(dispatch.Item{}, err)
				return
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// officeWalk is one conversion's walk of its records: the part's directory, the sheet being
// written, if any, and the loop's yield.
type officeWalk struct {
	dir   string
	sheet *sheetFiles
	n     int
	yield func(dispatch.Item, error) bool
}

// sheetFiles are one sheet's two outputs as they are written: its name and number, and its
// Markdown's and its rows' files, each buffered.
type sheetFiles struct {
	name, number   string
	md, rows       *os.File
	mdBuf, rowsBuf *bufio.Writer
}

// record takes rec, the conversion's next record after doc, the Document once it has come, and
// returns the Document: the Document yields its Markdown output; a Sheet ends the sheet before it
// and begins its own files; a Rows record is written to them; a warning is yielded.
func (w *officeWalk) record(rec office.Record, doc *office.Document) (*office.Document, error) {
	switch r := rec.(type) {
	case *office.Document:
		meta := map[string]string{}
		if r.ConvertedFrom != "" {
			meta["converted_from"] = r.ConvertedFrom
		}
		return r, w.item(dispatch.OutputItem(markdown("document", r.Markdown, meta)))
	case *office.Sheet:
		if err := w.flush(); err != nil {
			return doc, err
		}
		return doc, w.begin(r)
	case *office.Rows:
		return doc, w.rows(r)
	case *office.Warning:
		return doc, w.item(dispatch.WarningItem(dispatch.Warning(*r)))
	}
	// A line of a kind office doesn't know adds nothing to the outputs.
	return doc, nil
}

// item yields it, or returns errStopped once the loop stops.
func (w *officeWalk) item(it dispatch.Item) error {
	if !w.yield(it, nil) {
		return errStopped
	}
	return nil
}

// begin opens the files of sh, the next sheet, and writes its Markdown.
func (w *officeWalk) begin(sh *office.Sheet) error {
	w.n++
	number := strconv.Itoa(w.n)
	md, err := os.Create(filepath.Join(w.dir, "sheet-"+number+".md"))
	if err != nil {
		return fmt.Errorf("office stage: %w", err)
	}
	rows, err := os.Create(filepath.Join(w.dir, "sheet-"+number+".jsonl"))
	if err != nil {
		_ = md.Close()
		return fmt.Errorf("office stage: %w", err)
	}
	w.sheet = &sheetFiles{name: sh.Name, number: number, md: md, rows: rows, mdBuf: bufio.NewWriter(md), rowsBuf: bufio.NewWriter(rows)}
	_, err = w.sheet.mdBuf.WriteString(sh.Markdown)
	return err
}

// rows writes r to the current sheet's files: its Markdown, and its rows line without it.
func (w *officeWalk) rows(r *office.Rows) error {
	if w.sheet == nil {
		return errors.New("office stage: a rows line before any sheet line")
	}
	if _, err := w.sheet.mdBuf.WriteString(r.Markdown); err != nil {
		return fmt.Errorf("office stage: %w", err)
	}
	b, err := jsonv2.Marshal(office.Rows{Rows: r.Rows})
	if err != nil {
		return fmt.Errorf("office stage: %w", err)
	}
	if _, err := w.sheet.rowsBuf.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("office stage: %w", err)
	}
	return nil
}

// flush closes the current sheet's files, if any, and yields its two outputs.
func (w *officeWalk) flush() error {
	sh := w.sheet
	if sh == nil {
		return nil
	}
	w.sheet = nil
	err := errors.Join(sh.mdBuf.Flush(), sh.rowsBuf.Flush(), sh.md.Close(), sh.rows.Close())
	if err != nil {
		return fmt.Errorf("office stage: %w", err)
	}
	name := "sheet/" + sh.number
	meta := func() map[string]string { return map[string]string{dispatch.MetaSheet: sh.name} }
	if err := w.item(dispatch.OutputItem(dispatch.Output{
		Kind: dispatch.MarkdownOutput, Name: name, MediaType: "text/markdown", Path: sh.md.Name(), Meta: meta(),
	})); err != nil {
		return err
	}
	return w.item(dispatch.OutputItem(dispatch.Output{
		Kind: dispatch.RecordOutput, Name: name, MediaType: dispatch.MediaOOXML, Path: sh.rows.Name(), Meta: meta(),
	}))
}

// close closes the current sheet's files, if any, once the conversion has stopped.
func (w *officeWalk) close() {
	if w.sheet != nil {
		_ = w.sheet.md.Close()
		_ = w.sheet.rows.Close()
	}
}
