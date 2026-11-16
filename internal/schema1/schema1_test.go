package schema1_test

import (
	"slices"
	"testing"

	"github.com/standards-lab/docproc/internal/schema1"
	"github.com/standards-lab/docproc/internal/tooltest"
)

// TestMain runs the package's tests in a local zone that isn't UTC; they run no tool.
func TestMain(m *testing.M) {
	tooltest.Main(m, nil)
}

// TestReadStderr: a warning loses the name, its label, and the path; a failure's line its name,
// its label, and the path; a line not the program's stays as it is; blank lines are skipped. On
// failure the message is the first line that isn't a warning; on success every line is a
// warning.
func TestReadStderr(t *testing.T) {
	p := schema1.Program{Name: "prog", Labels: []string{"media not written", "refused"}}
	const path = "/w/spool-1"
	stderr := "prog: warning: message 3: odd\n" +
		"\n" +
		"prog: warning: /w/spool-1: header skipped\r\n" +
		"prog: refused: /w/spool-1: encrypted\n" +
		"prog: media not written: denied\n" +
		"Usage: prog <input>\n"

	warnings, message := p.ReadStderr(stderr, path, false)
	if want := []string{"message 3: odd", "header skipped"}; !slices.Equal(warnings, want) {
		t.Errorf("failure's warnings %q, want %q", warnings, want)
	}
	if message != "encrypted" {
		t.Errorf("message %q, want %q", message, "encrypted")
	}

	warnings, message = p.ReadStderr(stderr, path, true)
	if want := []string{"message 3: odd", "header skipped", "encrypted", "denied", "Usage: prog <input>"}; !slices.Equal(warnings, want) {
		t.Errorf("success's warnings %q, want %q", warnings, want)
	}
	if message != "" {
		t.Errorf("success's message %q, want none", message)
	}

	if _, message := p.ReadStderr("other: refused: x\n", path, false); message != "other: refused: x" {
		t.Errorf("another program's line: message %q", message)
	}
	if warnings, message := p.ReadStderr("", path, false); warnings != nil || message != "" {
		t.Errorf("empty stderr: %q, %q", warnings, message)
	}
}
