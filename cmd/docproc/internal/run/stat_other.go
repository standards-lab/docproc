//go:build !linux

package run

import "time"

// birth is the birth-time probe, which off Linux, with no statx, gives none; a test replaces it
// through export_test.go.
var birth = func(string) (time.Time, error) { return time.Time{}, nil }
