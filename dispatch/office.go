package dispatch

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/route"
)

// OfficeConfig is what an [OfficeStage] is built over: the caller's Converter, the version of
// the tools behind it, and the scale metafiles are rendered at.
type OfficeConfig struct {
	// Converter converts the documents; it is required.
	Converter *office.Converter
	// Version names the tools behind the Converter, office's, LibreOffice's, and magick's, as
	// [Versions] gives them; it is required.
	Version string
	// MetafileScale is the scale a document's EMF and WMF pictures are rendered at, as
	// office.Request's; part of the stage's identity.
	MetafileScale int
}

// OfficeStage prepares an office document, route.Office's parts, through office's Converter:
// the Markdown of every record office yields, concatenated in order into the document's whole
// rendering, as a [MarkdownOutput] named "document", its Meta naming the format a legacy document
// or an RTF was converted from, "rtf" for an RTF, and docproc-ooxml's warnings, a line each; and
// each file the Document's media name as a [Found] part, the dispatcher's to route and inspect
// again: a picture converted to PNG as its PNG, any other as stored, a picture whose converter
// the Converter wasn't given among them, so its own stage may convert it. A part's Where names the
// medium by its media name, "media <SHA-256>.<ext>", and ", as PNG" for a converted one.
//
// An RTF is office's to convert through LibreOffice to docx, as a .doc is. The stage has no
// processor yet for an ODF package, a WordPerfect document, or an Excel binary workbook, which
// LibreOffice would convert: each ends with [ErrNoProcessor], its reason naming the kind. A
// refusal internal/cli classes as encrypted ends it with [ErrEncrypted].
//
// Interim until slice 15, which moves the adapters into stages: the stage joins an xlsx's sheets
// into the one Markdown output, as before the reader streamed them.
type OfficeStage struct {
	converter *office.Converter
	scale     int
	id        Identity
}

// NewOfficeStage builds an OfficeStage over cfg, panicking naming Converter when it is nil, the
// caller's mistake, and refusing one with no Version.
func NewOfficeStage(cfg OfficeConfig) (*OfficeStage, error) {
	if cfg.Converter == nil {
		panic("dispatch: OfficeConfig.Converter is nil")
	}
	id, err := adapterIdentity("office", cfg.Version, struct {
		MetafileScale int `json:"metafile_scale"`
	}{cfg.MetafileScale})
	if err != nil {
		return nil, err
	}
	return &OfficeStage{converter: cfg.Converter, scale: cfg.MetafileScale, id: id}, nil
}

// Identity returns the stage's identity: "office", its tools' version, and its options' hash.
func (s *OfficeStage) Identity() Identity { return s.id }

// unread names the office kinds the stage has no processor for yet, each with its article.
var unread = map[route.Kind]string{
	route.ODF:         "an ODF package",
	route.WordPerfect: "a WordPerfect document",
}

// Prepare converts the document at p.Path, as the type's doc says.
func (s *OfficeStage) Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error] {
	return func(yield func(Item, error) bool) {
		what, ok := unread[p.ContentType.Kind]
		if !ok && p.ContentType.Kind == route.ZIP && strings.EqualFold(filepath.Ext(p.Name), ".xlsb") {
			what, ok = "an Excel binary workbook", true
		}
		if ok {
			yield(Item{}, fmt.Errorf("office stage: %w for %s", ErrNoProcessor, what))
			return
		}
		var doc *office.Document
		var md strings.Builder
		var warnings []string
		for rec, err := range s.converter.Convert(ctx, office.Request{
			Path: p.Path, Dir: dir, Name: p.Name, MetafileScale: s.scale,
		}) {
			if err != nil {
				yield(Item{}, officeError(err))
				return
			}
			switch r := rec.(type) {
			case *office.Document:
				doc = r
				md.WriteString(r.Markdown)
			case *office.Sheet:
				md.WriteString(r.Markdown)
			case *office.Rows:
				md.WriteString(r.Markdown)
			case *office.Warning:
				warnings = append(warnings, r.Where+": "+r.What)
			}
		}
		meta := map[string]string{}
		if doc.ConvertedFrom != "" {
			meta["converted_from"] = doc.ConvertedFrom
		}
		if len(warnings) > 0 {
			meta["warnings"] = strings.Join(warnings, "\n")
		}
		if !yield(OutputItem(markdown("document", md.String(), meta)), nil) {
			return
		}
		// A file named by several media, or two stored files of the same bytes under two
		// extensions sharing one PNG, is found once.
		found := map[string]bool{}
		for _, m := range doc.Media {
			if m.Path == "" || found[m.Path] {
				continue
			}
			found[m.Path] = true
			mime, where := m.ContentType, "media "+m.File
			if m.Converted {
				mime, where = "image/png", where+", as PNG"
			}
			if !yield(PartItem(Found{Path: m.Path, Name: filepath.Base(m.Path), MIME: mime, Where: where}), nil) {
				return
			}
		}
	}
}

// officeError returns err, a conversion's, as the stage's: a refusal internal/cli classes as
// encrypted matches ErrEncrypted.
//
// Interim until slice 15: internal/cli can't name dispatch's classes, so the stage maps its
// cli.Refusal to them.
func officeError(err error) error {
	if exit, ok := errors.AsType[*cli.ExitError](err); ok && exit.Refusal() == cli.Encrypted {
		return fmt.Errorf("office stage: %w: %w", ErrEncrypted, err)
	}
	return fmt.Errorf("office stage: %w", err)
}
