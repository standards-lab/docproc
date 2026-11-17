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

// qpdfReader is what a [documentStage] reads a PDF's structure through before rendering it:
// document's QPDF, which asks qpdf whether the PDF needs a password and reads its AcroForm.
type qpdfReader interface {
	RequiresPassword(ctx context.Context, pdf string) (bool, error)
	Form(ctx context.Context, pdf string) (document.Form, error)
}

// pdfDetacher is what a [documentStage] reads a PDF's embedded files through: document's
// Detacher, which lists them and saves one to a writer through pdfdetach.
type pdfDetacher interface {
	List(ctx context.Context, pdf string) ([]document.Attachment, error)
	Save(ctx context.Context, pdf string, number int, w io.Writer) error
}

// documentConfig is what a [documentStage] is built over: its Preparer, qpdf, and pdfdetach, the
// version of the tools behind them, the request each PDF's pages are prepared under, the
// inspection options whose bomb rule an embedded file is held to, and the memory bound the tools
// run under, which a death by a signal's reason names.
type documentConfig struct {
	// Preparer prepares the pages through gs; it is required.
	Preparer *document.Preparer
	// QPDF checks each PDF for a password before it is rendered and reads its AcroForm; it is
	// required.
	QPDF qpdfReader
	// PDFDetach saves each PDF's embedded files as parts; it is required.
	PDFDetach pdfDetacher
	// Version names gs's, qpdf's, and pdfdetach's version lines, as [versions] gives them; it is
	// required.
	Version string
	// Request is the request every PDF's pages are prepared under, checked by Build: its DPI,
	// Text, and MaxPixels, and its Concurrency, at least 1. Its Dir is each part's own.
	Request document.ImagesRequest
	// Inspect is the dispatcher's inspection options, validated by Build: its bomb rule is the
	// one an embedded file's saved bytes may not pass, against the whole PDF's size.
	Inspect inspect.Options
	Memory  int64
}

// documentStage prepares a PDF, route.Document's parts, through gs, qpdf, and pdfdetach, each of
// its units read on its own: the form, the attachments, and each page. A unit whose read fails as
// the content's, by [classify], is a [dispatch.Warning] naming the unit, "form", "attachments", or
// "page N", with its tool's name, and the part is prepared with the outputs it has; the part fails,
// recorded, only when nothing of it reads. A unit that fails as the run's ends the stage, so the
// part fails unrecorded.
//
// It first asks qpdf whether the PDF needs a password to open: one that does raises qpdf's
// finding as the part's own flag, inspect.PDFEncrypt, through dispatch.Item.Flag, then ends with
// dispatch.ErrEncrypted, nothing rendered; one encrypted but restricted only by its permissions,
// which opens with no password, is prepared as any other. It then reads the PDF's AcroForm, and a
// PDF whose AcroForm has fields yields, before anything else, a [dispatch.RecordOutput] of
// [dispatch.MediaForm] named "forms" holding a document.Form, its Meta's [dispatch.MetaFields]
// their count.
//
// It then saves each of the PDF's embedded files, the name tree's and the file attachment
// annotations': each is written into the part's directory under its content's SHA-256, never its
// stored name, streamed from pdfdetach's standard output while it is hashed, and is a
// [dispatch.Found] part, named by its stored name and found "attachment <name>". A file whose
// saved bytes pass the bomb rule, against the whole PDF's size, is stopped, its partial file
// removed, and is an unread child, quarantined, flagged inspect.ArchiveBomb; a file pdfdetach
// can't save is an unread child, failed, its reason why.
//
// The pages come last: each page's text layer, when the request reads it, as a
// [dispatch.TextOutput], and its image as a [dispatch.ImageOutput], the PNG file the Preparer
// writes in the part's directory, each named "page N" with the page's number and the PDF's page
// count in its Meta, [dispatch.MetaPage] and [dispatch.MetaCount], and an image the DPI it was
// rendered at, [dispatch.MetaDPI], which a page past the request's MaxPixels has lowered. A page
// whose text layer holds no text has no text output.
type documentStage struct {
	preparer *document.Preparer
	request  document.ImagesRequest
	qpdf     qpdfReader
	detach   pdfDetacher
	bomb     inspect.BombRule
	memory   int64
	id       dispatch.Identity
}

// newDocumentStage builds a documentStage over cfg, panicking naming a required field that is nil,
// the wiring's mistake.
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

// Identity returns the stage's identity: "document", its tools' version lines, and its request's
// and bomb rule's hash.
func (s *documentStage) Identity() dispatch.Identity { return s.id }

// UntrustedWhere marks the stage as one whose found parts' Where come from its attachments'
// stored names, which two siblings may share.
func (s *documentStage) UntrustedWhere() {}

// documentRun is one PDF's preparation: its part, the loop's yield, whether anything of it has
// been read, an output or a found part, and the reasons of the units of the content's class that
// failed, for the part's failure when nothing reads.
type documentRun struct {
	s       *documentStage
	ctx     context.Context
	p       dispatch.Part
	yield   func(dispatch.Item, error) bool
	read    bool
	failed  []string
	stopped bool
}

// emit yields it, noting what it reads; false once the loop stops.
func (r *documentRun) emit(it dispatch.Item) bool {
	if it.Output != nil || it.Part != nil {
		r.read = true
	}
	r.stopped = r.stopped || !r.yield(it, nil)
	return !r.stopped
}

// unit takes err, the failure of the unit where, read by program: a warning, its reason kept,
// when it is the content's; otherwise the stage's end. It reports whether the stage goes on.
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

// Prepare prepares the PDF at p.Path, as the type's doc says.
func (s *documentStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
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

// forms yields the forms record of the PDF, when its AcroForm has fields, or the form's warning.
// It reports whether the stage goes on.
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

// attachments saves each embedded file of the PDF into dir and yields its item, its part or its
// unread child, or the listing's warning. It reports whether the stage goes on.
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

// attachment returns the item of embedded file a: its part, written into dir by its SHA-256, or
// its unread child. An error is the stage's own, the directory unwritable or ctx ending; a file's
// content never fails it.
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
		return unread(where, a.Name, dispatch.Failed, why.Error()), nil
	}
	return dispatch.PartItem(dispatch.Found{Path: path, Name: a.Name, Where: where}), nil
}

// pages prepares the PDF's pages into dir and yields each one's outputs, or its warning. It
// reports whether the stage goes on.
func (r *documentRun) pages(dir string) bool {
	req := r.s.request
	req.Dir = dir
	for page, err := range r.s.preparer.Images(r.ctx, r.p.Path, req) {
		if err != nil {
			// A failed count names no page, and no page follows it.
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

// pageMeta returns a page's outputs' Meta: its number and the PDF's page count.
func pageMeta(page document.PageImage) map[string]string {
	return map[string]string{dispatch.MetaPage: strconv.Itoa(page.Number), dispatch.MetaCount: strconv.Itoa(page.Count)}
}
