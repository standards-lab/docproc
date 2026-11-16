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

// openSet opens a set's PDF, closed when t ends.
func openSet(t *testing.T, set string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", set+".pdf"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
