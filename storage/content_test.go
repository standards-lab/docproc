package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/storage"
	gostorage "github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

const prefix = "docproc/content/"

func newStore(t *testing.T, fake *storagetest.Fake, maxSize int64) *gostorage.Store {
	t.Helper()
	cfg := gostorage.Config{Container: "docproc", MaxObjectSize: maxSize}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	s := gostorage.New(fake, cfg)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s
}

func newContent(t *testing.T, client gostorage.Client, renew time.Duration) *storage.Content {
	t.Helper()
	c, err := storage.New(storage.Config{Client: client, Prefix: prefix, Renew: renew})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestContent_Suite(t *testing.T) {
	dispatchtest.Content(t, func(t *testing.T) dispatch.Content {
		return newContent(t, newStore(t, storagetest.NewFake(storagetest.Options{}), 0), time.Hour)
	})
}

func TestNew_NilClient(t *testing.T) {
	defer func() {
		if got, want := recover(), "storage: Config.Client is nil"; got != want {
			t.Errorf("panic %v, want %q", got, want)
		}
	}()
	_, _ = storage.New(storage.Config{Prefix: prefix, Renew: time.Hour})
}

func TestNew_NegativeRenew(t *testing.T) {
	client := storagetest.NewFake(storagetest.Options{})
	if _, err := storage.New(storage.Config{Client: client, Renew: -time.Second}); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("New with a negative renew age: %v, want ErrRequest", err)
	}
}

func TestContent_Key(t *testing.T) {
	fake := storagetest.NewFake(storagetest.Options{})
	c := newContent(t, newStore(t, fake, 0), time.Hour)
	b := []byte("laid out")
	sum := hash(b)
	if err := c.Put(t.Context(), sum, bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
	blob, err := fake.Get(t.Context(), prefix+sum[:2]+"/"+sum, gostorage.GetOptions{})
	if err != nil {
		t.Fatalf("get the content's key: %v", err)
	}
	defer func() { _ = blob.Body.Close() }()
	if got, err := io.ReadAll(blob.Body); err != nil || !bytes.Equal(got, b) {
		t.Errorf("the content's key holds %q (%v), want %q", got, err, b)
	}
}

func TestContent_Renew(t *testing.T) {
	const renew = time.Hour
	for _, tc := range []struct {
		name    string
		renew   time.Duration
		age     time.Duration
		rewrite bool
	}{
		{"past the age", renew, 2 * renew, true},
		{"within the age", renew, renew / 2, false},
		{"a renew age of zero", 0, 100 * 365 * 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var behind atomic.Int64
			behind.Store(int64(tc.age))
			fake := storagetest.NewFake(storagetest.Options{Clock: func() time.Time {
				return time.Now().Add(-time.Duration(behind.Load()))
			}})
			c := newContent(t, newStore(t, fake, 0), tc.renew)
			b := []byte("renewed")
			sum := hash(b)
			if err := c.Put(t.Context(), sum, bytes.NewReader(b)); err != nil {
				t.Fatal(err)
			}
			behind.Store(0)
			if err := c.Put(t.Context(), sum, bytes.NewReader(b)); err != nil {
				t.Fatalf("second put: %v", err)
			}
			want := int64(1)
			if tc.rewrite {
				want = 2
			}
			if got := fake.Puts(); got != want {
				t.Errorf("the Fake received %d puts, want %d", got, want)
			}
			obj, err := fake.Stat(t.Context(), prefix+sum[:2]+"/"+sum)
			if err != nil {
				t.Fatal(err)
			}
			if age := time.Since(obj.ModifiedAt); (age < tc.age/2) != tc.rewrite {
				t.Errorf("the content's age after the second put is %s; rewritten: %t", age, tc.rewrite)
			}
		})
	}
}

func TestContent_TooLarge(t *testing.T) {
	c := newContent(t, newStore(t, storagetest.NewFake(storagetest.Options{}), 4), time.Hour)
	b := []byte("too large")
	sum := hash(b)
	err := c.Put(t.Context(), sum, bytes.NewReader(b))
	if !errors.Is(err, dispatch.ErrTooLarge) || !errors.Is(err, gostorage.ErrTooLarge) {
		t.Errorf("put past the bound: %v, want dispatch's and go-storage's ErrTooLarge", err)
	}
	if _, err := c.Open(t.Context(), sum); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("open after a put past the bound: %v, want fs.ErrNotExist", err)
	}
	small := []byte("fits")
	if err := c.Put(t.Context(), hash(small), bytes.NewReader(small)); err != nil {
		t.Errorf("put within the bound: %v", err)
	}
}

func TestContent_MissingContainer(t *testing.T) {
	fake := storagetest.NewFake(storagetest.Options{})
	c := newContent(t, newStore(t, fake, 0), time.Hour)
	fake.DropContainer()
	sum := hash([]byte("nowhere"))
	_, err := c.Open(t.Context(), sum)
	if err == nil || errors.Is(err, fs.ErrNotExist) || !errors.Is(err, gostorage.ErrContainerNotFound) {
		t.Errorf("open in a missing container: %v, want ErrContainerNotFound, not fs.ErrNotExist", err)
	}
	err = c.Put(t.Context(), sum, strings.NewReader("nowhere"))
	if err == nil || errors.Is(err, fs.ErrNotExist) || !errors.Is(err, gostorage.ErrContainerNotFound) {
		t.Errorf("put in a missing container: %v, want ErrContainerNotFound, not fs.ErrNotExist", err)
	}
}
