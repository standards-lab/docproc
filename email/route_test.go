//go:build linux

package email_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/route"
)

// Constructed content, each its format's signature followed by filler. route's tests hold the
// signatures' cases; these route an attachment through route.File.
var (
	pdfBytes  = "%PDF-1.7\n%filler"
	tiffLE    = "II*\x00\x08\x00\x00\x00"
	ole2Bytes = "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1\x00\x00\x00\x00"
)

// writeContent writes content into dir under its SHA-256 and ext, as mail names it, and returns
// the name.
func writeContent(t *testing.T, dir, content, ext string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	name := hex.EncodeToString(sum[:]) + ext
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

// TestRouteName: within a container the extension is Filename's, else File's when the record has
// no Filename; a File's extension never overrides a Filename's.
func TestRouteName(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, filename, fileExt string
		want                    route.Route
	}{
		{"file's extension, no filename", "", ".xls", route.Office},
		{"filename's extension over file's", "budget.xls", ".bin", route.Office},
		{"filename without one, file's ignored", "budget", ".xls", route.Unrouted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeContent(t, dir, ole2Bytes+c.name, c.fileExt)
			a := email.Attachment{File: file, Filename: c.filename, Method: "by_value", Path: filepath.Join(dir, file)}
			if got, why, err := a.Route(); err != nil || got != c.want {
				t.Errorf("route %q (%s), %v; want %q", got, why, err, c.want)
			}
		})
	}
}

// TestRouteWithoutContent: an embedded message is nested, with or without its content; an
// attachment with no content file, one read without a directory, and one whose file wasn't
// written are unrouted; a file that is there but can't be read is an error.
func TestRouteWithoutContent(t *testing.T) {
	dir := t.TempDir()
	pdf := writeContent(t, dir, pdfBytes, ".pdf")
	cases := []struct {
		name string
		a    email.Attachment
		want route.Route
	}{
		{"embedded message in a .msg", email.Attachment{Method: "embedded_message", Message: &email.Message{Subject: "Inner"}}, route.Nested},
		{"embedded message in a .pst", email.Attachment{Method: "embedded_message"}, route.Nested},
		{"by reference", email.Attachment{Filename: "share.pdf", Method: "by_reference"}, route.Unrouted},
		{"no directory", email.Attachment{File: pdf, Filename: "a.pdf", Method: "by_value"}, route.Unrouted},
		{"not written", email.Attachment{File: "missing.pdf", Filename: "a.pdf", Method: "by_value", Path: filepath.Join(dir, "missing.pdf")}, route.Unrouted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why, err := c.a.Route()
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || why == "" {
				t.Errorf("route %q (%s), want %q", got, why, c.want)
			}
		})
	}
	t.Run("unreadable", func(t *testing.T) {
		sub := filepath.Join(dir, "a-directory.pdf")
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		a := email.Attachment{File: "a-directory.pdf", Method: "by_value", Path: sub}
		if got, _, err := a.Route(); err == nil {
			t.Errorf("route %q, want an error", got)
		}
	})
}

// attachmentLines are a .msg's lines whose attachments have files, one nested in an embedded
// message, and one that names no bare file.
const attachmentLines = `{"line":"store","schema":"1","format":"msg"}
{"line":"message","class":"IPM.Note","attachments":[{"file":"aa.pdf","filename":"a.pdf","method":"by_value","size":1},{"filename":"Fwd","method":"embedded_message","message":{"class":"IPM.Note","attachments":[{"file":"bb.png","filename":"b.png","method":"by_value","size":1}]}},{"filename":"share","method":"by_reference"},{"file":"../escape.pdf","method":"by_value","size":1}]}
`

// TestReadAttachments: a Request's attachments directory, made absolute, is passed to mail
// before the input, and each attachment with a bare File, nested ones included, is given its
// Path there; without a directory none is passed and no attachment has a Path.
func TestReadAttachments(t *testing.T) {
	r, mail := standinReader(t)
	mail.stdout(attachmentLines)
	cwd := t.TempDir()
	t.Chdir(cwd)
	dir, input := filepath.Join(cwd, "att"), filepath.Join(cwd, "in.msg")

	recs, err := collect(r.Read(t.Context(), email.Request{Input: "in.msg", Attachments: "att"}))
	if err != nil {
		t.Fatal(err)
	}
	m := recs[1].(*email.Message)
	got := []string{m.Attachments[0].Path, m.Attachments[1].Message.Attachments[0].Path, m.Attachments[2].Path, m.Attachments[3].Path}
	want := []string{filepath.Join(dir, "aa.pdf"), filepath.Join(dir, "bb.png"), "", ""}
	if !slices.Equal(got, want) {
		t.Errorf("paths %q, want %q", got, want)
	}

	recs, err = collect(r.Read(t.Context(), email.Request{Input: "in.msg"}))
	if err != nil {
		t.Fatal(err)
	}
	m = recs[1].(*email.Message)
	if p := m.Attachments[0].Path + m.Attachments[1].Message.Attachments[0].Path; p != "" {
		t.Errorf("paths %q without a directory, want none", p)
	}

	wantRuns := [][]string{{"--attachments", dir, input}, {input}}
	if all := mail.runs(); !reflect.DeepEqual(all, wantRuns) {
		t.Errorf("runs %q, want %q", all, wantRuns)
	}
}
