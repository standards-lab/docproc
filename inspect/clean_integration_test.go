//go:build integration && linux

package inspect_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

func TestCleanAttachments(t *testing.T) {
	tl, err := tool.NewExec(t.Context(), email.MailSpec())
	if err != nil {
		t.Fatalf("docproc-mail, built from tools/mail and put on PATH: %v", err)
	}
	reader, err := email.NewReader(email.Config{Mail: tl})
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	for _, root := range []string{tooltest.Tools(t, "mail/tests/fixtures"), "../email/testdata"} {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch ext := filepath.Ext(path); {
			case ext == ".pst" || ext == ".msg" || ext == ".eml" || ext == ".mboxrd" || strings.HasPrefix(d.Name(), "mbox."):
				inputs = append(inputs, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, input := range inputs {
		dir := t.TempDir()
		for rec, err := range reader.Read(t.Context(), email.Request{Input: input, Media: dir}) {
			if err != nil {
				t.Fatalf("%s: %v", input, err)
			}
			if msg, ok := rec.(*email.Message); ok {
				n += inspectAttachments(t, input, msg)
			}
		}
	}
	t.Logf("inspected %d attachments of %d inputs", n, len(inputs))
	if n == 0 {
		t.Error("no attachment inspected")
	}
}

var attachmentFlags = map[string][]inspect.Check{
	"example_sent_regular.msg alfresco.gif": {inspect.SignatureMismatch},
}

func inspectAttachments(t *testing.T, input string, msg *email.Message) int {
	t.Helper()
	n := 0
	for _, a := range msg.Attachments {
		if a.Message != nil {
			n += inspectAttachments(t, input, a.Message)
		}
		if a.Path == "" {
			continue
		}
		if _, err := os.Stat(a.Path); err != nil {
			t.Errorf("%s: %q: %v", input, a.Filename, err)
			continue
		}
		n++
		name := a.Filename
		if name == "" {
			name = a.File
		}
		r := mustInspect(t, a.Path, name, inspect.Options{})
		if got, want := checks(r), attachmentFlags[filepath.Base(input)+" "+a.Filename]; !slices.Equal(got, want) {
			t.Errorf("%s: %q: checks %v, want %v (%+v)", input, a.Filename, got, want, r.Flags)
		}
		if r.Verdict != inspect.Proceed {
			t.Errorf("%s: %q: verdict %s", input, a.Filename, r.Verdict)
		}
	}
	return n
}
