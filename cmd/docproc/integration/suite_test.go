//go:build integration

package integration_test

import (
	"testing"

	"github.com/standards-lab/docproc/cmd/docproc/integration"
)

func TestMain(m *testing.M) { integration.Main(m) }
