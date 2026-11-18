package integration_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/cmd/docproc/integration"
)

// writeTree writes each of files, a path under dir then its content, making its directories.
func writeTree(t *testing.T, dir string, files ...string) {
	t.Helper()
	for i := 0; i < len(files); i += 2 {
		path := filepath.Join(dir, filepath.FromSlash(files[i]))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[i+1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ReadFixtures returns the fixtures the list names, as absolute paths, in its order.
func TestReadFixtures(t *testing.T) {
	repo := t.TempDir()
	writeTree(t, repo,
		"stages/testdata/fixtures.tsv", "b/testdata/two.txt\ttext\tprepared\na/testdata/one.zip\tarchive\tprepared\n",
		"a/testdata/one.zip", "one", "b/testdata/two.txt", "two")

	got, err := integration.ReadFixtures(repo)

	want := []string{filepath.Join(repo, "b", "testdata", "two.txt"), filepath.Join(repo, "a", "testdata", "one.zip")}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ReadFixtures = %q, %v; want %q", got, err, want)
	}
}

// With no list, the fixture tree is absent: the error matches ErrNoFixtures and names the list.
func TestReadFixtures_NoTree(t *testing.T) {
	repo := t.TempDir()

	_, err := integration.ReadFixtures(repo)

	list := filepath.Join(repo, "stages", "testdata", "fixtures.tsv")
	if !errors.Is(err, integration.ErrNoFixtures) || !strings.Contains(err.Error(), list) {
		t.Errorf("ReadFixtures = %v, want an error matching ErrNoFixtures naming %s", err, list)
	}
}

// A fixture the list names that is missing from the tree is an error naming it, not an absent
// tree.
func TestReadFixtures_MissingFixture(t *testing.T) {
	repo := t.TempDir()
	writeTree(t, repo,
		"stages/testdata/fixtures.tsv", "a/testdata/one.zip\tarchive\tprepared\na/testdata/gone.pdf\tdocument\tprepared\n",
		"a/testdata/one.zip", "one")

	_, err := integration.ReadFixtures(repo)

	if err == nil || errors.Is(err, integration.ErrNoFixtures) || !strings.Contains(err.Error(), "a/testdata/gone.pdf") {
		t.Errorf("ReadFixtures = %v, want an error naming a/testdata/gone.pdf, not matching ErrNoFixtures", err)
	}
}
