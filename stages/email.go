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
	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/cli"
)

// emailConfig is what an [emailStage] is built over: its Reader, the version of the tool behind
// it, and the memory bound the tool runs under, which a death by a signal's reason names.
type emailConfig struct {
	// Reader reads the messages and stores; it is required.
	Reader *email.Reader
	// Version names docproc-mail's version line, as [versions] gives it; it is required.
	Version string
	Memory  int64
}

// emailStage prepares a message or a mail store, route.Nested's parts, a .msg, an .eml, an mbox,
// or a .pst, through email's Reader, its media written into the part's directory. Each of
// docproc-mail's lines is a [dispatch.RecordOutput] of [dispatch.MediaMail], its JSON as email's
// record marshals it back to its line, in its order: the store line, named "store"; each folder,
// "folder <id>", its Meta naming its id and path; each message, "message <id>", or "message" for a
// .msg's or an .eml's, which has none, then its Markdown, when it has one, a
// [dispatch.MarkdownOutput] of the same name, the Meta of both naming its id, its folder's path,
// its subject, and its Message-ID, and an embedded one's the message it is embedded in; and a line
// of a kind email doesn't know, by its kind. A message of a conversation class, a Skype for
// Business conversation or missed conversation, is read as email's Message.Conversation views it:
// a record of email.Conversation, "conversation <id>", or "conversation" without one, of
// [dispatch.MediaMailConversation], not MediaMail, since it is a view of the line, then its
// transcript's Markdown, of the same name, the Meta of both its topic and "missed", "true" or
// "false", in place of the subject and Message-ID, an embedded one's too, since the class decides.
//
// A message's attachments and media come before its own outputs, in their order: docproc-mail
// writes a message's files before its line, so each is a part dispatched, every part found in it
// with it, before the message's record and Markdown are yielded, and the message's last output,
// its Markdown or, with none, its record, is the last of everything the message holds. The one
// media rule, [stash.item], turns each into its item: an attachment whose content docproc-mail
// wrote is a [dispatch.Found] part, named by its filename and typed by its stored MIME type, at
// "<message>, attachment <n>"; a medium, a data: image the Markdown links, at "<message>, media
// <n>", named by its media name. An embedded message whose content is no file, as a .msg's or an
// .eml's is, is prepared inline, as a message of its own, in its place among the attachments, its
// name the attachment's place. An attachment or medium with no content, by reference, listed by
// its metadata alone, or not written, is an unread child.
//
// docproc-mail's warnings are [dispatch.Warning]s, each with its tool's name, where the Reader
// yields them. A skipped line naming a message or an attachment is an unread child, failed, its
// reason the tool's, at "message <id>" or "message <id>, skipped attachment <id>"; one naming a
// folder or a folder's table is a warning, at the folder.
//
// A part with an After, a dispatch.Input.After, is read from the message after it, as
// email.Request's After says, the last output of each message of the read's own carrying its id
// as the store's cursor; a .msg's or an .eml's read, or an After that names no message, fails. A
// refusal internal/cli classes as encrypted ends the stage with dispatch.ErrEncrypted, and any
// other failure is classified once, by [partErr].
type emailStage struct {
	reader *email.Reader
	memory int64
	id     dispatch.Identity
}

// newEmailStage builds an emailStage over cfg, panicking naming Reader when it is nil, the
// wiring's mistake.
func newEmailStage(cfg emailConfig) *emailStage {
	if cfg.Reader == nil {
		panic("stages: emailConfig.Reader is nil")
	}
	return &emailStage{reader: cfg.Reader, memory: cfg.Memory, id: identity("email", cfg.Version, struct {
		Schema  string        `json:"schema"`
		Request email.Request `json:"request"`
	}{cli.Schema, email.Request{}})}
}

// Identity returns the stage's identity: "email", docproc-mail's version line, and its request's
// and schema's hash.
func (s *emailStage) Identity() dispatch.Identity { return s.id }

// Prepare reads the message or store at p.Path, as the type's doc says.
func (s *emailStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		media := filepath.Join(dir, "media")
		if err := os.Mkdir(media, 0o700); err != nil {
			yield(dispatch.Item{}, fmt.Errorf("email stage: %w", err))
			return
		}
		w := emailWalk{stash: stash{dir: dir}, folders: map[uint64][]string{}, yield: yield}
		for rec, err := range s.reader.Read(ctx, email.Request{Input: p.Path, Media: media, After: p.After}) {
			if err != nil {
				yield(dispatch.Item{}, partErr(ctx, fmt.Errorf("email stage: %w", err), s.memory))
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

// errStopped is an adapter walk's word that the caller's loop stopped: nothing more is yielded.
var errStopped = errors.New("stopped")

// emailWalk is one read's walk of its records: its stash, its folders' paths by id, and the
// loop's yield.
type emailWalk struct {
	stash   stash
	folders map[uint64][]string
	yield   func(dispatch.Item, error) bool
}

// item yields it, or returns err, or errStopped once the loop stops.
func (w *emailWalk) item(it dispatch.Item, err error) error {
	if err != nil {
		return err
	}
	if !w.yield(it, nil) {
		return errStopped
	}
	return nil
}

// output yields o, or returns err, record's error, or errStopped once the loop stops.
func (w *emailWalk) output(o dispatch.Output, err error) error {
	if err != nil {
		return err
	}
	return w.item(dispatch.OutputItem(o), nil)
}

// folder returns the path of the folder of node id folder, as read so far, "folder <id>" when it
// isn't known.
func (w *emailWalk) folder(folder uint64) string {
	if path, ok := w.folders[folder]; ok {
		return strings.Join(path, "/")
	}
	return "folder " + strconv.FormatUint(folder, 10)
}

// record yields rec's outputs, parts, and warnings.
func (w *emailWalk) record(rec email.Record) error {
	switch r := rec.(type) {
	case *email.Store:
		return w.output(record("store", r, dispatch.MediaMail, map[string]string{dispatch.MetaFormat: r.Format}))
	case *email.Folder:
		w.folders[r.ID] = r.Path
		id := strconv.FormatUint(r.ID, 10)
		return w.output(record("folder "+id, r, dispatch.MediaMail, map[string]string{dispatch.MetaID: id, dispatch.MetaPath: strings.Join(r.Path, "/")}))
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
		return w.item(dispatch.WarningItem(dispatch.Warning(*r)), nil)
	case *email.Skipped:
		return w.skipped(r)
	case *email.UnknownRecord:
		return w.output(dispatch.Output{Kind: dispatch.RecordOutput, Name: r.Kind, MediaType: dispatch.MediaMail, Value: r.Raw}, nil)
	}
	return fmt.Errorf("email stage: a record of type %T", rec)
}

// skipped yields a skipped line: an unread child for a message or an attachment, a warning at its
// folder for a folder or a folder's table.
func (w *emailWalk) skipped(r *email.Skipped) error {
	id := ""
	if r.ID != nil {
		id = strconv.FormatUint(*r.ID, 10)
	}
	switch r.Kind {
	case "message":
		where := "message " + id
		return w.item(unread(where, where, dispatch.Failed, r.Reason), nil)
	case "attachment":
		where := "message " + strconv.FormatUint(r.Message, 10) + ", skipped attachment " + id
		return w.item(unread(where, where, dispatch.Failed, r.Reason), nil)
	}
	what := r.Kind + " skipped: " + r.Reason
	if r.Kind == "folder" {
		what = "folder " + id + " skipped: " + r.Reason
	}
	return w.item(dispatch.WarningItem(dispatch.Warning{Tool: "docproc-mail", Where: w.folder(r.Folder), What: what}), nil)
}

// message yields m's attachments and media, then its record and Markdown, named name: each
// attachment's file found, each embedded message without one prepared inline below name, and each
// other an unread child; each medium's file found. parent is the name of the message m is embedded
// in, "" for a message of the read's own. Its own outputs come last, so a caller that has
// persisted them has had every part of m.
func (w *emailWalk) message(m *email.Message, name, parent string) error {
	if err := w.attachments(m.Attachments, m.Media, name); err != nil {
		return err
	}
	meta := map[string]string{dispatch.MetaSubject: m.Subject}
	if m.MessageID != "" {
		meta[dispatch.MetaMessageID] = m.MessageID
	}
	return w.own(m, dispatch.MediaMail, name, parent, m.ID, m.Folder, m.Markdown, meta)
}

// conversation yields c's attachments and media, then its record and its transcript's Markdown,
// named name, as message yields a message's; the Meta of both names its topic and whether it was
// missed.
func (w *emailWalk) conversation(c *email.Conversation, name, parent string) error {
	if err := w.attachments(c.Attachments, c.Media, name); err != nil {
		return err
	}
	meta := map[string]string{dispatch.MetaTopic: c.Topic, dispatch.MetaMissed: strconv.FormatBool(c.Missed)}
	return w.own(c, dispatch.MediaMailConversation, name, parent, c.ID, c.Folder, c.Markdown, meta)
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
		md := medium{path: m.Path, sum: m.SHA256, name: m.File, mime: m.MimeType, none: dispatch.Failed, why: "not written: no media directory"}
		if err := w.item(w.stash.item(md, name+", media "+strconv.Itoa(i+1))); err != nil {
			return err
		}
	}
	return nil
}

// own yields a message's own outputs, named name: v, its record of mediaType, then md, its
// Markdown, when it has one. meta gains its id, its folder's path, and parent, the message it is
// embedded in, when it has each. The last carries the message's id as the store's cursor, a
// dispatch.Item's After, when the message is one of the read's own, with an id.
func (w *emailWalk) own(v any, mediaType, name, parent string, id, folder uint64, md string, meta map[string]string) error {
	if id != 0 {
		meta[dispatch.MetaID] = strconv.FormatUint(id, 10)
	}
	if path, ok := w.folders[folder]; ok && folder != 0 {
		meta[dispatch.MetaFolder] = strings.Join(path, "/")
	}
	if parent != "" {
		meta[dispatch.MetaEmbeddedIn] = parent
	}
	var cursor uint64
	if parent == "" {
		cursor = id
	}
	rec, err := record(name, v, mediaType, meta)
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
func (w *emailWalk) last(o dispatch.Output, cursor uint64) error {
	item := dispatch.OutputItem(o)
	item.After = cursor
	return w.item(item, nil)
}

// attachment yields a, at where in the message named parent, as message says.
func (w *emailWalk) attachment(a *email.Attachment, where, parent string) error {
	if a.Message != nil && a.File == "" {
		// The class decides, nested or not: an embedded message of a conversation class is read
		// as one of the store's own is.
		if c, ok := a.Message.Conversation(); ok {
			return w.conversation(c, where, parent)
		}
		return w.message(a.Message, where, parent)
	}
	m := medium{path: a.Path, sum: a.SHA256, name: cmp.Or(a.Filename, a.File, where), mime: a.MimeType}
	m.none, m.why = noContent(a)
	return w.item(w.stash.item(m, where))
}

// noContent returns the outcome and reason of a, an attachment with no content file to find:
// no processor yet for one by reference or a .pst's embedded message listed by its metadata
// alone, and failed for one whose content docproc-mail didn't write.
func noContent(a *email.Attachment) (dispatch.Outcome, string) {
	switch {
	case a.Method == "embedded_message":
		return dispatch.NoProcessor, "embedded message, listed without its content"
	case strings.HasPrefix(a.Method, "by_reference"), a.Method == "by_web_reference":
		return dispatch.NoProcessor, "attached by reference (" + a.Method + "), not read"
	case a.File == "":
		return dispatch.Failed, "no content file"
	}
	return dispatch.Failed, "not written: no media directory"
}
