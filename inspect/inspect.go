package inspect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/standards-lab/docproc/route"
)

// Class names what a flag's finding means for the part, as the package doc explains.
type Class string

const (
	// Active is content that could act, or that a reader can't see into: flagged, and the part
	// proceeds by default, since no stage executes it.
	Active Class = "active"
	// Unsafe is content unsafe to read: flagged, and the part is quarantined by default, so no
	// stage runs on it and its content is kept.
	Unsafe Class = "unsafe"
)

// Verdict names what becomes of a part once its flags are weighed under a policy.
type Verdict string

const (
	// Proceed lets the part go to its stage.
	Proceed Verdict = "proceed"
	// Quarantine keeps the part from every stage, its content kept and reported with its
	// evidence.
	Quarantine Verdict = "quarantine"
)

// Check names one inspection by its rule, never by the format it reads: each flag names the
// check that raised it.
type Check string

// The checks of class [Active].
const (
	// SignatureMismatch is a signature that disagrees with the name's extension, such as a
	// PNG named .pdf. It is a name check, [Name]'s.
	SignatureMismatch Check = "signature-mismatch"
	// Polyglot is content of one format carrying another's: a ZIP end-of-central-directory
	// record inside content that isn't a container, or a PDF header after another signature.
	Polyglot Check = "polyglot"
	// Executable is executable content, by route's PE, ELF, Mach-O, or "#!" kinds.
	Executable Check = "executable"
	// ExecutableExtension is a name whose extension is an executable's, such as .exe or .js. It
	// is a name check, [Name]'s.
	ExecutableExtension Check = "executable-extension"
	// DoubleExtension is an executable extension after another, such as "invoice.pdf.exe". It is
	// a name check, [Name]'s.
	DoubleExtension Check = "double-extension"
	// VBAProject is an OOXML package's VBA project, vbaProject.bin: macros.
	VBAProject Check = "vba-project"
	// MacroSheet is an OOXML workbook's Excel 4.0 macro sheet, under xl/macrosheets/.
	MacroSheet Check = "macro-sheet"
	// ActiveX is an OOXML package's ActiveX control part.
	ActiveX Check = "activex"
	// OLEObject is an OOXML package's OLE object, an embedding oleObject*.bin or a relationship
	// of the oleObject type.
	OLEObject Check = "ole-object"
	// ExternalRelationship is an OOXML relationship whose TargetMode is External, other than a
	// hyperlink: a remote template, a linked picture, an OLE link.
	ExternalRelationship Check = "external-relationship"
	// RelationshipsUnreadable is an OOXML relationships part whose scan ended early: XML that
	// doesn't decode, a charset it can't decode, or a part past the package's budget, so an
	// external relationship after the point it ended goes unseen.
	RelationshipsUnreadable Check = "relationships-unreadable"
	// OOXMLEncrypted is an encrypted OOXML package: an OLE2 compound file holding an
	// EncryptedPackage stream, whatever its name.
	OOXMLEncrypted Check = "ooxml-encrypted"

	// The PDF names pdfid counts, each a check of its own; [PDFEncrypt] marks encryption.
	PDFJavaScript   Check = "pdf-javascript"
	PDFJS           Check = "pdf-js"
	PDFOpenAction   Check = "pdf-openaction"
	PDFAA           Check = "pdf-aa"
	PDFLaunch       Check = "pdf-launch"
	PDFEmbeddedFile Check = "pdf-embeddedfile"
	PDFRichMedia    Check = "pdf-richmedia"
	PDFXFA          Check = "pdf-xfa"
	PDFEncrypt      Check = "pdf-encrypt"

	// SVGScript is an SVG's script element.
	SVGScript Check = "svg-script"
	// SVGEventHandler is an SVG attribute naming an event handler, one whose name begins "on".
	SVGEventHandler Check = "svg-event-handler"
	// SVGExternalHref is an SVG href or xlink:href that doesn't begin "#" or "data:".
	SVGExternalHref Check = "svg-external-href"
	// SVGForeignObject is an SVG's foreignObject element, which embeds other markup.
	SVGForeignObject Check = "svg-foreign-object"
	// SVGUnreadable is an SVG whose scan ended early: markup that doesn't decode, or a charset it
	// can't decode, so what follows the point it ended goes unseen.
	SVGUnreadable Check = "svg-unreadable"

	// ArchiveTraversal is an archive entry whose name has a ".." element. The entry is an unread
	// child: no stage reads it.
	ArchiveTraversal Check = "archive-traversal"
	// ArchiveAbsolutePath is an archive entry whose name is absolute or begins with a drive
	// letter. The entry is an unread child.
	ArchiveAbsolutePath Check = "archive-absolute-path"
	// ArchiveSymlink is an archive entry that is a symbolic link. The entry is an unread child:
	// no link is made.
	ArchiveSymlink Check = "archive-symlink"
	// ArchiveHardlink is an archive entry that is a hard link. The entry is an unread child.
	ArchiveHardlink Check = "archive-hardlink"
	// ArchiveDevice is an archive entry that is a character or block device, a FIFO, or a
	// socket: a node an extraction would make rather than a file. The entry is an unread child.
	ArchiveDevice Check = "archive-device"
	// ArchiveEncrypted is an archive entry its archive marks encrypted, which no stage can read.
	ArchiveEncrypted Check = "archive-encrypted"
)

// The archive checks' rules are seven, whatever the archive's format: [ArchiveTraversal],
// [ArchiveAbsolutePath], [ArchiveSymlink], [ArchiveHardlink], [ArchiveDevice], [ArchiveEncrypted],
// and [ArchiveBomb]. An archive whose directory can't be read is unreadable by its format's own
// check, as an SVG's or a relationships part's scan is.

// The checks of class [Unsafe].
const (
	// ArchiveBomb is an archive, or an entry of one, that expands past the bomb rule its
	// [Options] set, by the sizes it declares or the bytes a stage writes: an entry past both
	// the rule's size and its ratio against its compressed size, or against the whole archive's
	// where its format shares or doesn't declare one; the sum of an archive's entries past both
	// against the whole archive's size; and a ZIP whose entries' declared compressed sizes sum
	// past its own size, whatever the rule, since no ZIP's do.
	ArchiveBomb Check = "archive-bomb"
	// ZIPUnreadable is content with a ZIP signature whose central directory can't be read, or
	// whose end record lists more entries than its [Options] allow, so its entries can't all be
	// checked.
	ZIPUnreadable Check = "zip-unreadable"
	// TarUnreadable is content with a tar signature whose headers can't all be read, a truncated
	// tar's among them, or which holds more entries than its [Options] allow, so its entries
	// can't all be checked.
	TarUnreadable Check = "tar-unreadable"
)

// classes are every check's class.
var classes = map[Check]Class{
	SignatureMismatch: Active, Polyglot: Active, Executable: Active,
	ExecutableExtension: Active, DoubleExtension: Active,
	VBAProject: Active, MacroSheet: Active, ActiveX: Active, OLEObject: Active,
	ExternalRelationship: Active, RelationshipsUnreadable: Active, OOXMLEncrypted: Active,
	PDFJavaScript: Active, PDFJS: Active, PDFOpenAction: Active, PDFAA: Active,
	PDFLaunch: Active, PDFEmbeddedFile: Active, PDFRichMedia: Active, PDFXFA: Active,
	PDFEncrypt: Active,
	SVGScript:  Active, SVGEventHandler: Active, SVGExternalHref: Active, SVGForeignObject: Active,
	SVGUnreadable:    Active,
	ArchiveTraversal: Active, ArchiveAbsolutePath: Active, ArchiveSymlink: Active,
	ArchiveHardlink: Active, ArchiveDevice: Active, ArchiveEncrypted: Active,
	ArchiveBomb: Unsafe, ZIPUnreadable: Unsafe, TarUnreadable: Unsafe,
}

// Checks returns every check, sorted, for a caller configuring a policy.
func Checks() []Check {
	all := make([]Check, 0, len(classes))
	for c := range classes {
		all = append(all, c)
	}
	slices.Sort(all)
	return all
}

// Flag is one finding: the check that raised it, its class, and its evidence, in a few words
// naming where it is, such as `/JavaScript at offset 1146` or `entry "../evil.txt"`. A check
// raises one flag at most, its evidence the first finding and a count of any others. It is JSON
// on encoding/json, as a part's shape carries it.
type Flag struct {
	Check    Check  `json:"check"`
	Class    Class  `json:"class"`
	Evidence string `json:"evidence"`
}

// Report is what an inspection found in a part: the kind its signature names, its flags, sorted
// by check, and the verdict they come to under the [Options].
type Report struct {
	Kind    route.Kind
	Flags   []Flag
	Verdict Verdict
}

// Options tune an inspection. No bound has a default: the zero Options weighs each flag by its
// class, applies no bomb rule but the one no ZIP passes, and bounds no listing.
type Options struct {
	// Policy overrides the default action of the checks it names, Proceed or Quarantine, so a
	// caller may quarantine an active finding, such as a macro, or let an unsafe one proceed.
	Policy map[Check]Verdict
	// BombRatio is the expansion ratio, expanded to compressed size, past which an archive's
	// entry, or a stream, may be a bomb: with BombSize, the rule [Options.Bomb] returns. Both
	// are set, or neither, and neither applies no rule.
	BombRatio float64
	// BombSize is the expanded size in bytes past which an archive's entry, or a stream, may be
	// a bomb, with BombRatio.
	BombSize uint64
	// Entries is the most entries a ZIP or a tar may hold: one past it is unreadable, a ZIP
	// before archive/zip reads its central directory, by the count its end record lists, and a
	// tar at the header past it. Zero bounds none.
	Entries int
}

// ErrOptions is the error invalid [Options] match: a policy naming a check the package doesn't
// have or an action other than Proceed or Quarantine; a ratio that is negative, infinite, or
// not a number; one of BombRatio and BombSize set without the other; or negative Entries.
var ErrOptions = errors.New("inspect: invalid options")

// Validate refuses invalid options, its error matching [ErrOptions]. Every function that takes
// Options validates them; a caller that holds them, such as a dispatcher, validates them once
// at construction.
func (o Options) Validate() error {
	for c, v := range o.Policy {
		if classes[c] == "" {
			return fmt.Errorf("%w: policy names no check %q", ErrOptions, c)
		}
		if v != Proceed && v != Quarantine {
			return fmt.Errorf("%w: policy for %s is %q, not proceed or quarantine", ErrOptions, c, v)
		}
	}
	switch {
	case !(o.BombRatio >= 0) || math.IsInf(o.BombRatio, 1):
		return fmt.Errorf("%w: bomb ratio %g", ErrOptions, o.BombRatio)
	case (o.BombRatio == 0) != (o.BombSize == 0):
		return fmt.Errorf("%w: bomb ratio %g and size %d: set both or neither", ErrOptions, o.BombRatio, o.BombSize)
	case o.Entries < 0:
		return fmt.Errorf("%w: entries %d", ErrOptions, o.Entries)
	}
	return nil
}

// Bomb returns the bomb rule the options set, the zero rule when they set none.
func (o Options) Bomb() BombRule { return BombRule{Ratio: o.BombRatio, Size: o.BombSize} }

// Verdict is the verdict flags come to under the options' policy: Quarantine when any flag's
// action is, each flag's action the policy's for its check, or its class's default, Proceed for
// an active flag and Quarantine for an unsafe one.
func (o Options) Verdict(flags []Flag) Verdict {
	for _, f := range flags {
		action, ok := o.Policy[f.Check]
		if !ok {
			action = Proceed
			if f.Class == Unsafe {
				action = Quarantine
			}
		}
		if action == Quarantine {
			return Quarantine
		}
	}
	return Proceed
}

// BombRule is the bomb rule: content is a bomb when it expands past both Size bytes and Ratio
// times what it is weighed against. The zero rule is passed by nothing.
type BombRule struct {
	Ratio float64
	Size  uint64
}

// Past reports whether expanded bytes, weighed against against bytes, at least one, pass both
// of the rule's bounds. Inspect weighs an entry's and an archive's declared sizes by it, and a
// stage that writes an archive's entries weighs the bytes it writes.
func (b BombRule) Past(expanded, against uint64) bool {
	return b.Size > 0 && expanded > b.Size && float64(expanded)/float64(max(against, 1)) > b.Ratio
}

// Inspect inspects the part at path, named name, before its stage, and returns its report: the
// content's flags, as [Content] finds them in the file, and the name's, as [Name] finds them,
// sorted by check, and their verdict under opts. The name may differ from path's, as a file
// named by its SHA-256 does. It is for a caller that triages a file before queueing it; a
// dispatcher, which has the file open and its kind, calls Content, and Name at each of the
// part's names. Inspect returns an error for opts it refuses, matching [ErrOptions], for a file
// it can't read, and for ctx's end.
func Inspect(ctx context.Context, path, name string, opts Options) (Report, error) {
	if err := opts.Validate(); err != nil {
		return Report{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Report{}, fmt.Errorf("inspect: %w", err)
	}
	defer func() { _ = f.Close() }() // read only: a close error loses nothing
	info, err := f.Stat()
	if err != nil {
		return Report{}, fmt.Errorf("inspect: %w", err)
	}
	head := make([]byte, route.HeadLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Report{}, fmt.Errorf("inspect: %s: %w", path, err)
	}
	kind := route.Identify(head[:n])
	rep, err := Content(ctx, f, info.Size(), kind, opts)
	if err != nil {
		return Report{}, err
	}
	rep.Flags = append(rep.Flags, Name(name, kind)...)
	slices.SortFunc(rep.Flags, func(a, b Flag) int { return strings.Compare(string(a.Check), string(b.Check)) })
	rep.Verdict = opts.Verdict(rep.Flags)
	return rep, nil
}

// Content inspects the content r holds, size bytes, of kind, as route.Identify names it, and
// returns its report: its content flags, sorted by check, and their verdict under opts. Its
// flags depend on the kind and the bytes alone, so the same content gives the same report under
// the same options on every run, whatever it is named. It runs no stage and no program: it reads
// r streaming, its memory bounded whatever its size, a ZIP by its central directory and a tar by
// its headers, no entry extracted. It returns an error for opts it refuses, matching
// [ErrOptions], for r's own, and for ctx's end.
func Content(ctx context.Context, r io.ReaderAt, size int64, kind route.Kind, opts Options) (Report, error) {
	if err := opts.Validate(); err != nil {
		return Report{}, err
	}
	in := &inspection{file: r, size: size, kind: kind, opts: opts, found: map[Check]*finding{}}
	if executableKinds[kind] {
		in.flag(Executable, fmt.Sprintf("%s signature", kind))
	}
	if err := in.stream(ctx); err != nil {
		return Report{}, err
	}
	var err error
	switch kind {
	case route.OLE2:
		var encrypted bool
		if encrypted, err = route.EncryptedPackage(r, size); err != nil {
			return Report{}, fmt.Errorf("inspect: %w", err)
		}
		if encrypted {
			in.flag(OOXMLEncrypted, "OLE2 compound file with an EncryptedPackage stream")
		}
	case route.ZIP, route.ODF:
		err = in.zip(ctx)
	case route.Tar:
		err = in.tar(ctx)
	case route.SVG:
		err = in.svg(ctx)
	}
	if err != nil {
		return Report{}, err
	}
	flags := in.flags()
	return Report{Kind: kind, Flags: flags, Verdict: opts.Verdict(flags)}, nil
}

// inspection is one inspection's state: the content, its kind, and the findings so far, by
// check.
type inspection struct {
	file  io.ReaderAt
	size  int64
	kind  route.Kind
	opts  Options
	found map[Check]*finding
	// rels is what remains of the package's budget for its relationship parts, once a ZIP's
	// scan has set it.
	rels int64
}

// finding is a check's first evidence and how many more findings it had.
type finding struct {
	evidence string
	more     int
}

// flag records a finding of check c.
func (in *inspection) flag(c Check, evidence string) {
	if f, ok := in.found[c]; ok {
		f.more++
		return
	}
	in.found[c] = &finding{evidence: evidence}
}

// flags returns the findings as flags, sorted by check, each evidence counting the findings
// after its first.
func (in *inspection) flags() []Flag {
	var flags []Flag
	for c, f := range in.found {
		evidence := f.evidence
		if f.more > 0 {
			evidence = fmt.Sprintf("%s, and %d more", evidence, f.more)
		}
		flags = append(flags, Flag{Check: c, Class: classes[c], Evidence: evidence})
	}
	slices.SortFunc(flags, func(a, b Flag) int { return strings.Compare(string(a.Check), string(b.Check)) })
	return flags
}

// executableExtensions are the extensions of programs and scripts a desktop runs when opened.
var executableExtensions = map[string]bool{
	".exe": true, ".dll": true, ".js": true, ".vbs": true, ".scr": true, ".bat": true,
	".cmd": true, ".ps1": true, ".lnk": true, ".hta": true, ".jar": true, ".msi": true,
}

// executableKinds are route's kinds of executable content.
var executableKinds = map[route.Kind]bool{
	route.PE: true, route.ELF: true, route.MachO: true, route.Shebang: true,
}

// Name returns the flags of a part's name, of content of kind, sorted by check: an executable
// extension, and one after another extension, as in "invoice.pdf.exe"; and a signature that
// disagrees with the extension. It reads no content, so a dispatcher calls it at every name a
// part has. An OOXML extension on an OLE2 compound file is no mismatch, since an encrypted
// package is one; [Content] says whether it is.
func Name(name string, kind route.Kind) []Flag {
	in := &inspection{kind: kind, found: map[Check]*finding{}}
	ext := route.Ext(name)
	if executableExtensions[ext] {
		in.flag(ExecutableExtension, fmt.Sprintf("extension %s", ext))
		trimmed := strings.TrimRight(baseName(name), ". ")
		stem := trimmed[:len(trimmed)-len(ext)]
		if inner := path.Ext(stem); plausibleExtension(inner) {
			in.flag(DoubleExtension, fmt.Sprintf("%q: %s, then %s", name, strings.ToLower(inner), ext))
		}
	}
	if want, ok := extensionKinds[ext]; ok && !slices.Contains(want, kind) && (kind != route.OLE2 || !route.OOXMLExtension(ext)) {
		got := string(kind)
		if kind == route.Unknown {
			got = "no signature"
		}
		in.flag(SignatureMismatch, fmt.Sprintf("extension %s, %s", ext, got))
	}
	return in.flags()
}

// baseName returns the last element of a name, which may be a path by either separator.
func baseName(n string) string { return path.Base(strings.ReplaceAll(n, `\`, "/")) }

// plausibleExtension reports whether ext, with its dot, looks like a file extension: one to
// five letters or digits, as ".pdf" or ".docx" does and ".report" doesn't.
func plausibleExtension(ext string) bool {
	if len(ext) < 2 || len(ext) > 6 {
		return false
	}
	return !strings.ContainsFunc(ext[1:], func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
}

// extensionKinds are the kinds a signature may be for an extension that names a format; an
// extension not listed, such as .txt, which no signature marks, isn't checked.
var extensionKinds = map[string][]route.Kind{
	".pdf": {route.PDF},
	".png": {route.PNG}, ".jpg": {route.JPEG}, ".jpeg": {route.JPEG}, ".gif": {route.GIF},
	".bmp": {route.BMP}, ".tif": {route.TIFF}, ".tiff": {route.TIFF}, ".emf": {route.EMF},
	".wmf": {route.WMF}, ".svg": {route.SVG}, ".heic": {route.HEIC}, ".webp": {route.WebP},
	".avif": {route.AVIF},
	".docx": {route.ZIP}, ".docm": {route.ZIP}, ".dotx": {route.ZIP}, ".dotm": {route.ZIP},
	".xlsx": {route.ZIP}, ".xlsm": {route.ZIP}, ".xltx": {route.ZIP}, ".xltm": {route.ZIP},
	".xlsb": {route.ZIP},
	".pptx": {route.ZIP}, ".pptm": {route.ZIP}, ".potx": {route.ZIP}, ".potm": {route.ZIP},
	".ppsx": {route.ZIP},
	".doc":  {route.OLE2}, ".dot": {route.OLE2}, ".xls": {route.OLE2}, ".xlt": {route.OLE2},
	".ppt": {route.OLE2}, ".pps": {route.OLE2}, ".pot": {route.OLE2}, ".msg": {route.OLE2},
	".odt": {route.ODF, route.ZIP}, ".ods": {route.ODF, route.ZIP}, ".odp": {route.ODF, route.ZIP},
	".odg": {route.ODF, route.ZIP},
	".rtf": {route.RTF}, ".wpd": {route.WordPerfect},
	".pst": {route.PST}, ".ost": {route.OST},
	".one": {route.OneStore}, ".onetoc2": {route.OneStore}, ".onepkg": {route.MSCF},
	".cab": {route.MSCF},
	".zip": {route.ZIP}, ".jar": {route.ZIP}, ".epub": {route.ZIP, route.ODF},
	".7z": {route.SevenZip}, ".gz": {route.Gzip}, ".tgz": {route.Gzip}, ".zst": {route.Zstd},
	".tar": {route.Tar},
	".wav": {route.WAV}, ".flac": {route.FLAC}, ".mp3": {route.MP3}, ".ogg": {route.Ogg},
	".m4a": {route.M4A}, ".mp4": {route.MP4}, ".mkv": {route.MKV}, ".webm": {route.WebM},
	".avi": {route.AVI},
	".exe": {route.PE}, ".dll": {route.PE}, ".scr": {route.PE},
}
