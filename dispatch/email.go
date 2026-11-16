package dispatch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/cli"
)

// EmailConfig is what an [EmailStage] is built over: the caller's Reader and the version of the
// tool behind it.
type EmailConfig struct {
	// Reader reads the messages and stores; it is required.
	Reader *email.Reader
	// Version names mail's version, as [Versions] gives it; it is required.
	Version string
}

// EmailStage prepares a message or a mail store, route.Nested's parts, a .msg, an .eml, an mbox, or
// a .pst, through email's Reader, its media written into the part's directory. Each of
// docproc-mail's records is a [RecordOutput], its JSON as email's record marshals it, in its
// order: the store line, named "store"; each folder, "folder <id>", its Meta naming its id and
// path; each message, "message <id>", or "message" for a .msg's or an .eml's, which has none, then
// its Markdown, when it has one, a [MarkdownOutput] of the same name, the Meta of both naming its id, its
// folder's path, its subject, and its Message-ID, and an embedded one's the message it is embedded
// in, and the record's Meta "markdown" saying "follows" when a Markdown follows it; each message of
// a conversation class, a Skype for Business conversation or missed conversation, as email's
// Message.Conversation views it, an email.Conversation record, "conversation <id>", or "conversation" without one, then its
// transcript's Markdown, of the same name, in the same order, with the same Meta less the subject
// and Message-ID and with its topic and "missed", "true" or "false", an embedded one's too, since
// the class decides; each skipped record, "skipped <kind>", its Meta naming its reason; and a
// record of a kind email doesn't know, by its kind. docproc-mail's warnings, when it gave any, are a
// last record, "warnings", a JSON array, each "<where>: <what>".
//
// A message's attachments and media come before its own outputs, in their order: docproc-mail
// writes a message's files before its line, so each is a part dispatched, every part found in it
// with it, before the message's record and Markdown are yielded, and the message's last output, its
// Markdown or, with none, its record, is the last of everything the message holds. Each attachment
// whose content docproc-mail wrote is a [Found] part, the dispatcher's to route and inspect again,
// named by its filename and typed by its stored MIME type, its Where naming its message and its
// place, "message <id>, attachment <n>"; each medium, a data: image the Markdown links, is one
// too, named by its media name, "message <id>, media <n>". An embedded message whose content is no
// file, as a .msg's or an .eml's is, docproc-mail giving no bytes of it, is prepared inline, as a message of its
// own, in its place among the attachments: its own attachments found below it, "message <id>,
// attachment <n>, attachment <m>", at any depth, then its record and Markdown, named by its place,
// "message <id>, attachment <n>". An attachment with neither, by reference, not read, or a .pst's
// embedded message listed by its metadata alone, is a record named by its place, its Meta naming
// why it has no content.
//
// A part with an After, an [Input.After], is read from the message after it, as email.Request's
// After says: the store, every folder, and the messages after it; a .msg's or an .eml's read, or
// an After that names no message, fails. A refusal internal/cli classes as encrypted ends the
// stage with [ErrEncrypted].
type EmailStage struct {
	reader *email.Reader
	id     Identity
}

// NewEmailStage builds an EmailStage over cfg, panicking naming Reader when it is nil, the caller's
// mistake, and refusing one with no Version.
func NewEmailStage(cfg EmailConfig) (*EmailStage, error) {
	if cfg.Reader == nil {
		panic("dispatch: EmailConfig.Reader is nil")
	}
	id, err := adapterIdentity("email", cfg.Version, struct {
		Schema string `json:"schema"`
	}{cli.Schema})
	if err != nil {
		return nil, err
	}
	return &EmailStage{reader: cfg.Reader, id: id}, nil
}

// Identity returns the stage's identity: "email", docproc-mail's version, and the schema's hash.
func (s *EmailStage) Identity() Identity { return s.id }

// Prepare reads the message or store at p.Path, as the type's doc says.
func (s *EmailStage) Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error] {
	return func(yield func(Item, error) bool) {
		media := filepath.Join(dir, "media")
		if err := os.Mkdir(media, 0o700); err != nil {
			yield(Item{}, fmt.Errorf("email stage: %w", err))
			return
		}
		w := emailWalk{stash: stash{dir: dir}, folders: map[uint64][]string{}, yield: yield}
		for rec, err := range s.reader.Read(ctx, email.Request{Input: p.Path, Media: media, After: p.After}) {
			if err != nil {
				yield(Item{}, emailError(err))
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

// emailError returns err, a read's, as the stage's: a refusal internal/cli classes as encrypted
// matches ErrEncrypted.
//
// Interim until slice 15: internal/cli can't name dispatch's classes, so the stage maps its
// cli.Refusal to them.
func emailError(err error) error {
	if exit, ok := errors.AsType[*cli.ExitError](err); ok && exit.Refusal() == cli.Encrypted {
		return fmt.Errorf("email stage: %w: %w", ErrEncrypted, err)
	}
	return err
}

// errStopped is an adapter walk's word that the caller's loop stopped: nothing more is yielded.
var errStopped = errors.New("stopped")

// emailWalk is one read's walk of its records: its stash, its folders' paths by id, its warnings
// so far, each "<where>: <what>", and the loop's yield.
type emailWalk struct {
	stash    stash
	folders  map[uint64][]string
	warnings []string
	yield    func(Item, error) bool
}

// output yields o, or returns err, record's error, or errStopped once the loop stops.
func (w *emailWalk) output(o Output, err error) error {
	if err != nil {
		return err
	}
	if !w.yield(OutputItem(o), nil) {
		return errStopped
	}
	return nil
}

// record yields rec's outputs and parts.
func (w *emailWalk) record(rec email.Record) error {
	switch r := rec.(type) {
	case *email.Store:
		return w.output(record("store", r, map[string]string{"format": r.Format}))
	case *email.Folder:
		w.folders[r.ID] = r.Path
		id := strconv.FormatUint(r.ID, 10)
		return w.output(record("folder "+id, r, map[string]string{"id": id, "path": strings.Join(r.Path, "/")}))
	case *email.Message:
		id := ""
		if r.ID != 0 {
			id = " " + strconv.FormatUint(r.ID, 10)
		}
		if c, ok := r.Conversation(); ok {
			return w.conversation(c, "conversation"+id, "")
		}
		return w.message(r, "message"+id, "")
	case *email.Warning:
		w.warnings = append(w.warnings, r.Where+": "+r.What)
		return nil
	case *email.Skipped:
		return w.output(record("skipped "+r.Kind, r, map[string]string{"reason": r.Reason}))
	case *email.UnknownRecord:
		return w.output(Output{Kind: RecordOutput, Name: r.Kind, MediaType: "application/json", Value: r.Raw}, nil)
	}
	return fmt.Errorf("email stage: a record of type %T", rec)
}

// message yields m's attachments and media, then its record and Markdown, named name: each
// attachment's file found, each embedded message without one prepared inline below name, and each
// other a record; each medium's file found. parent is the name of the message m is embedded in, "" for a message of the read's own.
// Its own outputs come last, so a caller that has persisted them has had every part of m.
func (w *emailWalk) message(m *email.Message, name, parent string) error {
	if err := w.attachments(m.Attachments, m.Media, name); err != nil {
		return err
	}
	meta := map[string]string{"subject": m.Subject}
	if m.MessageID != "" {
		meta["message_id"] = m.MessageID
	}
	return w.own(m, name, parent, m.ID, m.Folder, m.Markdown, meta)
}

// conversation yields c's attachments and media, then its record and its transcript's Markdown,
// named name, as message yields a message's; the Meta of both names its topic and whether it was
// missed.
func (w *emailWalk) conversation(c *email.Conversation, name, parent string) error {
	if err := w.attachments(c.Attachments, c.Media, name); err != nil {
		return err
	}
	meta := map[string]string{"topic": c.Topic, "missed": strconv.FormatBool(c.Missed)}
	return w.own(c, name, parent, c.ID, c.Folder, c.Markdown, meta)
}

// attachments yields each of a message's attachments, then each of its media, in their order, the
// message named name.
func (w *emailWalk) attachments(as []email.Attachment, media []email.Medium, name string) error {
	for i := range as {
		where := name + ", attachment " + strconv.Itoa(i+1)
		if err := w.attachment(&as[i], where, name); err != nil {
			return err
		}
	}
	for i, m := range media {
		if m.Path == "" {
			continue
		}
		f, ok, err := w.stash.find(m.Path, Found{Name: m.File, MIME: m.MimeType, Where: name + ", media " + strconv.Itoa(i+1)})
		if err != nil {
			return err
		}
		if ok && !w.yield(PartItem(f), nil) {
			return errStopped
		}
	}
	return nil
}

// own yields a message's own outputs, named name: v, its record, then md, its Markdown, when it
// has one. meta gains its id, its folder's path, and parent, the message it is embedded in, when
// it has each; the record's says "markdown" "follows" when a Markdown follows it, so a caller
// knows which output is the message's last. The last carries the message's id as the store's
// cursor, an [Item]'s After, when the message is one of the read's own, with an id.
func (w *emailWalk) own(v any, name, parent string, id, folder uint64, md string, meta map[string]string) error {
	if id != 0 {
		meta["id"] = strconv.FormatUint(id, 10)
	}
	if path, ok := w.folders[folder]; ok && folder != 0 {
		meta["folder"] = strings.Join(path, "/")
	}
	if parent != "" {
		meta["embedded_in"] = parent
	}
	var cursor uint64
	if parent == "" {
		cursor = id
	}
	recMeta := meta
	if md != "" {
		recMeta = maps.Clone(meta)
		recMeta["markdown"] = "follows"
	}
	rec, err := record(name, v, recMeta)
	if err != nil {
		return err
	}
	if md == "" {
		return w.last(rec, cursor)
	}
	if err := w.output(rec, nil); err != nil {
		return err
	}
	return w.last(markdown(name, md, meta), cursor)
}

// last yields o, a message's last output, with cursor as its After; errStopped once the loop
// stops.
func (w *emailWalk) last(o Output, cursor uint64) error {
	item := OutputItem(o)
	item.After = cursor
	if !w.yield(item, nil) {
		return errStopped
	}
	return nil
}

// attachment yields a, at where in the message named parent, as message says.
func (w *emailWalk) attachment(a *email.Attachment, where, parent string) error {
	if a.Path != "" {
		f, ok, err := w.stash.find(a.Path, Found{Name: cmp.Or(a.Filename, a.File), MIME: a.MimeType, Where: where})
		if err != nil {
			return err
		}
		if ok {
			if !w.yield(PartItem(f), nil) {
				return errStopped
			}
			return nil
		}
	}
	if a.Message != nil && a.File == "" {
		// The class decides, nested or not: an embedded message of a conversation class is read
		// as one of the store's own is.
		if c, ok := a.Message.Conversation(); ok {
			return w.conversation(c, where, parent)
		}
		return w.message(a.Message, where, parent)
	}
	return w.output(record(where, a, map[string]string{"reason": noContent(a), "filename": a.Filename}))
}

// noContent says, in a few words, why a, an attachment yielded as a record, has no content to find.
func noContent(a *email.Attachment) string {
	switch {
	case a.Method == "embedded_message":
		return "embedded message, listed without its content"
	case a.File == "":
		return "no content file"
	case a.Path == "":
		return "not written: no media directory"
	}
	return "not written"
}
