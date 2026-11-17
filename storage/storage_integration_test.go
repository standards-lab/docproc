//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"

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

// TestLive_PutGet proves the harness: it creates a bucket of its own on the gateway, puts an
// object through go-storage's S3 provider, and reads the same bytes back. The harness keeps its
// data in tmpfs, so the bucket goes when the service stops.
func TestLive_PutGet(t *testing.T) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	cfg := gostorage.Config{
		Endpoint:  env(t, endpointEnv),
		Container: "docproc-" + hex.EncodeToString(b[:]),
		Account:   env(t, accessKeyEnv),
		Key:       env(t, secretKeyEnv),
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	client, err := s3.New(cfg)
	if err != nil {
		t.Fatalf("s3.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := client.EnsureContainer(ctx); err != nil {
		t.Fatalf("EnsureContainer: %v", err)
	}

	want := []byte("docproc's harness")
	opts := gostorage.PutOptions{Size: int64(len(want))}
	if _, err := client.Put(ctx, "harness", bytes.NewReader(want), opts); err != nil {
		t.Fatalf("Put: %v", err)
	}
	blob, err := client.Get(ctx, "harness", gostorage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = blob.Body.Close() }()
	got, err := io.ReadAll(blob.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Get returned %q, want %q", got, want)
	}
}
