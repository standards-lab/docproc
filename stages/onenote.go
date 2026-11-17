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

// onenoteConfig is what a [onenoteStage] is built over: its Reader, the version of the tool behind
// it, and the memory bound the tool runs under, which a death by a signal's reason names.
type onenoteConfig struct {
	// Reader reads the sections and packages; it is required.
	Reader *onenote.Reader
	// Version names docproc-onenote's version line, as [versions] gives it; it is required.
	Version string
	Memory  int64
}

// onenoteRequest is the request every part is read under, but for its Input and Media, which are
// each part's own: --file-only, so a table of contents is refused, each of its sections read as
// its own input.
var onenoteRequest = onenote.Request{FileOnly: true}

// onenoteStage prepares a OneNote section or package, route.OneNote's parts, through onenote's
// Reader under --file-only, its media written into the part's directory. Each of docproc-onenote's
// lines is a [dispatch.RecordOutput] of [dispatch.MediaOneNote], its JSON as onenote's record
// marshals it back to its line, in its order: the notebook, named "notebook"; each section group,
// "section group <id>", and section, "section <id>", their Meta naming their path; each page,
// "page <id>", then its Markdown, a [dispatch.MarkdownOutput] of the same name, the Meta of both
// naming its id, its section's id, and its title; and a line of a kind onenote doesn't know, by its
// kind. docproc-onenote's warnings are [dispatch.Warning]s, each with its tool's name.
//
// After each page's outputs, the one media rule, [stash.item], turns each of its media, its images,
// embedded files, and ink, into its item at "page <id>, media <n>": a [dispatch.Found] part, named
// by the name it was inserted or attached under, else by its media name, its SHA-256 claimed by
// its media name; or an unread child, failed, for a medium docproc-onenote wrote no content for,
// its data missing or invalid. A recording, an embedded file whose type is "audio" or "video",
// carries that type as its stored type, so content of no signature route knows is routed by it. A
// page's ink is an SVG, which route routes vision-converted, to the picture stage's PNG. A skipped
// section is an unread child at "skipped section <name>": encrypted, for a password-protected one,
// and otherwise failed, its reason the tool's.
//
// A refusal internal/cli classes as encrypted, a password-protected section's, ends the stage
// with dispatch.ErrEncrypted; one it classes as read elsewhere, a table of contents refused under
// --file-only, ends it with dispatch.ErrReadElsewhere, its sections read as their own parts. Any
// other failure is classified once, by [partErr].
type onenoteStage struct {
	reader *onenote.Reader
	memory int64
	id     dispatch.Identity
}

// newOneNoteStage builds a onenoteStage over cfg, panicking naming Reader when it is nil, the
// wiring's mistake.
func newOneNoteStage(cfg onenoteConfig) *onenoteStage {
	if cfg.Reader == nil {
		panic("stages: onenoteConfig.Reader is nil")
	}
	return &onenoteStage{reader: cfg.Reader, memory: cfg.Memory, id: identity("onenote", cfg.Version, struct {
		Schema  string          `json:"schema"`
		Request onenote.Request `json:"request"`
	}{cli.Schema, onenoteRequest})}
}

// Identity returns the stage's identity: "onenote", docproc-onenote's version line, and its
// request's and schema's hash.
func (s *onenoteStage) Identity() dispatch.Identity { return s.id }

// Prepare reads the section or package at p.Path, as the type's doc says.
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

// onenoteWalk is one read's walk of its records: its stash and the loop's yield.
type onenoteWalk struct {
	stash stash
	yield func(dispatch.Item, error) bool
}

// item yields it, or returns err, or errStopped once the loop stops.
func (w *onenoteWalk) item(it dispatch.Item, err error) error {
	if err != nil {
		return err
	}
	if !w.yield(it, nil) {
		return errStopped
	}
	return nil
}

// output yields o, or returns err, record's error, or errStopped once the loop stops.
func (w *onenoteWalk) output(o dispatch.Output, err error) error {
	if err != nil {
		return err
	}
	return w.item(dispatch.OutputItem(o), nil)
}

// record yields rec's outputs, parts, and warnings.
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

// page yields p's record and Markdown, then each of its media, as the stage's doc says.
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

// recordingType returns the stored type of m, a recording's "audio" or "video", which route
// takes for content of no signature it knows; "" for any other medium.
func recordingType(m onenote.Medium) string {
	if m.Kind == onenote.EmbeddedFileMedium && (m.Type == "audio" || m.Type == "video") {
		return m.Type
	}
	return ""
}
