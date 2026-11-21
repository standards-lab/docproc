package dispatchtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
)

// Content runs the content store's contract against the stores newContent makes, a new, empty one
// for each case, whose renew age, if it has one, outlasts the test.
func Content(t *testing.T, newContent func(t *testing.T) dispatch.Content) {
	t.Helper()
	ctx := context.Background()

	t.Run("put then open", func(t *testing.T) {
		c := newContent(t)
		for _, b := range [][]byte{[]byte("hello"), {}, bytes.Repeat([]byte("0123456789abcdef"), 1<<16)} {
			sum := hash(b)
			if err := c.Put(ctx, sum, bytes.NewReader(b)); err != nil {
				t.Fatalf("put %d bytes: %v", len(b), err)
			}
			if got := open(t, c, sum); !bytes.Equal(got, b) {
				t.Errorf("open %d bytes: got %d bytes", len(b), len(got))
			}
		}
	})

	t.Run("open misses", func(t *testing.T) {
		c := newContent(t)
		if _, err := c.Open(ctx, hash([]byte("absent"))); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("open of an absent content: %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("put is all or nothing", func(t *testing.T) {
		c := newContent(t)
		sum := hash([]byte("whole"))
		r := io.MultiReader(strings.NewReader("who"), failing{})
		if err := c.Put(ctx, sum, r); err == nil {
			t.Fatal("put of a reader that fails: no error")
		}
		if _, err := c.Open(ctx, sum); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("open after a failed put: %v, want fs.ErrNotExist", err)
		}
		ended, cancel := context.WithCancel(ctx)
		cancel()
		if err := c.Put(ended, sum, strings.NewReader("whole")); err == nil {
			t.Error("put under an ended context: no error")
		}
		if _, err := c.Open(ctx, sum); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("open after a put under an ended context: %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("put of a present content reads nothing", func(t *testing.T) {
		c := newContent(t)
		b := []byte("present")
		sum := hash(b)
		if err := c.Put(ctx, sum, bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
		if err := c.Put(ctx, sum, failing{}); err != nil {
			t.Errorf("put of a present content: %v", err)
		}
		if got := open(t, c, sum); !bytes.Equal(got, b) {
			t.Errorf("open after a second put: %q, want %q", got, b)
		}
	})

	t.Run("concurrent puts", func(t *testing.T) {
		c := newContent(t)
		b := bytes.Repeat([]byte("concurrent"), 4096)
		sum := hash(b)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() { errs <- c.Put(ctx, sum, bytes.NewReader(b)) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Errorf("concurrent put: %v", err)
			}
		}
		if got := open(t, c, sum); !bytes.Equal(got, b) {
			t.Errorf("open after concurrent puts: %d bytes, want %d", len(got), len(b))
		}
	})

	t.Run("a name that isn't a SHA-256 is refused", func(t *testing.T) {
		c := newContent(t)
		for _, name := range []string{"", "abc", strings.ToUpper(hash(nil)), "../" + hash(nil)[3:], hash(nil) + "0"} {
			if err := c.Put(ctx, name, strings.NewReader("x")); !errors.Is(err, dispatch.ErrRequest) {
				t.Errorf("put %q: %v, want ErrRequest", name, err)
			}
			if _, err := c.Open(ctx, name); !errors.Is(err, dispatch.ErrRequest) {
				t.Errorf("open %q: %v, want ErrRequest", name, err)
			}
		}
	})
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func open(t *testing.T, c dispatch.Content, sum string) []byte {
	t.Helper()
	rc, err := c.Open(context.Background(), sum)
	if err != nil {
		t.Fatalf("open %s: %v", sum, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", sum, err)
	}
	return b
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("dispatchtest: the reader fails") }
