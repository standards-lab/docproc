//go:build integration && linux

package email_test

import (
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/tooltest"
)

func TestRealThreads(t *testing.T) {
	r := realReader(t)
	root := mailFixture(t, "pst/tika")
	t.Run("testPST.pst", func(t *testing.T) {
		var tr email.Threader
		for rec, err := range r.Read(t.Context(), email.Request{Input: filepath.Join(root, "testPST.pst")}) {
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
		threads, err := email.Threads(r.Read(t.Context(), email.Request{Input: filepath.Join(root, "testPST_variousBodyTypes.pst")}))
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

func TestRealThreadsIndex(t *testing.T) {
	r := realReader(t)
	root := mailFixture(t, "pst/tika")
	for _, store := range []string{"testPST.pst", "testPST_variousBodyTypes.pst"} {
		t.Run(store, func(t *testing.T) {
			var tr email.Threader
			for rec, err := range r.Read(t.Context(), email.Request{Input: filepath.Join(root, store)}) {
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

func TestRealThreadsGenerated(t *testing.T) {
	r := realReader(t)
	path := mailFixture(t, "generated/pst/mailbox.pst")
	threads, err := email.Threads(r.Read(t.Context(), email.Request{Input: path}))
	if err != nil {
		t.Fatal(err)
	}
	want := realThreads["generated/pst/mailbox.pst"]
	if got := render(threads); got != want {
		t.Errorf("threads %s, want %s", got, want)
	}
}

func TestRealThreadsMIME(t *testing.T) {
	r := realReader(t)
	inputs := mimeInputs(t)
	if got := slices.Sorted(maps.Keys(mimeThreads)); !slices.Equal(got, slices.Sorted(slices.Values(inputs))) {
		t.Errorf("mimeThreads covers %q, the inputs are %q", got, inputs)
	}
	for _, rel := range inputs {
		t.Run(rel, func(t *testing.T) {
			input := filepath.Join("testdata", builtEML)
			if rel != builtEML {
				input = mailFixture(t, rel)
			}
			threads, err := email.Threads(r.Read(t.Context(), email.Request{Input: input}))
			if err != nil {
				t.Fatal(err)
			}
			var tr email.Threader
			for rec, err := range r.Read(t.Context(), email.Request{Input: input}) {
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
			if rel == "mime/nine.mboxrd" && threads[0].MessageID != "<notes-1@example.org>" {
				t.Errorf("root Message-ID %s, want <notes-1@example.org>", threads[0].MessageID)
			}
		})
	}
}

var realThreads = map[string]string{
	"pst/tika/testPST.pst":                  "2097348 2097252[2097188] 2097220 2097316 2097284 2097380",
	"pst/tika/testPST_variousBodyTypes.pst": "2097188[2097220 2097252 2097284]",
	"generated/pst/mailbox.pst":             "2097156 2097188 2097220 2097252 2097284 2097316 2097348 2097380",
}

var mimeThreads = map[string]string{
	"generated/eml/01-plain.eml":                    "0",
	"generated/eml/02-reply-latin1.eml":             "0",
	"generated/eml/03-html-inline-image.eml":        "0",
	"generated/eml/04-attachment-cyrillic.eml":      "0",
	"generated/eml/05-japanese.eml":                 "0",
	"generated/eml/06-chinese.eml":                  "0",
	"generated/eml/07-html-only-bcc.eml":            "0",
	"generated/eml/08-forward-attached-message.eml": "0",
	"markdown/eml/10-markdown.eml":                  "0",
	"mime/eml/09-threading.eml":                     "0",
	"mime/nine.mboxrd":                              "1[2 9] 3 4 5 6 7 8",
	"mime/rlug/mbox.rlug":                           "1[4] 2 3 5",
	"mime/rlug/mbox.rlug-0":                         "0",
	"mime/rlug/mbox.rlug-1":                         "0",
	"mime/rlug/mbox.rlug-2":                         "0",
	"mime/rlug/mbox.rlug-3":                         "0",
	"mime/rlug/mbox.rlug-4":                         "0",
	builtEML:                                        "0",
}

const builtEML = "built/eml/office-attachments.eml"

func mimeInputs(t *testing.T) []string {
	t.Helper()
	root := tooltest.Tools(t, fixtures)
	var mime []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if ext := filepath.Ext(path); ext == ".eml" || ext == ".mboxrd" || strings.HasPrefix(d.Name(), "mbox.") {
			mime = append(mime, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(mime, builtEML)
}
