package dispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// Stage prepares the parts of one route. The dispatcher calls Prepare once for each part it
// prepares, never for content the index already holds, and recurses into every part the stage
// finds before taking the stage's next item.
type Stage interface {
	// Identity names the stage, the version of the tool behind it, and a hash of its
	// configuration: the index keys a part's outputs by its SHA-256 and this identity, so a new
	// tool version or a changed configuration prepares the content again. Stages that share a
	// name share their identity: [New] refuses two that don't.
	Identity() Identity
	// Prepare prepares p, the file at p.Path, writing whatever files it makes, the parts it
	// finds among them, into dir, a directory the dispatcher made for p alone and removes once
	// the caller is done with p. It yields each output it prepares, each warning, each flag it
	// raises about p, and each part it finds, in any order, as it has them; a loop that stops
	// early stops the stage. Each part it finds has a Where unique among p's found parts, unless
	// the stage implements [UntrustedWhere]. An error ends the stage: one matching
	// [ErrNoProcessor] says the stage has no processor for this kind of content, [ErrEncrypted]
	// that it can't read the content because it is encrypted, [ErrReadElsewhere] that the content
	// is read through its siblings, [ErrRefused] that the content can't be read at all, which is
	// recorded, and any other that the run failed, which isn't, so a later dispatch tries again.
	Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error]
}

// UntrustedWhere is implemented by a stage whose found parts' Where come from names the content
// holds, such as an archive's entries or a PDF's attachments, so two siblings may share one: the
// dispatcher keeps the set of a part's found parts' Where for such a stage alone, and gives a
// Where a sibling already has " (n)", n the smallest from 2 that no sibling's takes. The set is
// bounded by what the content lists, an archive's by inspect.Options.Entries.
type UntrustedWhere interface {
	Stage
	// UntrustedWhere marks the stage; it is never called.
	UntrustedWhere()
}

// Stages are the stages a caller registers, by the route whose parts each prepares. A route
// with no stage is reported [NoProcessor].
type Stages map[route.Route]Stage

// Identity is a stage's identity: its name, the version of the tool behind it, and a hash of
// its configuration, such as [HashConfig] returns. It is comparable, and part of every key the
// index records a stage's entry under; as JSON it is the stage an event and an entry name.
type Identity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Config  string `json:"config"`
}

// String returns the identity as name@version#config, for a reason or a log.
func (id Identity) String() string { return id.Name + "@" + id.Version + "#" + id.Config }

// HashConfig returns the lowercase hex SHA-256 of v's JSON, for a stage's [Identity.Config]:
// the same configuration gives the same hash on every run, a map's keys being sorted.
func HashConfig(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("dispatch: hash config: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ErrRequest is the cause of a refused request: a Pool's workers under 1, a [Config] the
// dispatcher can't take, or an [Input] that names no content or names it twice.
var ErrRequest = errors.New("invalid request")

// The errors a stage ends with to name its outcome; a stage wraps one with its reason, which
// becomes the event's. A stage's error matching none of them fails the part as the run's fault,
// unrecorded.
var (
	// ErrNoProcessor is a stage's word that it has no processor for this part's kind yet, such
	// as office given an ODF formula: the part's outcome is [NoProcessor].
	ErrNoProcessor = errors.New("no processor yet")
	// ErrEncrypted is a stage's word that it can't read the part because it is encrypted: the
	// part's outcome is [Encrypted].
	ErrEncrypted = errors.New("encrypted")
	// ErrReadElsewhere is a stage's word that the part's content is read as other parts, such as
	// a OneNote table of contents whose sections are read as their own: the part's outcome is
	// [ReadElsewhere], no failure.
	ErrReadElsewhere = errors.New("read elsewhere")
	// ErrRefused is a stage's word that nothing of the part's content can be read: the part is
	// [Failed], recorded, so a later dispatch doesn't read it again.
	ErrRefused = errors.New("refused")
	// ErrTooLarge is a content store's word that an output is past its size: the part is
	// [Failed], recorded. A provider maps its store's own error to it.
	ErrTooLarge = errors.New("too large for the content store")
)

// The errors a dispatch ends with that aren't a part's.
var (
	// ErrSHA256 is a content's SHA-256 that isn't the one claimed for it: an [Input]'s, which
	// stops the dispatch, or a [Found] part's, which fails that part as the run's fault.
	ErrSHA256 = errors.New("SHA-256 mismatch")
	// ErrClaimLost is a claim the index no longer holds for its token: a renewal, an append, or
	// a record refused, after the claim's lease ran out and another claimant took it over. It
	// ends the dispatch, and is transient: a later dispatch waits on the new claimant.
	ErrClaimLost = errors.New("claim lost")
)

// StoreError is a failure of the index or the content store, which stops the dispatch: the
// store's, never the input's, so a host stops its run on it rather than going on to the next
// input. Store is "index" or "content". A claim lost isn't one: it matches [ErrClaimLost].
type StoreError struct {
	Store string
	Err   error
}

func (e *StoreError) Error() string { return "dispatch: " + e.Store + ": " + e.Err.Error() }

// Unwrap returns the store's error.
func (e *StoreError) Unwrap() error { return e.Err }

func storeErr(store string, err error) error {
	if errors.Is(err, ErrClaimLost) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &StoreError{Store: store, Err: err}
}

// Item is one thing a stage yields: an output it prepared, a part it found, a warning, or a flag
// it raises about its own part. Exactly one of Output, Part, Warning, and Flag is set.
//
// A flag yielded before the part's first output is weighed with the part's others, under the
// dispatcher's inspect.Options, and a quarantine verdict stops the stage; one yielded later is
// recorded and reported on the part's own event, and never unwinds events already out.
//
// After is a store's cursor, set on a message's last output: the message's id, which [Input.After]
// takes to resume after it. It is opaque, compared by equality alone, never ordered.
type Item struct {
	Output  *Output
	Part    *Found
	Warning *Warning
	Flag    *inspect.Flag
	After   uint64
}

// OutputItem returns an Item holding o.
func OutputItem(o Output) Item { return Item{Output: &o} }

// PartItem returns an Item holding f.
func PartItem(f Found) Item { return Item{Part: &f} }

// WarningItem returns an Item holding w.
func WarningItem(w Warning) Item { return Item{Warning: &w} }

// FlagItem returns an Item holding f.
func FlagItem(f inspect.Flag) Item { return Item{Flag: &f} }

// Found is a part a stage found in the part it prepares: a file the stage wrote, usually in
// the directory Prepare was given, its name, its stored MIME type, if it has one, and where it
// was found, in the stage's words, such as "attachment 2" or "page 3, outline 1, image 2". The
// dispatcher hashes it, routes it by route.File, and dispatches it as a part of its own, then
// removes the file once the caller's loop body returns for its own event.
//
// SHA256, when the stage knows the content's SHA-256, lowercase hex, is its claim, such as a
// medium the envelope's media name rule names: the dispatcher checks it against the hash it takes
// anyway, and a mismatch, as a file a killed tool left short, fails the part as the run's fault,
// an error matching [ErrSHA256].
//
// A found part with no file and an Outcome is an unread child: an occurrence the stage names but
// can't read, such as an archive's link or encrypted entry, an entry past the bomb rule, a medium
// with no content, or a section a tool skipped. Its Outcome is [Quarantined], [Encrypted],
// [NoProcessor], or [Failed], with its Reason and its Flags, or [Limited], an entry the stage
// stopped writing at the bytes cap, as [Remaining] says, whose Reason the dispatcher gives; it has
// a name and a Where and no SHA-256 or size, and is reported and recorded as a child, so counts by
// outcome are true.
//
// Release serves a stage that holds content a later item names again, as a CLI that writes each
// distinct content once: the dispatcher calls it, once, after the part's own event, when the part
// is settled: the index holds the content's entry under its stage, the part prepared and recorded
// or seen, or its route has no stage, the part no processor yet, and SHA256 is the SHA-256 the
// dispatcher took of Path's file. The stage may then remove what it holds of the content, and,
// for a later item naming the same content, find it with Path "", no Outcome, and the same
// SHA256: the dispatcher dispatches it as the part it released was, of its size, kind, and route,
// whatever the later name, and sees it from the index, or, its route having no stage, reports it
// no processor yet again, with the flags it had, so no file is needed. A part the dispatcher
// didn't settle, as one failed, limited, quarantined, or a cycle, is never released, so the stage
// keeps its content for the later item, which is dispatched from its file again. A part found
// with no file, no Outcome, and a SHA256 this dispatch released none of fails.
type Found struct {
	Path    string
	Name    string
	MIME    string
	Where   string
	SHA256  string
	Release func()
	Outcome Outcome
	Reason  string
	Flags   []inspect.Flag
}

// OutputKind names what an output holds.
type OutputKind string

const (
	// TextOutput is plain text, such as a page's text layer.
	TextOutput OutputKind = "text"
	// MarkdownOutput is Markdown, such as an office document's or a OneNote page's.
	MarkdownOutput OutputKind = "markdown"
	// ImageOutput is a picture prepared for a model's vision, such as a PDF page's PNG.
	ImageOutput OutputKind = "image"
	// RecordOutput is a structured record, as JSON, such as a mail message or an office document's
	// model.
	RecordOutput OutputKind = "record"
	// AudioOutput is audio prepared for a model's audio input, such as a speech window's WAV.
	AudioOutput OutputKind = "audio"
)

// Output is one thing a stage prepared for a model: its kind, a name the stage gives it, such as
// "page 3", its media type, and either a file the stage wrote, at Path, or its bytes inline, in
// Value: text and Markdown as UTF-8, a record as its media type says. Meta holds whatever else the
// stage says of it, such as a page's number and count, under the keys [MetaPage] and the rest.
//
// SHA256 is its content's SHA-256, lowercase hex, which the dispatcher fills: the content store
// holds the content by it, and the index records the output with it. Path and Value are local and
// never marshal, so a recorded or replayed output carries its SHA256 alone, its content found in
// the content store.
type Output struct {
	Kind      OutputKind        `json:"kind"`
	Name      string            `json:"name,omitzero"`
	MediaType string            `json:"media_type,omitzero"`
	Meta      map[string]string `json:"meta,omitempty"`
	SHA256    string            `json:"sha256"`
	Path      string            `json:"-"`
	Value     []byte            `json:"-"`
}

// Part is one part of an input, the input itself among them. ID names its occurrence in the
// input, stable across runs: "/" for the input, and a found part's its parent's ID followed by its
// Where as one more segment, "%" and "/" percent-encoded. It has the name it was given, its size
// and SHA-256, lowercase hex, its [ContentType], what routing read of it, its flags, its parent's
// ID, "" for the input, its depth, 0 for the input, and where it was found, in its parent's
// stage's words.
//
// Path and After are local and never marshal: Path is its file, while its stage runs, and After
// the input's [Input.After], the message its stage resumes after, on the input's part alone; a
// stage that doesn't resume ignores it.
type Part struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Size        int64          `json:"size"`
	SHA256      string         `json:"sha256"`
	ContentType ContentType    `json:"content_type"`
	Flags       []inspect.Flag `json:"flags,omitempty"`
	Parent      string         `json:"parent,omitzero"`
	Depth       int            `json:"depth"`
	Where       string         `json:"where,omitzero"`
	Path        string         `json:"-"`
	After       uint64         `json:"-"`
}
