//go:build integration && linux

package email_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/route"
)

// These tests run the real mail binary, which the unit tier doesn't provide, so they run in the
// integration tier.

// TestRealRoutes runs the real pinned mail over every copied fixture and MIME input with an
// attachments directory, and routes each attachment as it is read: each is routed as
// fixtureRoutes or mimeRoutes says, and each with a file has its content at Path, under the
// SHA-256 name mail gives it. Read again without a directory, nothing is written, every by-value
// attachment is unrouted, and embedded messages are still nested.
func TestRealRoutes(t *testing.T) {
	r := realReader(t)
	root, err := filepath.Abs(filepath.Join(mailCopy, "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		rel, path string
		want      []routed
	}
	var inputs []input
	stores, mime := realInputs(t)
	for _, rel := range stores {
		inputs = append(inputs, input{rel, filepath.Join(root, rel), fixtureRoutes[rel]})
	}
	for _, rel := range mime {
		inputs = append(inputs, input{rel, filepath.Join(testdata, rel), mimeRoutes[rel]})
	}
	for _, in := range inputs {
		rel := in.rel
		t.Run(rel, func(t *testing.T) {
			dir := t.TempDir()
			var got []routed
			s := mustRead(t)(r.Read(t.Context(), email.Request{Input: in.path, Attachments: dir}))
			for rec, err := range s.Records() {
				if err != nil {
					t.Fatal(err)
				}
				for _, a := range attachmentsOf([]email.Record{rec}) {
					rt, why, err := a.Route()
					if err != nil {
						t.Fatal(err)
					}
					got = append(got, routed{a.Filename, rt})
					if a.File != "" {
						checkWritten(t, dir, a)
					}
					t.Logf("%s: %s (%s)", a.Filename, rt, why)
				}
			}
			if want := in.want; !slices.Equal(got, want) {
				t.Errorf("routes %v, want %v", got, want)
			}
		})
		t.Run(rel+"/without a directory", func(t *testing.T) {
			cwd := t.TempDir()
			t.Chdir(cwd)
			recs, err := collect(r.Read(t.Context(), email.Request{Input: in.path}))
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range attachmentsOf(recs) {
				want := route.Unrouted
				if a.Method == "embedded_message" {
					want = route.Nested
				}
				if got, why, err := a.Route(); err != nil || got != want || a.Path != "" {
					t.Errorf("%s: route %q (%s), %v, path %q; want %q and no path", a.Filename, got, why, err, a.Path, want)
				}
			}
			if entries, err := os.ReadDir(cwd); err != nil || len(entries) != 0 {
				t.Errorf("working directory holds %v, %v; want nothing written", entries, err)
			}
		})
	}
}

// TestRealRoutesSinceV050 runs the real pinned mail over every copied fixture, MIME input, and
// the built .msg with an attachments directory: every attachment's route and reason are v0.5.0's,
// as v050Routes recorded them, but for those sinceV050 lists, which are its route and reason now,
// each of them changing at least once; any other change fails.
func TestRealRoutesSinceV050(t *testing.T) {
	r := realReader(t)
	want := map[string][]string{}
	data, err := os.ReadFile(v050Routes)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(f) != 4 {
			t.Fatalf("%s: line %q, want four fields", v050Routes, line)
		}
		want[f[0]] = append(want[f[0]], f[1]+": "+f[2]+" ("+f[3]+")")
	}
	stores, mime := realInputs(t)
	inputs := []string{"built/" + builtMsg}
	for _, rel := range stores {
		inputs = append(inputs, "mail/fixtures/"+rel)
	}
	inputs = append(inputs, mime...)
	changed := map[string]bool{}
	for _, rel := range inputs {
		recs, err := collect(r.Read(t.Context(), email.Request{Input: filepath.Join("testdata", rel), Attachments: t.TempDir()}))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		var got []string
		for i, a := range attachmentsOf(recs) {
			rt, why, err := a.Route()
			if err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			now := string(rt) + " (" + why + ")"
			if c, ok := sinceV050[a.Filename]; ok && i < len(want[rel]) && want[rel][i] == a.Filename+": "+c[0] && now == c[1] {
				changed[a.Filename] = true
				now = strings.TrimPrefix(want[rel][i], a.Filename+": ")
			}
			got = append(got, a.Filename+": "+now)
		}
		if !slices.Equal(got, want[rel]) {
			t.Errorf("%s: routes %q, v0.5.0's %q", rel, got, want[rel])
		}
		delete(want, rel)
	}
	for rel := range want {
		t.Errorf("%s: in the golden, not read", rel)
	}
	for name := range sinceV050 {
		if !changed[name] {
			t.Errorf("%s: listed as changed since v0.5.0, unchanged", name)
		}
	}
}

// TestRealNestedEML runs the real pinned mail over generated 08's .msg with an attachments
// directory: its forward, by value, "Notes on the engine (draft).eml", stored as message/rfc822,
// 324 bytes, is routed nested, and a second read of the file mail wrote gives the forwarded
// message: an .eml store and one message, Ada Lovelace's draft, without attachments.
func TestRealNestedEML(t *testing.T) {
	r := realReader(t)
	dir := t.TempDir()
	input := filepath.Join(mailCopy, "fixtures", "generated", "msg", "08-forward-attached-message.msg")
	recs, err := collect(r.Read(t.Context(), email.Request{Input: input, Attachments: dir}))
	if err != nil {
		t.Fatal(err)
	}
	all := attachmentsOf(recs)
	i := slices.IndexFunc(all, func(a email.Attachment) bool { return a.Filename == "Notes on the engine (draft).eml" })
	if i < 0 {
		t.Fatalf("no forwarded .eml among %d attachments", len(all))
	}
	a := all[i]
	if a.Method != "by_value" || a.MimeType != "message/rfc822" || a.Size == nil || *a.Size != 324 {
		t.Errorf("forward: method %q, type %q, size %v; want by_value message/rfc822, 324 bytes", a.Method, a.MimeType, a.Size)
	}
	if rt, why, err := a.Route(); err != nil || rt != route.Nested {
		t.Fatalf("forward: route %q (%s), %v; want %q", rt, why, err, route.Nested)
	}

	again, err := collect(r.Read(t.Context(), email.Request{Input: a.Path, Attachments: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(again))
	for i, rec := range again {
		got[i] = describe(rec)
	}
	if want := []string{"store eml", "message"}; !slices.Equal(got, want) {
		t.Fatalf("read again: %q, want %q", got, want)
	}
	m := again[1].(*email.Message)
	if m.Subject != "Notes on the engine (draft)" || m.Sender.Address != "ada@example.org" {
		t.Errorf("read again: subject %q, sender %+v; want Ada Lovelace's draft", m.Subject, m.Sender)
	}
}

// routed is one attachment's expected route: its filename and route.
type routed struct {
	filename string
	route    route.Route
}

// fixtureRoutes is the route of every attachment the copied fixtures hold, written by hand from
// the goldens, per fixture in the order a read gives them, each attachment followed by those of
// the message it nests. A fixture not listed holds none.
var fixtureRoutes = map[string][]routed{
	"generated/msg/03-html-inline-image.msg":   {{"moth.png", route.Vision}},
	"generated/msg/04-attachment-cyrillic.msg": {{"отчёт.csv", route.Text}},
	"generated/msg/08-forward-attached-message.msg": {
		{"note-g.txt", route.Text},
		// MsgKit's forward attaches the message by value, as an .eml, header-shaped and stored
		// as message/rfc822.
		{"Notes on the engine (draft).eml", route.Nested},
	},
	"generated/pst/mailbox.pst": {
		{"moth.png", route.Vision},
		{"отчёт.csv", route.Text},
		{"note-g.txt", route.Text},
	},
	// alfresco.gif is a BMP, named .gif and stored as image/gif: its signature routes it.
	"msg/poi/example_sent_regular.msg": {{"alfresco.gif", route.Vision}},
	"msg/tika/testMSG_att_msg.msg": {
		{"Test Attachment", route.Nested},
		{"smbprn.00009008.KdcPjl.pdf", route.Document},
	},
	"msg/tika/test-outlook2003.msg": outlookJPEGs,
	// The .pst stores' embedded messages, attach method 5, are listed without their content.
	"pst/java-libpst/dist-list.pst": {{"", route.Nested}, {"", route.Nested}},
	"pst/libyal/outlook.pst":        slices.Concat(outlookJPEGs, outlookJPEGs),
	"pst/tika/testPST.pst":          {{"", route.Nested}},
}

// mimeRoutes is the route of every attachment the MIME inputs hold, written by hand from the
// inputs, keyed by the input's path under testdata/ as fixtureRoutes is under fixtures/. Generated
// 03's picture goes to vision; 04's CSV and 08's text are text, by their extensions; 08's
// forwarded message, a message/rfc822 part, is an embedded message, without a filename, nested
// under its record, and holds no attachment of its own. The mboxrd holds 03, 04, and 08 among
// its nine. The built .msg's .eml source attaches builtSources, each routed as the .msg's is. An
// input not listed holds none.
var mimeRoutes = map[string][]routed{
	"mail/fixtures/generated/eml/03-html-inline-image.eml":   {{"moth.png", route.Vision}},
	"mail/fixtures/generated/eml/04-attachment-cyrillic.eml": {{"отчёт.csv", route.Text}},
	"mail/fixtures/generated/eml/08-forward-attached-message.eml": {
		{"note-g.txt", route.Text}, {"", route.Nested},
	},
	"mail/fixtures/mime/nine.mboxrd": {
		{"moth.png", route.Vision},
		{"отчёт.csv", route.Text},
		{"note-g.txt", route.Text}, {"", route.Nested},
	},
	"built/" + builtEML: builtRoutes(),
}

// builtRoutes returns builtSources' filenames and routes, in order.
func builtRoutes() []routed {
	var all []routed
	for _, s := range builtSources {
		all = append(all, routed{s.filename, s.route})
	}
	return all
}

// outlookJPEGs are the JPEGs of Outlook 2003's welcome message, 11 of them, no 11.jpg among them.
var outlookJPEGs = []routed{
	{"1.jpg", route.Vision}, {"2.jpg", route.Vision}, {"3.jpg", route.Vision},
	{"4.jpg", route.Vision}, {"5.jpg", route.Vision}, {"6.jpg", route.Vision},
	{"7.jpg", route.Vision}, {"8.jpg", route.Vision}, {"9.jpg", route.Vision},
	{"10.jpg", route.Vision}, {"12.jpg", route.Vision},
}

// v050Routes is the golden of v0.5.0's routes, under testdata/: every attachment's route and
// reason over the fixtures, the MIME inputs, and the built .msg, recorded at b05438a, before the
// route package.
var v050Routes = filepath.Join("testdata", "routes-v0.5.0.tsv")

// sinceV050 are the attachments whose route changed since v0.5.0, by filename, each its v0.5.0
// route and reason, then its route and reason now: those a route named since claims. v0.5.0 had
// no text route, so the CSV and the text file were unrouted.
var sinceV050 = map[string][2]string{
	"отчёт.csv":  {"unrouted (no routed signature)", "text (text by its .csv extension)"},
	"note-g.txt": {"unrouted (no routed signature)", "text (text by its .txt extension)"},
}

// attachmentsOf returns every attachment of recs' messages, each followed by those of the message
// it nests.
func attachmentsOf(recs []email.Record) []email.Attachment {
	var all []email.Attachment
	var walk func(m *email.Message)
	walk = func(m *email.Message) {
		for _, a := range m.Attachments {
			all = append(all, a)
			if a.Message != nil {
				walk(a.Message)
			}
		}
	}
	for _, rec := range recs {
		if m, ok := rec.(*email.Message); ok {
			walk(m)
		}
	}
	return all
}

// checkWritten checks that a's content was written at its Path in dir, under the lowercase hex
// SHA-256 of its bytes and its File's extension, its size the record's.
func checkWritten(t *testing.T, dir string, a email.Attachment) {
	t.Helper()
	if a.Path != filepath.Join(dir, a.File) {
		t.Errorf("%s: path %q, want %s in %s", a.Filename, a.Path, a.File, dir)
		return
	}
	content, err := os.ReadFile(a.Path)
	if err != nil {
		t.Errorf("%s: %v", a.Filename, err)
		return
	}
	sum := sha256.Sum256(content)
	if want := hex.EncodeToString(sum[:]) + filepath.Ext(a.File); a.File != want {
		t.Errorf("%s: file %s, its content's name %s", a.Filename, a.File, want)
	}
	if a.Size == nil || *a.Size != int64(len(content)) {
		t.Errorf("%s: size %v, the file's %d", a.Filename, a.Size, len(content))
	}
}
