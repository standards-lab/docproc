//go:build linux && integration

package dispatch_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/route"
)

// storeCaller is a caller dispatching a mail store as the package doc's "Resuming a store" says:
// it gathers each message's outputs, those named "message ...", and commits them, its cursor
// advanced to the message's id, once the message's last output has come, its Markdown, or its
// record when the record's Meta says none follows; each message record of the read's own is
// committed decoded, for a Threader to be fed. It saves a checkpoint at every message: the
// fixtures' few messages make every message a crash point the test can resume from.
type storeCaller struct {
	// committed are the committed message outputs, as "kind name", and messages the committed
	// message records of the read's own, decoded.
	committed []string
	messages  []*email.Message
	// pending are the outputs of the message whose last output hasn't come, and pendingMessage
	// its record, decoded.
	pending        []string
	pendingMessage *email.Message
	// cursor is the last committed message's id.
	cursor uint64
	// prepared are the SHA-256s of the parts below the input whose stage prepared them.
	prepared []string
	// events are the parts' own events.
	events []dispatch.Event
}

// take takes ev, as the caller's loop body would.
func (c *storeCaller) take(t *testing.T, ev dispatch.Event) {
	t.Helper()
	if ev.Output == nil {
		c.events = append(c.events, ev)
		if ev.Part.Depth > 0 && ev.Outcome == dispatch.Prepared {
			c.prepared = append(c.prepared, ev.Part.SHA256)
		}
		return
	}
	o := ev.Output
	if ev.Part.Depth > 0 || !strings.HasPrefix(o.Name, "message") {
		return
	}
	c.pending = append(c.pending, string(o.Kind)+" "+o.Name)
	own := o.Meta["embedded_in"] == "" && o.Meta["id"] != ""
	if !own {
		return
	}
	if o.Kind == dispatch.RecordOutput {
		c.pendingMessage = new(email.Message)
		if err := jsonv2.Unmarshal(o.Value, c.pendingMessage); err != nil {
			t.Fatal(err)
		}
		if o.Meta["markdown"] == "follows" {
			return
		}
	}
	id, err := strconv.ParseUint(o.Meta["id"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	c.committed = append(c.committed, c.pending...)
	c.messages = append(c.messages, c.pendingMessage)
	c.pending, c.pendingMessage = nil, nil
	c.cursor = id
}

// threads returns the threads of messages, a Threader fed each, as a consumer threads the
// message records it persisted.
func threads(messages ...[]*email.Message) []*email.Thread {
	var tr email.Threader
	for _, ms := range messages {
		for _, m := range ms {
			tr.Add(m)
		}
	}
	return tr.Threads()
}

// crashing is an index that, once crashed is set, releases nothing, as a process that died
// would leave its claims and pending outputs behind.
type crashing struct {
	dispatch.Index
	crashed atomic.Bool
}

func (c *crashing) Release(ctx context.Context, key dispatch.Key, token string) error {
	if c.crashed.Load() {
		return nil
	}
	return c.Index.Release(ctx, key, token)
}

// TestStoreCrashResume dispatches mailbox.pst and nine.mboxrd whole over a file index, then, on a
// fresh one, again until a crash just after message K's last output: at the end of the next
// message's first attachment where there is one, so that attachment is recorded, else at once.
// The crash either stops the loop, the dispatcher releasing its claims, or leaves the claims'
// temporary logs behind, a dead process's, which the resumed dispatch's index removes as it
// opens. The dispatch resumed with After set to the cursor, K's id, commits the rest: the
// committed message outputs of the two equal the whole dispatch's, in order, none twice; every
// part prepared before the crash is seen; the threads of the message records committed by the
// two are the whole read's; and no temporary log is left.
func TestStoreCrashResume(t *testing.T) {
	stages := dispatch.Stages{route.Nested: emailStage(t), route.Vision: dispatch.NewVisionStage()}
	for _, rel := range []string{"generated/pst/mailbox.pst", "mime/nine.mboxrd"} {
		path := filepath.Join(mailFixtures(t), rel)
		t.Run(filepath.Base(rel), func(t *testing.T) {
			whole := &storeCaller{}
			for ev, err := range newDispatcher(t, stages, fileIndex(t, t.TempDir())).Dispatch(t.Context(), dispatch.Input{Path: path}) {
				if err != nil {
					t.Fatal(err)
				}
				whole.take(t, ev)
			}
			if len(whole.pending) != 0 || len(whole.committed) == 0 {
				t.Fatalf("the whole dispatch left %q uncommitted, committed %d", whole.pending, len(whole.committed))
			}
			wantThreads := threads(whole.messages)
			k, attached := crashPoint(t, whole)

			for _, leftBehind := range []bool{false, true} {
				t.Run("left behind "+strconv.FormatBool(leftBehind), func(t *testing.T) {
					dir := t.TempDir()
					idx := &crashing{Index: fileIndex(t, dir)}
					crashed := &storeCaller{}
					past := false
					for ev, err := range newDispatcher(t, stages, idx).Dispatch(t.Context(), dispatch.Input{Path: path}) {
						if err != nil {
							t.Fatal(err)
						}
						if past && ev.Output != nil && ev.Part.Depth == 0 && strings.HasPrefix(ev.Output.Name, "message") {
							// The next message's own output, with no attachment before it.
							break
						}
						crashed.take(t, ev)
						if past && ev.Output == nil {
							// The next message's first attachment, recorded.
							break
						}
						past = crashed.cursor == k
						if past && leftBehind {
							idx.crashed.Store(true)
						}
					}
					if crashed.cursor != k {
						t.Fatalf("crashed at cursor %d, want %d", crashed.cursor, k)
					}
					if attached && len(crashed.prepared) == 0 {
						t.Fatal("no attachment was prepared before the crash")
					}
					if leftBehind {
						if left, _ := filepath.Glob(filepath.Join(dir, "*", ".tmp-*")); len(left) == 0 {
							t.Fatal("the crash left no temporary log behind")
						}
					}

					resumedIdx := fileIndex(t, dir)
					resumed := &storeCaller{}
					for ev, err := range newDispatcher(t, stages, resumedIdx).Dispatch(t.Context(), dispatch.Input{Path: path, After: k}) {
						if err != nil {
							t.Fatal(err)
						}
						resumed.take(t, ev)
					}
					if len(resumed.pending) != 0 {
						t.Errorf("the resumed dispatch left %q uncommitted", resumed.pending)
					}
					if got := slices.Concat(crashed.committed, resumed.committed); !slices.Equal(got, whole.committed) {
						t.Errorf("committed message outputs:\n got %q\nwant %q", got, whole.committed)
					}
					if len(crashed.committed) == 0 || len(resumed.committed) == 0 {
						t.Errorf("%d committed before the crash, %d after; want some of each", len(crashed.committed), len(resumed.committed))
					}
					seen := 0
					for _, ev := range resumed.events {
						if ev.Part.Depth > 0 && slices.Contains(crashed.prepared, ev.Part.SHA256) {
							if ev.Outcome != dispatch.Seen {
								t.Errorf("%s, prepared before the crash, is %s on resuming", ev.Part.Name, ev.Outcome)
							}
							seen++
						}
					}
					if attached && seen == 0 {
						t.Error("the attachment prepared before the crash isn't met again on resuming")
					}
					if store := resumed.events[len(resumed.events)-1]; store.Part.Depth != 0 || store.Outcome != dispatch.Prepared {
						t.Errorf("the resumed store: %s@%d", store.Outcome, store.Part.Depth)
					}
					if got := threads(crashed.messages, resumed.messages); !reflect.DeepEqual(got, wantThreads) {
						t.Errorf("the resumed threads differ from the whole read's: %d roots, want %d", len(got), len(wantThreads))
					}
					if left, err := filepath.Glob(filepath.Join(dir, "*", ".tmp-*")); err != nil || len(left) != 0 {
						t.Errorf("temporary logs left: %v, %v", left, err)
					}
				})
			}
		})
	}
}

// crashPoint returns the id of the message the crash comes after: the one before the first
// message, past the first, whose first attachment is found as a part a stage prepares, so the
// crash comes at that attachment's end, and true; else the middle message, and false.
func crashPoint(t *testing.T, whole *storeCaller) (uint64, bool) {
	t.Helper()
	var ids []uint64
	for _, c := range whole.committed {
		name, ok := strings.CutPrefix(c, string(dispatch.RecordOutput)+" message ")
		if !ok || strings.Contains(name, ",") {
			continue
		}
		id, err := strconv.ParseUint(name, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) < 3 {
		t.Fatalf("%d messages, want at least 3", len(ids))
	}
	for i, id := range ids[1:] {
		first := slices.IndexFunc(whole.events, func(ev dispatch.Event) bool {
			return ev.Part.Depth == 1 && strings.HasPrefix(ev.Part.Where, "message "+strconv.FormatUint(id, 10)+", ")
		})
		if first >= 0 && whole.events[first].Outcome == dispatch.Prepared {
			return ids[i], true
		}
	}
	return ids[len(ids)/2], false
}

// fileIndex returns a file index in dir.
func fileIndex(t *testing.T, dir string) *dispatch.FileIndex {
	t.Helper()
	idx, err := dispatch.NewFileIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}
