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

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
)

const officeVersion = "3"

type officeConfig struct {
	Converter     *office.Converter
	Version       string
	MetafileScale int
	Memory        int64
}

type officeStage struct {
	converter *office.Converter
	scale     int
	memory    int64
	id        dispatch.Identity
}

func newOfficeStage(cfg officeConfig) *officeStage {
	if cfg.Converter == nil {
		panic("stages: officeConfig.Converter is nil")
	}
	scale := cmp.Or(cfg.MetafileScale, picture.DefaultMetafileScale)
	return &officeStage{converter: cfg.Converter, scale: scale, memory: cfg.Memory, id: identity("office", cfg.Version, struct {
		Stage   string         `json:"stage"`
		Schema  string         `json:"schema"`
		Request office.Request `json:"request"`
	}{officeVersion, cli.Schema, office.Request{MetafileScale: scale}})}
}

func (s *officeStage) Identity() dispatch.Identity { return s.id }

func (s *officeStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
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

type officeWalk struct {
	dir   string
	sheet *sheetFiles
	n     int
	yield func(dispatch.Item, error) bool
}

type sheetFiles struct {
	name, number   string
	md, rows       *os.File
	mdBuf, rowsBuf *bufio.Writer
}

func (w *officeWalk) record(rec office.Record, doc *office.Document) (*office.Document, error) {
	switch r := rec.(type) {
	case *office.Document:
		meta := map[string]string{}
		if r.ConvertedFrom != "" {
			meta[dispatch.MetaConvertedFrom] = r.ConvertedFrom
		}
		if err := w.item(dispatch.OutputItem(markdown("document", r.Markdown, meta))); err != nil {
			return r, err
		}
		return r, w.metadata(r)
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
	return doc, nil
}

func (w *officeWalk) metadata(doc *office.Document) error {
	rec := doc.Metadata()
	if rec == nil {
		return nil
	}
	o, err := record("metadata", rec, rec.MediaType(), nil)
	if err != nil {
		return fmt.Errorf("office stage: %w", err)
	}
	return w.item(dispatch.OutputItem(o))
}

func (w *officeWalk) item(it dispatch.Item) error {
	if !w.yield(it, nil) {
		return errStopped
	}
	return nil
}

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

func (w *officeWalk) close() {
	if w.sheet != nil {
		_ = w.sheet.md.Close()
		_ = w.sheet.rows.Close()
	}
}
