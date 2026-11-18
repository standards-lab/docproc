package run

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// birth is the birth-time probe, statxBirth; a test replaces it through export_test.go.
var birth = statxBirth

// statxBirth returns the birth time of the file at path through statx, following a symbolic
// link, or the zero time when the kernel gives none: statx's mask lacks STATX_BTIME where the
// filesystem records no birth time.
func statxBirth(path string) (time.Time, error) {
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &stx); err != nil {
		return time.Time{}, &os.PathError{Op: "statx", Path: path, Err: err}
	}
	if stx.Mask&unix.STATX_BTIME == 0 {
		return time.Time{}, nil
	}
	return time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec)), nil
}
