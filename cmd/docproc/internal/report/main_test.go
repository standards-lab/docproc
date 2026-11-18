package report_test

import (
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
)

// TestMain runs the tests in a local zone that isn't UTC, so a page that read the local zone
// would differ from its golden; they run no tool.
func TestMain(m *testing.M) {
	tooltest.Main(m, nil)
}
