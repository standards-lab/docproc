//go:build linux

package email_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/standards-lab/docproc/email"
)

// TestMessageConversation: Message.Conversation reads a message of a conversation class, matched
// without regard to case, and no other: a Voice subclass, a note, and no class are messages. The
// topic is the stored conversation topic, else the subject; the participants are the sender, when
// there is one, then To, CC, and BCC in order; the start and end are the submit and delivery
// times; and missed is the Missed class's.
func TestMessageConversation(t *testing.T) {
	a := email.Address{Name: "A", Address: "a@example.com"}
	b := email.Address{Name: "B", Address: "b@example.com", AddressType: "SMTP"}
	c := email.Address{Name: "C", Address: "c@example.com"}
	d := email.Address{Name: "D", Address: "d@example.com"}
	start := time.Date(2026, 3, 10, 14, 2, 11, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	full := email.Message{
		ID: 7, Folder: 3, Class: email.ConversationClass, Subject: "subject",
		ConversationTopic: "topic", Sender: a, To: []email.Address{b}, CC: []email.Address{c},
		BCC: []email.Address{d}, SubmitTime: start, DeliveryTime: end,
		Body: email.Body{HTML: "<p>x</p>", Derived: email.Derived{Markdown: "x"}},
	}
	tests := []struct {
		name string
		edit func(m *email.Message)
		want *email.Conversation
	}{
		{"conversation", func(*email.Message) {}, &email.Conversation{
			ID: 7, Folder: 3, Class: email.ConversationClass, Topic: "topic",
			Participants: []email.Address{a, b, c, d}, Start: start, End: end,
			Markdown: "x", HTML: "<p>x</p>",
		}},
		{"missed, its class in another case", func(m *email.Message) { m.Class = "ipm.note.microsoft.missed" }, &email.Conversation{
			ID: 7, Folder: 3, Class: "ipm.note.microsoft.missed", Topic: "topic",
			Participants: []email.Address{a, b, c, d}, Start: start, End: end, Missed: true,
			Markdown: "x", HTML: "<p>x</p>",
		}},
		{"no topic, no sender", func(m *email.Message) { m.ConversationTopic, m.Sender = "", email.Address{} }, &email.Conversation{
			ID: 7, Folder: 3, Class: email.ConversationClass, Topic: "subject",
			Participants: []email.Address{b, c, d}, Start: start, End: end,
			Markdown: "x", HTML: "<p>x</p>",
		}},
		{"voice", func(m *email.Message) { m.Class = email.ConversationClass + ".Voice" }, nil},
		{"missed voice", func(m *email.Message) { m.Class = email.MissedClass + ".Voice" }, nil},
		{"note", func(m *email.Message) { m.Class = "IPM.Note" }, nil},
		{"no class", func(m *email.Message) { m.Class = "" }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := full
			tt.edit(&m)
			got, ok := m.Conversation()
			if ok != (tt.want != nil) || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Conversation() = %+v, %t; want %+v", got, ok, tt.want)
			}
		})
	}
}
