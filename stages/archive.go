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

// sevenZipReader is what an [archiveStage] reads 7-Zip's kinds through: sevenzip's Reader, which
// lists an archive, reads a listed archive's items whole, and extracts one item to a writer,
// through 7zz's standard output alone.
type sevenZipReader interface {
	List(ctx context.Context, req sevenzip.ListRequest) (sevenzip.Listing, error)
	Read(ctx context.Context, req sevenzip.ReadRequest) iter.Seq2[sevenzip.Item, error]
	Extract(ctx context.Context, req sevenzip.ExtractRequest, w io.Writer) error
}

// archiveConfig is what an [archiveStage] is built over: 7-Zip, the version of the 7zz behind it,
// the inspection options whose bomb rule, entry bound, and policy it enforces, and the memory
// bound 7zz runs under, which a death by a signal's reason names.
type archiveConfig struct {
	// SevenZip reads a zstd stream, a 7z archive, a cabinet, and a ZIP entry of a method
	// archive/zip doesn't read; it is required.
	SevenZip sevenZipReader
	// Version names 7zz's version line, as [versions] gives it; it is required.
	Version string
	// Inspect is the dispatcher's inspection options, validated by Build: its bomb rule, none
	// when its BombRatio and BombSize are zero, is the one an archive entry's, or a gzip or zstd
	// stream's, written bytes may not pass; its Entries bounds a 7-Zip listing; and its Policy is
	// the one a 7-Zip listing's flags are weighed under, as inspect.CheckListing weighs them.
	Inspect inspect.Options
	Memory  int64
}

// archiveStage prepares route.Archive's parts: a ZIP that is neither an office document nor an
// ODF package, a tar, and a gzip stream, on the standard library, and a zstd stream, a 7z archive,
// and a cabinet not named .onepkg, through 7-Zip.
//
// A ZIP is read through archive/zip, one entry at a time, in the central directory's order. Each
// file entry is written into the part's directory under its content's SHA-256, streamed into a
// temporary file while it is hashed, then renamed, and is a [dispatch.Found] part, the
// dispatcher's to route and inspect again, named by its stored name and found "zip entry <name>".
// A directory's entry is skipped. A stored, deflated, or bzip2 entry is decompressed by the
// standard library, bzip2 through compress/bzip2 as an archive/zip decompressor; an entry of any
// other method, such as Deflate64, implode, LZMA, xz, zstd, or PPMd, is read through 7-Zip,
// extracted alone to 7zz's standard output, under the same rules.
//
// An entry the stage doesn't write is an unread child, named by its stored name at its place,
// with no file, never dropped:
//
//   - an entry inspect flags active by its name or its type, a ".." element, an absolute name or
//     a drive letter, a symbolic or hard link, or a device, which no stage reads: quarantined,
//     with its flag, and no link or node is made;
//   - an encrypted entry, which archive/zip can't decrypt: encrypted, flagged
//     inspect.ArchiveEncrypted;
//   - an entry whose written bytes pass the bomb rule of [archiveConfig.Inspect], against its
//     compressed size, whatever its central directory declares: its writing stops there and its
//     partial file is removed, and it is quarantined, flagged inspect.ArchiveBomb with the
//     evidence;
//   - an entry whose bytes don't match its central directory, its CRC-32 or its size, or that
//     doesn't decompress: failed, its reason why;
//   - an entry whose written bytes would pass what the dispatch may still admit, as
//     dispatch.Remaining says, less what the stage has written for the entries before it: its
//     writing stops there and its partial file is removed, and it is limited, with no size or
//     SHA-256, the dispatcher giving it the bytes cap's reason. Every kind's entries, and a gzip's
//     or zstd's content, are held to it.
//
// A ZIP with an encrypted entry ends, once every other entry is dispatched, with
// dispatch.ErrEncrypted, naming how many of its entries are encrypted, so its outcome is
// encrypted, its event holding its readable entries' parts and its encrypted entries' children.
//
// A tar is read through archive/tar, one entry at a time, in the tar's order, its headers as
// inspect reads them. Each regular file, a sparse one among them, is written as a ZIP's file
// entry is and found "tar entry <name>". A directory is no part. A symbolic or hard link, a
// device, or a FIFO, and an entry of a traversing or absolute name, is an unread child, as a
// ZIP's is. An entry whose written bytes pass the bomb rule, against the whole tar's size, since
// an entry's bytes come from no more than the tar, as only a sparse entry's holes can, is stopped
// and quarantined as a ZIP's is. An entry of another type, or whose content doesn't read, is
// failed, and a header that can't be read fails the tar once the entries before it are
// dispatched.
//
// A gzip stream is decompressed through compress/gzip into one part, written by its SHA-256 and
// found "gzip content <name>", named by its header's name, else by the stream's own name without
// its .gz, or with a .tgz's .tar. The dispatcher routes it again, so a .tar.gz is a gzip part
// holding a tar part. Content whose written bytes pass the bomb rule, against the stream's size,
// is stopped and quarantined as a ZIP entry is, and content that doesn't decompress is failed; a
// header that doesn't read fails the part.
//
// A 7z archive and a cabinet are read through 7-Zip: listed first, by 7zz's technical listing,
// bounded by the options' Entries, and the listing checked by inspect.CheckListing under the rules
// inspect holds a tar to, before anything is extracted. The listing's flags are yielded as the
// archive's own, through dispatch.Item.Flag, and a verdict to quarantine them under the
// dispatcher's options stops the stage, so the archive is quarantined with that evidence and
// nothing of it is extracted. Otherwise its files are read in the listing's order, through one 7zz
// run of the whole archive to its standard output, an unsized or encrypted entry alone, written as
// a ZIP's entry is, and found "7z entry <name>" or "cabinet entry <name>"; a directory is no part.
// The bomb rule weighs the bytes read from 7zz against the whole archive's size, and the bytes are
// held to the listing's size and CRC-32. A link, a device, a traversing or absolute name, and an
// encrypted entry are unread children, as a ZIP's are, and the archive is encrypted once its
// readable entries are dispatched; a 7z whose headers are encrypted, so nothing of it can be
// listed, ends with dispatch.ErrEncrypted at once.
//
// A zstd stream is read through 7-Zip into one part, as a gzip stream is, found "zstd content
// <name>", named by the stream's own name without its .zst, or with a .tzst's .tar, and routed
// again, so a .tar.zst is a zstd part holding a tar part. A content size its frame header
// declares is checked first, as a listing is, and the bomb rule weighs the bytes read from 7zz
// against the stream's size.
//
// A failure of 7zz is classified once, by [partErr].
type archiveStage struct {
	bomb     inspect.BombRule
	opts     inspect.Options
	sevenZip sevenZipReader
	memory   int64
	id       dispatch.Identity
}

// newArchiveStage builds an archiveStage over cfg, panicking naming SevenZip when it is nil, the
// wiring's mistake.
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

// Identity returns the stage's identity: "archive", 7zz's version line, and its bomb rule's and
// entry bound's hash.
func (s *archiveStage) Identity() dispatch.Identity { return s.id }

// UntrustedWhere marks the stage as one whose found parts' Where come from its entries' stored
// names, which two siblings may share.
func (s *archiveStage) UntrustedWhere() {}

// Prepare reads the archive at p.Path by its kind, as the type's doc says.
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
		case route.Zstd:
			s.zstd(ctx, p, dir, yield)
		case route.SevenZip, route.MSCF:
			s.listed(ctx, p, dir, sevenZipKinds[p.ContentType.Kind], yield)
		default:
			yield(dispatch.Item{}, fmt.Errorf("archive stage: content of kind %s: %w", p.ContentType.Kind, dispatch.ErrNoProcessor))
		}
	}
}

// entryFlags returns the flags inspect raises for an archive entry by its name and its type, and
// its encryption, through inspect.CheckListing's rules for one entry, so the stage and inspect
// hold one rule: a traversing or absolute name, a link, a device, or an encrypted entry.
func entryFlags(name string, typ inspect.EntryType, link string, encrypted bool) []inspect.Flag {
	rep, err := inspect.CheckListing(route.SevenZip, 0, []inspect.Listed{{Name: name, Type: typ, Link: link, Encrypted: encrypted}}, inspect.Options{})
	if err != nil {
		// The zero options and a 7z's listing are always checked.
		panic(err)
	}
	return rep.Flags
}

// flagged returns the unread child of an entry inspect flags, at where, named name: encrypted when
// a flag says so, otherwise quarantined, its reason what the entry is.
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

// bombChild returns the unread child of content whose written bytes, written, passed the bomb
// rule against whole, the bytes they come from, at where, named name, as the evidence's subject
// what says.
func (s *archiveStage) bombChild(where, name, what string, written, whole uint64) dispatch.Item {
	return unread(where, name, dispatch.Quarantined, "past the bomb rule as written", inspect.Flag{
		Check: inspect.ArchiveBomb, Class: inspect.Unsafe,
		Evidence: fmt.Sprintf("%s: %d bytes written from %d, %.0f:1; past %d bytes and %g:1",
			what, written, whole, float64(written)/float64(max(whole, 1)), s.bomb.Size, s.bomb.Ratio),
	})
}

// errBomb is a written entry's word that its bytes passed both bomb thresholds.
var errBomb = errors.New("past the bomb thresholds")

// errLimited is a written entry's word that its bytes would pass what the dispatch may still
// admit, its writeCap's.
var errLimited = errors.New("past the bytes cap")

// writeCapKey is the key of a Prepare's writeCap on its context.
type writeCapKey struct{}

// writeCap is what one Prepare may still write of the parts it finds: what dispatch.Remaining
// said as Prepare started, less what writeBySum has written for the parts found before it.
type writeCap struct {
	left uint64
}

// withWriteCap returns ctx, a Prepare's, holding a writeCap of what dispatch.Remaining says, or
// ctx itself when the dispatch has no bytes cap. A stage calls it once a Prepare, inside its
// iterator, so each run of the stage counts its own writes.
func withWriteCap(ctx context.Context) context.Context {
	n, ok := dispatch.Remaining(ctx)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, writeCapKey{}, &writeCap{left: uint64(max(n, 0))})
}

// notWritten returns the unread child of content that has no file for why, a writeBySum's reason
// other than errBomb, at where, named name: limited for errLimited, the dispatcher giving it the
// bytes cap's reason, and otherwise failed, its reason why.
func notWritten(where, name string, why error) dispatch.Item {
	if errors.Is(why, errLimited) {
		return unread(where, name, dispatch.Limited, "")
	}
	return unread(where, name, dispatch.Failed, why.Error())
}

// copyFrom returns a fill for extract that copies r.
func copyFrom(r io.Reader) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := io.Copy(w, r)
		return err
	}
}

// extract writes the bytes fill writes into dir, streamed into a temporary file while their
// SHA-256 and CRC-32 are taken, then renamed to their SHA-256, and returns the path and the bytes
// written. fill copies a reader, or runs 7zz with its standard output the writer it is given. The
// bomb rule weighs the bytes written against compressed, the bytes they come from; past both
// thresholds the writer refuses the next write, and fill, a reader's copy or 7zz, stops. So it
// does at a write that would take the bytes past ctx's writeCap, when it holds one. whole, given
// the writer and fill's error once the bytes are written, says why they aren't the content whole,
// or nil when they are. why is the reason the content has no file, errBomb, errLimited, or
// whole's word, its partial file removed; err is the stage's own failure.
func (s *archiveStage) extract(ctx context.Context, fill func(io.Writer) error, dir string, compressed uint64,
	whole func(w *bombWriter, fillErr error) error,
) (path string, written uint64, why, err error) {
	return writeBySum(ctx, "archive stage", s.bomb, fill, dir, compressed, whole)
}

// writeBySum is extract's writing, for any stage that writes content it reads from a program or
// a reader under the bomb rule, its errors naming stage. Content it writes whole, a part the
// dispatcher will admit, is taken from ctx's writeCap, so the parts one Prepare writes come to no
// more than the dispatch may still admit.
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
			_ = os.Remove(tmp.Name()) // a temporary file of the part's own directory
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
	// A bomb, a bound, or the file's failure is decided by the writer's own record, whatever
	// fill returns: fill's error is the content's, which whole weighs, and a failed write is the
	// stage's.
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
		// The content is a part the dispatcher admits whole, from here on.
		budget.left -= w.n
	}
	path = filepath.Join(dir, hex.EncodeToString(w.sum.Sum(nil)))
	// The same content met earlier in this archive may still be at path: its bytes are these, so
	// it is found again rather than replaced.
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

// bombWriter writes an entry's bytes to w while hashing them, and stops with errBomb, setting
// bomb, once they pass the bomb rule against compressed, the bytes they come from, or with ctx's
// error once it ends. With room set, the bytes its Prepare may still write, it refuses a write
// that would take them past it, before writing it, with errLimited, setting limited. writeErr is
// w's own failure, which is the stage's rather than the content's. Every fill, 7-Zip's Extract
// among them, returns it wrapped in an error that whole would read as the content's, so
// writeBySum checks writeErr before it calls whole.
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

// streamStem returns the name a single stream's content takes from the stream's own name: the
// name without ext, or, for short, an extension naming a tar in the stream, the name with .tar in
// its place, as gzip -d and zstd -d name it; a name with neither is kept.
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
