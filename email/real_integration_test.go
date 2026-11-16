//go:build integration && linux

package email_test

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

// These tests run the real docproc-mail, built from tools/mail in this checkout and found on PATH,
// which the unit tier doesn't provide, so they run in the integration tier.

// fixtures is docproc-mail's committed inputs, read in place under tools/.
const fixtures = "mail/tests/fixtures"

// inputExtensions are the extensions of docproc-mail's committed inputs, as its golden test
// names them; mbox.rlug's splits, mbox.rlug-0 to -4, are .eml inputs of their own without
// goldens.
var inputExtensions = []string{".pst", ".msg", ".eml", ".mboxrd", ".rlug"}

// TestRealGoldens runs the real docproc-mail over every committed input that has a golden, with a
// media directory, and holds its records to the golden as TestReplayGoldens does: one record per
// line, each written back as its line, the senders' added SMTP addresses their headers', and the
// golden's warnings. Each attachment's file is in the media directory, its content's SHA-256 its
// name's and its size the record's.
func TestRealGoldens(t *testing.T) {
	r := realReader(t)
	root := tooltest.Tools(t, goldens)
	in := tooltest.Tools(t, fixtures)
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		stem := strings.TrimSuffix(path, ".jsonl")
		rel, err := filepath.Rel(root, stem)
		if err != nil {
			return err
		}
		input := ""
		for _, ext := range inputExtensions {
			if _, err := os.Stat(filepath.Join(in, rel+ext)); err == nil {
				input = filepath.Join(in, rel+ext)
			}
		}
		n++
		t.Run(rel, func(t *testing.T) {
			if input == "" {
				t.Fatalf("no input for the golden %s", rel)
			}
			media := t.TempDir()
			recs, err := collect(r.Read(t.Context(), email.Request{Input: input, Media: media}))
			if err != nil {
				t.Fatal(err)
			}
			checkReplay(t, stem, media, recs)
			for _, rec := range recs {
				if m, ok := rec.(*email.Message); ok {
					checkWritten(t, m)
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatalf("no goldens under %s", root)
	}
}

// checkWritten checks that each of m's attachments and media with a file, a nested message's
// included, was written at its Path, its content's SHA-256 its SHA256 and an attachment's size the
// record's.
func checkWritten(t *testing.T, m *email.Message) {
	t.Helper()
	check := func(file, path, sum string, size *int64) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			return
		}
		got := sha256.Sum256(content)
		if hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: its content's SHA-256 is %x", file, got)
		}
		if size != nil && *size != int64(len(content)) {
			t.Errorf("%s: size %d, the file's %d", file, *size, len(content))
		}
	}
	for _, a := range m.Attachments {
		if a.File != "" {
			check(a.File, a.Path, a.SHA256, a.Size)
		}
		if a.Message != nil {
			checkWritten(t, a.Message)
		}
	}
	for _, md := range m.Media {
		check(md.File, md.Path, md.SHA256, nil)
	}
}

// TestRealBuilt runs the real docproc-mail over the built .msg with a media directory. Its one
// message holds builtSources' attachments, in order, each by value and written as its source, byte
// for byte; the forwarded .msg, whose source docproc-mail's fixtures no longer hold byte for byte,
// as an OLE2 compound file, a file and not an embedded message.
func TestRealBuilt(t *testing.T) {
	r := realReader(t)
	media := t.TempDir()
	recs, err := collect(r.Read(t.Context(), email.Request{Input: filepath.Join("testdata", "built", "msg", "office-attachments.msg"), Media: media}))
	if err != nil {
		t.Fatal(err)
	}
	var m *email.Message
	for _, rec := range recs {
		if msg, ok := rec.(*email.Message); ok {
			m = msg
		}
	}
	if m == nil || len(m.Attachments) != len(builtSources) {
		t.Fatalf("message %+v, want %d attachments", m, len(builtSources))
	}
	checkWritten(t, m)
	for i, s := range builtSources {
		a := m.Attachments[i]
		if a.Filename != s.filename || a.Method != "by_value" || a.Message != nil {
			t.Errorf("attachment %d: %q, method %q, nested %t; want %q by value", i+1, a.Filename, a.Method, a.Message != nil, s.filename)
			continue
		}
		got, err := os.ReadFile(a.Path)
		if err != nil {
			t.Fatal(err)
		}
		if s.path == "" {
			if !strings.HasPrefix(string(got), "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1") {
				t.Errorf("%s: not an OLE2 compound file", s.filename)
			}
			continue
		}
		if want := s.content(t); string(got) != string(want) {
			t.Errorf("%s: %d bytes, not its source's %d", s.filename, len(got), len(want))
		}
	}
}

// builtSource is one of the built .msg's attachments: its filename and its source, a file, or a
// member of a ZIP, under the repository; none for the forwarded .msg.
type builtSource struct {
	filename, path, member string
}

// builtSources are the built .msg's attachments, in order, as testdata/README.md gives them.
var builtSources = []builtSource{
	{"minutes.docx", "../office/testdata/ooxml/tables.libreoffice.docx", ""},
	{"budget.xls", "../office/testdata/legacy/generated/formulas.libreoffice.xls", ""},
	{"scan.tif", "../office/testdata/pictures/tiff.docx", "word/media/image1.tiff"},
	{"diagram.emf", "../office/testdata/ooxml/word-with-attachments.docx", "word/media/image4.emf"},
	{"forwarded.msg", "", ""},
}

// content returns s's source's bytes.
func (s builtSource) content(t *testing.T) []byte {
	t.Helper()
	if s.member == "" {
		b, err := os.ReadFile(s.path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return zipMember(t, s.path, s.member)
}

// zipMember returns the bytes of member in the ZIP at path.
func zipMember(t *testing.T, path, member string) []byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	f, err := zr.Open(member)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestRealMboxOrdinals: an mbox message's id is its ordinal in the file, so an empty entry, a
// separator line with the next straight after it, skips a number, and a message docproc-mail can't
// read, one whose header block has no From or Date, is a skipped record of kind message whose id
// is its ordinal, with a warning naming it.
func TestRealMboxOrdinals(t *testing.T) {
	r := realReader(t)
	input := filepath.Join(t.TempDir(), "ordinals.mbox")
	content := "From a\nFrom: Ada <ada@example.org>\nSubject: one\n\nfirst\n\n" +
		"From b\n" +
		"From c\nFrom: Ada <ada@example.org>\nSubject: three\n\nthird\n\n" +
		"From d\nSubject: four\n\nno sender, no date\n"
	if err := os.WriteFile(input, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := collect(r.Read(t.Context(), email.Request{Input: input}))
	if err != nil {
		t.Fatal(err)
	}
	var got, warnings []string
	for _, rec := range recs {
		if _, ok := rec.(*email.Warning); ok {
			warnings = append(warnings, describe(rec))
			continue
		}
		got = append(got, describe(rec))
	}
	if want := []string{"store mbox", "message 1", "message 3", "skipped message id 4"}; !slices.Equal(got, want) {
		t.Errorf("records %q, want %q", got, want)
	}
	if want := []string{"warning message 4: skipped: it has no header block with a From or Date field"}; !slices.Equal(warnings, want) {
		t.Errorf("warnings %q, want %q", warnings, want)
	}
}

// TestRealRefused: the real docproc-mail refuses an .ost, and a file that is no mail, neither a
// .pst, a .msg, an .eml, nor an mbox, with exit code 3, and the error carries its reason, which
// isn't classed, without the input's path.
func TestRealRefused(t *testing.T) {
	r := realReader(t)
	// An .ost's header: a PST's, its client magic SO, padded to the header's size.
	ost := append([]byte("!BDN\x00\x00\x00\x00SO\x24\x00"), make([]byte, 552)...)
	cases := []struct {
		name    string
		content []byte
		reason  string
	}{
		{"mailbox.ost", ost, "an .ost, the offline cache of a server mailbox"},
		{"notes.txt", []byte("plain text, not a store"), "neither a .pst, a .msg, an .eml nor an mbox"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.name)
			if err := os.WriteFile(path, c.content, 0o600); err != nil {
				t.Fatal(err)
			}
			recs, err := collect(r.Read(t.Context(), email.Request{Input: path}))
			exit, ok := errors.AsType[*cli.ExitError](err)
			if !errors.Is(err, email.ErrRefused) || !ok || exit.Code != 3 || exit.Refusal() != cli.Unclassed {
				t.Fatalf("got %v, want an unclassed refusal, exit code 3", err)
			}
			if len(recs) != 0 {
				t.Errorf("records %v, want none", recs)
			}
			if strings.Contains(exit.Message, path) || !strings.HasPrefix(exit.Message, c.reason) {
				t.Errorf("message %q: want docproc-mail's reason alone, beginning %q", exit.Message, c.reason)
			}
		})
	}
}

// realReader builds a Reader over the real docproc-mail on PATH, through the exec runner.
func realReader(t *testing.T) *email.Reader {
	t.Helper()
	tl, err := tool.NewExec(t.Context(), email.MailSpec())
	if err != nil {
		t.Fatalf("docproc-mail, built from tools/mail and put on PATH: %v", err)
	}
	r, err := email.NewReader(email.Config{Mail: tl})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// mailFixture returns the path of rel, a path under docproc-mail's tests/fixtures, read in place.
func mailFixture(t *testing.T, rel string) string {
	t.Helper()
	return tooltest.Tools(t, fixtures+"/"+rel)
}

// describe gives rec's kind, its ids, and its skips in a few words: a store's format; a message's
// id, if any, and each attachment's method, an embedded message's subject after it; a skipped
// record's kind, its message, and its id; a warning's where and what.
func describe(rec email.Record) string {
	switch rec := rec.(type) {
	case *email.Store:
		return "store " + rec.Format
	case *email.Folder:
		return "folder " + strconv.FormatUint(rec.ID, 10)
	case *email.Message:
		s := "message"
		if rec.ID != 0 {
			s += " " + strconv.FormatUint(rec.ID, 10)
		}
		if len(rec.Attachments) > 0 {
			var methods []string
			for _, a := range rec.Attachments {
				m := a.Method
				if a.Message != nil {
					m += "(" + a.Message.Subject + ")"
				}
				methods = append(methods, m)
			}
			s += " [" + strings.Join(methods, " ") + "]"
		}
		return s
	case *email.Skipped:
		s := "skipped " + rec.Kind
		if rec.Message != 0 {
			s += " message " + strconv.FormatUint(rec.Message, 10)
		}
		if rec.ID != nil {
			s += " id " + strconv.FormatUint(*rec.ID, 10)
		}
		return s
	case *email.Warning:
		return "warning " + rec.Where + ": " + rec.What
	case *email.UnknownRecord:
		return rec.Kind
	}
	return "?"
}
