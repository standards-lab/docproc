package tooltest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Tools returns the path of rel, a slash-separated path of a file or directory under the
// repository's tools/ tree, such as "envelope/cases.json" or a tool's goldens, for a test to read
// in place. The tree is found beside the go.mod nearest the working directory, the base module's,
// as `go test` runs a package's tests in its own directory.
//
// When that directory holds no tools/go.mod, the tools module isn't beside the base module, as in
// the base module's zip, which the nested go.mod keeps tools/ out of: Tools skips the test, naming
// the tree it looked for. When the tree is present and rel isn't in it, Tools fails the test.
func Tools(t testing.TB, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path, err := toolsPath(dir, rel)
	var absent *treeAbsentError
	switch {
	case errors.As(err, &absent):
		t.Skip(err)
	case err != nil:
		t.Fatal(err)
	}
	return path
}

// treeAbsentError is toolsPath's error when the base module has no tools module beside it.
type treeAbsentError struct {
	tree string
	rel  string
}

func (e *treeAbsentError) Error() string {
	return fmt.Sprintf("tooltest: no tools tree at %s, as in the module's zip, so %s isn't read", e.tree, e.rel)
}

// toolsPath returns the path of rel under the tools tree beside the go.mod nearest dir: a
// *treeAbsentError when the tree has no go.mod of its own, and an error naming rel when the tree
// is present and rel isn't.
func toolsPath(dir, rel string) (string, error) {
	root, err := moduleRoot(dir)
	if err != nil {
		return "", err
	}
	tree := filepath.Join(root, "tools")
	if _, err := os.Stat(filepath.Join(tree, "go.mod")); errors.Is(err, fs.ErrNotExist) {
		return "", &treeAbsentError{tree: tree, rel: rel}
	} else if err != nil {
		return "", fmt.Errorf("tooltest: %w", err)
	}
	path := filepath.Join(tree, filepath.FromSlash(rel))
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("tooltest: the tools tree is present, and %s isn't read: %w", rel, err)
	}
	return path, nil
}

// moduleRoot returns the nearest directory at or above dir that holds a go.mod.
func moduleRoot(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("tooltest: %w", err)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("tooltest: no go.mod at or above %s", dir)
		}
		d = parent
	}
}
