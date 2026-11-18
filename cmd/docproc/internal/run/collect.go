package run

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// input is one input to dispatch: its path, as the terminal prints it, the path argument it came
// from joined to its path under it; and that path and its argument, each absolute and cleaned, as
// the record writes them.
type input struct {
	path   string
	abs    string
	absArg string
	// rel is its path relative to its argument, with slashes, "." for a file argument: its
	// File's "path".
	rel string
}

// collect returns the inputs args name, in order, each file once: a file argument is one input,
// and a directory argument yields each regular file under it, in lexical order. A named argument
// that is a symbolic link is followed, to a file or a directory; a link under a directory is not,
// and is no input, like any file under it that isn't regular. An argument that is neither a file
// nor a directory, one that can't be read, and arguments that yield no input are errors. A file
// reached twice is one input, from the argument that reached it first.
func collect(args []string) ([]input, error) {
	var inputs []input
	seen := map[string]bool{}
	add := func(arg, rel string) error {
		path := filepath.Join(arg, filepath.FromSlash(rel))
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if seen[abs] {
			return nil
		}
		seen[abs] = true
		absArg, err := filepath.Abs(arg)
		if err != nil {
			return err
		}
		inputs = append(inputs, input{path: path, abs: abs, absArg: absArg, rel: rel})
		return nil
	}
	for _, arg := range args {
		arg = filepath.Clean(arg)
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		switch {
		case info.Mode().IsRegular():
			err = add(arg, ".")
		case info.IsDir():
			err = walk(arg, add)
		default:
			err = fmt.Errorf("%s is neither a file nor a directory: %s", arg, info.Mode().Type())
		}
		if err != nil {
			return nil, err
		}
	}
	if len(inputs) == 0 {
		return nil, errors.New("the arguments yield no input: no regular file is under them")
	}
	return inputs, nil
}

// walk calls add with dir and each regular file's path under it, with slashes, in lexical order.
// dir may be a symbolic link to a directory, which is followed; no link under it is.
func walk(dir string, add func(arg, rel string) error) error {
	// filepath.WalkDir follows no link, its root's included, so a linked directory is walked
	// from where it leads, each file's path taken relative to that and joined to dir again.
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return add(dir, filepath.ToSlash(rel))
	})
}
