package run

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type input struct {
	path   string
	abs    string
	absArg string
	rel    string
}

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

func walk(dir string, add func(arg, rel string) error) error {
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
