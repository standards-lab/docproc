package run

import (
	"testing"
	"time"
)

// SetBirth makes probe the birth-time probe for the rest of t, so a test decides whether the
// kernel gives an input's birth time. t must not run in parallel.
func SetBirth(t *testing.T, probe func(path string) (time.Time, error)) {
	t.Helper()
	old := birth
	birth = probe
	t.Cleanup(func() { birth = old })
}
