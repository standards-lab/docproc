package dispatchtest_test

import (
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
)

// TestIndex runs the index suite against the module's two indexes, the memory index with claims
// that never expire and with a lease, so the lease's cases run too. The lease is 1 s: every case
// holds a claim across its steps unrenewed, and the lease's case renews at a third of it, so a
// shorter one lets a pause on a loaded machine end a claim the case still holds.
func TestIndex(t *testing.T) {
	t.Run("MemoryIndex", func(t *testing.T) {
		dispatchtest.Index(t, func(*testing.T) dispatch.Index { return dispatch.NewMemoryIndex(0) })
	})
	t.Run("MemoryIndex with a lease", func(t *testing.T) {
		dispatchtest.Index(t, func(*testing.T) dispatch.Index { return dispatch.NewMemoryIndex(time.Second) })
	})
	t.Run("FileIndex", func(t *testing.T) {
		dispatchtest.Index(t, func(t *testing.T) dispatch.Index {
			idx, err := dispatch.NewFileIndex(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return idx
		})
	})
}

// TestContent runs the content suite against the module's two content stores, the file store
// with no renew age and with one that outlasts the test.
func TestContent(t *testing.T) {
	t.Run("MemoryContent", func(t *testing.T) {
		dispatchtest.Content(t, func(*testing.T) dispatch.Content { return dispatch.NewMemoryContent() })
	})
	for _, renew := range []time.Duration{0, time.Hour} {
		t.Run("FileContent renewing "+renew.String(), func(t *testing.T) {
			dispatchtest.Content(t, func(t *testing.T) dispatch.Content {
				c, err := dispatch.NewFileContent(t.TempDir(), renew)
				if err != nil {
					t.Fatal(err)
				}
				return c
			})
		})
	}
}
