// Package onenote reads OneNote sections, .one files from desktop OneNote 2016 and later or
// downloaded from OneDrive, and .onepkg packages, and, for an operator, .onetoc2 notebooks and
// notebook directories, for a model through docproc-onenote: an input in, its records streamed out
// as typed Go values while docproc-onenote runs. It provides pre-inference preparation only; a
// layer above sends what it returns to a model. The package builds on every platform, on the
// standard library and this module's tool and internal/cli packages; the program it runs is a
// tool.Tool the caller built through the exec runner.
//
// A Reader is built at the composition root over a docproc-onenote Tool, OneNoteSpec declaring it.
// NewReader refuses one older than OneNoteMinimum, 0.1.0, its first schema "2" release, and one
// the container runner built, since docproc-onenote reads the input's path in place; the Tool's own
// construction refuses a missing one, or one whose --version doesn't name schema "2". The contract
// is docproc-onenote's README and context/layers.md's "docproc-onenote": its JSON Lines, schema
// "2", its media names, its warnings, its refusal reasons, and its exit codes.
//
// Reader.Read returns the sequence of one input's records. Ranging over it runs docproc-onenote
// once, through internal/cli's envelope reader, which checks that the first line is a schema "2"
// notebook line, and yields each line docproc-onenote writes as it writes it, decoded through
// encoding/json/v2 into the typed value of its kind, Notebook, SectionGroup, Section, Page, or
// Skipped, each time UTC: the notebook first, then its entries depth-first in its table of
// contents' order, each section group before the entries it holds and each section before its
// pages, then a skipped record for each section the read left out. A section input is a notebook,
// its one section, and its pages. A line of a kind the package doesn't know is an UnknownRecord,
// never an error. Each warning docproc-onenote writes to stderr is a Warning record among them,
// parsed into where and what.
//
// A page's text is its Markdown, docproc-onenote's: its rich text, lists, tables, math, note tags,
// and recognised handwriting. Its Media are its images, embedded files, and ink drawings, each
// naming its file by the envelope's media name rule; given a media directory in its Request,
// docproc-onenote writes each distinct content there once, and each Medium's Path names its file,
// its SHA256 the hash the name claims. A medium whose data is missing or invalid has no file. A
// page's InkAlternatives are the recognised ink words' other guesses, a label the Markdown leaves
// out. The package routes nothing: the caller hands each file onward.
//
// docproc-onenote's exit codes 1 to 4, and a death by a signal, are a *cli.ExitError, which matches
// its code's error through errors.Is, ErrNotWritten, ErrUsage (a fault in this package), ErrRefused
// (with docproc-onenote's reason), or ErrFailed, each internal/cli's shared error, and names a
// refusal's class by its Refusal method, through cli.RefusalClass: a password-protected section's
// reason begins "encrypted", dispatch.ErrEncrypted, and a table of contents refused under
// --file-only is dispatch.ErrReadElsewhere, its sections read as their own inputs. A read whose
// context ends returns an error matching the context's, as every Tool run does. A loop that stops
// early stops docproc-onenote.
//
// Known limits, docproc-onenote's, as its README states: a password-protected section is refused
// as an input, and skipped inside a notebook; a page is read at its latest revision only; merged
// table cells are read as their own; a section that fails inside a section group is lost without
// a skipped record or a warning; and skipped records come after the notebook's entries, not in
// their place.
//
// Its exported identifiers:
//
//   - [OneNoteSpec] declares docproc-onenote for the exec runner's constructor.
//   - [OneNoteMinimum] is the oldest docproc-onenote the Reader accepts, 0.1.0.
//   - [Config] is the Tool a Reader is built over.
//   - [Reader] reads OneNote sections, packages, and notebooks through docproc-onenote.
//   - [NewReader] builds a Reader over a Config.
//   - [Reader.Read] returns the sequence of one input's records.
//   - [Request] is one input's read: its path, the media directory docproc-onenote writes
//     images, embedded files, and ink into, if any, and whether a table of contents is refused.
//   - [ErrRequest] is the cause of a refused request, and of a container-built OneNote.
//   - [ErrNotWritten], [ErrUsage], [ErrRefused], and [ErrFailed] are docproc-onenote's exit codes
//     1 to 4, internal/cli's.
//   - [ErrSchema] is the cause of a first line that isn't a schema "2" notebook line.
//   - [Record] is one item of a read: [Notebook], [SectionGroup], [Section], [Page], [Skipped],
//     [Warning], or [UnknownRecord].
//   - [Medium] is one of a page's media, of its [MediumKind]: [ImageMedium],
//     [EmbeddedFileMedium], or [InkMedium]; its Path names its file in the media directory.
//   - [InkAlternative] is a recognised ink word's other guesses.
package onenote
