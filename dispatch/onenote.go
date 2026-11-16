package dispatch

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

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/onenote"
)

// OneNoteConfig is what a [OneNoteStage] is built over: the caller's Reader and the version of
// the tool behind it.
type OneNoteConfig struct {
	// Reader reads the notebooks and sections; it is required.
	Reader *onenote.Reader
	// Version names docproc-onenote's version, as [Versions] gives it; it is required.
	Version string
}

// OneNoteStage prepares a OneNote section, table of contents, or package, route.OneNote's parts,
// through onenote's Reader, its media written into the part's directory. Each of
// docproc-onenote's records is a [RecordOutput], its JSON as onenote's record marshals it, in its
// order: the notebook, named "notebook"; each section group, "section group <id>", and section,
// "section <id>", their Meta naming their path; each page, "page <id>", then its Markdown, a
// [MarkdownOutput] of the same name, the Meta of both naming its id, its section's id, and its
// title; each skipped record, "skipped <kind>", its Meta naming the record and its reason; and a
// record of a kind onenote doesn't know, by its kind. docproc-onenote's warnings, when it gave
// any, are a last record, "warnings", a JSON array, each "<where>: <what>".
//
// Each of a page's media, its images, embedded files, and ink, that docproc-onenote wrote content
// for is a [Found] part, the dispatcher's to route and inspect again, named by the name it was
// inserted or attached under, else by its media name, its Where naming the page and the medium's
// place among the page's media, "page <id>, media <n>". A page's ink is an SVG, which route.File
// routes vision-converted, to the picture stage's PNG. A medium docproc-onenote wrote no content
// for, its data missing or invalid, or whose file isn't in the media directory, is a record named
// by its place, its Meta naming why it has none, never dropped.
//
// A refusal internal/cli classes as encrypted, a password-protected section's, ends the stage
// with [ErrEncrypted]; one it classes as read elsewhere, a table of contents refused under
// --file-only, ends it with [ErrReadElsewhere], its sections read as their own parts.
type OneNoteStage struct {
	reader *onenote.Reader
	id     Identity
}

// NewOneNoteStage builds a OneNoteStage over cfg, panicking naming Reader when it is nil, the
// caller's mistake, and refusing one with no Version.
func NewOneNoteStage(cfg OneNoteConfig) (*OneNoteStage, error) {
	if cfg.Reader == nil {
		panic("dispatch: OneNoteConfig.Reader is nil")
	}
	id, err := adapterIdentity("onenote", cfg.Version, struct {
		Schema string `json:"schema"`
	}{cli.Schema})
	if err != nil {
		return nil, err
	}
	return &OneNoteStage{reader: cfg.Reader, id: id}, nil
}

// Identity returns the stage's identity: "onenote", docproc-onenote's version, and the schema's
// hash.
func (s *OneNoteStage) Identity() Identity { return s.id }

// Prepare reads the notebook or section at p.Path, as the type's doc says.
func (s *OneNoteStage) Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error] {
	return func(yield func(Item, error) bool) {
		media := filepath.Join(dir, "media")
		if err := os.Mkdir(media, 0o700); err != nil {
			yield(Item{}, fmt.Errorf("onenote stage: %w", err))
			return
		}
		w := onenoteWalk{stash: stash{dir: dir}, yield: yield}
		for rec, err := range s.reader.Read(ctx, onenote.Request{Input: p.Path, Media: media}) {
			if err != nil {
				yield(Item{}, onenoteError(err))
				return
			}
			if err := w.record(rec); err != nil {
				if !errors.Is(err, errStopped) {
					yield(Item{}, err)
				}
				return
			}
		}
		if len(w.warnings) > 0 {
			if err := w.output(record("warnings", w.warnings, nil)); err != nil && !errors.Is(err, errStopped) {
				yield(Item{}, err)
			}
		}
	}
}

// onenoteError returns err, a read's, as the stage's: a refusal internal/cli classes as encrypted
// matches ErrEncrypted, and one it classes as read elsewhere ErrReadElsewhere.
//
// Interim until slice 15: internal/cli can't name dispatch's classes, so the stage maps its
// cli.Refusal to them.
func onenoteError(err error) error {
	if exit, ok := errors.AsType[*cli.ExitError](err); ok {
		switch exit.Refusal() {
		case cli.Encrypted:
			return fmt.Errorf("onenote stage: %w: %w", ErrEncrypted, err)
		case cli.ReadElsewhere:
			return fmt.Errorf("onenote stage: %w: %w", ErrReadElsewhere, err)
		}
	}
	return err
}

// onenoteWalk is one read's walk of its records: its stash, its warnings so far, each
// "<where>: <what>", and the loop's yield.
type onenoteWalk struct {
	stash    stash
	warnings []string
	yield    func(Item, error) bool
}

// output yields o, or returns err, record's error, or errStopped once the loop stops.
func (w *onenoteWalk) output(o Output, err error) error {
	if err != nil {
		return err
	}
	if !w.yield(OutputItem(o), nil) {
		return errStopped
	}
	return nil
}

// record yields rec's outputs and parts.
func (w *onenoteWalk) record(rec onenote.Record) error {
	switch r := rec.(type) {
	case *onenote.Notebook:
		return w.output(record("notebook", r, map[string]string{"id": r.ID, "name": r.Name}))
	case *onenote.SectionGroup:
		return w.output(record("section group "+r.ID, r, map[string]string{"id": r.ID, "path": strings.Join(r.Path, "/")}))
	case *onenote.Section:
		return w.output(record("section "+r.ID, r, map[string]string{"id": r.ID, "path": strings.Join(r.Path, "/")}))
	case *onenote.Page:
		return w.page(r)
	case *onenote.Skipped:
		return w.output(record("skipped "+r.Kind, r, map[string]string{"name": r.Name, "reason": r.Reason}))
	case *onenote.Warning:
		w.warnings = append(w.warnings, r.Where+": "+r.What)
		return nil
	case *onenote.UnknownRecord:
		return w.output(Output{Kind: RecordOutput, Name: r.Kind, MediaType: "application/json", Value: r.Raw}, nil)
	}
	return fmt.Errorf("onenote stage: a record of type %T", rec)
}

// The Meta reasons of a medium the stage finds no file for: one docproc-onenote wrote no content
// for, and one whose file it named but isn't in the media directory.
const (
	onenoteNoContent = "docproc-onenote wrote no content for it: its data is missing or invalid"
	onenoteNoFile    = "its file isn't in the media directory"
)

// page yields p's record and Markdown, then each of its media, as the stage's doc says.
func (w *onenoteWalk) page(p *onenote.Page) error {
	name := "page " + p.ID
	meta := map[string]string{"id": p.ID, "section": p.Section, "title": p.Title}
	if err := w.output(record(name, p, meta)); err != nil {
		return err
	}
	if err := w.output(markdown(name, p.Markdown, meta), nil); err != nil {
		return err
	}
	for i, m := range p.Media {
		where := name + ", media " + strconv.Itoa(i+1)
		f, ok := Found{Name: cmp.Or(m.Filename, m.File), Where: where}, false
		if m.Path != "" {
			var err error
			if f, ok, err = w.stash.find(m.Path, f); err != nil {
				return err
			}
		}
		if !ok {
			reason := onenoteNoContent
			if m.File != "" {
				reason = onenoteNoFile
			}
			if err := w.output(record(where, m, map[string]string{"reason": reason})); err != nil {
				return err
			}
			continue
		}
		if !w.yield(PartItem(f), nil) {
			return errStopped
		}
	}
	return nil
}
