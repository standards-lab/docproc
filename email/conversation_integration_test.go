//go:build integration && linux

package email_test

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/docproc/email"
)

// These tests run the real docproc-mail, which the unit tier doesn't provide, so they run in the
// integration tier.

// TestRealConversations runs the real docproc-mail over its generated conversations.pst, read in
// place, and holds each item of every folder layout.json lists items for to its hand truth in
// truth.json, in the layout's order. An item of a conversation class is a Message whose
// Conversation view equals the one the truth gives: its class, its topic the truth's subject, as
// the store's conversation topic is, its participants the sender then the recipients, its start
// and end the submit and delivery times, missed by its class, and its Markdown. An item of any
// other class, the IPM.Note in the same folder, has no Conversation view, and its class, subject,
// sender, recipients, four times, and Markdown are the truth's. The fixture holds every kind: a
// conversation, a missed one, and a note.
func TestRealConversations(t *testing.T) {
	generated := mailFixture(t, "generated")
	var l layout
	readJSON(t, filepath.Join(generated, "layout.json"), &l)
	var truth struct {
		Items map[string]itemTruth `json:"items"`
	}
	readJSON(t, filepath.Join(generated, "truth.json"), &truth)
	r := realReader(t)
	classes := map[string]int{}
	stores := 0
	for _, store := range l.Stores {
		var items []string
		for _, f := range store.Folders {
			items = append(items, f.Items...)
		}
		if len(items) == 0 {
			continue
		}
		stores++
		t.Run(store.File, func(t *testing.T) {
			recs, err := collect(r.Read(t.Context(), email.Request{Input: filepath.Join(generated, store.File)}))
			if err != nil {
				t.Fatal(err)
			}
			var got []*email.Message
			for _, rec := range recs {
				if m, ok := rec.(*email.Message); ok {
					got = append(got, m)
				}
			}
			if len(got) != len(items) {
				t.Fatalf("%d items, the layout's %d", len(got), len(items))
			}
			for i, name := range items {
				want, ok := truth.Items[name]
				if !ok {
					t.Fatalf("%s: no hand truth", name)
				}
				classes[want.Class]++
				checkItem(t, name, got[i], want)
			}
		})
	}
	if stores == 0 {
		t.Fatal("layout.json lists items in no store")
	}
	for _, class := range []string{email.ConversationClass, email.MissedClass, "IPM.Note"} {
		if classes[class] == 0 {
			t.Errorf("no item of class %s", class)
		}
	}
}

// layout is docproc-mail's generated layout.json: each store's file and folders, each folder listing the
// .eml sources of its messages, or the item sources of its items, in the store's order.
type layout struct {
	Stores []struct {
		File    string `json:"file"`
		Folders []struct {
			Path     []string `json:"path"`
			Messages []string `json:"messages"`
			Items    []string `json:"items"`
		} `json:"folders"`
	} `json:"stores"`
}

// itemTruth is one item source's hand truth in truth.json's "items": the item as the store holds
// it, field by field.
type itemTruth struct {
	Class            string          `json:"class"`
	Subject          string          `json:"subject"`
	Sender           email.Address   `json:"sender"`
	To               []email.Address `json:"to"`
	CC               []email.Address `json:"cc"`
	SubmitTime       time.Time       `json:"submit_time"`
	DeliveryTime     time.Time       `json:"delivery_time"`
	CreationTime     time.Time       `json:"creation_time"`
	ModificationTime time.Time       `json:"modification_time"`
	Markdown         string          `json:"markdown"`
}

// readJSON decodes the file at path into v.
func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// bare returns addrs with each address type and SMTP address dropped: the hand truth gives a name
// and an address alone, and docproc-mail gives a recipient's type as stored, SMTP.
func bare(addrs []email.Address) []email.Address {
	out := make([]email.Address, len(addrs))
	for i, a := range addrs {
		out[i] = email.Address{Name: a.Name, Address: a.Address}
	}
	return out
}

// checkItem checks m, the message read for the item source name, against want, its hand truth.
func checkItem(t *testing.T, name string, m *email.Message, want itemTruth) {
	t.Helper()
	c, ok := m.Conversation()
	if !email.IsConversation(want.Class) {
		if ok {
			t.Errorf("%s: a conversation, want a message of class %s", name, want.Class)
			return
		}
		got := itemTruth{
			Class: m.Class, Subject: m.Subject, Sender: m.Sender, To: bare(m.To), CC: bare(m.CC),
			SubmitTime: m.SubmitTime, DeliveryTime: m.DeliveryTime,
			CreationTime: m.CreationTime, ModificationTime: m.ModificationTime,
			Markdown: m.Markdown,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: message\n%+v\nthe truth's\n%+v", name, got, want)
		}
		return
	}
	if !ok {
		t.Errorf("%s: a message of class %s, want a conversation of class %s", name, m.Class, want.Class)
		return
	}
	if c.ID == 0 || c.Folder == 0 {
		t.Errorf("%s: conversation id %d in folder %d, want both", name, c.ID, c.Folder)
	}
	expect := email.Conversation{
		ID:           c.ID,
		Folder:       c.Folder,
		Class:        want.Class,
		Topic:        want.Subject,
		Participants: slices.Concat([]email.Address{want.Sender}, want.To, want.CC),
		Start:        want.SubmitTime,
		End:          want.DeliveryTime,
		Missed:       want.Class == email.MissedClass,
		Markdown:     want.Markdown,
	}
	got := *c
	got.Participants = bare(c.Participants)
	if !reflect.DeepEqual(got, expect) {
		t.Errorf("%s: conversation\n%+v\nthe truth's\n%+v", name, got, expect)
	}
}
