//go:build linux

package email_test

import (
	"errors"
	"iter"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/email"
)

func render(threads []*email.Thread) string {
	var b strings.Builder
	var write func([]*email.Thread)
	write = func(list []*email.Thread) {
		for i, t := range list {
			if i > 0 {
				b.WriteString(" ")
			}
			if t.Placeholder {
				b.WriteString("*" + t.MessageID)
			} else {
				b.WriteString(strconv.FormatUint(t.ID, 10))
			}
			if len(t.Children) > 0 {
				b.WriteString("[")
				write(t.Children)
				b.WriteString("]")
			}
		}
	}
	write(threads)
	return b.String()
}

var day = time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)

func msg(id uint64, messageID, inReplyTo, references string) *email.Message {
	return &email.Message{
		ID:         id,
		SubmitTime: day.Add(time.Duration(id) * time.Minute),
		MessageID:  messageID,
		InReplyTo:  inReplyTo,
		References: references,
	}
}

func subj(id uint64, subject string) *email.Message {
	m := msg(id, "", "", "")
	m.Subject = subject
	return m
}

func indexed(id uint64, index string) *email.Message {
	m := msg(id, "", "", "")
	m.ConversationIndex = index
	return m
}

func threadOf(msgs ...*email.Message) string {
	var t email.Threader
	for _, m := range msgs {
		t.Add(m)
	}
	return render(t.Threads())
}

func TestThreadsReferences(t *testing.T) {
	cases := []struct {
		name string
		msgs []*email.Message
		want string
	}{
		{
			name: "chain",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "<a@x>", "<a@x>"),
				msg(3, "<c@x>", "<b@x>", "<a@x> <b@x>"),
			},
			want: "1[2[3]]",
		},
		{
			name: "chain links earlier messages",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "", ""),
				msg(3, "<c@x>", "", "<a@x> <b@x>"),
			},
			want: "1[2[3]]",
		},
		{
			name: "no references breaks a link",
			msgs: []*email.Message{
				msg(3, "<c@x>", "", "<a@x> <b@x>"),
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "", ""),
			},
			want: "1 2[3]",
		},
		{
			name: "in-reply-to",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "<a@x> (sent by Ada <ada@x>)", ""),
			},
			want: "1[2]",
		},
		{
			name: "references over in-reply-to",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "", ""),
				msg(3, "<c@x>", "<a@x>", "<b@x>"),
			},
			want: "1 2[3]",
		},
		{
			name: "missing parent, two children",
			msgs: []*email.Message{
				msg(1, "<b@x>", "", "<gone@x>"),
				msg(2, "<c@x>", "", "<gone@x>"),
			},
			want: "*<gone@x>[1 2]",
		},
		{
			name: "missing parent, one child",
			msgs: []*email.Message{
				msg(1, "<b@x>", "", "<gone@x>"),
			},
			want: "1",
		},
		{
			name: "missing ancestors in a chain",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<d@x>", "", "<a@x> <gone@x> <gone2@x>"),
			},
			want: "1[2]",
		},
		{
			name: "missing root, nested placeholders",
			msgs: []*email.Message{
				msg(1, "<b@x>", "", "<root@x> <mid@x>"),
				msg(2, "<c@x>", "", "<root@x> <mid@x>"),
				msg(3, "<d@x>", "", "<root@x> <other@x>"),
			},
			want: "*<root@x>[1 2 3]",
		},
		{
			name: "duplicate message-id",
			msgs: []*email.Message{
				msg(2, "<a@x>", "", ""),
				msg(1, "<a@x>", "", ""),
				msg(3, "<r@x>", "", "<a@x>"),
			},
			want: "1 2[3]",
		},
		{
			name: "without message-id",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "", "", "<a@x>"),
				msg(3, "not-an-id", "", "<a@x>"),
				msg(4, "", "", ""),
				msg(5, "", "", ""),
			},
			want: "1[2 3] 4 5",
		},
		{
			name: "invalid references",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "<a@x>", "<no-at-sign>"),
			},
			want: "1[2]",
		},
		{
			name: "without angle brackets",
			msgs: []*email.Message{
				msg(1, "a@x", "", ""),
				msg(2, "b@x", "a@x", ""),
			},
			want: "1[2]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := threadOf(c.msgs...); got != c.want {
				t.Errorf("threads %s, want %s", got, c.want)
			}
		})
	}
}

func TestThreadsLoops(t *testing.T) {
	cases := []struct {
		name string
		msgs []*email.Message
		want string
	}{
		{
			name: "each other",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", "<b@x>"),
				msg(2, "<b@x>", "", "<a@x>"),
			},
			want: "2[1]",
		},
		{
			name: "repeated reference",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", ""),
				msg(2, "<b@x>", "", "<a@x>"),
				msg(3, "<c@x>", "", "<a@x> <b@x> <a@x>"),
			},
			want: "1[2 3]",
		},
		{
			name: "itself",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", "<a@x>"),
			},
			want: "1",
		},
		{
			name: "longer loop",
			msgs: []*email.Message{
				msg(1, "<a@x>", "", "<c@x>"),
				msg(2, "<b@x>", "", "<a@x>"),
				msg(3, "<c@x>", "", "<b@x>"),
			},
			want: "3[1[2]]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := threadOf(c.msgs...); got != c.want {
				t.Errorf("threads %s, want %s", got, c.want)
			}
		})
	}
}

func TestThreadsSubjects(t *testing.T) {
	cases := []struct {
		name string
		msgs []*email.Message
		want string
	}{
		{
			name: "reply",
			msgs: []*email.Message{subj(1, "Lunch plans"), subj(2, "Re: Lunch plans")},
			want: "1[2]",
		},
		{
			name: "reply first",
			msgs: []*email.Message{subj(1, "RE: Lunch plans"), subj(2, "Lunch plans")},
			want: "2[1]",
		},
		{
			name: "neither a reply",
			msgs: []*email.Message{subj(1, "Budget"), subj(2, "budget")},
			want: "*[1 2]",
		},
		{
			name: "both replies",
			msgs: []*email.Message{subj(1, "Re: Status"), subj(2, "Fwd: Status")},
			want: "*[1 2]",
		},
		{
			name: "three alike",
			msgs: []*email.Message{subj(1, "Budget"), subj(2, "Budget"), subj(3, "Budget")},
			want: "*[1 2 3]",
		},
		{
			name: "placeholder takes the others",
			msgs: []*email.Message{
				func() *email.Message { m := msg(1, "<k@x>", "", "<z@x>"); m.Subject = "Re: Plan"; return m }(),
				func() *email.Message { m := msg(2, "<l@x>", "", "<z@x>"); m.Subject = "Re: Plan"; return m }(),
				subj(0, "Plan"),
				subj(3, "Re: Plan"),
			},
			want: "*<z@x>[0 1 2 3]",
		},
		{
			name: "empty base subject",
			msgs: []*email.Message{subj(1, "Re:"), subj(2, "Re: "), subj(3, "")},
			want: "1 2 3",
		},
		{
			name: "threaded and gathered",
			msgs: []*email.Message{
				func() *email.Message { m := msg(1, "<a@x>", "", ""); m.Subject = "Report"; return m }(),
				func() *email.Message { m := msg(2, "<b@x>", "", "<a@x>"); m.Subject = "Re: Report"; return m }(),
				subj(3, "Re: Report"),
			},
			want: "1[2 3]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := threadOf(c.msgs...); got != c.want {
				t.Errorf("threads %s, want %s", got, c.want)
			}
		})
	}
}

func TestThreadsBaseSubject(t *testing.T) {
	const reply, alike, apart = "1[2]", "*[1 2]", "1 2"
	cases := []struct {
		subject string
		want    string
	}{
		{"Notes", alike},
		{"NOTES", alike},
		{"  Notes  ", alike},
		{"Notes\t", alike},
		{"Re: Notes", reply},
		{"re:Notes", reply},
		{"RE : Notes", reply},
		{"Fw: Notes", reply},
		{"FWD: Notes", reply},
		{"Re: Re: Fwd: Notes", reply},
		{"Re[2]: Notes", reply},
		{"Re [2]: Notes", reply},
		{"[list] Re: Notes", reply},
		{"Re: [list] Notes", reply},
		{"[list] [other] Notes", alike},
		{"[list] Notes", alike},
		{"Notes (fwd)", reply},
		{"Notes (FWD) ", reply},
		{"[Fwd: Notes]", reply},
		{"[fwd: Re: Notes]", reply},
		{"Re: [Fwd: [list] Notes (fwd)]", reply},
		{"=?utf-8?q?Re=3A_Notes?=", reply},
		{"Re:  Notes", reply},
		{"Notes again", apart},
		{"Fwd: Notes (draft)", apart},
		{"[Notes]", apart},
		{"Notes:", apart},
		{"Ré: Notes", apart},
	}
	for _, c := range cases {
		t.Run(c.subject, func(t *testing.T) {
			if got := threadOf(subj(1, "Notes"), subj(2, c.subject)); got != c.want {
				t.Errorf("threads %s, want %s", got, c.want)
			}
		})
	}
	if got := threadOf(subj(1, "[PATCH]"), subj(2, "Re: [PATCH]")); got != reply {
		t.Errorf("[PATCH]: threads %s, want %s", got, reply)
	}
}

const (
	header  = "01d321c5c2dc64ee4b2f50da41d68f42001487a4d229"
	block1  = "0000049f70"
	block2  = "00000b50b0"
	header2 = "01d6c441ee0a7d408d320c2c40f6ac88aae17faebcda"
)

func TestThreadsIndex(t *testing.T) {
	cases := []struct {
		name string
		msgs []*email.Message
		want string
	}{
		{
			name: "parent found",
			msgs: []*email.Message{indexed(1, header), indexed(2, header+block1)},
			want: "1[2]",
		},
		{
			name: "child read first",
			msgs: []*email.Message{indexed(2, header+block1), indexed(1, header)},
			want: "1[2]",
		},
		{
			name: "grandchildren",
			msgs: []*email.Message{
				indexed(1, header),
				indexed(2, header+block1),
				indexed(3, header+block1+block2),
				indexed(4, header+block2),
			},
			want: "1[2[3] 4]",
		},
		{
			name: "upper case hex",
			msgs: []*email.Message{indexed(1, header), indexed(2, strings.ToUpper(header+block1))},
			want: "1[2]",
		},
		{
			name: "parent missing, two children",
			msgs: []*email.Message{indexed(1, header2+block1), indexed(2, header2+block2)},
			want: "*[1 2]",
		},
		{
			name: "parent missing, one child",
			msgs: []*email.Message{indexed(1, header2+block1)},
			want: "1",
		},
		{
			name: "grandparent missing",
			msgs: []*email.Message{indexed(1, header2+block1), indexed(2, header2+block1+block2)},
			want: "1[2]",
		},
		{
			name: "shorter than the header",
			msgs: []*email.Message{indexed(1, header[:24]), indexed(2, header[:24]+block1)},
			want: "1 2",
		},
		{
			name: "partial child block",
			msgs: []*email.Message{indexed(1, header), indexed(2, header+block1[:8])},
			want: "1 2",
		},
		{
			name: "partial child block in the parent",
			msgs: []*email.Message{indexed(1, header+"00"), indexed(2, header+"00"+block1)},
			want: "1 2",
		},
		{
			name: "not hex",
			msgs: []*email.Message{indexed(1, header), indexed(2, header+"zz00000000")},
			want: "1 2",
		},
		{
			name: "odd hex digits",
			msgs: []*email.Message{indexed(1, header), indexed(2, header+block1+"0")},
			want: "1 2",
		},
		{
			name: "headers take precedence",
			msgs: []*email.Message{
				indexed(1, header),
				msg(2, "<o@x>", "", ""),
				func() *email.Message {
					m := msg(3, "<c@x>", "", "<o@x>")
					m.ConversationIndex = header + block1
					return m
				}(),
			},
			want: "1 2[3]",
		},
		{
			name: "missing header parent over the index",
			msgs: []*email.Message{
				indexed(1, header),
				func() *email.Message {
					m := msg(2, "<c@x>", "<gone@x>", "")
					m.ConversationIndex = header + block1
					return m
				}(),
			},
			want: "1 2",
		},
		{
			name: "duplicate index",
			msgs: []*email.Message{indexed(2, header), indexed(1, header), indexed(3, header+block1)},
			want: "1 2[3]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := threadOf(c.msgs...); got != c.want {
				t.Errorf("threads %s, want %s", got, c.want)
			}
		})
	}
}

func TestThreadsOrder(t *testing.T) {
	at := func(m *email.Message, submit, delivery time.Time) *email.Message {
		m.SubmitTime, m.DeliveryTime = submit, delivery
		return m
	}
	msgs := []*email.Message{
		at(msg(30, "<a@x>", "", ""), day, time.Time{}),
		at(msg(10, "<b@x>", "", ""), day, time.Time{}),
		at(msg(20, "<c@x>", "", ""), time.Time{}, day.Add(-time.Hour)),
		at(msg(5, "<d@x>", "", ""), time.Time{}, time.Time{}),
		at(msg(7, "<e@x>", "", "<a@x>"), day.Add(2*time.Hour), time.Time{}),
		at(msg(6, "<f@x>", "", "<a@x>"), day.Add(time.Hour), time.Time{}),
		at(msg(8, "<g@x>", "", "<a@x>"), day.Add(time.Hour), time.Time{}),
		at(msg(0, "", "", "<gone@x>"), day.Add(-2*time.Hour), time.Time{}),
		at(msg(0, "", "", "<gone@x>"), day.Add(-2*time.Hour), time.Time{}),
	}
	msgs[7].Subject, msgs[8].Subject = "first", "second"
	var tr email.Threader
	for _, m := range msgs {
		tr.Add(m)
	}
	first := tr.Threads()
	const want = "5 *<gone@x>[0 0] 20 10 30[6 8 7]"
	if got := render(first); got != want {
		t.Fatalf("threads %s, want %s", got, want)
	}
	if got := render(tr.Threads()); got != want {
		t.Errorf("built again: threads %s, want %s", got, want)
	}
	if got := threadOf(msgs...); got != want {
		t.Errorf("from the same messages: threads %s, want %s", got, want)
	}
}

func TestThreadsRecords(t *testing.T) {
	nested := msg(3, "<n@x>", "", "<a@x>")
	recs := []email.Record{
		&email.Store{Schema: "2", Format: "pst"},
		&email.Warning{Tool: "docproc-mail", Where: "store", What: "odd"},
		&email.Folder{ID: 32802},
		msg(1, "<a@x>", "", ""),
		&email.Skipped{Kind: "message", Folder: 32802},
		&email.UnknownRecord{Kind: "calendar"},
		&email.Message{ID: 4, Class: email.ConversationClass, Subject: "Re: chat", References: "<a@x>"},
		func() *email.Message {
			m := msg(2, "<b@x>", "", "<a@x>")
			m.Attachments = []email.Attachment{{Method: "embedded_message", Message: nested}}
			return m
		}(),
	}
	seq := func(err error) iter.Seq2[email.Record, error] {
		return func(yield func(email.Record, error) bool) {
			for _, rec := range recs {
				if !yield(rec, nil) {
					return
				}
			}
			if err != nil {
				yield(nil, err)
			}
		}
	}
	threads, err := email.Threads(seq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(threads), "1[2]"; got != want {
		t.Errorf("threads %s, want %s", got, want)
	}
	if threads[0].MessageID != "<a@x>" || threads[0].Placeholder {
		t.Errorf("root %+v, want message <a@x>", threads[0])
	}
	failed := errors.New("read failed")
	threads, err = email.Threads(seq(failed))
	if !errors.Is(err, failed) || threads != nil {
		t.Errorf("got %v, %v; want no threads and the read's error", threads, err)
	}
}

func TestThreadsStandin(t *testing.T) {
	t.Parallel()
	r, mail := standinReader(t)
	mail.stdout(`{"line":"store","schema":"2","format":"pst"}
{"line":"folder","id":32802}
{"line":"message","id":2097156,"folder":32802,"subject":"Hello","message_id":"<a@x>","submit_time":"2026-01-12T14:15:00Z"}
{"line":"folder","id":32866,"parent":32802}
{"line":"message","id":2097188,"folder":32866,"subject":"Re: Hello","in_reply_to":"<a@x>","submit_time":"2026-01-12T15:15:00Z"}
`)
	threads, err := email.Threads(r.Read(t.Context(), email.Request{Input: "store.pst"}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(threads), "2097156[2097188]"; got != want {
		t.Errorf("threads %s, want %s", got, want)
	}
}
