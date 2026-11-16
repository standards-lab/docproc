package email

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
)

// Record is one item of a read, by its kind: *Store, *Folder, *Message, or *Skipped for
// docproc-mail's lines of those kinds, *Warning for a warning it wrote to stderr, and
// *UnknownRecord for a line of a kind schema "2" adds later, since the contract grows by addition.
//
// Each line's type mirrors its line: a field the line leaves out is the zero value, which means
// none, as the contract means it; an optional number whose zero is meaningful is a pointer, nil
// for none. Each marshals back, through encoding/json/v2, to its line's fields less "line" and any
// false boolean, but for the fields the Reader adds, which it marks as its own.
type Record interface {
	record()
}

// Store is the store line, the first of every read: the schema, the input's format, and, for a
// .pst, its encoding, whether it is password-protected, and its display name; an .eml, an mbox,
// and a .msg have none of the three.
type Store struct {
	// Schema is the envelope's schema, cli's "2" for every Store a read yields.
	Schema string `json:"schema,omitzero"`
	// Format is the input's format, sniffed from its content: "pst", "msg", "eml", or "mbox".
	Format string `json:"format,omitzero"`
	// Encoding is a .pst's: "unicode" or "ansi".
	Encoding string `json:"encoding,omitzero"`
	// Password is whether a .pst is password-protected.
	Password bool `json:"password,omitzero"`
	// DisplayName is a .pst's display name, a label for the store.
	DisplayName string `json:"display_name,omitzero"`
}

// Folder is a folder line of a .pst, which comes before its messages' lines and they before its
// subfolders'.
type Folder struct {
	// ID is the folder's node id.
	ID uint64 `json:"id,omitzero"`
	// Parent is its parent folder's node id; zero for the IPM subtree's root, which has none.
	Parent uint64 `json:"parent,omitzero"`
	// Path is the display names from the IPM subtree's root down to this folder, its own last.
	Path []string `json:"path,omitzero"`
	// ContainerClass is its container class, such as IPF.Note.
	ContainerClass string `json:"container_class,omitzero"`
}

// Message is a message line, whatever its message class, or a message nested under a .msg's,
// an .eml's, or an mbox's attachment record. docproc-mail writes no class-specific field, and
// groups nothing into threads. An .eml's or an mbox's message has no class and no creation or
// modification time, and its addresses are all SMTP. A message of a conversation class is a
// Message too; Conversation reads it as one.
type Message struct {
	// ID is, in a .pst, the message's node id; in an mbox, its 1-based ordinal in the file, where
	// an empty entry still takes a number, so the ids can skip one. A .msg's and an .eml's message
	// has none.
	ID uint64 `json:"id,omitzero"`
	// Folder is, in a .pst, the node id of its folder; nothing else has one.
	Folder uint64 `json:"folder,omitzero"`
	// Class is the message class, such as IPM.Note.
	Class string `json:"class,omitzero"`
	// Subject is the subject, its prefix marker dropped.
	Subject string `json:"subject,omitzero"`
	// Sender is the sender's address. Its SMTPAddress, when the line leaves it out, is the
	// Reader's, taken from the From field of TransportHeaders.
	Sender Address `json:"sender,omitzero"`
	// To, CC, and BCC are the recipients' addresses, in the recipient table's order.
	To  []Address `json:"to,omitzero"`
	CC  []Address `json:"cc,omitzero"`
	BCC []Address `json:"bcc,omitzero"`
	// SubmitTime, DeliveryTime, CreationTime, and ModificationTime are UTC, to the second.
	SubmitTime       time.Time `json:"submit_time,omitzero"`
	DeliveryTime     time.Time `json:"delivery_time,omitzero"`
	CreationTime     time.Time `json:"creation_time,omitzero"`
	ModificationTime time.Time `json:"modification_time,omitzero"`
	// Importance is "low", "normal", or "high".
	Importance string `json:"importance,omitzero"`
	// Sensitivity is "normal", "personal", "private", or "confidential".
	Sensitivity string `json:"sensitivity,omitzero"`
	// TransportHeaders are the raw transport headers, as stored.
	TransportHeaders string `json:"transport_headers,omitzero"`
	// MessageID, InReplyTo, and References are the Internet threading fields, as stored.
	MessageID  string `json:"message_id,omitzero"`
	InReplyTo  string `json:"in_reply_to,omitzero"`
	References string `json:"references,omitzero"`
	// ConversationTopic is the conversation topic, as stored.
	ConversationTopic string `json:"conversation_topic,omitzero"`
	// ConversationIndex is the lowercase hex of the stored conversation index's bytes.
	ConversationIndex string `json:"conversation_index,omitzero"`
	// Markdown is the message's body as Markdown, made by docproc-mail from its stored HTML, the
	// HTML its RTF encapsulates, its text body, or its RTF's text, in that order. Its images link
	// media names: an attachment's File, or a Medium's.
	Markdown string `json:"markdown,omitzero"`
	// Attachments are its attachment records, in the attachment table's order.
	Attachments []Attachment `json:"attachments,omitzero"`
	// Media are the payloads the Markdown links that no attachment names, a data: image's, in the
	// Markdown's order.
	Media []Medium `json:"media,omitzero"`
}

// The message classes a Skype for Business client stores its conversation history under, in the
// Conversation History folder: an instant-message conversation, and one its user missed. Each such
// message reads as a Conversation. Their Voice subclasses, a call's, read as any other class.
const (
	ConversationClass = "IPM.Note.Microsoft.Conversation"
	MissedClass       = "IPM.Note.Microsoft.Missed"
)

// Conversation is a message of a conversation class, ConversationClass or MissedClass, read as one
// conversation: who took part, when it ran, its topic, whether it was missed, and its transcript as
// docproc-mail's Markdown. It is a view of a Message, Message.Conversation's, never a record the
// Reader yields. The class decides, matched without regard to case as message classes are, never
// the folder: a message of any other class in the Conversation History folder isn't one. A
// Conversation is one per stored item, never its turns: the transcript's lines, each a speaker, a
// time, and a text, are only in its Markdown, which the package doesn't parse.
type Conversation struct {
	// ID is the message's node id in a .pst, as Message.ID; docproc-mail's --after takes it, so a
	// read resumes after a conversation as after any message.
	ID uint64 `json:"id,omitzero"`
	// Folder is, in a .pst, the node id of its folder.
	Folder uint64 `json:"folder,omitzero"`
	// Class is the message class, ConversationClass or MissedClass as stored.
	Class string `json:"class,omitzero"`
	// Topic is the conversation topic as stored, else, when there is none, the subject.
	Topic string `json:"topic,omitzero"`
	// Participants are the sender, when there is one, then the recipients, To, CC, and BCC, each in
	// the recipient table's order, each as the message holds it.
	Participants []Address `json:"participants,omitzero"`
	// Start is when the conversation began, the message's submit time; End is when it ended, its
	// delivery time, when its history was saved. Both are UTC, to the second.
	Start time.Time `json:"start,omitzero"`
	End   time.Time `json:"end,omitzero"`
	// Missed is whether its user missed it: its class is MissedClass.
	Missed bool `json:"missed,omitzero"`
	// Markdown is the transcript, the message's Markdown.
	Markdown string `json:"markdown,omitzero"`
	// Attachments and Media are the message's.
	Attachments []Attachment `json:"attachments,omitzero"`
	Media       []Medium     `json:"media,omitzero"`
}

// IsConversation reports whether class is a conversation class, ConversationClass or MissedClass,
// without regard to case.
func IsConversation(class string) bool {
	return strings.EqualFold(class, ConversationClass) || strings.EqualFold(class, MissedClass)
}

// Conversation returns m read as a Conversation, and true, when its class is a conversation class;
// nil and false otherwise. A caller holding a message, yielded by a read or decoded from a stored
// record, reads it as a conversation through this view.
func (m *Message) Conversation() (*Conversation, bool) {
	if !IsConversation(m.Class) {
		return nil, false
	}
	c := &Conversation{
		ID:          m.ID,
		Folder:      m.Folder,
		Class:       m.Class,
		Topic:       m.ConversationTopic,
		Start:       m.SubmitTime,
		End:         m.DeliveryTime,
		Missed:      strings.EqualFold(m.Class, MissedClass),
		Markdown:    m.Markdown,
		Attachments: m.Attachments,
		Media:       m.Media,
	}
	if c.Topic == "" {
		c.Topic = m.Subject
	}
	if m.Sender != (Address{}) {
		c.Participants = append(c.Participants, m.Sender)
	}
	c.Participants = append(c.Participants, m.To...)
	c.Participants = append(c.Participants, m.CC...)
	c.Participants = append(c.Participants, m.BCC...)
	return c, true
}

// Address is a sender's or recipient's address, each field as stored but a sender's SMTPAddress,
// which the Reader may add.
type Address struct {
	Name    string `json:"name,omitzero"`
	Address string `json:"address,omitzero"`
	// AddressType is the address's type, such as SMTP or EX; an EX address is its X.500
	// distinguished name.
	AddressType string `json:"address_type,omitzero"`
	// SMTPAddress is an EX address's SMTP address, where the input holds it: a recipient's
	// PidTagSmtpAddress, the sender's PidTagSenderSmtpAddress. When the line leaves a sender's out,
	// the Reader takes it from the From field of the message's transport headers, where they have
	// one that parses, whatever the address's type.
	SMTPAddress string `json:"smtp_address,omitzero"`
}

// Attachment is one of a message's attachment records.
type Attachment struct {
	// File is the media name of its content's file: the lowercase hex SHA-256 of the bytes, plus
	// "." and the stored extension when that is 1 to 8 ASCII letters and digits. "" for an
	// attachment whose content isn't a file.
	File string `json:"file,omitzero"`
	// Filename is the long filename, else the short one.
	Filename string `json:"filename,omitzero"`
	// MimeType is the MIME type, as stored.
	MimeType string `json:"mime_type,omitzero"`
	// ContentID is the content id, as stored, without angle brackets.
	ContentID string `json:"content_id,omitzero"`
	// Inline is whether the Markdown links it.
	Inline bool `json:"inline,omitzero"`
	// Method is the attach method: "none", "by_value", "by_reference", "by_reference_only",
	// "embedded_message", "storage", or "by_web_reference".
	Method string `json:"method,omitzero"`
	// Size is the content's size in bytes, nil when its content wasn't read.
	Size *int64 `json:"size,omitzero"`
	// Message is, in a .msg, an .eml, or an mbox, an embedded message, a message/rfc822 or
	// message/global part in the latter two, nested with its own Markdown and attachments; nil
	// otherwise. In a .pst an embedded message is listed with its metadata alone.
	Message *Message `json:"message,omitzero"`
	// Path is the content's file in the read's media directory, which docproc-mail writes each
	// distinct content into before the record's line; "" for a read without one, and for an
	// attachment with no File. It is the Reader's, never marshalled.
	Path string `json:"-"`
	// SHA256 is the content's SHA-256, lowercase hex, as its File claims it; "" with no File. It is
	// the Reader's, never marshalled.
	SHA256 string `json:"-"`
}

// Medium is a payload a message's Markdown links that no attachment names, a data: image's,
// written as media and named by its File.
type Medium struct {
	// File is the payload's media name, as an Attachment's is.
	File string `json:"file,omitzero"`
	// MimeType is its MIME type, as the data: URL gives it.
	MimeType string `json:"mime_type,omitzero"`
	// Path and SHA256 are as an Attachment's: the Reader's, never marshalled.
	Path   string `json:"-"`
	SHA256 string `json:"-"`
}

// Skipped is a skipped line: a record docproc-mail couldn't read inside a readable input, in the
// place the record would have had.
type Skipped struct {
	// Kind is "folder", "message", or "attachment"; or "contents" or "hierarchy" for a folder's
	// table. An .eml's are "attachment"; an mbox's, "message" or "attachment".
	Kind string `json:"kind,omitzero"`
	// Folder is the node id of the folder the record is in: a skipped folder names its parent,
	// and a skipped table the folder whose table it is.
	Folder uint64 `json:"folder,omitzero"`
	// Message is an attachment's message's node id; in an mbox, its ordinal. An .eml's skipped
	// attachment names none.
	Message uint64 `json:"message,omitzero"`
	// ID is the record's own id, nil for a table: in a .pst, its node id, an attachment's within
	// its message; in a .msg, an attachment's number, which may be zero; in an .eml or an mbox,
	// an attachment's 1-based number among its message's attachment parts, and in an mbox, a
	// skipped message's ordinal.
	ID *uint64 `json:"id,omitzero"`
	// Reason is why it couldn't be read.
	Reason string `json:"reason,omitzero"`
}

// Warning is a warning docproc-mail wrote to stderr, what it skipped or read with a loss: its
// tool's name, where in the input, and what. A .pst's Where names a folder by its path, the
// folder's display names joined by "/", in place of the node id docproc-mail writes, once the
// folder's line has been read. A read with warnings still succeeds.
//
// Interim until slice 15: email can't import dispatch while dispatch holds the stage adapters,
// so Warning is internal/cli's, which stands for dispatch.Warning.
type Warning cli.Warning

// UnknownRecord is a line of a kind the package doesn't know, kept with its kind and its JSON as
// docproc-mail wrote it, so a later addition to the contract is never an error.
type UnknownRecord struct {
	Kind string
	Raw  jsontext.Value
}

func (*Store) record()         {}
func (*Folder) record()        {}
func (*Message) record()       {}
func (*Skipped) record()       {}
func (*Warning) record()       {}
func (*UnknownRecord) record() {}

// decodeLine decodes one of docproc-mail's lines, of kind kind, into its Record, every time UTC. A
// message's sender, and each nested message's, takes its SMTP address from its transport headers
// when the line leaves it out. The raw JSON is the reader's to keep, as cli.Read yields it, so an
// UnknownRecord holds it as it is.
func decodeLine(kind string, raw jsontext.Value) (Record, error) {
	var rec Record
	switch kind {
	case "store":
		rec = new(Store)
	case "folder":
		rec = new(Folder)
	case "message":
		rec = new(Message)
	case "skipped":
		rec = new(Skipped)
	default:
		return &UnknownRecord{Kind: kind, Raw: raw}, nil
	}
	if err := json.Unmarshal(raw, rec, cli.UTC); err != nil {
		return nil, fmt.Errorf("email: decoding a %s line: %w", kind, err)
	}
	if m, ok := rec.(*Message); ok {
		fillSenders(m)
	}
	return rec, nil
}

// fillSenders sets the SMTP address of m's sender, and of each nested message's, from its
// transport headers' From when the line left it out (design ML-2).
func fillSenders(m *Message) {
	if m.Sender.SMTPAddress == "" {
		m.Sender.SMTPAddress = headerFrom(m.TransportHeaders)
	}
	for i := range m.Attachments {
		if inner := m.Attachments[i].Message; inner != nil {
			fillSenders(inner)
		}
	}
}

// headerFrom returns the address of the From field of headers, a raw header block as a message
// stores it; "" when it has no From field or the field doesn't parse. A line that isn't a field,
// such as the "Microsoft Mail Internet Headers Version 2.0" line Exchange may begin a block with,
// is passed over, and a folded field is unfolded. The first From field is read, and of a list of
// addresses, the first.
func headerFrom(headers string) string {
	var value strings.Builder
	in := false
	for line := range strings.Lines(headers) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			// The header block's end.
			break
		}
		if line[0] == ' ' || line[0] == '\t' {
			if in {
				value.WriteString(line)
			}
			continue
		}
		if in {
			break
		}
		if name, rest, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "From") {
			in = true
			value.WriteString(rest)
		}
	}
	if !in {
		return ""
	}
	field := strings.TrimSpace(value.String())
	if a, err := mail.ParseAddress(field); err == nil {
		return a.Address
	}
	if list, err := mail.ParseAddressList(field); err == nil && len(list) > 0 {
		return list[0].Address
	}
	return ""
}
