//go:build integration && linux

package email_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/email"
)

func TestRealResume(t *testing.T) {
	r := realReader(t)
	for _, rel := range []string{"generated/pst/mailbox.pst", "pst/tika/testPST.pst", "mime/nine.mboxrd"} {
		t.Run(rel, func(t *testing.T) {
			input := mailFixture(t, rel)
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
				var after email.Threader
				for rec, err := range r.Read(t.Context(), email.Request{Input: input}) {
					if err != nil {
						t.Fatal(err)
					}
					after.Add(rec)
					if m, ok := rec.(*email.Message); ok && m.ID == k {
						break
					}
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

func describeAll(recs []email.Record) []string {
	var out []string
	for _, rec := range recs {
		if _, ok := rec.(*email.Warning); !ok {
			out = append(out, describe(rec))
		}
	}
	return out
}
