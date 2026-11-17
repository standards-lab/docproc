package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

var _ dispatch.Index = (*Index)(nil)

// Config configures an [Index]. Every field is required; none has a default.
type Config struct {
	// DB is the session over the host's pool, which must be on pgx's database/sql driver,
	// github.com/jackc/pgx/v5/stdlib, with the docproc set applied.
	DB *sqlate.DB
	// Lease is every claim's lease, from its last Claim or Renew, on the database's clock. It is
	// positive: a claim not renewed within it may be taken over.
	Lease time.Duration
	// Flush is how many log items a claim buffers before it writes them in one multi-row insert,
	// and how many items Items reads in a page. It is positive.
	Flush int
}

// Index is a [dispatch.Index] over PostgreSQL, through the sqlate session its [Config] names, in
// the schema [Migrations] creates. Each of a claim, a renewal, a flush of appends, a record, and
// a release is one statement conditional on the entry's current token, so the database fences a
// taken-over claimant; a takeover also deletes the old token's log, in the claim's transaction.
//
// A claim's appends are buffered in memory, Flush at a time, so a token is honored only by the
// Index that issued it: an Append or a Record through another Index, or after the claim ended
// here, fails matching [dispatch.ErrClaimLost]. Record writes the buffer's remainder and the
// entry in one statement, so an entry's log commits whole with it or not at all.
//
// It is safe for concurrent use. Its zero value is not ready; [New] makes one.
type Index struct {
	db    *sqlate.DB
	lease time.Duration
	flush int

	mu      sync.Mutex
	buffers map[claimKey]*buffer
}

// claimKey names a claim this Index issued: its key and its token.
type claimKey struct {
	key   dispatch.Key
	token string
}

// buffer is a claim's log not yet written: its items as JSON and the position the first of them
// takes. Its mutex orders the claim's flushes.
type buffer struct {
	mu    sync.Mutex
	seq   int
	items []string
}

// Errors the index gives.
var (
	// errNotClaimed refuses a renewal, an append, or a record whose token doesn't hold the key's
	// claim.
	errNotClaimed = fmt.Errorf("postgres: index: the token doesn't hold the key's claim: %w", dispatch.ErrClaimLost)
	// errReplaced ends a read of an entry's log that was replaced or dropped while it read.
	errReplaced = errors.New("postgres: index: the entry was replaced or dropped while its log was read")
	// errWait is Wait's, until it is implemented.
	errWait = fmt.Errorf("postgres: index: Wait is not implemented yet: %w", errors.ErrUnsupported)
)

// The statements the index runs, bound once.
var (
	claimEntry   = statements.Statement("claim_entry").Scan(query.Scalar[sql.NullString])
	discardItems = statements.Statement("discard_items")
	renewClaim   = statements.Statement("renew_claim")
	appendItems  = statements.Statement("append_items")
	recordEntry  = statements.Statement("record_entry").Scan(query.Scalar[int64])
	releaseClaim = statements.Statement("release_claim")
	dropEntry    = statements.Statement("drop_entry")
	lookupEntry  = statements.Statement("lookup_entry").Scan(func(r query.Row) (stored, error) {
		var s stored
		return s, r.Scan(&s.version, &s.data)
	})
	lookupLog = statements.Statement("lookup_log").Scan(func(r query.Row) (stored, error) {
		var s stored
		return s, r.Scan(&s.version, &s.data)
	})
	listItems = statements.Statement("list_items").Scan(func(r query.Row) (page, error) {
		var p page
		return p, r.Scan(&p.seq, &p.item)
	})
)

// stored is a row's dispatch.Version and one of its values: an entry's JSON, or its log's token.
type stored struct {
	version string
	data    []byte
}

// page is one item of a page of a log: its position and its JSON.
type page struct {
	seq  int
	item []byte
}

// New returns an Index over cfg. A nil DB is the caller's mistake, and panics naming the field. A
// non-positive Lease or Flush, or a pool on a driver other than pgx's database/sql driver, is
// refused, matching [dispatch.ErrRequest]. The driver is read from a connection New takes from
// the pool under ctx, so a pool it can't reach fails too.
func New(ctx context.Context, cfg Config) (*Index, error) {
	if cfg.DB == nil {
		panic("postgres: Config.DB is nil")
	}
	refuse := func(format string, args ...any) (*Index, error) {
		return nil, fmt.Errorf("postgres: "+format+": %w", append(args, dispatch.ErrRequest)...)
	}
	if cfg.Lease <= 0 {
		return refuse("lease %s isn't positive", cfg.Lease)
	}
	if cfg.Flush <= 0 {
		return refuse("flush %d isn't positive", cfg.Flush)
	}
	if err := checkDriver(ctx, cfg.DB); err != nil {
		return nil, err
	}
	return &Index{db: cfg.DB, lease: cfg.Lease, flush: cfg.Flush, buffers: map[claimKey]*buffer{}}, nil
}

// checkDriver refuses db unless its driver is pgx's database/sql driver, naming the driver
// connection's type it found. A pinned connection is the one way to reach the driver through a
// sqlate session, and the way a LISTEN reaches pgx.
func checkDriver(ctx context.Context, db *sqlate.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("postgres: reading the pool's driver: %w", err)
	}
	defer func() { _ = conn.Close() }()
	return conn.Raw(func(dc any) error {
		if _, ok := dc.(*stdlib.Conn); !ok {
			return fmt.Errorf("postgres: the pool's driver connection is %T, not pgx's database/sql driver's *stdlib.Conn: %w", dc, dispatch.ErrRequest)
		}
		return nil
	})
}

// args returns key as the statements' arguments.
func args(key dispatch.Key) query.Args {
	return query.Args{
		"sha256":        key.SHA256,
		"stage_name":    key.Stage.Name,
		"stage_version": key.Stage.Version,
		"stage_config":  key.Stage.Config,
	}
}

// checkVersion refuses a row written under another dispatch.Version, whose JSON this build may
// not read as written.
func checkVersion(key dispatch.Key, version string) error {
	if version != dispatch.Version {
		return fmt.Errorf("postgres: index: %s %s was written under dispatch version %q, not %q", key.SHA256, key.Stage, version, dispatch.Version)
	}
	return nil
}

// Lookup returns key's recorded entry and whether there is one.
func (x *Index) Lookup(ctx context.Context, key dispatch.Key) (dispatch.Entry, bool, error) {
	row, err := lookupEntry.One(ctx, x.db, args(key))
	if errors.Is(err, sql.ErrNoRows) {
		return dispatch.Entry{}, false, nil
	}
	if err != nil {
		return dispatch.Entry{}, false, fmt.Errorf("postgres: index: lookup: %w", err)
	}
	if err := checkVersion(key, row.version); err != nil {
		return dispatch.Entry{}, false, err
	}
	var e dispatch.Entry
	if err := json.Unmarshal(row.data, &e); err != nil {
		return dispatch.Entry{}, false, fmt.Errorf("postgres: index: lookup: %w", err)
	}
	return e, true, nil
}

// Items yields key's log, in append order, reading it in pages of Flush items under the token
// whose log the entry holds, and holding one page at a time. Once the last page is read, it reads
// the entry's token again, and fails when the entry was replaced or dropped while it read.
func (x *Index) Items(ctx context.Context, key dispatch.Key) iter.Seq2[dispatch.Recorded, error] {
	return func(yield func(dispatch.Recorded, error) bool) {
		fail := func(err error) { yield(dispatch.Recorded{}, err) }
		a := args(key)
		row, err := lookupLog.One(ctx, x.db, a)
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if err != nil {
			fail(fmt.Errorf("postgres: index: items: %w", err))
			return
		}
		if err := checkVersion(key, row.version); err != nil {
			fail(err)
			return
		}
		token := string(row.data)
		a = a.With("token", token).With("size", x.flush)
		for after := -1; ; {
			items, err := listItems.All(ctx, x.db, a.With("after", after))
			if err != nil {
				fail(fmt.Errorf("postgres: index: items: %w", err))
				return
			}
			for _, it := range items {
				var r dispatch.Recorded
				if err := json.Unmarshal(it.item, &r); err != nil {
					fail(fmt.Errorf("postgres: index: items: %w", err))
					return
				}
				if !yield(r, nil) {
					return
				}
				after = it.seq
			}
			if len(items) < x.flush {
				break
			}
		}
		now, err := lookupLog.One(ctx, x.db, args(key))
		if errors.Is(err, sql.ErrNoRows) || (err == nil && string(now.data) != token) {
			fail(errReplaced)
		} else if err != nil {
			fail(fmt.Errorf("postgres: index: items: %w", err))
		}
	}
}

// Claim claims key unless it is recorded or another holds an unexpired claim, taking over a
// claim whose lease ran out and discarding its log, in one transaction.
func (x *Index) Claim(ctx context.Context, key dispatch.Key) (string, time.Duration, bool, error) {
	token := rand.Text()
	a := args(key).With("token", token).With("lease", x.lease).With("version", dispatch.Version)
	claimed, err := sqlate.Transact(ctx, x.db, func(tx *sqlate.Tx) (bool, error) {
		old, err := claimEntry.One(ctx, tx, a)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if old.Valid {
			if _, err := discardItems.Exec(ctx, tx, args(key).With("token", old.String)); err != nil {
				return false, err
			}
		}
		return true, nil
	}, nil)
	if err != nil {
		return "", 0, false, fmt.Errorf("postgres: index: claim: %w", err)
	}
	if !claimed {
		return "", 0, false, nil
	}
	x.mu.Lock()
	x.buffers[claimKey{key, token}] = &buffer{}
	x.mu.Unlock()
	return token, x.lease, true, nil
}

// Renew renews token's claim of key for another lease.
func (x *Index) Renew(ctx context.Context, key dispatch.Key, token string) error {
	n, err := renewClaim.Exec(ctx, x.db, args(key).With("token", token).With("lease", x.lease))
	if err != nil {
		return fmt.Errorf("postgres: index: renew: %w", err)
	}
	if n == 0 {
		x.forget(key, token)
		return errNotClaimed
	}
	return nil
}

// Wait is not implemented yet: it fails matching errors.ErrUnsupported.
func (x *Index) Wait(context.Context, dispatch.Key) (dispatch.Entry, bool, error) {
	return dispatch.Entry{}, false, errWait
}

// Append buffers r's JSON in token's claim's log, writing the buffer in one statement once it
// holds Flush items. A claim lost surfaces here at a flush, or at Record.
func (x *Index) Append(ctx context.Context, key dispatch.Key, token string, r dispatch.Recorded) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkRecorded(r); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("postgres: index: append: %w", err)
	}
	buf := x.buffer(key, token)
	if buf == nil {
		return errNotClaimed
	}
	buf.mu.Lock()
	defer buf.mu.Unlock()
	buf.items = append(buf.items, string(b))
	if len(buf.items) < x.flush {
		return nil
	}
	n, err := appendItems.Exec(ctx, x.db, args(key).With("token", token).With("seq", buf.seq).With("items", buf.items))
	if err != nil {
		// The items stay buffered, for the next flush or the record.
		return fmt.Errorf("postgres: index: append: %w", err)
	}
	if n == 0 {
		x.forget(key, token)
		return errNotClaimed
	}
	buf.seq += len(buf.items)
	buf.items = nil
	return nil
}

// checkRecorded refuses a log item that doesn't set exactly one of its Output, Warning, and
// Child, as dispatch's own indexes do.
func checkRecorded(r dispatch.Recorded) error {
	n := 0
	for _, set := range []bool{r.Output != nil, r.Warning != nil, r.Child != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("postgres: index: a log item sets %d of output, warning, and child, want 1: %w", n, dispatch.ErrRequest)
	}
	return nil
}

// Record records e as key's entry, with the rest of token's buffered log, in one statement, and
// ends the claim.
func (x *Index) Record(ctx context.Context, key dispatch.Key, token string, e dispatch.Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("postgres: index: record: %w", err)
	}
	buf := x.buffer(key, token)
	if buf == nil {
		return errNotClaimed
	}
	buf.mu.Lock()
	defer buf.mu.Unlock()
	items := buf.items
	if items == nil {
		items = []string{}
	}
	a := args(key).With("token", token).With("seq", buf.seq).With("items", items).
		With("entry", string(b)).With("version", dispatch.Version)
	n, err := recordEntry.One(ctx, x.db, a)
	if err != nil {
		// The buffer stays, so a Record retried after a failure the claim survived writes it.
		return fmt.Errorf("postgres: index: record: %w", err)
	}
	x.forget(key, token)
	if n == 0 {
		return errNotClaimed
	}
	return nil
}

// Release ends token's claim of key, discarding its buffer and its written log; a claim another
// now holds, or a recorded entry, is left.
func (x *Index) Release(ctx context.Context, key dispatch.Key, token string) error {
	x.forget(key, token)
	if _, err := releaseClaim.Exec(ctx, x.db, args(key).With("token", token)); err != nil {
		return fmt.Errorf("postgres: index: release: %w", err)
	}
	return nil
}

// Drop removes key's entry and its log.
func (x *Index) Drop(ctx context.Context, key dispatch.Key) error {
	if _, err := dropEntry.Exec(ctx, x.db, args(key)); err != nil {
		return fmt.Errorf("postgres: index: drop: %w", err)
	}
	return nil
}

// buffer returns the buffer of the claim token holds of key, issued by this Index, or nil.
func (x *Index) buffer(key dispatch.Key, token string) *buffer {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.buffers[claimKey{key, token}]
}

// forget drops the buffer of token's claim of key, once the claim ended or was lost.
func (x *Index) forget(key dispatch.Key, token string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.buffers, claimKey{key, token})
}
