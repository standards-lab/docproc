package rundir_test

import (
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
)

// TestMain runs the package's tests in a local zone that isn't UTC; they run no tool.
func TestMain(m *testing.M) {
	tooltest.Main(m, nil)
}
