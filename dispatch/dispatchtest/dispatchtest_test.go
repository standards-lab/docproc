package dispatchtest_test

import (
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
)

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
