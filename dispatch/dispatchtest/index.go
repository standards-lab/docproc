package dispatchtest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

// Index runs the index contract against the indexes newIndex makes, a new, empty one for each
// case. A store whose claims have a lease, a positive one from Claim, is also held to the lease's
// contract: a renewal keeps the claim, and a claim not renewed within its lease is taken over, its
// token's renewals, appends, and record failing and its appends discarded; the cases wait out the
// lease, so newIndex should make one of a fraction of a second.
func Index(t *testing.T, newIndex func(t *testing.T) dispatch.Index) {
	t.Helper()
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		idx := newIndex(t)
		k := key("empty")
		if _, ok, err := idx.Lookup(ctx, k); err != nil || ok {
			t.Errorf("lookup: %v, %v, want no entry", ok, err)
		}
		if got := items(t, idx, k); len(got) != 0 {
			t.Errorf("items: %d, want none", len(got))
		}
		if _, ok, err := idx.Wait(ctx, k); err != nil || ok {
			t.Errorf("wait on an unclaimed key: %v, %v, want no entry at once", ok, err)
		}
		if err := idx.Drop(ctx, k); err != nil {
			t.Errorf("drop of an absent entry: %v", err)
		}
	})

	t.Run("record", func(t *testing.T) {
		idx := newIndex(t)
		k := key("record")
		token := claim(t, idx, k)
		if _, _, ok, err := idx.Claim(ctx, k); err != nil || ok {
			t.Errorf("a second claim: %v, %v, want refused", ok, err)
		}
		for _, r := range log() {
			if err := idx.Append(ctx, k, token, r); err != nil {
				t.Fatalf("append: %v", err)
			}
		}
		e := entry()
		if err := idx.Record(ctx, k, token, e); err != nil {
			t.Fatalf("record: %v", err)
		}
		got, ok, err := idx.Lookup(ctx, k)
		if err != nil || !ok || !reflect.DeepEqual(got, e) {
			t.Errorf("lookup: %+v, %v, %v, want %+v", got, ok, err, e)
		}
		if got := items(t, idx, k); !reflect.DeepEqual(got, recorded(log())) {
			t.Errorf("items:\n got %s\nwant %s", show(got), show(recorded(log())))
		}
		if _, _, ok, err := idx.Claim(ctx, k); err != nil || ok {
			t.Errorf("a claim of a recorded key: %v, %v, want refused", ok, err)
		}
		if got, ok, err := idx.Wait(ctx, k); err != nil || !ok || !reflect.DeepEqual(got, e) {
			t.Errorf("wait on a recorded key: %+v, %v, %v, want the entry", got, ok, err)
		}
		if err := idx.Renew(ctx, k, token); err == nil {
			t.Error("renew after the record: no error")
		}
	})

	t.Run("an empty log", func(t *testing.T) {
		idx := newIndex(t)
		k := key("empty log")
		token := claim(t, idx, k)
		e := dispatch.Entry{Outcome: dispatch.NoProcessor, Reason: "nothing", Stages: []dispatch.Identity{k.Stage}}
		if err := idx.Record(ctx, k, token, e); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := idx.Lookup(ctx, k); err != nil || !ok || !reflect.DeepEqual(got, e) {
			t.Errorf("lookup: %+v, %v, %v, want %+v", got, ok, err, e)
		}
		if got := items(t, idx, k); len(got) != 0 {
			t.Errorf("items: %d, want none", len(got))
		}
	})

	t.Run("keys", func(t *testing.T) {
		idx := newIndex(t)
		a := key("keys")
		b := a
		b.Stage.Version = "2"
		ta := claim(t, idx, a)
		tb := claim(t, idx, b)
		append1(t, idx, a, ta, warning("a"))
		append1(t, idx, b, tb, warning("b"))
		for _, kt := range []struct {
			k     dispatch.Key
			token string
		}{{a, ta}, {b, tb}} {
			if err := idx.Record(ctx, kt.k, kt.token, entry()); err != nil {
				t.Fatal(err)
			}
		}
		for k, want := range map[dispatch.Key]string{a: "a", b: "b"} {
			if got := items(t, idx, k); len(got) != 1 || got[0].Warning.Where != want {
				t.Errorf("items of %v: %s, want warning %q", k.Stage, show(got), want)
			}
		}
	})

	t.Run("a token that doesn't hold the claim", func(t *testing.T) {
		idx := newIndex(t)
		k := key("token")
		token := claim(t, idx, k)
		other := token + "x"
		if err := idx.Renew(ctx, k, other); !errors.Is(err, dispatch.ErrClaimLost) {
			t.Errorf("renew: %v, want ErrClaimLost", err)
		}
		if err := idx.Append(ctx, k, other, warning("w")); !errors.Is(err, dispatch.ErrClaimLost) {
			t.Errorf("append: %v, want ErrClaimLost", err)
		}
		if err := idx.Record(ctx, k, other, entry()); !errors.Is(err, dispatch.ErrClaimLost) {
			t.Errorf("record: %v, want ErrClaimLost", err)
		}
		if err := idx.Release(ctx, k, other); err != nil {
			t.Errorf("release: %v", err)
		}
		if _, _, ok, err := idx.Claim(ctx, k); err != nil || ok {
			t.Errorf("claim after another token's release: %v, %v, want refused", ok, err)
		}
		if _, ok, err := idx.Lookup(ctx, k); err != nil || ok {
			t.Errorf("lookup: %v, %v, want no entry", ok, err)
		}
		if err := idx.Renew(ctx, k, token); err != nil {
			t.Errorf("renew by the holder: %v", err)
		}
	})

	t.Run("an item sets one field", func(t *testing.T) {
		idx := newIndex(t)
		k := key("item")
		token := claim(t, idx, k)
		w := dispatch.Warning{Tool: "t", Where: "w", What: "x"}
		for _, r := range []dispatch.Recorded{{}, {After: 3}, {Output: &dispatch.Output{}, Warning: &w}} {
			if err := idx.Append(ctx, k, token, r); !errors.Is(err, dispatch.ErrRequest) {
				t.Errorf("append %+v: %v, want ErrRequest", r, err)
			}
		}
	})

	t.Run("release discards the log", func(t *testing.T) {
		idx := newIndex(t)
		k := key("release")
		old := claim(t, idx, k)
		append1(t, idx, k, old, warning("old"))
		if err := idx.Release(ctx, k, old); err != nil {
			t.Fatal(err)
		}
		if err := idx.Append(ctx, k, old, warning("late")); err == nil {
			t.Error("append after the release: no error")
		}
		if _, ok, err := idx.Lookup(ctx, k); err != nil || ok {
			t.Errorf("lookup after the release: %v, %v, want no entry", ok, err)
		}
		token := claim(t, idx, k)
		append1(t, idx, k, token, warning("new"))
		if err := idx.Record(ctx, k, token, entry()); err != nil {
			t.Fatal(err)
		}
		if got := items(t, idx, k); len(got) != 1 || got[0].Warning.Where != "new" {
			t.Errorf("items: %s, want the new claim's alone", show(got))
		}
	})

	t.Run("wait", func(t *testing.T) {
		idx := newIndex(t)
		for _, record := range []bool{true, false} {
			k := key(fmt.Sprintf("wait %v", record))
			token := claim(t, idx, k)
			type waited struct {
				e   dispatch.Entry
				ok  bool
				err error
			}
			done := make(chan waited, 1)
			go func() {
				e, ok, err := idx.Wait(ctx, k)
				done <- waited{e, ok, err}
			}()
			select {
			case w := <-done:
				t.Fatalf("wait returned while the key was claimed: %+v", w)
			case <-time.After(50 * time.Millisecond):
			}
			if record {
				if err := idx.Record(ctx, k, token, entry()); err != nil {
					t.Fatal(err)
				}
			} else if err := idx.Release(ctx, k, token); err != nil {
				t.Fatal(err)
			}
			w := <-done
			if w.err != nil || w.ok != record || (record && !reflect.DeepEqual(w.e, entry())) {
				t.Errorf("wait, the claim recorded %v: %+v, %v, %v", record, w.e, w.ok, w.err)
			}
		}
		k := key("wait cancelled")
		claim(t, idx, k)
		ended, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		if _, _, err := idx.Wait(ended, k); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("wait past its context: %v, want the context's error", err)
		}
	})

	t.Run("drop", func(t *testing.T) {
		idx := newIndex(t)
		k := key("drop")
		token := claim(t, idx, k)
		append1(t, idx, k, token, warning("dropped"))
		if err := idx.Record(ctx, k, token, entry()); err != nil {
			t.Fatal(err)
		}
		if err := idx.Drop(ctx, k); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := idx.Lookup(ctx, k); err != nil || ok {
			t.Errorf("lookup after the drop: %v, %v, want no entry", ok, err)
		}
		if got := items(t, idx, k); len(got) != 0 {
			t.Errorf("items after the drop: %s, want none", show(got))
		}
		token = claim(t, idx, k)
		append1(t, idx, k, token, warning("again"))
		if err := idx.Record(ctx, k, token, entry()); err != nil {
			t.Fatal(err)
		}
		if got := items(t, idx, k); len(got) != 1 || got[0].Warning.Where != "again" {
			t.Errorf("items of the entry recorded again: %s, want its own", show(got))
		}
	})

	t.Run("items fails when its entry is replaced or dropped", func(t *testing.T) {
		for _, replace := range []bool{false, true} {
			idx := newIndex(t)
			k := key(fmt.Sprintf("replaced %v", replace))
			record := func(where ...string) {
				t.Helper()
				token := claim(t, idx, k)
				for _, w := range where {
					append1(t, idx, k, token, warning(w))
				}
				if err := idx.Record(ctx, k, token, entry()); err != nil {
					t.Fatal(err)
				}
			}
			record("1", "2", "3")
			var failed bool
			n := 0
			for _, err := range idx.Items(ctx, k) {
				if err != nil {
					failed = true
					break
				}
				if n++; n == 1 {
					if err := idx.Drop(ctx, k); err != nil {
						t.Fatal(err)
					}
					if replace {
						record("a", "b", "c")
					}
				}
			}
			if !failed {
				t.Errorf("items over an entry replaced %v while read: read %d items and no error", replace, n)
			}
		}
	})

	t.Run("one claimant", func(t *testing.T) {
		idx := newIndex(t)
		k := key("one claimant")
		var won atomic.Int32
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				_, _, ok, err := idx.Claim(ctx, k)
				if err != nil {
					t.Error(err)
				}
				if ok {
					won.Add(1)
				}
			})
		}
		wg.Wait()
		if n := won.Load(); n != 1 {
			t.Errorf("%d of 16 concurrent claims won, want 1", n)
		}
	})

	t.Run("lease", func(t *testing.T) {
		idx := newIndex(t)
		k := key("lease")
		token, lease, ok, err := idx.Claim(ctx, k)
		if err != nil || !ok {
			t.Fatalf("claim: %v, %v", ok, err)
		}
		if lease < 0 {
			t.Fatalf("lease %s, want none or a positive one", lease)
		}
		if lease == 0 {
			t.Skip("the store's claims never expire")
		}
		append1(t, idx, k, token, warning("taken over"))

		// Renewed at a third of its lease, the claim outlasts two leases.
		for end := time.Now().Add(2 * lease); time.Now().Before(end); {
			time.Sleep(lease / 3)
			if err := idx.Renew(ctx, k, token); err != nil {
				t.Fatalf("renew within the lease: %v", err)
			}
			if _, _, ok, err := idx.Claim(ctx, k); err != nil || ok {
				t.Fatalf("claim of a renewed claim: %v, %v, want refused", ok, err)
			}
		}

		// Not renewed, it is taken over once its lease runs out, and a waiter returns.
		if _, ok, err := idx.Wait(ctx, k); err != nil || ok {
			t.Errorf("wait on a claim not renewed: %v, %v, want no entry once its lease ran out", ok, err)
		}
		var next string
		for deadline := time.Now().Add(10 * lease); next == ""; time.Sleep(lease / 10) {
			if time.Now().After(deadline) {
				t.Fatal("the claim wasn't taken over within ten leases")
			}
			if tok, _, ok, err := idx.Claim(ctx, k); err != nil {
				t.Fatal(err)
			} else if ok {
				next = tok
			}
		}
		if err := idx.Renew(ctx, k, token); !errors.Is(err, dispatch.ErrClaimLost) {
			t.Errorf("the old token's renewal after a takeover: %v, want ErrClaimLost", err)
		}
		if err := idx.Append(ctx, k, token, warning("late")); err == nil {
			// A buffered log may refuse it at the record instead.
			if err := idx.Record(ctx, k, token, entry()); err == nil {
				t.Error("the old token's append and record after a takeover: no error")
			}
		} else if err := idx.Record(ctx, k, token, entry()); err == nil {
			t.Error("the old token's record after a takeover: no error")
		}
		append1(t, idx, k, next, warning("taker"))
		if err := idx.Record(ctx, k, next, entry()); err != nil {
			t.Fatal(err)
		}
		if got := items(t, idx, k); len(got) != 1 || got[0].Warning.Where != "taker" {
			t.Errorf("items: %s, want the new claim's alone", show(got))
		}
	})
}

// key returns a key for a case: name's SHA-256 under the fixture stage's identity.
func key(name string) dispatch.Key {
	return dispatch.Key{SHA256: hash([]byte(name)), Stage: dispatch.Identity{Name: "fixture", Version: "1", Config: "fixture"}}
}

// claim claims k in idx, failing the test unless it is claimed.
func claim(t *testing.T, idx dispatch.Index, k dispatch.Key) string {
	t.Helper()
	token, _, ok, err := idx.Claim(context.Background(), k)
	if err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	return token
}

// append1 appends r to the log of the claim token holds.
func append1(t *testing.T, idx dispatch.Index, k dispatch.Key, token string, r dispatch.Recorded) {
	t.Helper()
	if err := idx.Append(context.Background(), k, token, r); err != nil {
		t.Fatalf("append: %v", err)
	}
}

// items reads k's log whole.
func items(t *testing.T, idx dispatch.Index, k dispatch.Key) []dispatch.Recorded {
	t.Helper()
	var got []dispatch.Recorded
	for r, err := range idx.Items(context.Background(), k) {
		if err != nil {
			t.Fatalf("items: %v", err)
		}
		got = append(got, r)
	}
	return got
}

// warning returns a log item holding a warning where where.
func warning(where string) dispatch.Recorded {
	return dispatch.Recorded{Warning: &dispatch.Warning{Tool: "fixture", Where: where, What: "unreadable"}}
}

// entry returns an entry with every field set.
func entry() dispatch.Entry {
	return dispatch.Entry{
		Outcome: dispatch.Prepared, Reason: "why", Outputs: 1, Children: 1,
		Stages: []dispatch.Identity{
			{Name: "fixture", Version: "1", Config: "fixture"}, {Name: "leaf", Version: "2", Config: "c"},
		},
		Flags: []inspect.Flag{{Check: inspect.SignatureMismatch, Class: inspect.Active, Evidence: "extension .pdf, no signature"}},
	}
}

// log returns a log of one item of each kind, every field set, the local ones among them.
func log() []dispatch.Recorded {
	return []dispatch.Recorded{
		{Output: &dispatch.Output{
			Kind: dispatch.TextOutput, Name: "page 1", MediaType: "text/plain",
			Meta: map[string]string{dispatch.MetaPage: "1"}, SHA256: hash([]byte("page 1")),
			Path: "/local/page-1.txt", Value: []byte("local"),
		}, After: 7},
		{Warning: &dispatch.Warning{Tool: "fixture", Where: "page 2", What: "unreadable"}},
		{Child: &dispatch.Child{Part: dispatch.Part{
			ID: "/line 1", Name: "a.pdf", Size: 3, SHA256: hash([]byte("a")),
			ContentType: dispatch.ContentType{
				Kind: route.PDF, Route: route.Document, RouteReason: "signature", Stored: "application/pdf",
			},
			Flags:  []inspect.Flag{{Check: inspect.PDFJavaScript, Class: inspect.Active, Evidence: "/JavaScript"}},
			Parent: "/", Depth: 1, Where: "line 1", Path: "/local/a.pdf", After: 9,
		}, Outcome: dispatch.Prepared}},
	}
}

// recorded returns log as an index gives it back: its local fields left out.
func recorded(log []dispatch.Recorded) []dispatch.Recorded {
	out := make([]dispatch.Recorded, len(log))
	for i, r := range log {
		if r.Output != nil {
			o := *r.Output
			o.Path, o.Value = "", nil
			r.Output = &o
		}
		if r.Child != nil {
			c := *r.Child
			c.Part.Path, c.Part.After = "", 0
			r.Child = &c
		}
		out[i] = r
	}
	return out
}

// show returns log for a failure's message.
func show(log []dispatch.Recorded) string {
	var s strings.Builder
	for _, r := range log {
		switch {
		case r.Output != nil:
			fmt.Fprintf(&s, "[output %+v after %d]", *r.Output, r.After)
		case r.Warning != nil:
			fmt.Fprintf(&s, "[warning %+v]", *r.Warning)
		case r.Child != nil:
			fmt.Fprintf(&s, "[child %+v]", *r.Child)
		default:
			s.WriteString("[empty]")
		}
	}
	return s.String()
}
