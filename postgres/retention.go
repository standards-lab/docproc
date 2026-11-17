package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// reclaimEntries is the retention's statement, outside the Index's: no Index method drops by age.
var reclaimEntries = statements.Statement("reclaim_entries")

// Retention drops the entries the index no longer needs, a budget at a time: those created more
// than its age ago, a recorded entry counting from its record. [NewRetention] makes one. Its Pass
// is a reclaimer's, so a host's sweeper that bounds each pass by a budget drives it.
//
// It is safe for concurrent use, and passes on several hosts over one database skip each other's
// rows rather than block.
type Retention struct {
	db  *sqlate.DB
	age time.Duration
}

// NewRetention returns the index's retention over db: each pass drops, with their logs, the
// entries created more than age ago on the database's clock, oldest first. A claim under a live
// lease, or one that never expires, is never dropped; a claim whose lease ran out is, once old
// enough. A dropped key's content is prepared again on its next dispatch, so age is how long a
// result is worth keeping, which is the host's call: the module has no default.
//
// A nil db is the caller's mistake, and panics naming it. An age that isn't positive is refused,
// matching [dispatch.ErrRequest].
func NewRetention(db *sqlate.DB, age time.Duration) (*Retention, error) {
	if db == nil {
		panic("postgres: NewRetention's db is nil")
	}
	if age <= 0 {
		return nil, fmt.Errorf("postgres: retention age %s isn't positive: %w", age, dispatch.ErrRequest)
	}
	return &Retention{db: db, age: age}, nil
}

// Pass drops at most budget entries older than the retention's age, oldest first, with their
// logs, in one statement that notifies each dropped key's waiters, and reports more while
// eligible entries may remain. more is true exactly when the pass dropped its whole budget: a
// pass that dropped fewer found every eligible entry it could take, and one that dropped budget
// may have left more. An index holding exactly budget eligible entries therefore costs one extra
// pass that drops none and reports false; the rule needs no second query. An entry another
// transaction holds, such as a claim taking it over, is skipped for a later pass. A budget below
// 1 is refused, matching [dispatch.ErrRequest].
func (r *Retention) Pass(ctx context.Context, budget int) (more bool, err error) {
	if budget < 1 {
		return false, fmt.Errorf("postgres: retention budget %d is below 1: %w", budget, dispatch.ErrRequest)
	}
	n, err := reclaimEntries.Exec(ctx, r.db, query.Args{"age": r.age, "budget": budget})
	if err != nil {
		return false, fmt.Errorf("postgres: retention: %w", err)
	}
	return n == int64(budget), nil
}
