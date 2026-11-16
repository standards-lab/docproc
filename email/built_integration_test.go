//go:build integration && linux

package email_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/route"
)

// These tests run the real mail binary, which the unit tier doesn't provide, so they run in the
// integration tier.

// TestBuiltRoutes runs the real pinned mail over the built .msg with an attachments directory.
// Its one message holds builtSources' attachments, in order, each routed as they say and written
// under the SHA-256 name mail gives it, its content byte for byte its source's: the docx and the
// legacy xls go to office, the TIFF and the EMF to vision through office's conversion, and the
// .msg, by value, not an embedded message, is nested by its OLE2 signature and .msg extension.
func TestBuiltRoutes(t *testing.T) {
	r := realReader(t)
	dir := t.TempDir()
	recs, err := collect(r.Read(t.Context(), email.Request{Input: filepath.Join(builtDir, builtMsg), Attachments: dir}))
	if err != nil {
		t.Fatal(err)
	}
	all := attachmentsOf(recs)
	if len(all) != len(builtSources) {
		t.Fatalf("%d attachments, want %d", len(all), len(builtSources))
	}
	for i, s := range builtSources {
		a := all[i]
		t.Run(s.filename, func(t *testing.T) {
			if a.Filename != s.filename || a.Method != "by_value" {
				t.Fatalf("attachment %d: %q, method %q; want %q by value", i, a.Filename, a.Method, s.filename)
			}
			checkWritten(t, dir, a)
			content, err := os.ReadFile(a.Path)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := sha256Hex(content), sha256Hex(sourceContent(t, s)); got != want {
				t.Errorf("content SHA-256 %s, its source's %s", got, want)
			}
			rt, why, err := a.Route()
			if err != nil {
				t.Fatal(err)
			}
			if rt != s.route {
				t.Errorf("route %q (%s), want %q", rt, why, s.route)
			}
			if a.Message != nil || (rt == route.Nested && why != "OLE2 signature, .msg extension") {
				t.Errorf("route %q (%s), nests a message %t; want a by-value file routed by its signature", rt, why, a.Message != nil)
			}
			t.Logf("%s: %s (%s), stored as %s", a.Filename, rt, why, a.MimeType)
		})
	}
}

// builtEML is the built .msg's .eml source, under builtDir.
const builtEML = "eml/office-attachments.eml"
