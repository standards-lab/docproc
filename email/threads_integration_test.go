//go:build integration && linux

package email_test

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/email"
)

// These tests run the real mail binary, which the unit tier doesn't provide, so they run in the
// integration tier.

// TestRealThreads runs the real pinned mail over Tika's two stores and threads them, against
// realThreads. One store is threaded through a Threader fed while reading, the other through
// Threads.
//
// testPST.pst: Re: Feature Generators (2097188) refers to Feature Generators (2097252). The two
// JIRA mails (2097284, 2097316) and FW: First email (2097380) refer to messages the store doesn't
// hold, each placeholder with one child, so each is a root. The base subjects are all different,
// "[WEBINAR] " and "[jira] [Resolved] " removed as subj-blobs, so no roots are gathered. By date:
// WEBINAR 02-24, Feature Generators 02-25 10:17, init tokenizer 02-25 21:20, TIKA-1250 02-26
// 12:12, TIKA-1249 02-26 12:20, FW: First email 2020.
//
// testPST_variousBodyTypes.pst: the three FW: original email (2097220, 2097252, 2097284) each
// refer to original email (2097188), and are its children by date.
func TestRealThreads(t *testing.T) {
	r := realReader(t)
	root := filepath.Join(mailCopy, "fixtures", "pst", "tika")
	t.Run("testPST.pst", func(t *testing.T) {
		var tr email.Threader
		for rec, err := range mustRead(t)(r.Read(t.Context(), email.Request{Input: filepath.Join(root, "testPST.pst")})).Records() {
			if err != nil {
				t.Fatal(err)
			}
			tr.Add(rec)
		}
		want := realThreads["pst/tika/testPST.pst"]
		if got := render(tr.Threads()); got != want {
			t.Errorf("threads %s, want %s", got, want)
		}
	})
	t.Run("testPST_variousBodyTypes.pst", func(t *testing.T) {
		threads, err := email.Threads(mustRead(t)(r.Read(t.Context(), email.Request{Input: filepath.Join(root, "testPST_variousBodyTypes.pst")})).Records())
		if err != nil {
			t.Fatal(err)
		}
		want := realThreads["pst/tika/testPST_variousBodyTypes.pst"]
		if got := render(threads); got != want {
			t.Errorf("threads %s, want %s", got, want)
		}
		if got := threads[0].MessageID; got != "<MWHPR09MB1391E30131B0D193163AA6E0C79C0@MWHPR09MB1391.namprd09.prod.outlook.com>" {
			t.Errorf("root Message-ID %s", got)
		}
	})
}

// TestRealThreadsIndex threads Tika's two stores by their conversation indexes alone: each
// message's Message-ID, In-Reply-To, References, and subject cleared as it is read. By the
// indexes in tika-threading.json: in testPST.pst, Re: Feature Generators' is Feature Generators'
// and one child block, and FW: First email's names a parent the store doesn't hold, a placeholder
// with one child, so a root; the others are 22-byte headers. In testPST_variousBodyTypes.pst, each
// FW: original email's is original email's and one child block. The trees are those the headers
// give, realThreads'.
func TestRealThreadsIndex(t *testing.T) {
	r := realReader(t)
	root := filepath.Join(mailCopy, "fixtures", "pst", "tika")
	for _, store := range []string{"testPST.pst", "testPST_variousBodyTypes.pst"} {
		t.Run(store, func(t *testing.T) {
			var tr email.Threader
			for rec, err := range mustRead(t)(r.Read(t.Context(), email.Request{Input: filepath.Join(root, store)})).Records() {
				if err != nil {
					t.Fatal(err)
				}
				if m, ok := rec.(*email.Message); ok {
					if m.ConversationIndex == "" {
						t.Fatalf("message %d: no conversation index", m.ID)
					}
					m.MessageID, m.InReplyTo, m.References, m.Subject = "", "", "", ""
				}
				tr.Add(rec)
			}
			if got, want := render(tr.Threads()), realThreads["pst/tika/"+store]; got != want {
				t.Errorf("threads %s, want %s", got, want)
			}
		})
	}
}

// TestRealThreadsGenerated: the generated store carries no threading fields, so its messages
// thread by subject alone. None shares a base subject with another: "Re: Notes on the engine à la
// française" and "Fwd: Notes on the engine (draft)" are not "Notes on the engine". Each of its 8
// messages is a root, by date.
func TestRealThreadsGenerated(t *testing.T) {
	r := realReader(t)
	path := filepath.Join(mailCopy, "fixtures", "generated", "pst", "mailbox.pst")
	threads, err := email.Threads(mustRead(t)(r.Read(t.Context(), email.Request{Input: path})).Records())
	if err != nil {
		t.Fatal(err)
	}
	want := realThreads["generated/pst/mailbox.pst"]
	if got := render(threads); got != want {
		t.Errorf("threads %s, want %s", got, want)
	}
}

// TestRealThreadsMIME runs the real pinned mail over every MIME input and threads it, against
// mimeThreads. Each is threaded twice, through Threads and through a Threader fed while reading,
// and the two agree. Each root is a message, never a placeholder, and nine.mboxrd's first root is
// Notes on the engine, <notes-1@example.org>.
func TestRealThreadsMIME(t *testing.T) {
	r := realReader(t)
	_, inputs := realInputs(t)
	if got := slices.Sorted(maps.Keys(mimeThreads)); !slices.Equal(got, slices.Sorted(slices.Values(inputs))) {
		t.Errorf("mimeThreads covers %q, the inputs are %q", got, inputs)
	}
	for _, rel := range inputs {
		t.Run(rel, func(t *testing.T) {
			input, err := filepath.Abs(filepath.Join("testdata", rel))
			if err != nil {
				t.Fatal(err)
			}
			threads, err := email.Threads(mustRead(t)(r.Read(t.Context(), email.Request{Input: input})).Records())
			if err != nil {
				t.Fatal(err)
			}
			var tr email.Threader
			for rec, err := range mustRead(t)(r.Read(t.Context(), email.Request{Input: input})).Records() {
				if err != nil {
					t.Fatal(err)
				}
				tr.Add(rec)
			}
			if got, want := render(tr.Threads()), render(threads); got != want {
				t.Errorf("Threader's threads %s, Threads' %s", got, want)
			}
			if got, want := render(threads), mimeThreads[rel]; got != want {
				t.Errorf("threads %s, want %s", got, want)
			}
			for _, root := range threads {
				if root.Placeholder {
					t.Errorf("root placeholder %s", root.MessageID)
				}
			}
			if rel == "mail/fixtures/mime/nine.mboxrd" && threads[0].MessageID != "<notes-1@example.org>" {
				t.Errorf("root Message-ID %s, want <notes-1@example.org>", threads[0].MessageID)
			}
		})
	}
}

// realThreads is the threads of the copied stores that have a hand truth, each by its path under
// fixtures/, rendered: Tika's two, derived by hand from the stored fields tika-threading.json holds
// and the dates mail's goldens give, and the generated store's, by subject alone. TestRealThreads,
// TestRealThreadsIndex, and TestRealThreadsGenerated check them, and the findings tier scores
// against them.
var realThreads = map[string]string{
	"pst/tika/testPST.pst":                  "2097348 2097252[2097188] 2097220 2097316 2097284 2097380",
	"pst/tika/testPST_variousBodyTypes.pst": "2097188[2097220 2097252 2097284]",
	"generated/pst/mailbox.pst":             "2097156 2097188 2097220 2097252 2097284 2097316 2097348 2097380",
}

// mimeThreads is the threads of the MIME inputs, each by its path under testdata/ as mimeTruth
// keys it, rendered: derived by hand from each input's Message-ID, In-Reply-To, References,
// subject, and Date headers. An mbox message's id is its ordinal; an .eml's message has none, so
// each .eml is one root, 0. TestRealThreadsMIME checks them.
//
// nine.mboxrd, by REFERENCES: The reply (2) refers to Notes on the engine (1), and the threading
// message (9) to <notes-1> then <table-1>, which the file doesn't hold: <table-1>'s placeholder,
// under 1 with one child, is pruned and 9 promoted to 1's child. The forward (8) refers to
// <notes-0>, the message it carries attached, which is no message of the file: its placeholder,
// a root with one child, is pruned, and 8 is a root. No base subjects match: "Notes on the engine
// à la française", "Notes on the engine (draft)", and "Note G — the table" are not "Notes on the
// engine". By date, UTC: 1 01-12 14:15, its children 2 01-12 18:02 and 9 01-13 09:29; 3 02-03,
// 4 03-18, 5 04-09, 6 04-26, 7 05-04, 8 05-05.
//
// mbox.rlug: 4 refers to 1, the ZAPP question, and is its reply. 2 refers to four messages the
// file doesn't hold, a chain of placeholders each with one child, pruned to 2 as a root. 3 and 5
// refer to nothing, and no root shares a base subject with another. By date, UTC: 1 02-07 16:35,
// 2 02-07 17:21, 3 02-07 17:36, 5 02-07 23:58.
var mimeThreads = map[string]string{
	"mail/fixtures/generated/eml/01-plain.eml":                    "0",
	"mail/fixtures/generated/eml/02-reply-latin1.eml":             "0",
	"mail/fixtures/generated/eml/03-html-inline-image.eml":        "0",
	"mail/fixtures/generated/eml/04-attachment-cyrillic.eml":      "0",
	"mail/fixtures/generated/eml/05-japanese.eml":                 "0",
	"mail/fixtures/generated/eml/06-chinese.eml":                  "0",
	"mail/fixtures/generated/eml/07-html-only-bcc.eml":            "0",
	"mail/fixtures/generated/eml/08-forward-attached-message.eml": "0",
	"mail/fixtures/mime/eml/09-threading.eml":                     "0",
	"mail/fixtures/mime/nine.mboxrd":                              "1[2 9] 3 4 5 6 7 8",
	"mail/fixtures/mime/rlug/mbox.rlug":                           "1[4] 2 3 5",
	"mail/fixtures/mime/rlug/mbox.rlug-0":                         "0",
	"mail/fixtures/mime/rlug/mbox.rlug-1":                         "0",
	"mail/fixtures/mime/rlug/mbox.rlug-2":                         "0",
	"mail/fixtures/mime/rlug/mbox.rlug-3":                         "0",
	"mail/fixtures/mime/rlug/mbox.rlug-4":                         "0",
	"built/" + builtEML:                                           "0",
}
