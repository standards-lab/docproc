package dispatch_test

import (
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
)

// TestMain runs the tests in a local zone that isn't UTC; they run no tool.
func TestMain(m *testing.M) {
	tooltest.Main(m, nil)
}

// wantPanic fails the test unless build panics with want, a constructor's naming of the nil field
// that is the caller's mistake.
func wantPanic(t *testing.T, want string, build func()) {
	t.Helper()
	defer func() {
		t.Helper()
		if got := recover(); got != want {
			t.Errorf("panic %v, want %q", got, want)
		}
	}()
	build()
}
