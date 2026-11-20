//go:build integration && linux

package document_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// truthOf returns a set's pages' ground truth, in page order.
func truthOf(t *testing.T, set string) []string {
	t.Helper()
	var truth []string
	for page := 1; ; page++ {
		b, err := os.ReadFile(filepath.Join("testdata", set, fmt.Sprintf("page-%d.txt", page)))
		if os.IsNotExist(err) {
			return truth
		}
		if err != nil {
			t.Fatal(err)
		}
		truth = append(truth, string(b))
	}
}

// fixtureCopy returns the path of a copy of the committed fixture at testdata/name, under its
// own name in a directory of the test's own. A gs run over a page of its own works in the PDF's
// directory; a run over the copy keeps whatever it writes there out of testdata, which the stages
// package's whole-fixture test walks while this package's tests run.
func fixtureCopy(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(name))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
