package stages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"io/fs"
	"iter"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
)

type sevenZipReader interface {
	List(ctx context.Context, req sevenzip.ListRequest) (sevenzip.Listing, error)
	Read(ctx context.Context, req sevenzip.ReadRequest) iter.Seq2[sevenzip.Item, error]
	Extract(ctx context.Context, req sevenzip.ExtractRequest, w io.Writer) error
}

type archiveConfig struct {
	SevenZip sevenZipReader
	Version  string
	Inspect  inspect.Options
	Memory   int64
}

type archiveStage struct {
	bomb     inspect.BombRule
	opts     inspect.Options
	sevenZip sevenZipReader
	memory   int64
	id       dispatch.Identity
}

func newArchiveStage(cfg archiveConfig) *archiveStage {
	if cfg.SevenZip == nil {
		panic("stages: archiveConfig.SevenZip is nil")
	}
	bomb := cfg.Inspect.Bomb()
	return &archiveStage{
		bomb: bomb, opts: cfg.Inspect, sevenZip: cfg.SevenZip, memory: cfg.Memory,
		id: identity("archive", cfg.Version, struct {
			Bomb    inspect.BombRule `json:"bomb"`
			Entries int              `json:"entries"`
		}{bomb, cfg.Inspect.Entries}),
	}
}

func (s *archiveStage) Identity() dispatch.Identity { return s.id }

func (s *archiveStage) UntrustedWhere() {}

func (s *archiveStage) Prepare(ctx context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		ctx := withWriteCap(ctx)
		switch p.ContentType.Kind {
		case route.ZIP:
			s.zip(ctx, p, dir, yield)
		case route.Tar:
			s.tar(ctx, p, dir, yield)
		case route.Gzip:
			s.gzip(ctx, p, dir, yield)
		case route.Zstd, route.XZ:
			s.stream(ctx, p, dir, yield)
		case route.RAR4, route.RAR5:
			s.rar(ctx, p, dir, yield)
		case route.SevenZip, route.MSCF:
			s.listed(ctx, p, dir, sevenZipKinds[p.ContentType.Kind], yield)
		default:
			yield(dispatch.Item{}, fmt.Errorf("archive stage: content of kind %s: %w", p.ContentType.Kind, dispatch.ErrNoProcessor))
		}
	}
}

func entryFlags(name string, typ inspect.EntryType, link string, encrypted bool) []inspect.Flag {
	rep, err := inspect.CheckListing(route.SevenZip, 0, []inspect.Listed{{Name: name, Type: typ, Link: link, Encrypted: encrypted}}, inspect.Options{})
	if err != nil {
		panic(err)
	}
	return rep.Flags
}

func flagged(where, name string, typ inspect.EntryType, flags []inspect.Flag) dispatch.Item {
	for _, f := range flags {
		if f.Check == inspect.ArchiveEncrypted {
			return unread(where, name, dispatch.Encrypted, "encrypted", flags...)
		}
	}
	reason := string(typ) + ": not read"
	if typ == inspect.FileEntry {
		reason = "its name is unsafe: not read"
	}
	return unread(where, name, dispatch.Quarantined, reason, flags...)
}

func (s *archiveStage) bombChild(where, name, what string, written, whole uint64) dispatch.Item {
	return unread(where, name, dispatch.Quarantined, "past the bomb rule as written", inspect.Flag{
		Check: inspect.ArchiveBomb, Class: inspect.Unsafe,
		Evidence: fmt.Sprintf("%s: %d bytes written from %d, %.0f:1; past %d bytes and %g:1",
			what, written, whole, float64(written)/float64(max(whole, 1)), s.bomb.Size, s.bomb.Ratio),
	})
}

var errBomb = errors.New("past the bomb thresholds")

var errLimited = errors.New("past the bytes cap")

type writeCapKey struct{}

type writeCap struct {
	left uint64
}

func withWriteCap(ctx context.Context) context.Context {
	n, ok := dispatch.Remaining(ctx)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, writeCapKey{}, &writeCap{left: uint64(max(n, 0))})
}

func notWritten(where, name string, why error) dispatch.Item {
	if errors.Is(why, errLimited) {
		return unread(where, name, dispatch.Limited, "")
	}
	return unread(where, name, dispatch.Failed, why.Error())
}

func copyFrom(r io.Reader) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := io.Copy(w, r)
		return err
	}
}

func (s *archiveStage) extract(ctx context.Context, fill func(io.Writer) error, dir string, compressed uint64,
	whole func(w *bombWriter, fillErr error) error,
) (path string, written uint64, why, err error) {
	return writeBySum(ctx, "archive stage", s.bomb, fill, dir, compressed, whole)
}

func writeBySum(ctx context.Context, stage string, rule inspect.BombRule, fill func(io.Writer) error, dir string,
	compressed uint64, whole func(w *bombWriter, fillErr error) error,
) (path string, written uint64, why, err error) {
	tmp, err := os.CreateTemp(dir, "entry-*")
	if err != nil {
		return "", 0, nil, fmt.Errorf("%s: %w", stage, err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmp.Name())
		}
	}()
	w := &bombWriter{
		ctx: ctx, w: tmp, sum: sha256.New(), crc: crc32.NewIEEE(),
		compressed: compressed, rule: rule,
	}
	budget, capped := ctx.Value(writeCapKey{}).(*writeCap)
	if capped {
		w.room = &budget.left
	}
	fillErr := fill(w)
	if err := tmp.Close(); err != nil {
		return "", w.n, nil, fmt.Errorf("%s: %w", stage, err)
	}
	switch {
	case ctx.Err() != nil:
		return "", w.n, nil, ctx.Err()
	case w.bomb:
		return "", w.n, errBomb, nil
	case w.limited:
		return "", w.n, errLimited, nil
	case w.writeErr != nil:
		return "", w.n, nil, fmt.Errorf("%s: %w", stage, w.writeErr)
	}
	if why := whole(w, fillErr); why != nil {
		return "", w.n, why, nil
	}
	if capped {
		budget.left -= w.n
	}
	path = filepath.Join(dir, hex.EncodeToString(w.sum.Sum(nil)))
	if _, err := os.Lstat(path); err == nil {
		return path, w.n, nil, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", w.n, nil, fmt.Errorf("%s: %w", stage, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", w.n, nil, fmt.Errorf("%s: %w", stage, err)
	}
	keep = true
	return path, w.n, nil, nil
}

type bombWriter struct {
	ctx        context.Context
	w          io.Writer
	sum        hash.Hash
	crc        hash.Hash32
	n          uint64
	compressed uint64
	rule       inspect.BombRule
	bomb       bool
	room       *uint64
	limited    bool
	writeErr   error
}

func (b *bombWriter) Write(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.bomb {
		return 0, errBomb
	}
	if b.limited || b.room != nil && uint64(len(p)) > *b.room-b.n {
		b.limited = true
		return 0, errLimited
	}
	n, err := b.w.Write(p)
	b.n += uint64(n)
	b.sum.Write(p[:n])
	b.crc.Write(p[:n])
	if err != nil {
		b.writeErr = err
		return n, err
	}
	if b.rule.Past(b.n, b.compressed) {
		b.bomb = true
		return n, errBomb
	}
	return n, nil
}

func streamStem(name, ext, short string) string {
	got := path.Ext(name)
	switch strings.ToLower(got) {
	case ext:
		if stem := strings.TrimSuffix(name, got); stem != "" {
			return stem
		}
	case short:
		return strings.TrimSuffix(name, got) + ".tar"
	}
	return name
}
