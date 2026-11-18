//go:build integration

package integration_test

import (
	"testing"

	"github.com/standards-lab/docproc/cmd/docproc/integration"
)

// The suite's TestMain builds docproc once for the run. The unit tier never enters here: without
// the tag the harness type-checks and needs no binary.
func TestMain(m *testing.M) { integration.Main(m) }
