package stages_test

import (
	"testing"
	"time"

	"github.com/standards-lab/docproc/stages"
)

func TestZZDiag(t *testing.T) {
	t.Parallel()
	for range 64 {
		t.Run("p", func(t *testing.T) {
			t.Parallel()
			standinSet(t, onenoteSettings{Stderr: "docproc-onenote: error: x\n", Exit: 3}, stages.Options{Exclude: onenoteOnly(), Memory: testMemory, Idle: 5 * time.Second})
		})
	}
}
