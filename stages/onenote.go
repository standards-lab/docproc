package stages

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/onenote"
)

type onenoteConfig struct {
	Reader  *onenote.Reader
	Version string
	Memory  int64
}

var onenoteRequest = onenote.Request{FileOnly: true}

type onenoteStage struct {
	reader *onenote.Reader
	memory int64
	id     dispatch.Identity
}

func newOneNoteStage(cfg onenoteConfig) *onenoteStage {
	if cfg.Reader == nil {
		panic("stages: onenoteConfig.Reader is nil")
	}
	return &onenoteStage{reader: cfg.Reader, memory: cfg.Memory, id: identity("onenote", cfg.Version, struct {
		Schema  string          `json:"schema"`
		Request onenote.Request `json:"request"`
	}{cli.Schema, onenoteRequest})}
}

func (s *onenoteStage) Identity() dispatch.Identity { return s.id }

func (s *onenoteStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		media := filepath.Join(dir, "media")
		if err := os.Mkdir(media, 0o700); err != nil {
			yield(dispatch.Item{}, fmt.Errorf("onenote stage: %w", err))
			return
		}
		w := onenoteWalk{stash: stash{dir: dir}, yield: yield}
		req := onenoteRequest
		req.Input, req.Media = p.Path, media
		for rec, err := range s.reader.Read(ctx, req) {
			if err != nil {
				yield(dispatch.Item{}, partErr(ctx, fmt.Errorf("onenote stage: %w", err), s.memory))
				return
			}
			if err := w.record(rec); err != nil {
				if !errors.Is(err, errStopped) {
					yield(dispatch.Item{}, err)
				}
				return
			}
		}
	}
}

type onenoteWalk struct {
	stash stash
	yield func(dispatch.Item, error) bool
}

func (w *onenoteWalk) item(it dispatch.Item, err error) error {
	if err != nil {
		return err
	}
	if !w.yield(it, nil) {
		return errStopped
	}
	return nil
}

func (w *onenoteWalk) output(o dispatch.Output, err error) error {
	if err != nil {
		return err
	}
	return w.item(dispatch.OutputItem(o), nil)
}

func (w *onenoteWalk) record(rec onenote.Record) error {
	switch r := rec.(type) {
	case *onenote.Notebook:
		return w.output(record("notebook", r, dispatch.MediaOneNote, map[string]string{dispatch.MetaID: r.ID, dispatch.MetaName: r.Name}))
	case *onenote.SectionGroup:
		return w.output(record("section group "+r.ID, r, dispatch.MediaOneNote, map[string]string{dispatch.MetaID: r.ID, dispatch.MetaPath: strings.Join(r.Path, "/")}))
	case *onenote.Section:
		return w.output(record("section "+r.ID, r, dispatch.MediaOneNote, map[string]string{dispatch.MetaID: r.ID, dispatch.MetaPath: strings.Join(r.Path, "/")}))
	case *onenote.Page:
		return w.page(r)
	case *onenote.Skipped:
		where := "skipped " + r.Kind + " " + r.Name
		outcome := dispatch.Failed
		if r.Encrypted {
			outcome = dispatch.Encrypted
		}
		return w.item(unread(where, r.Name, outcome, r.Reason), nil)
	case *onenote.Warning:
		return w.item(dispatch.WarningItem(dispatch.Warning(*r)), nil)
	case *onenote.UnknownRecord:
		return w.output(dispatch.Output{Kind: dispatch.RecordOutput, Name: r.Kind, MediaType: dispatch.MediaOneNote, Value: r.Raw}, nil)
	}
	return fmt.Errorf("onenote stage: a record of type %T", rec)
}

func (w *onenoteWalk) page(p *onenote.Page) error {
	name := "page " + p.ID
	meta := map[string]string{dispatch.MetaID: p.ID, dispatch.MetaSection: p.Section, dispatch.MetaTitle: p.Title}
	if err := w.output(record(name, p, dispatch.MediaOneNote, meta)); err != nil {
		return err
	}
	if err := w.output(markdown(name, p.Markdown, meta), nil); err != nil {
		return err
	}
	for i, m := range p.Media {
		md := medium{
			path: m.Path, sum: m.SHA256, name: cmp.Or(m.Filename, m.File, string(m.Kind)), mime: recordingType(m),
			none: dispatch.Failed, why: "docproc-onenote wrote no content for it: its data is missing or invalid",
		}
		if m.File != "" {
			md.why = "not written: no media directory"
		}
		if err := w.item(w.stash.item(md, name+", media "+strconv.Itoa(i+1))); err != nil {
			return err
		}
	}
	return nil
}

func recordingType(m onenote.Medium) string {
	if m.Kind == onenote.EmbeddedFileMedium && (m.Type == "audio" || m.Type == "video") {
		return m.Type
	}
	return ""
}
