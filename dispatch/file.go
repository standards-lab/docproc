package dispatch

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileIndex is an [Index] on disk, on the standard library alone, for the dispatches of one
// process. Its claims are [MemoryIndex]'s in-memory table, and only entries and logs are files: a
// claim never expires while the process lives, so Claim returns a lease of zero, none to renew.
// Two processes sharing a directory may each prepare the same content, the later record replacing
// the earlier whole.
//
// Each key is named by the hex SHA-256 of its JSON, and its files sit in a directory named by the
// name's first two hex digits: its entry, <name>.json, and the log the entry names,
// <name>.<token>.jsonl, one JSON line per item. A claim's log is written beside them under a
// temporary name, .tmp-<name>.<token>.jsonl, buffered, so its memory holds none of it; Record
// flushes and syncs it, renames it to its log's name, then writes the entry naming it, by
// temporary file and rename, so a reader sees a whole entry and its whole log or none, and then
// removes the log of the entry it replaced. Release removes the temporary log. NewFileIndex
// removes the temporary files a process that died left, every name beginning .tmp-, since no
// other process holds a claim on the directory.
type FileIndex struct {
	dir string

	mu     sync.Mutex
	claims claims[*pendingLog]
}

type pendingLog struct {
	path string
	file *os.File
	buf  *bufio.Writer
	enc  *json.Encoder
}

const tmpPrefix = ".tmp-"

// NewFileIndex returns a FileIndex in dir, making dir when it doesn't exist and removing the
// temporary files a process that died left in it. An empty dir is refused, matching [ErrRequest].
func NewFileIndex(dir string) (*FileIndex, error) {
	if dir == "" {
		return nil, fmt.Errorf("dispatch: file index: no directory: %w", ErrRequest)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("dispatch: file index: %w", err)
	}
	leftover, err := filepath.Glob(filepath.Join(dir, "*", tmpPrefix+"*"))
	if err != nil {
		return nil, fmt.Errorf("dispatch: file index: %w", err)
	}
	for _, path := range leftover {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("dispatch: file index: %w", err)
		}
	}
	f := &FileIndex{dir: dir}
	f.claims = newClaims(0, f.discard)
	return f, nil
}

func keyName(key Key) string {
	b, _ := json.Marshal(key)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (f *FileIndex) shard(name string) string { return filepath.Join(f.dir, name[:2]) }

type fileEntry struct {
	Key   Key    `json:"key"`
	Entry Entry  `json:"entry"`
	Log   string `json:"log,omitzero"`
}

func (f *FileIndex) entryPath(key Key) string {
	name := keyName(key)
	return filepath.Join(f.shard(name), name+".json")
}

func (f *FileIndex) readEntry(ctx context.Context, key Key) (fileEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return fileEntry{}, false, err
	}
	b, err := os.ReadFile(f.entryPath(key))
	if errors.Is(err, fs.ErrNotExist) {
		return fileEntry{}, false, nil
	}
	var fe fileEntry
	if err == nil {
		err = json.Unmarshal(b, &fe)
	}
	if err != nil {
		return fileEntry{}, false, fmt.Errorf("dispatch: file index: %w", err)
	}
	if fe.Key != key {
		return fileEntry{}, false, fmt.Errorf("dispatch: file index: entry %s holds another key", keyName(key))
	}
	return fe, true, nil
}

func (f *FileIndex) recorded(key Key) (bool, error) {
	_, err := os.Stat(f.entryPath(key))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dispatch: file index: %w", err)
	}
	return true, nil
}

// Lookup reads key's entry file.
func (f *FileIndex) Lookup(ctx context.Context, key Key) (Entry, bool, error) {
	fe, ok, err := f.readEntry(ctx, key)
	return fe.Entry, ok, err
}

// Items reads key's log one JSON line at a time. Once it has read the log through, it reads the
// entry again, failing when the entry no longer names the log it read, replaced or dropped.
func (f *FileIndex) Items(ctx context.Context, key Key) iter.Seq2[Recorded, error] {
	return func(yield func(Recorded, error) bool) {
		fe, ok, err := f.readEntry(ctx, key)
		if err != nil {
			yield(Recorded{}, err)
			return
		}
		if !ok || fe.Log == "" {
			return
		}
		file, err := os.Open(filepath.Join(f.shard(fe.Log), fe.Log))
		if errors.Is(err, fs.ErrNotExist) {
			yield(Recorded{}, errReplaced)
			return
		}
		if err != nil {
			yield(Recorded{}, fmt.Errorf("dispatch: file index: %w", err))
			return
		}
		defer func() { _ = file.Close() }()
		dec := json.NewDecoder(bufio.NewReader(file))
		for {
			if err := ctx.Err(); err != nil {
				yield(Recorded{}, err)
				return
			}
			var r Recorded
			err := dec.Decode(&r)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				yield(Recorded{}, fmt.Errorf("dispatch: file index: %w", err))
				return
			}
			if !yield(r, nil) {
				return
			}
		}
		now, ok, err := f.readEntry(ctx, key)
		if err != nil {
			yield(Recorded{}, err)
			return
		}
		if !ok || now.Log != fe.Log {
			yield(Recorded{}, errReplaced)
		}
	}
}

// Claim claims key unless it has an entry file or this process holds its claim.
func (f *FileIndex) Claim(ctx context.Context, key Key) (string, time.Duration, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if ok, err := f.recorded(key); err != nil || ok {
		return "", 0, false, err
	}
	token, ok := f.claims.take(key, nil)
	return token, 0, ok, nil
}

// Renew checks that token holds key's claim, which never expires.
func (f *FileIndex) Renew(ctx context.Context, key Key, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims.renew(key, token)
}

// Wait waits for key's claim to end, then reads key's entry.
func (f *FileIndex) Wait(ctx context.Context, key Key) (Entry, bool, error) {
	f.mu.Lock()
	done, lease := f.claims.waitOn(key)
	f.mu.Unlock()
	if done != nil {
		if err := wait(ctx, done, lease); err != nil {
			return Entry{}, false, err
		}
	}
	return f.Lookup(ctx, key)
}

// Append encodes r as a line of token's claim's temporary log, which it creates on the claim's
// first append.
func (f *FileIndex) Append(ctx context.Context, key Key, token string, r Recorded) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkRecorded(r); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.claims.holder(key, token)
	if err != nil {
		return err
	}
	if c.log == nil {
		name := keyName(key)
		if err := os.MkdirAll(f.shard(name), 0o755); err != nil {
			return fmt.Errorf("dispatch: file index: append: %w", err)
		}
		path := filepath.Join(f.shard(name), tmpPrefix+logName(key, token))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("dispatch: file index: append: %w", err)
		}
		buf := bufio.NewWriter(file)
		c.log = &pendingLog{path: path, file: file, buf: buf, enc: json.NewEncoder(buf)}
	}
	if err := c.log.enc.Encode(r); err != nil {
		return fmt.Errorf("dispatch: file index: append: %w", err)
	}
	return nil
}

func logName(key Key, token string) string { return keyName(key) + "." + token + ".jsonl" }

// Record flushes, syncs, and renames token's claim's temporary log to its log's name, writes key's
// entry file naming it, removes the log of the entry it replaced, if any, then ends the claim. A
// record that fails ends the claim, removing the log it renamed, which no entry names.
func (f *FileIndex) Record(ctx context.Context, key Key, token string, e Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.claims.holder(key, token)
	if err != nil {
		return err
	}
	old, had, err := f.readEntry(ctx, key)
	if err != nil {
		had = false
	}
	fe := fileEntry{Key: key, Entry: e}
	log := c.log
	c.log = nil
	if err := f.recordLog(key, token, log, &fe); err != nil {
		f.claims.end(key, token)
		return err
	}
	if err := writeJSON(f.entryPath(key), fe); err != nil {
		if fe.Log != "" {
			_ = os.Remove(filepath.Join(f.shard(fe.Log), fe.Log))
		}
		f.claims.end(key, token)
		return err
	}
	if had && old.Log != "" && old.Log != fe.Log {
		_ = os.Remove(filepath.Join(f.shard(old.Log), old.Log))
	}
	f.claims.commit(key, c)
	return nil
}

func (f *FileIndex) recordLog(key Key, token string, log *pendingLog, fe *fileEntry) error {
	if log == nil {
		return nil
	}
	err := log.buf.Flush()
	if err == nil {
		err = log.file.Sync()
	}
	if cerr := log.file.Close(); err == nil {
		err = cerr
	}
	name := logName(key, token)
	if err == nil {
		err = os.Rename(log.path, filepath.Join(f.shard(name), name))
	}
	if err != nil {
		_ = os.Remove(log.path)
		return fmt.Errorf("dispatch: file index: record: %w", err)
	}
	fe.Log = name
	return nil
}

func (f *FileIndex) discard(log *pendingLog) {
	if log == nil {
		return
	}
	_ = log.file.Close()
	_ = os.Remove(log.path)
}

// Release ends token's claim of key, removing its temporary log.
func (f *FileIndex) Release(ctx context.Context, key Key, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims.end(key, token)
	return ctx.Err()
}

// Drop removes key's entry file, then its log.
func (f *FileIndex) Drop(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fe, ok, err := f.readEntry(ctx, key)
	if err != nil {
		ok = false
	}
	if err := os.Remove(f.entryPath(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("dispatch: file index: drop: %w", err)
	}
	if ok && fe.Log != "" {
		if err := os.Remove(filepath.Join(f.shard(fe.Log), fe.Log)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("dispatch: file index: drop: %w", err)
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("dispatch: file index: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("dispatch: file index: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPrefix+"*")
	if err != nil {
		return fmt.Errorf("dispatch: file index: %w", err)
	}
	_, err = tmp.Write(b)
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
		return fmt.Errorf("dispatch: file index: %w", err)
	}
	return nil
}
