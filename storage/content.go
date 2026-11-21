package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	gostorage "github.com/standards-lab/go-storage"
)

var _ dispatch.Content = (*Content)(nil)

// Config configures a [Content].
type Config struct {
	// Client is the store the contents are kept in, such as a go-storage Store over a provider;
	// it is required, and its container must exist.
	Client gostorage.Client
	// Prefix begins every content's key, joined as given; empty keeps the contents at the
	// container's root.
	Prefix string
	// Renew is the renew age: a content put again once its object is older than it is
	// rewritten. Zero never rewrites.
	Renew time.Duration
}

// Content is a [dispatch.Content] over a go-storage Client, each content an object at its key,
// as the package says. A put is the Client's Put, all or nothing. A sha256 that isn't 64
// lowercase hex digits is refused, matching [dispatch.ErrRequest]. It is safe for concurrent use
// as its Client is. Its zero value is not ready; [New] makes one.
type Content struct {
	client gostorage.Client
	prefix string
	renew  time.Duration
}

// New returns a Content over cfg. A nil Client is the caller's mistake, and panics naming the
// field. A negative renew age is refused, matching [dispatch.ErrRequest].
func New(cfg Config) (*Content, error) {
	if cfg.Client == nil {
		panic("storage: Config.Client is nil")
	}
	if cfg.Renew < 0 {
		return nil, fmt.Errorf("storage: renew age %s is negative: %w", cfg.Renew, dispatch.ErrRequest)
	}
	return &Content{client: cfg.Client, prefix: cfg.Prefix, renew: cfg.Renew}, nil
}

func (c *Content) key(sha256 string) (string, error) {
	if !isSHA256(sha256) {
		return "", fmt.Errorf("storage: content %q isn't a SHA-256 as lowercase hex: %w", sha256, dispatch.ErrRequest)
	}
	return c.prefix + sha256[:2] + "/" + sha256, nil
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Put stats sha256's key, then puts r's bytes there unless the object is there and no older than
// the renew age. An object past the Client's size bound fails matching [dispatch.ErrTooLarge] as
// well as go-storage's ErrTooLarge. A put of a long stream stops when ctx ends.
func (c *Content) Put(ctx context.Context, sha256 string, r io.Reader) error {
	key, err := c.key(sha256)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	obj, err := c.client.Stat(ctx, key)
	switch {
	case err == nil && (c.renew == 0 || time.Since(obj.ModifiedAt) <= c.renew):
		return nil
	case err != nil && !isMiss(err):
		return fmt.Errorf("storage: content: put %s: %w", sha256, err)
	}
	_, err = c.client.Put(ctx, key, contextReader{ctx, r}, gostorage.PutOptions{})
	if errors.Is(err, gostorage.ErrTooLarge) {
		return fmt.Errorf("storage: content: put %s: %w: %w", sha256, dispatch.ErrTooLarge, err)
	}
	if err != nil {
		return fmt.Errorf("storage: content: put %s: %w", sha256, err)
	}
	return nil
}

// Open gets sha256's object. A miss matches fs.ErrNotExist as well as go-storage's ErrNotFound;
// a missing container is no miss, and never matches fs.ErrNotExist.
func (c *Content) Open(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	key, err := c.key(sha256)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	blob, err := c.client.Get(ctx, key, gostorage.GetOptions{})
	if isMiss(err) {
		return nil, fmt.Errorf("storage: content: %s: %w: %w", sha256, fs.ErrNotExist, err)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: content: %s: %w", sha256, err)
	}
	return blob.Body, nil
}

func isMiss(err error) bool {
	return errors.Is(err, gostorage.ErrNotFound) && !errors.Is(err, gostorage.ErrContainerNotFound)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
