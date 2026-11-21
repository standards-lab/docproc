package stages

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strconv"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
)

type qpdfReader interface {
	RequiresPassword(ctx context.Context, pdf string) (bool, error)
	Form(ctx context.Context, pdf string) (document.Form, error)
}

type pdfDetacher interface {
	List(ctx context.Context, pdf string) ([]document.Attachment, error)
	Save(ctx context.Context, pdf string, number int, w io.Writer) error
}

type documentConfig struct {
	Preparer  *document.Preparer
	QPDF      qpdfReader
	PDFDetach pdfDetacher
	Version   string
	Request   document.ImagesRequest
	Inspect   inspect.Options
	Memory    int64
}

type documentStage struct {
	preparer *document.Preparer
	request  document.ImagesRequest
	qpdf     qpdfReader
	detach   pdfDetacher
	bomb     inspect.BombRule
	memory   int64
	id       dispatch.Identity
}

func newDocumentStage(cfg documentConfig) *documentStage {
	switch {
	case cfg.Preparer == nil:
		panic("stages: documentConfig.Preparer is nil")
	case cfg.QPDF == nil:
		panic("stages: documentConfig.QPDF is nil")
	case cfg.PDFDetach == nil:
		panic("stages: documentConfig.PDFDetach is nil")
	}
	req := cfg.Request
	req.DPI = cmp.Or(req.DPI, document.DefaultDPI)
	bomb := cfg.Inspect.Bomb()
	return &documentStage{
		preparer: cfg.Preparer, request: req, qpdf: cfg.QPDF, detach: cfg.PDFDetach, bomb: bomb, memory: cfg.Memory,
		id: identity("document", cfg.Version, struct {
			Request document.ImagesRequest `json:"request"`
			Bomb    inspect.BombRule       `json:"bomb"`
		}{req, bomb}),
	}
}

func (s *documentStage) Identity() dispatch.Identity { return s.id }

func (s *documentStage) UntrustedWhere() {}

type documentRun struct {
	s       *documentStage
	ctx     context.Context
	p       dispatch.Part
	yield   func(dispatch.Item, error) bool
	read    bool
	failed  []string
	stopped bool
}

func (r *documentRun) emit(it dispatch.Item) bool {
	if it.Output != nil || it.Part != nil {
		r.read = true
	}
	r.stopped = r.stopped || !r.yield(it, nil)
	return !r.stopped
}

func (r *documentRun) unit(program, where string, err error) bool {
	w, runErr := unitWarning(r.ctx, program, where, err, r.s.memory)
	if runErr != nil {
		r.stopped = true
		r.yield(dispatch.Item{}, runErr)
		return false
	}
	r.failed = append(r.failed, where+": "+w.What)
	return r.emit(dispatch.WarningItem(w))
}

func (s *documentStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		ctx := withWriteCap(ctx)
		r := &documentRun{s: s, ctx: ctx, p: p, yield: yield}
		locked, err := s.qpdf.RequiresPassword(ctx, p.Path)
		switch {
		case err != nil:
			if !r.unit("qpdf", "form", err) {
				return
			}
		case locked:
			flag := inspect.Flag{Check: inspect.PDFEncrypt, Class: inspect.Active, Evidence: "qpdf: the PDF needs a password to open"}
			if r.emit(dispatch.FlagItem(flag)) {
				yield(dispatch.Item{}, fmt.Errorf("document stage: the PDF needs a password: %w", dispatch.ErrEncrypted))
			}
			return
		default:
			if !r.forms() {
				return
			}
		}
		if !r.attachments(dir) || !r.pages(dir) {
			return
		}
		if !r.read && len(r.failed) > 0 {
			yield(dispatch.Item{}, fmt.Errorf("document stage: nothing of the PDF reads: %v: %w", r.failed, dispatch.ErrRefused))
		}
	}
}

func (r *documentRun) forms() bool {
	form, err := r.s.qpdf.Form(r.ctx, r.p.Path)
	switch {
	case err != nil:
		return r.unit("qpdf", "form", err)
	case len(form.Fields) == 0:
		return true
	}
	o, err := record("forms", form, dispatch.MediaForm, map[string]string{dispatch.MetaFields: strconv.Itoa(len(form.Fields))})
	if err != nil {
		r.yield(dispatch.Item{}, err)
		return false
	}
	return r.emit(dispatch.OutputItem(o))
}

func (r *documentRun) attachments(dir string) bool {
	list, err := r.s.detach.List(r.ctx, r.p.Path)
	if err != nil {
		return r.unit("pdfdetach", "attachments", err)
	}
	for _, a := range list {
		item, err := r.attachment(a, dir)
		if err != nil {
			r.yield(dispatch.Item{}, err)
			return false
		}
		if !r.emit(item) {
			return false
		}
	}
	return true
}

func (r *documentRun) attachment(a document.Attachment, dir string) (dispatch.Item, error) {
	where := "attachment " + a.Name
	whole := uint64(r.p.Size)
	bomb := r.s.bomb
	path, written, why, err := writeBySum(r.ctx, "document stage", bomb, func(w io.Writer) error {
		return r.s.detach.Save(r.ctx, r.p.Path, a.Number, w)
	}, dir, whole, func(_ *bombWriter, saveErr error) error {
		if saveErr != nil {
			return fmt.Errorf("doesn't save: %w", saveErr)
		}
		return nil
	})
	switch {
	case err != nil:
		return dispatch.Item{}, err
	case errors.Is(why, errBomb):
		return unread(where, a.Name, dispatch.Quarantined, "past the bomb rule as written", inspect.Flag{
			Check: inspect.ArchiveBomb, Class: inspect.Unsafe,
			Evidence: fmt.Sprintf("attachment %q: %d bytes written from a %d-byte PDF, %.0f:1; past %d bytes and %g:1",
				a.Name, written, whole, float64(written)/float64(max(whole, 1)), bomb.Size, bomb.Ratio),
		}), nil
	case why != nil:
		return notWritten(where, a.Name, why), nil
	}
	return dispatch.PartItem(dispatch.Found{Path: path, Name: a.Name, Where: where}), nil
}

func (r *documentRun) pages(dir string) bool {
	req := r.s.request
	req.Dir = dir
	for page, err := range r.s.preparer.Images(r.ctx, r.p.Path, req) {
		if err != nil {
			where := "pages"
			if page.Number != 0 {
				where = "page " + strconv.Itoa(page.Number)
			}
			if !r.unit("gs", where, err) {
				return false
			}
			continue
		}
		name := "page " + strconv.Itoa(page.Number)
		if req.Text && document.HasText(page.Text) {
			o := dispatch.Output{Kind: dispatch.TextOutput, Name: name, MediaType: "text/plain", Value: []byte(page.Text), Meta: pageMeta(page)}
			if !r.emit(dispatch.OutputItem(o)) {
				return false
			}
		}
		meta := pageMeta(page)
		meta[dispatch.MetaDPI] = strconv.Itoa(page.DPI)
		o := dispatch.Output{Kind: dispatch.ImageOutput, Name: name, MediaType: "image/png", Path: page.Path, Meta: meta}
		if !r.emit(dispatch.OutputItem(o)) {
			return false
		}
	}
	return true
}

func pageMeta(page document.PageImage) map[string]string {
	return map[string]string{dispatch.MetaPage: strconv.Itoa(page.Number), dispatch.MetaCount: strconv.Itoa(page.Count)}
}
