package run

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

var birth = statxBirth

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
