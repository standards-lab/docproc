//go:build integration && linux

package office_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRTFRoundTrip converts each committed RTF through soffice in the default image to docx, then
// docproc-ooxml. Each result is converted from "rtf", and its Markdown holds the text the RTF
// holds, as its source reads, each line given here as office's Markdown writes it: memo.rtf's
// heading, its paragraph with a windows-1252 é and a bold run, which office's Markdown writes
// plain, its table's rows, and its last paragraph; hidden-entry.rtf's paragraph, one sentence
// eleven times. The title above that paragraph is a \tc table-of-contents entry, hidden text RTF
// doesn't display, and the Markdown holds none of it. No golden Markdown is recorded: the asserted
// lines are the content, not LibreOffice's layout.
func TestRTFRoundTrip(t *testing.T) {
	c := containerConverter(t)
	sentence := "Sluwe Sjaantje sloeg de slome slager."
	for _, tc := range []struct {
		path   string
		want   []string
		hidden string
	}{
		{filepath.Join("testdata", "rtf", "memo.rtf"), []string{
			"# Harbor memo",
			"The survey boat left at dawn and returned with three samples from the café pier.",
			"| Sample | Depth |",
			"| North | 12 m |",
			"| South | 9 m |",
			"Next survey: Thursday.",
		}, ""},
		{filepath.Join("testdata", "rtf", "hidden-entry.rtf"), []string{
			strings.TrimSpace(strings.Repeat(sentence+" ", 11)),
		}, "Harbor title"},
	} {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			res := convertPath(t, c, tc.path)
			if res.doc.ConvertedFrom != "rtf" {
				t.Errorf("converted from %q, want rtf", res.doc.ConvertedFrom)
			}
			for _, w := range res.warnings {
				t.Logf("warning: %s: %s", w.Where, w.What)
			}
			lines := map[string]bool{}
			for line := range strings.Lines(res.markdown) {
				lines[strings.TrimSpace(line)] = true
			}
			for _, w := range tc.want {
				if !lines[w] {
					t.Errorf("no line %q in the Markdown:\n%s", w, res.markdown)
				}
			}
			if tc.hidden != "" && strings.Contains(res.markdown, tc.hidden) {
				t.Errorf("the hidden %q is in the Markdown:\n%s", tc.hidden, res.markdown)
			}
		})
	}
}
