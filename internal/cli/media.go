package cli

import (
	"fmt"
	"path"
	"path/filepath"
)

const (
	hashLen      = 64
	extensionMax = 8
)

// Medium is a medium a line names in its `file`, resolved by the envelope's media name rule.
type Medium struct {
	// Path is the medium's file in the media directory; empty when the run was given none, and
	// the names stand alone.
	Path string
	// SHA256 is the lowercase hex SHA-256 of the medium's bytes, as its name claims it.
	SHA256 string
}

// ResolveMedium resolves name, a line's `file`, in dir, the --media directory the run was given,
// or "" for none. A name the rule can't have made, such as one holding a separator, an uppercase
// hash, or an extension past 8 characters, is an error, so no line names a file outside dir.
func ResolveMedium(dir, name string) (Medium, error) {
	if !isMediaName(name) {
		return Medium{}, fmt.Errorf("cli: %q isn't a media name", name)
	}
	m := Medium{SHA256: name[:hashLen]}
	if dir != "" {
		m.Path = filepath.Join(dir, name)
	}
	return m, nil
}

func isMediaName(name string) bool {
	if len(name) < hashLen {
		return false
	}
	for i := range hashLen {
		if c := name[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	rest := name[hashLen:]
	return rest == "" || rest[0] == '.' && isExtension(rest[1:])
}

func mediaName(sha256, stored string) string {
	ext := path.Ext(path.Base("/" + stored))
	if ext == "" || !isExtension(ext[1:]) {
		return sha256
	}
	return sha256 + ext
}

func isExtension(ext string) bool {
	if len(ext) == 0 || len(ext) > extensionMax {
		return false
	}
	for i := range len(ext) {
		c := ext[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}
