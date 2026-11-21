package dispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Content is the content store: it holds every output's content by its SHA-256, and a host puts
// its own content in it the same way. An implementation is safe for concurrent use, and has no
// renewal of its own: it reads a content's age from its store's modification time, so a content
// put again once it is older than the store's renew age is rewritten, its age restarting.
//
// Package dispatchtest holds this contract as a conformance suite, which every implementation
// runs.
type Content interface {
	// Put stores r's bytes as the content sha256 names, all or nothing, when that content is
	// absent or older than the store's renew age; otherwise it skips the write, reading nothing
	// of r. A sha256 that isn't 64 lowercase hex digits is refused, matching [ErrRequest].
	Put(ctx context.Context, sha256 string, r io.Reader) error
	// Open opens the content sha256 names; a miss matches fs.ErrNotExist. A sha256 that isn't 64
	// lowercase hex digits is refused, matching [ErrRequest].
	Open(ctx context.Context, sha256 string) (io.ReadCloser, error)
}

// ContentFile is the file of the content sha256 names, relative to a [FileContent]'s directory:
// its first two hex digits, a directory, then the whole name, as "ab/ab12…". The layout is stable,
// for a reader that isn't Go to follow. It returns "" for a sha256 that isn't 64 lowercase hex
// digits.
func ContentFile(sha256 string) string {
	if !isSHA256(sha256) {
		return ""
	}
	return filepath.Join(sha256[:2], sha256)
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

func checkSHA256(sha256 string) error {
	if !isSHA256(sha256) {
		return fmt.Errorf("dispatch: content %q isn't a SHA-256 as lowercase hex: %w", sha256, ErrRequest)
	}
	return nil
}

// FileContent is a [Content] in a directory, on the standard library alone, each content in its
// [ContentFile]. A put writes a temporary file beside the content's, synced, and renames it into
// place, so a reader never sees part of a content; the content's age is its file's modification
// time. Its zero value is not ready; [NewFileContent] makes one.
type FileContent struct {
	dir   string
	renew time.Duration
}

// NewFileContent returns a FileContent in dir, making dir when it doesn't exist, which rewrites a
// content put again once its file is older than renew; a renew of zero never rewrites. An empty
// dir or a negative renew is refused, matching [ErrRequest].
func NewFileContent(dir string, renew time.Duration) (*FileContent, error) {
	if dir == "" {
		return nil, fmt.Errorf("dispatch: file content: no directory: %w", ErrRequest)
	}
	if renew < 0 {
		return nil, fmt.Errorf("dispatch: file content: renew age %s is negative: %w", renew, ErrRequest)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("dispatch: file content: %w", err)
	}
	return &FileContent{dir: dir, renew: renew}, nil
}

// Put writes r's bytes to sha256's file, as FileContent says, unless the file is there and no
// older than the renew age.
func (c *FileContent) Put(ctx context.Context, sha256 string, r io.Reader) error {
	if err := checkSHA256(sha256); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(c.dir, ContentFile(sha256))
	info, err := os.Stat(path)
	switch {
	case err == nil && (c.renew == 0 || time.Since(info.ModTime()) <= c.renew):
		return nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("dispatch: file content: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("dispatch: file content: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPrefix+"*")
	if err != nil {
		return fmt.Errorf("dispatch: file content: %w", err)
	}
	_, err = io.Copy(tmp, contextReader{ctx, r})
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("dispatch: file content: put %s: %w", sha256, err)
	}
	return nil
}

// Open opens sha256's file.
func (c *FileContent) Open(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	if err := checkSHA256(sha256); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(c.dir, ContentFile(sha256)))
	if err != nil {
		return nil, fmt.Errorf("dispatch: file content: %w", err)
	}
	return f, nil
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

// MemoryContent is a [Content] in memory, for tests and a host whose contents fit there. It never
// rewrites a content it holds, so it has no renew age. Its zero value is not ready;
// [NewMemoryContent] makes one.
type MemoryContent struct {
	mu       sync.Mutex
	contents map[string][]byte
}

// NewMemoryContent returns an empty MemoryContent.
func NewMemoryContent() *MemoryContent {
	return &MemoryContent{contents: map[string][]byte{}}
}

// Put reads r whole, then holds its bytes as sha256's content, unless it holds that content.
func (c *MemoryContent) Put(ctx context.Context, sha256 string, r io.Reader) error {
	if err := checkSHA256(sha256); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	_, ok := c.contents[sha256]
	c.mu.Unlock()
	if ok {
		return nil
	}
	b, err := io.ReadAll(contextReader{ctx, r})
	if err != nil {
		return fmt.Errorf("dispatch: memory content: put %s: %w", sha256, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.contents[sha256]; !ok {
		c.contents[sha256] = b
	}
	return nil
}

// Open returns a reader of sha256's content.
func (c *MemoryContent) Open(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	if err := checkSHA256(sha256); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	b, ok := c.contents[sha256]
	c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("dispatch: memory content: %s: %w", sha256, fs.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
