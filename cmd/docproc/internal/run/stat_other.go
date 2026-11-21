//go:build !linux

package run

import "time"

var birth = func(string) (time.Time, error) { return time.Time{}, nil }
