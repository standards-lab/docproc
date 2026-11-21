package run

import (
	"testing"
	"time"
)

func SetBirth(t *testing.T, probe func(path string) (time.Time, error)) {
	t.Helper()
	old := birth
	birth = probe
	t.Cleanup(func() { birth = old })
}
