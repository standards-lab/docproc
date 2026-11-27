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

const emailVersion = "2"

type emailConfig struct {
	Reader  *email.Reader
	Version string
	Memory  int64
}

type emailStage struct {
	reader *email.Reader
	memory int64
	id     dispatch.Identity
}

func newEmailStage(cfg emailConfig) *emailStage {
	if cfg.Reader == nil {
		panic("stages: emailConfig.Reader is nil")
	}
	return &emailStage{reader: cfg.Reader, memory: cfg.Memory, id: identity("email", cfg.Version, struct {
		Stage   string        `json:"stage"`
		Schema  string        `json:"schema"`
		Request email.Request `json:"request"`
	}{emailVersion, cli.Schema, email.Request{}})}
}

func (s *emailStage) Identity() dispatch.Identity { return s.id }

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

var errStopped = errors.New("stopped")

type emailWalk struct {
	stash   stash
	folders map[uint64][]string
	yield   func(dispatch.Item, error) bool
}

func (w *emailWalk) item(it dispatch.Item, err error) error {
	if err != nil {
		return err
	}
	if !w.yield(it, nil) {
		return errStopped
	}
	return nil
}

func (w *emailWalk) output(o dispatch.Output, err error) error {
	if err != nil {
		return err
	}
	return w.item(dispatch.OutputItem(o), nil)
}

func (w *emailWalk) folder(folder uint64) string {
	if path, ok := w.folders[folder]; ok {
		return strings.Join(path, "/")
	}
	return "folder " + strconv.FormatUint(folder, 10)
}

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

func (w *emailWalk) message(m *email.Message, name, parent string) error {
	if err := w.attachments(m.Attachments, m.Media, name); err != nil {
		return err
	}
	meta := map[string]string{dispatch.MetaSubject: m.Subject}
	if m.MessageID != "" {
		meta[dispatch.MetaMessageID] = m.MessageID
	}
	if m.InReplyTo != "" {
		meta[dispatch.MetaInReplyTo] = m.InReplyTo
	}
	if m.References != "" {
		meta[dispatch.MetaReferences] = m.References
	}
	return w.own(m, dispatch.MediaMail, name, parent, m.ID, m.Folder, m.Markdown, meta)
}

func (w *emailWalk) conversation(c *email.Conversation, name, parent string) error {
	if err := w.attachments(c.Attachments, c.Media, name); err != nil {
		return err
	}
	meta := map[string]string{dispatch.MetaTopic: c.Topic, dispatch.MetaMissed: strconv.FormatBool(c.Missed)}
	return w.own(c, dispatch.MediaMailConversation, name, parent, c.ID, c.Folder, c.Markdown, meta)
}

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

func (w *emailWalk) last(o dispatch.Output, cursor uint64) error {
	item := dispatch.OutputItem(o)
	item.After = cursor
	return w.item(item, nil)
}

func (w *emailWalk) attachment(a *email.Attachment, where, parent string) error {
	if a.Message != nil && a.File == "" {
		if c, ok := a.Message.Conversation(); ok {
			return w.conversation(c, where, parent)
		}
		return w.message(a.Message, where, parent)
	}
	m := medium{path: a.Path, sum: a.SHA256, name: cmp.Or(a.Filename, a.File, where), mime: a.MimeType}
	m.none, m.why = noContent(a)
	return w.item(w.stash.item(m, where))
}

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
