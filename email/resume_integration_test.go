//go:build integration && linux

package email_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/email"
)

// These tests run the real mail binary, which the unit tier doesn't provide, so they run in the
// integration tier.

// TestRealResume runs the real pinned mail over the generated .pst, Tika's testPST.pst, which
// threads by its headers, and nine.mboxrd. Each is read whole and threaded; then, for its first
// message, one in the middle, its next to last, and its last, read up to and including that
// message, K, the Threader saved, and the read resumed with After K into a Threader restored from
// it. The resumed read's records are the whole read's store, folders, and the records after K's,
// in order, and the restored Threader's threads are the whole read's.
func TestRealResume(t *testing.T) {
	r := realReader(t)
	root := filepath.Join(mailCopy, "fixtures")
	for _, rel := range []string{"generated/pst/mailbox.pst", "pst/tika/testPST.pst", "mime/nine.mboxrd"} {
		t.Run(rel, func(t *testing.T) {
			input := filepath.Join(root, rel)
			whole, err := collect(r.Read(t.Context(), email.Request{Input: input}))
			if err != nil {
				t.Fatal(err)
			}
			var wholeThreader email.Threader
			var ids []uint64
			for _, rec := range whole {
				wholeThreader.Add(rec)
				if m, ok := rec.(*email.Message); ok {
					ids = append(ids, m.ID)
				}
			}
			want := wholeThreader.Threads()
			if len(ids) < 4 {
				t.Fatalf("%d messages, want at least 4", len(ids))
			}
			for _, k := range []uint64{ids[0], ids[len(ids)/2], ids[len(ids)-2], ids[len(ids)-1]} {
				var before email.Threader
				s := mustRead(t)(r.Read(t.Context(), email.Request{Input: input}))
				for rec, err := range s.Records() {
					if err != nil {
						t.Fatal(err)
					}
					before.Add(rec)
					if m, ok := rec.(*email.Message); ok && m.ID == k {
						break
					}
				}
				saved, err := before.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				var after email.Threader
				if err := after.UnmarshalBinary(saved); err != nil {
					t.Fatal(err)
				}
				resumed, err := collect(r.Read(t.Context(), email.Request{Input: input, After: k}))
				if err != nil {
					t.Fatalf("after %d: %v", k, err)
				}
				for _, rec := range resumed {
					after.Add(rec)
				}
				if got, want := describeAll(resumed), describeAll(tail(whole, k)); !slices.Equal(got, want) {
					t.Errorf("after %d, records:\n got %q\nwant %q", k, got, want)
				}
				if got := after.Threads(); !reflect.DeepEqual(got, want) {
					t.Errorf("after %d, threads %s, the whole read's %s", k, render(got), render(want))
				}
			}
		})
	}
}

// tail returns what a read resumed after message k gives of whole, a whole read's records: its
// store, folders, and folders' skipped records, then every record after k's message.
func tail(whole []email.Record, k uint64) []email.Record {
	var out []email.Record
	past := false
	for _, rec := range whole {
		switch r := rec.(type) {
		case *email.Store, *email.Folder:
			out = append(out, rec)
		case *email.Skipped:
			if past || r.Kind == "folder" || r.Kind == "contents" || r.Kind == "hierarchy" {
				out = append(out, rec)
			}
		default:
			if past {
				out = append(out, rec)
			}
		}
		if m, ok := rec.(*email.Message); ok && m.ID == k {
			past = true
		}
	}
	return out
}

// describeAll returns describe of each of recs.
func describeAll(recs []email.Record) []string {
	out := make([]string, len(recs))
	for i, rec := range recs {
		out[i] = describe(rec)
	}
	return out
}
