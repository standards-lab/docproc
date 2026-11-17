//go:build integration

package storage_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/storage"
	gostorage "github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/s3"
)

// The environment the live tier reads: the S3 gateway's URL and the credential it accepts, which
// the root mise.toml sets for the compose harness's SeaweedFS.
const (
	endpointEnv  = "DOCPROC_S3_ENDPOINT"
	accessKeyEnv = "DOCPROC_S3_ACCESS_KEY"
	secretKeyEnv = "DOCPROC_S3_SECRET_KEY"
)

// env returns the variable's value, failing the test when it is unset: the integration tier
// states that the gateway is expected.
func env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is not set; run with the S3 gateway it names up", name)
	}
	return v
}

// random returns a random name of n bytes in hex.
func random(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// liveConfig returns the finalized go-storage configuration of a bucket of the test's own on the
// gateway, which nothing has created yet. The harness keeps its data in tmpfs, so the bucket goes
// when the service stops.
func liveConfig(t *testing.T) gostorage.Config {
	t.Helper()
	cfg := gostorage.Config{
		Endpoint:  env(t, endpointEnv),
		Container: "docproc-" + random(t, 6),
		Account:   env(t, accessKeyEnv),
		Key:       env(t, secretKeyEnv),
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return cfg
}

// liveClient returns go-storage's S3 provider over cfg.
func liveClient(t *testing.T, cfg gostorage.Config) *s3.Client {
	t.Helper()
	client, err := s3.New(cfg)
	if err != nil {
		t.Fatalf("s3.New: %v", err)
	}
	return client
}

// TestLive_Suite runs the Content suite over go-storage's S3 provider, through a started Store,
// in one bucket of the test's own, each case's store under a prefix of its own, so each starts
// empty.
func TestLive_Suite(t *testing.T) {
	cfg := liveConfig(t)
	store := gostorage.New(liveClient(t, cfg), cfg)
	if err := store.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = store.Shutdown(context.Background()) }) // the S3 client closes nothing
	dispatchtest.Content(t, func(t *testing.T) dispatch.Content {
		c, err := storage.New(storage.Config{Client: store, Prefix: random(t, 6) + "/", Renew: time.Hour})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	})
}

// TestLive_MissingContainer checks that a bucket the gateway doesn't have is no miss: an open
// fails, never matching fs.ErrNotExist, and a put fails too.
func TestLive_MissingContainer(t *testing.T) {
	cfg := liveConfig(t)
	c, err := storage.New(storage.Config{Client: liveClient(t, cfg), Prefix: "docproc/", Renew: time.Hour})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sum := hash([]byte("nowhere"))
	_, err = c.Open(t.Context(), sum)
	if err == nil || errors.Is(err, fs.ErrNotExist) || !errors.Is(err, gostorage.ErrContainerNotFound) {
		t.Errorf("open in a missing bucket: %v, want ErrContainerNotFound, not fs.ErrNotExist", err)
	}
	err = c.Put(t.Context(), sum, strings.NewReader("nowhere"))
	if err == nil || errors.Is(err, fs.ErrNotExist) || !errors.Is(err, gostorage.ErrContainerNotFound) {
		t.Errorf("put in a missing bucket: %v, want ErrContainerNotFound, not fs.ErrNotExist", err)
	}
}
