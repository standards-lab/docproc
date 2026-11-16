//go:build linux

package email_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

// TestNewReader: a stand-in at the minimum, or a build past it, is accepted; an older one fails at
// construction, whether the Tool's Spec or NewReader refuses it, and one whose --version names
// another schema fails as the Tool is built; one the container runner built is refused with
// ErrRequest; a nil Tool panics, naming the field.
func TestNewReader(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.1.0", "0.1.0+0b6b5a4", "0.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			mail := installStandin(t, version)
			tl, err := tool.NewExec(t.Context(), mail.spec())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := email.NewReader(email.Config{Mail: tl}); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("older", func(t *testing.T) {
		t.Parallel()
		mail := installStandin(t, "0.0.9")
		_, err := tool.NewExec(t.Context(), mail.spec())
		if !errors.Is(err, tool.ErrTooOld) {
			t.Fatalf("spec's own minimum: got %v, want ErrTooOld", err)
		}
		spec := mail.spec()
		spec.Minimum = "0.0.1"
		tl, err := tool.NewExec(t.Context(), spec)
		if err != nil {
			t.Fatal(err)
		}
		_, err = email.NewReader(email.Config{Mail: tl})
		var verr *tool.VersionError
		if !errors.Is(err, tool.ErrTooOld) || !errors.As(err, &verr) || verr.Found != "0.0.9" || verr.Minimum != email.MailMinimum {
			t.Fatalf("NewReader: got %v, want a VersionError for 0.0.9 under %s", err, email.MailMinimum)
		}
	})
	t.Run("schema 1", func(t *testing.T) {
		t.Parallel()
		mail := installStandin(t, "0.1.0")
		mail.set(func(s *mailSettings) { s.Schema = "1" })
		if _, err := tool.NewExec(t.Context(), mail.spec()); !errors.Is(err, tool.ErrNoVersion) {
			t.Errorf("NewExec over a schema 1 docproc-mail: got %v, want ErrNoVersion", err)
		}
	})
	t.Run("container", func(t *testing.T) {
		t.Parallel()
		mail := installStandin(t, "0.1.0")
		docker := tooltest.Link(t, t.TempDir(), "docker", mail.settings)
		image := tool.Image{Ref: "example.invalid/mail:1@sha256:" + strings.Repeat("0", 64)}
		boxed, err := tool.NewContainer(t.Context(), docker, image, mail.spec())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := email.NewReader(email.Config{Mail: boxed}); !errors.Is(err, email.ErrRequest) {
			t.Errorf("NewReader over a container's docproc-mail: got %v, want ErrRequest", err)
		}
	})
	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if r := recover(); r != "email: Config.Mail is nil" {
				t.Errorf("recovered %v, want the panic naming Config.Mail", r)
			}
		}()
		_, _ = email.NewReader(email.Config{})
		t.Error("NewReader returned")
	})
}

// fullLines are a .pst's lines with every field of schema "2" set, an unknown kind among them.
var fullLines = `{"line":"store","schema":"2","format":"pst","encoding":"unicode","password":true,"display_name":"Personal Folders"}
{"line":"folder","id":32802,"path":["Top"]}
{"line":"folder","id":32898,"parent":32802,"path":["Top","Inbox"],"container_class":"IPF.Note"}
{"line":"message","id":2097188,"folder":32898,"class":"IPM.Note","subject":"Notes","sender":{"name":"Ada","address":"/O=EX/CN=ADA","address_type":"EX","smtp_address":"ada@example.org"},"to":[{"name":"Charles","address":"/O=EX/CN=CHARLES","address_type":"EX","smtp_address":"charles@example.org"},{"address":"engine@example.org"}],"cc":[{"address":"mary@example.org"}],"bcc":[{"address":"x@example.org"}],"submit_time":"2026-01-12T14:15:00Z","delivery_time":"2026-01-12T14:16:30Z","creation_time":"2026-01-12T14:17:00Z","modification_time":"2026-01-12T14:18:00Z","importance":"high","sensitivity":"private","transport_headers":"Received: x\r\n","message_id":"<b@example.org>","in_reply_to":"<a@example.org>","references":"<a@example.org>","conversation_topic":"Notes","conversation_index":"0101cf32","markdown":"plain ![](` + mediaPNG + `)","attachments":[{"file":"` + mediaPDF + `","filename":"a.pdf","mime_type":"application/pdf","content_id":"part1","inline":true,"method":"by_value","size":0},{"filename":"Fwd","inline":false,"method":"embedded_message","message":{"class":"IPM.Note","subject":"Inner","attachments":[{"filename":"deep.txt","inline":false,"method":"by_value","size":4}]}}],"media":[{"file":"` + mediaPNG + `","mime_type":"image/png"}]}
{"line":"skipped","kind":"attachment","folder":32898,"message":2097188,"id":0,"reason":"broken"}
{"line":"skipped","kind":"contents","folder":32802,"reason":"unreadable table"}
{"line":"calendar","when":"later"}
`

// mediaName returns the media name of content stored with ext, an extension with its dot or "".
func mediaName(content, ext string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:]) + ext
}

// The media names fullLines gives its attachment and its data: image.
var (
	mediaPDF = mediaName("a pdf", ".pdf")
	mediaPNG = mediaName("a png", ".png")
)

// TestRead: the stand-in's lines are yielded in its order, each decoded into its typed record,
// every field of schema "2" set; a line of an unknown kind is kept with its JSON. docproc-mail
// runs once, on the input made absolute, with no media directory, and the files the lines name
// have their SHA-256 and no Path.
func TestRead(t *testing.T) {
	r, mail := standinReader(t)
	mail.stdout(fullLines)
	t.Chdir(t.TempDir())
	seq := r.Read(t.Context(), email.Request{Input: "-store.pst"})
	if got := mail.runs(); len(got) != 0 {
		t.Fatalf("runs %q before the sequence was ranged over", got)
	}
	recs, err := collect(seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 7 {
		t.Fatalf("%d records, want 7: %v", len(recs), recs)
	}
	if got, want := recs[0], (&email.Store{Schema: "2", Format: "pst", Encoding: "unicode", Password: true, DisplayName: "Personal Folders"}); !reflect.DeepEqual(got, want) {
		t.Errorf("store %+v, want %+v", got, want)
	}
	if got, want := recs[2], (&email.Folder{ID: 32898, Parent: 32802, Path: []string{"Top", "Inbox"}, ContainerClass: "IPF.Note"}); !reflect.DeepEqual(got, want) {
		t.Errorf("folder %+v, want %+v", got, want)
	}
	at := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	zero, four := int64(0), int64(4)
	want := &email.Message{
		ID: 2097188, Folder: 32898, Class: "IPM.Note", Subject: "Notes",
		Sender: email.Address{Name: "Ada", Address: "/O=EX/CN=ADA", AddressType: "EX", SMTPAddress: "ada@example.org"},
		To: []email.Address{
			{Name: "Charles", Address: "/O=EX/CN=CHARLES", AddressType: "EX", SMTPAddress: "charles@example.org"},
			{Address: "engine@example.org"},
		},
		CC:               []email.Address{{Address: "mary@example.org"}},
		BCC:              []email.Address{{Address: "x@example.org"}},
		SubmitTime:       at("2026-01-12T14:15:00Z"),
		DeliveryTime:     at("2026-01-12T14:16:30Z"),
		CreationTime:     at("2026-01-12T14:17:00Z"),
		ModificationTime: at("2026-01-12T14:18:00Z"),
		Importance:       "high", Sensitivity: "private",
		TransportHeaders: "Received: x\r\n",
		MessageID:        "<b@example.org>", InReplyTo: "<a@example.org>", References: "<a@example.org>",
		ConversationTopic: "Notes", ConversationIndex: "0101cf32",
		Markdown: "plain ![](" + mediaPNG + ")",
		Attachments: []email.Attachment{
			{File: mediaPDF, Filename: "a.pdf", MimeType: "application/pdf", ContentID: "part1", Inline: true, Method: "by_value", Size: &zero, SHA256: mediaPDF[:64]},
			{Filename: "Fwd", Method: "embedded_message", Message: &email.Message{
				Class: "IPM.Note", Subject: "Inner",
				Attachments: []email.Attachment{{Filename: "deep.txt", Method: "by_value", Size: &four}},
			}},
		},
		Media: []email.Medium{{File: mediaPNG, MimeType: "image/png", SHA256: mediaPNG[:64]}},
	}
	if got := recs[3]; !reflect.DeepEqual(got, want) {
		t.Errorf("message %+v\nwant %+v", got, want)
	}
	id := uint64(0)
	if got, want := recs[4], (&email.Skipped{Kind: "attachment", Folder: 32898, Message: 2097188, ID: &id, Reason: "broken"}); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped %+v, want %+v", got, want)
	}
	if got, want := recs[5], (&email.Skipped{Kind: "contents", Folder: 32802, Reason: "unreadable table"}); !reflect.DeepEqual(got, want) {
		t.Errorf("skipped table %+v, want %+v", got, want)
	}
	unknown, ok := recs[6].(*email.UnknownRecord)
	if !ok || unknown.Kind != "calendar" || string(unknown.Raw) != `{"line":"calendar","when":"later"}` {
		t.Errorf("unknown record %#v", recs[6])
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mail.runs(), [][]string{{filepath.Join(dir, "-store.pst")}}; !reflect.DeepEqual(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}
}

// TestReadMedia: a Request's media directory, made absolute, reaches docproc-mail as --media, and
// each attachment's and medium's Path is its file there, a nested message's too, the file
// docproc-mail wrote; a file name the media name rule can't have made ends the read with an error.
func TestReadMedia(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	nested := mediaName("nested", ".txt")
	mail.set(func(s *mailSettings) { s.Media = []string{mediaPDF, mediaPNG, nested} })
	mail.stdout(`{"line":"store","schema":"2","format":"eml"}
{"line":"message","attachments":[{"file":"` + mediaPDF + `","method":"by_value"},{"method":"embedded_message","message":{"attachments":[{"file":"` + nested + `","method":"by_value"}]}}],"media":[{"file":"` + mediaPNG + `"}]}
`)
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	input := filepath.Join(dir, "in.eml")
	recs, err := collect(r.Read(t.Context(), email.Request{Input: input, Media: media}))
	if err != nil {
		t.Fatal(err)
	}
	m := recs[1].(*email.Message)
	paths := map[string]string{
		mediaPDF: m.Attachments[0].Path,
		nested:   m.Attachments[1].Message.Attachments[0].Path,
		mediaPNG: m.Media[0].Path,
	}
	for name, path := range paths {
		if path != filepath.Join(media, name) {
			t.Errorf("%s: path %q, want it in %s", name, path, media)
			continue
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != name {
			t.Errorf("%s: %q, %v; want the file docproc-mail wrote", name, b, err)
		}
	}
	if got, want := mail.runs(), [][]string{{"--media", media, input}}; !reflect.DeepEqual(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}

	for _, bad := range []string{"../escape.pdf", strings.ToUpper(mediaPDF), mediaPDF[:64] + ".toolongext"} {
		mail.stdout(`{"line":"store","schema":"2","format":"eml"}
{"line":"message","attachments":[{"file":"` + bad + `","method":"by_value"}]}
`)
		recs, err := collect(r.Read(t.Context(), email.Request{Input: input, Media: media}))
		if err == nil || len(recs) != 1 {
			t.Errorf("file %q: %d records, %v; want the store and an error", bad, len(recs), err)
		}
	}
}

// TestReadAfter: a Request's After reaches docproc-mail as --after and its decimal id, after the
// media directory and before the input; a zero After passes none. An After past docproc-mail's
// range is refused before anything runs, with ErrRequest.
func TestReadAfter(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	dir := t.TempDir()
	input, media := filepath.Join(dir, "store.pst"), filepath.Join(dir, "m")
	for _, req := range []email.Request{
		{Input: input, Media: media, After: 2097188},
		{Input: input, After: 1},
		{Input: input, After: math.MaxUint32},
		{Input: input},
	} {
		if _, err := collect(r.Read(t.Context(), req)); err != nil {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	want := [][]string{
		{"--media", media, "--after", "2097188", input},
		{"--after", "1", input},
		{"--after", "4294967295", input},
		{input},
	}
	if got := mail.runs(); !reflect.DeepEqual(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}
	_, err := collect(r.Read(t.Context(), email.Request{Input: input, After: math.MaxUint32 + 1}))
	if !errors.Is(err, email.ErrRequest) {
		t.Errorf("after past docproc-mail's range: %v, want ErrRequest", err)
	}
	if got := len(mail.runs()); got != len(want) {
		t.Errorf("%d runs, want %d: the refused request ran", got, len(want))
	}
}

// TestReadMIMESkipped: an .eml's and an mbox's skipped records decode as docproc-mail writes them:
// an .eml's skipped attachment names no message, an mbox's names its message's ordinal, and an
// mbox's skipped message has its ordinal for its id.
func TestReadMIMESkipped(t *testing.T) {
	t.Parallel()
	one, two, three := uint64(1), uint64(2), uint64(3)
	cases := []struct {
		name, lines string
		want        []email.Record
	}{
		{"eml", `{"line":"store","schema":"2","format":"eml"}
{"line":"message","subject":"Notes"}
{"line":"skipped","kind":"attachment","id":1,"reason":"broken"}
`, []email.Record{
			&email.Store{Schema: "2", Format: "eml"},
			&email.Message{Subject: "Notes"},
			&email.Skipped{Kind: "attachment", ID: &one, Reason: "broken"},
		}},
		{"mbox", `{"line":"store","schema":"2","format":"mbox"}
{"line":"message","id":1,"subject":"Notes"}
{"line":"skipped","kind":"attachment","message":1,"id":2,"reason":"broken"}
{"line":"skipped","kind":"message","id":3,"reason":"it has no header block with a From or Date field"}
`, []email.Record{
			&email.Store{Schema: "2", Format: "mbox"},
			&email.Message{ID: 1, Subject: "Notes"},
			&email.Skipped{Kind: "attachment", Message: 1, ID: &two, Reason: "broken"},
			&email.Skipped{Kind: "message", ID: &three, Reason: "it has no header block with a From or Date field"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, mail := standinReader(t)
			mail.stdout(c.lines)
			recs, err := collect(r.Read(t.Context(), email.Request{Input: "mail." + c.name}))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(recs, c.want) {
				t.Errorf("records %+v\nwant %+v", recs, c.want)
			}
		})
	}
}

// TestReadWarnings: a read whose stderr holds warnings, with records skipped, succeeds; each
// warning line is one Warning record, docproc-mail's name its Tool, in order. A where that names a
// folder names it by its path, its display names joined by "/", the warning coming after the
// folder's line though docproc-mail wrote it first; one whose folder has no line keeps its id, as
// does any other where.
func TestReadWarnings(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	mail.stdout(`{"line":"store","schema":"2","format":"pst"}
{"line":"folder","id":32802,"path":["Top of Personal Folders"]}
{"line":"folder","id":32898,"parent":32802,"path":["Top of Personal Folders","Contacts"]}
{"line":"skipped","kind":"message","folder":32898,"id":2097188,"reason":"Missing Sub-Node Tree on message"}
`)
	mail.stderr("docproc-mail: warning: folder 32898, message 2097188: skipped: Missing Sub-Node Tree on message\n" +
		"docproc-mail: warning: folder 32802: an odd folder\n" +
		"docproc-mail: warning: folder 40000, contents table: skipped: unreadable\n" +
		"docproc-mail: warning: message, attachment 2: an embedded message\n")
	recs, err := collect(r.Read(t.Context(), email.Request{Input: "store.pst"}))
	if err != nil {
		t.Fatal(err)
	}
	var warnings []email.Warning
	folderAt, firstAt := -1, -1
	for i, rec := range recs {
		if f, ok := rec.(*email.Folder); ok && f.ID == 32898 {
			folderAt = i
		}
		if w, ok := rec.(*email.Warning); ok {
			if firstAt < 0 {
				firstAt = i
			}
			warnings = append(warnings, *w)
		}
	}
	if firstAt < folderAt {
		t.Errorf("the first warning is record %d, before its folder's, %d", firstAt, folderAt)
	}
	if n := len(recs) - len(warnings); n != 4 {
		t.Errorf("%d records besides the warnings, want 4", n)
	}
	want := []email.Warning{
		{Tool: "docproc-mail", Where: "folder Top of Personal Folders/Contacts, message 2097188", What: "skipped: Missing Sub-Node Tree on message"},
		{Tool: "docproc-mail", Where: "folder Top of Personal Folders", What: "an odd folder"},
		{Tool: "docproc-mail", Where: "folder 40000, contents table", What: "skipped: unreadable"},
		{Tool: "docproc-mail", Where: "message, attachment 2", What: "an embedded message"},
	}
	if !slices.Equal(warnings, want) {
		t.Errorf("warnings %+v\nwant %+v", warnings, want)
	}
}

// TestReadSenderFromHeaders: a sender whose line leaves its SMTP address out takes the address of
// its transport headers' From field (design ML-2): an Exchange sender's, a folded field's, one
// after a line that isn't a field, the first of a list, and a nested message's. A sender with its
// own SMTP address keeps it, and one whose headers have no From, or one that doesn't parse, or
// whose From comes after the header block's end, has none.
func TestReadSenderFromHeaders(t *testing.T) {
	t.Parallel()
	ex := `"sender":{"name":"Ada","address":"/O=EX/CN=ADA","address_type":"EX"}`
	cases := []struct {
		name, message, want string
	}{
		{"exchange", `{"line":"message",` + ex + `,"transport_headers":"Received: x\r\nFrom: Ada Lovelace <ada@example.org>\r\nTo: b@example.org\r\n"}`, "ada@example.org"},
		{"case and fold", `{"line":"message",` + ex + `,"transport_headers":"FROM:\r\n \"Lovelace, Ada\"\r\n\t<ada@example.org>\r\nSubject: s\r\n"}`, "ada@example.org"},
		{"not a field first", `{"line":"message",` + ex + `,"transport_headers":"Microsoft Mail Internet Headers Version 2.0\r\nFrom: ada@example.org\r\n"}`, "ada@example.org"},
		{"a list", `{"line":"message",` + ex + `,"transport_headers":"From: ada@example.org, charles@example.org\r\n"}`, "ada@example.org"},
		{"its own", `{"line":"message","sender":{"address":"/O=EX/CN=ADA","address_type":"EX","smtp_address":"ada@corp.example"},"transport_headers":"From: ada@example.org\r\n"}`, "ada@corp.example"},
		{"no from", `{"line":"message",` + ex + `,"transport_headers":"Received: x\r\nSender: ada@example.org\r\n"}`, ""},
		{"unparsed", `{"line":"message",` + ex + `,"transport_headers":"From: not an address\r\n"}`, ""},
		{"after the block", `{"line":"message",` + ex + `,"transport_headers":"Received: x\r\n\r\nFrom: ada@example.org\r\n"}`, ""},
		{"no headers", `{"line":"message",` + ex + `}`, ""},
		{"nested", `{"line":"message","attachments":[{"method":"embedded_message","message":{` + ex + `,"transport_headers":"From: <ada@example.org>\r\n"}}]}`, "ada@example.org"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, mail := standinReader(t)
			mail.stdout(`{"line":"store","schema":"2","format":"msg"}` + "\n" + c.message + "\n")
			recs, err := collect(r.Read(t.Context(), email.Request{Input: "in.msg"}))
			if err != nil {
				t.Fatal(err)
			}
			m := recs[1].(*email.Message)
			if len(m.Attachments) > 0 {
				m = m.Attachments[0].Message
			}
			if m.Sender.SMTPAddress != c.want {
				t.Errorf("sender's SMTP address %q, want %q", m.Sender.SMTPAddress, c.want)
			}
		})
	}
}

// TestReadExit: exit codes 1 to 4 are a *cli.ExitError matching the code's error and no other,
// carrying docproc-mail's reason and unwrapping to the Tool's *tool.ExitError; a warning before
// the error is still yielded, as is a usage error's usage, any line but the error's a warning. A refusal whose reason begins "encrypted" is classed Encrypted, any
// other Unclassed.
func TestReadExit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code    int
		want    error
		stderr  string
		msg     string
		refusal cli.Refusal
	}{
		{1, email.ErrNotWritten, "docproc-mail: error: writing stdout: Broken pipe\n", "writing stdout: Broken pipe", cli.Unclassed},
		{2, email.ErrUsage, "docproc-mail: error: unknown option --bogus\nusage: docproc-mail [--media <dir>] [--after <id>] <input>\n", "unknown option --bogus", cli.Unclassed},
		{3, email.ErrRefused, "docproc-mail: error: an .ost, the offline cache of a server mailbox; export it from Outlook as a .pst\n", "an .ost, the offline cache of a server mailbox; export it from Outlook as a .pst", cli.Unclassed},
		{3, email.ErrRefused, "docproc-mail: error: encrypted message; decrypt it first\n", "encrypted message; decrypt it first", cli.Encrypted},
		{4, email.ErrFailed, "docproc-mail: warning: store: odd\ndocproc-mail: error: internal\n", "internal", cli.Unclassed},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.code)+" "+c.msg, func(t *testing.T) {
			t.Parallel()
			r, mail := standinReader(t)
			mail.stdout("")
			mail.stderr(c.stderr)
			mail.exit(c.code)
			recs, err := collect(r.Read(t.Context(), email.Request{Input: "in.pst"}))
			exit, ok := errors.AsType[*cli.ExitError](err)
			if !ok || !errors.Is(err, c.want) {
				t.Fatalf("got %v, want a *cli.ExitError matching %v", err, c.want)
			}
			if _, ok := errors.AsType[*tool.ExitError](err); !ok {
				t.Errorf("%v doesn't unwrap to a *tool.ExitError", err)
			}
			for _, other := range []error{email.ErrNotWritten, email.ErrUsage, email.ErrRefused, email.ErrFailed} {
				if other != c.want && errors.Is(err, other) {
					t.Errorf("matches %v too", other)
				}
			}
			if exit.Code != c.code || exit.Message != c.msg || exit.Refusal() != c.refusal {
				t.Errorf("code %d, message %q, refusal %v; want %d, %q, %v", exit.Code, exit.Message, exit.Refusal(), c.code, c.msg, c.refusal)
			}
			for _, rec := range recs {
				if _, ok := rec.(*email.Warning); !ok {
					t.Errorf("record %+v, want warnings alone", rec)
				}
			}
			if c.code == 4 && (len(recs) != 1 || *recs[0].(*email.Warning) != email.Warning{Tool: "docproc-mail", Where: "store", What: "odd"}) {
				t.Errorf("records %+v, want the warning", recs)
			}
		})
	}
}

// TestReadSchema: a store line of another schema, or of none, or a first line that isn't a store
// line, ends the read with ErrSchema, and nothing is yielded.
func TestReadSchema(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"schema 1":  `{"line":"store","schema":"1","format":"pst"}` + "\n" + `{"line":"folder","id":1}` + "\n",
		"none":      `{"line":"store","format":"pst"}` + "\n",
		"not store": `{"line":"folder","id":1}` + "\n",
		"empty":     "",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, mail := standinReader(t)
			mail.stdout(stdout)
			recs, err := collect(r.Read(t.Context(), email.Request{Input: "in.pst"}))
			if !errors.Is(err, email.ErrSchema) {
				t.Fatalf("got %v, want ErrSchema", err)
			}
			if len(recs) != 0 {
				t.Errorf("records %v, want none", recs)
			}
		})
	}
}

// TestReadMalformed: output that isn't docproc-mail's lines is an error, whatever was yielded
// before it.
func TestReadMalformed(t *testing.T) {
	t.Parallel()
	store := `{"line":"store","schema":"2"}` + "\n"
	cases := map[string]string{
		"no line":    store + `{"id":1}` + "\n",
		"not JSON":   store + "not JSON\n",
		"cut short":  store + `{"line":"folder",`,
		"wrong type": store + `{"line":"folder","id":"x"}` + "\n",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, mail := standinReader(t)
			mail.stdout(stdout)
			recs, err := collect(r.Read(t.Context(), email.Request{Input: "in.pst"}))
			if err == nil {
				t.Fatal("want an error")
			}
			if len(recs) != 1 {
				t.Errorf("%d records, want the store alone", len(recs))
			}
		})
	}
}

// TestReadCancel: records are yielded while docproc-mail runs, and a read whose context is
// cancelled while it runs stops it and ends with an error matching context.Canceled, never an
// ExitError, well before docproc-mail would have exited.
func TestReadCancel(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	mail.sleep()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	start := time.Now()
	var recs []email.Record
	var err error
	for rec, rerr := range r.Read(ctx, email.Request{Input: "in.msg"}) {
		if rerr != nil {
			err = rerr
			break
		}
		recs = append(recs, rec)
		if len(recs) == 2 {
			cancel()
		}
	}
	if _, ok := errors.AsType[*cli.ExitError](err); !errors.Is(err, context.Canceled) || ok {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if len(recs) != 2 {
		t.Errorf("%d records before the cancel, want 2", len(recs))
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("took %v", d)
	}
}

// TestReadDeadline: a read whose deadline passes while docproc-mail runs ends with an error
// matching context.DeadlineExceeded.
func TestReadDeadline(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	mail.sleep()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	recs, err := collect(r.Read(ctx, email.Request{Input: "in.msg"}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if len(recs) != 2 {
		t.Errorf("%d records, want 2", len(recs))
	}
}

// TestReadStop: a loop that stops early stops docproc-mail, without waiting for it to exit, and
// nothing more is yielded.
func TestReadStop(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	mail.sleep()
	start := time.Now()
	n := 0
	for _, err := range r.Read(t.Context(), email.Request{Input: "in.msg"}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		break
	}
	if n != 1 {
		t.Errorf("%d records", n)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("took %v", d)
	}
}

// TestReadRefused: Read refuses an invalid request with an error matching ErrRequest before
// anything runs: an empty Input, and, from a working directory that has been removed, a relative
// Input or Media, which can't be made absolute. A cancelled context runs nothing either. An
// absolute Input with no Media is still read from there.
func TestReadRefused(t *testing.T) {
	r, mail := standinReader(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(t.TempDir(), "in.msg")
	cases := map[string]email.Request{
		"no input":       {Media: "m"},
		"relative input": {Input: "in.msg"},
		"relative media": {Input: abs, Media: "m"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			recs, err := collect(r.Read(t.Context(), req))
			if !errors.Is(err, email.ErrRequest) || len(recs) != 0 {
				t.Errorf("got %d records, %v; want ErrRequest and none", len(recs), err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := collect(r.Read(ctx, email.Request{Input: abs})); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: got %v", err)
	}
	if got := mail.runs(); len(got) != 0 {
		t.Errorf("runs %q, want none", got)
	}
	if _, err := collect(r.Read(t.Context(), email.Request{Input: abs})); err != nil {
		t.Errorf("absolute input: %v", err)
	}
}

// TestReadUTC: a time docproc-mail writes with an offset is decoded as the same instant in UTC.
func TestReadUTC(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	mail.stdout(`{"line":"store","schema":"2","format":"msg"}
{"line":"message","subject":"Offset","submit_time":"2026-01-12T19:45:00+05:30"}
`)
	recs, err := collect(r.Read(t.Context(), email.Request{Input: "in.msg"}))
	if err != nil {
		t.Fatal(err)
	}
	got := recs[1].(*email.Message).SubmitTime
	if want := time.Date(2026, 1, 12, 14, 15, 0, 0, time.UTC); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("submit time %v, want %v in UTC", got, want)
	}
}
