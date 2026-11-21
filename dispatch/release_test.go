package dispatch_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
)

type store struct {
	refs     []string
	contents map[string][]byte
	lie      bool

	mu       sync.Mutex
	releases int
}

func (s *store) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "store", Version: "1", Config: fmt.Sprintf("%d/%v", len(s.refs), s.lie)}
}

func (s *store) released() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.releases
}

func (s *store) Prepare(_ context.Context, _ dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		att := filepath.Join(dir, "attachments")
		if err := os.Mkdir(att, 0o700); err != nil {
			yield(dispatch.Item{}, err)
			return
		}
		written := map[string]bool{}
		released := map[string]bool{}
		for i, ref := range s.refs {
			content := s.contents[ref]
			sum := sha(content)
			path := filepath.Join(att, sum+".bin")
			if !written[sum] {
				if err := os.WriteFile(path, content, 0o600); err != nil {
					yield(dispatch.Item{}, err)
					return
				}
				written[sum] = true
			}
			where := fmt.Sprintf("record %d", i)
			f := dispatch.Found{Name: ref + ".bin", Where: where, SHA256: sum}
			link := ""
			switch _, err := os.Stat(path); {
			case err == nil:
				link = filepath.Join(dir, fmt.Sprintf("found-%d", i), sum+".bin")
				if err := os.Mkdir(filepath.Dir(link), 0o700); err != nil {
					yield(dispatch.Item{}, err)
					return
				}
				if err := os.Link(path, link); err != nil {
					yield(dispatch.Item{}, err)
					return
				}
				f.Path = link
				if s.lie {
					f.SHA256 = sha([]byte("not " + ref))
				}
				f.Release = func() {
					s.mu.Lock()
					s.releases++
					s.mu.Unlock()
					if err := os.Remove(path); err == nil {
						released[sum] = true
					}
				}
			case released[sum]:
			default:
				yield(dispatch.Item{}, fmt.Errorf("store: %s's content is lost", where))
				return
			}
			if !yield(dispatch.PartItem(f), nil) {
				return
			}
			if link != "" {
				_ = os.Remove(filepath.Dir(link))
			}
			if !yield(dispatch.OutputItem(dispatch.Output{Kind: dispatch.RecordOutput, Name: where}), nil) {
				return
			}
		}
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type picky struct {
	fail map[string]bool

	mu    sync.Mutex
	calls int
}

func (p *picky) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "picky", Version: "1", Config: "test"}
}

func (p *picky) Prepare(_ context.Context, part dispatch.Part, _ string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		p.mu.Lock()
		p.calls++
		p.mu.Unlock()
		if p.fail[part.Where] {
			yield(dispatch.Item{}, errors.New("picky: told to fail"))
			return
		}
		yield(dispatch.OutputItem(dispatch.Output{Kind: dispatch.TextOutput, Name: "content", Value: []byte(part.SHA256)}), nil)
	}
}

func onDisk(t *testing.T, dir string) (int, int64) {
	t.Helper()
	n, size := 0, int64(0)
	err := filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if e.Type().IsRegular() {
			info, err := e.Info()
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			n++
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n, size
}

func heavyStore(n, every, size int) *store {
	s := &store{contents: map[string][]byte{}}
	pad := func(key string) []byte {
		return append([]byte(key), bytes.Repeat([]byte{0}, size-len(key))...)
	}
	s.contents["shared"] = pad("shared")
	for i := range n {
		ref := "shared"
		if i%every != 0 {
			ref = fmt.Sprintf("own-%d", i)
			s.contents[ref] = pad(ref)
		}
		s.refs = append(s.refs, ref)
	}
	return s
}

type probe struct {
	peakFiles int
	peakBytes int64
	outcomes  map[string]dispatch.Outcome
	reasons   map[string]string
	last      dispatch.Event
}

func probeStore(t *testing.T, d *dispatch.Dispatcher, dir string, each func(*dispatch.Event)) probe {
	t.Helper()
	pr := probe{outcomes: map[string]dispatch.Outcome{}, reasons: map[string]string{}}
	for ev, err := range d.Dispatch(t.Context(), dispatch.Input{Path: input(t, "store.txt", "store")}) {
		if err != nil {
			t.Fatal(err)
		}
		n, size := onDisk(t, dir)
		pr.peakFiles, pr.peakBytes = max(pr.peakFiles, n), max(pr.peakBytes, size)
		if each != nil {
			each(&ev)
		}
		switch {
		case ev.Output != nil:
		case ev.Part.Depth == 1:
			pr.outcomes[ev.Part.Where] = ev.Outcome
			pr.reasons[ev.Part.Where] = ev.Reason
		default:
			pr.last = ev
		}
	}
	return pr
}

func storeDispatcher(t *testing.T, s *store, leaf dispatch.Stage) (*dispatch.Dispatcher, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages: dispatch.Stages{route.Text: s, route.Unrouted: leaf}, Index: dispatch.NewMemoryIndex(0), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d, dir
}

func TestReleaseBoundsDisk(t *testing.T) {
	const n, every, size = 10_000, 10, 1024
	s := heavyStore(n, every, size)
	leaf := &picky{}
	d, dir := storeDispatcher(t, s, leaf)
	pr := probeStore(t, d, dir, nil)

	if pr.last.Outcome != dispatch.Prepared || pr.last.Children != n || pr.last.Outputs != n {
		t.Fatalf("the store: %s, %d children, %d outputs", pr.last.Outcome, pr.last.Children, pr.last.Outputs)
	}
	distinct := n - n/every + 1
	counts := map[dispatch.Outcome]int{}
	for i := range n {
		where := fmt.Sprintf("record %d", i)
		got, ok := pr.outcomes[where]
		if !ok {
			t.Fatalf("%s: no event", where)
		}
		counts[got]++
		if want := dispatch.Prepared; i%every == 0 && i > 0 {
			if got != dispatch.Seen {
				t.Fatalf("%s, the shared content again: %s, %q", where, got, pr.reasons[where])
			}
		} else if got != want {
			t.Fatalf("%s: %s, %q", where, got, pr.reasons[where])
		}
	}
	if counts[dispatch.Prepared] != distinct || counts[dispatch.Seen] != n/every-1 {
		t.Errorf("outcomes %v, want %d prepared and %d seen", counts, distinct, n/every-1)
	}
	if leaf.calls != distinct || s.released() != distinct {
		t.Errorf("%d contents prepared and %d released, want each of the %d once", leaf.calls, s.released(), distinct)
	}
	if pr.peakFiles > 3 || pr.peakBytes > 3*size {
		t.Errorf("the working directory held up to %d files, %d bytes", pr.peakFiles, pr.peakBytes)
	}
	t.Logf("peak %d files, %d bytes, of %d contents, %d bytes", pr.peakFiles, pr.peakBytes, distinct, distinct*size)
}

func TestReleaseKeepsUnrecorded(t *testing.T) {
	const n, every, size = 1000, 10, 64
	s := heavyStore(n, every, size)
	leaf := &picky{fail: map[string]bool{"record 0": true}}
	d, dir := storeDispatcher(t, s, leaf)
	pr := probeStore(t, d, dir, nil)
	if pr.last.Outcome != dispatch.Prepared {
		t.Fatalf("the store: %s, %q", pr.last.Outcome, pr.last.Reason)
	}
	for i := 0; i < n; i += every {
		where := fmt.Sprintf("record %d", i)
		want := dispatch.Seen
		switch i {
		case 0:
			want = dispatch.Failed
		case every:
			want = dispatch.Prepared
		}
		if got := pr.outcomes[where]; got != want {
			t.Fatalf("%s: %s, %q, want %s", where, got, pr.reasons[where], want)
		}
	}
	if distinct := n - n/every + 1; s.released() != distinct {
		t.Errorf("%d released, want %d", s.released(), distinct)
	}
}

func TestReleaseRefused(t *testing.T) {
	t.Run("a lying SHA-256", func(t *testing.T) {
		s := heavyStore(30, 10, 64)
		s.lie = true
		d, dir := storeDispatcher(t, s, &picky{})
		pr := probeStore(t, d, dir, nil)
		if s.released() != 0 || pr.last.Outcome != dispatch.Prepared || pr.outcomes["record 10"] != dispatch.Failed ||
			!strings.Contains(pr.reasons["record 10"], dispatch.ErrSHA256.Error()) {
			t.Fatalf("%d released; the store %s; record 10 %s, %q", s.released(), pr.last.Outcome, pr.outcomes["record 10"], pr.reasons["record 10"])
		}
	})
	t.Run("no file", func(t *testing.T) {
		d, err := dispatch.New(dispatch.Config{
			Content: dispatch.NewMemoryContent(), Puts: 2,
			Stages: dispatch.Stages{route.Text: ghost{}}, Index: dispatch.NewMemoryIndex(0), Dir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		events := dispatchAll(t, d, input(t, "a.txt", "ghost"), nil)
		wantSteps(t, events, []step{{"ghost.bin", 1, dispatch.Failed}, {"a.txt", 0, dispatch.Prepared}})
		if ev := events[0]; !strings.Contains(ev.Reason, "no file") {
			t.Errorf("ghost.bin: %q", ev.Reason)
		}
	})
}

type ghost struct{}

func (ghost) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "ghost", Version: "1", Config: "test"}
}

func (ghost) Prepare(context.Context, dispatch.Part, string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		yield(dispatch.PartItem(dispatch.Found{Name: "ghost.bin", SHA256: sum("ghost")}), nil)
	}
}

func unstagedDispatcher(t *testing.T, s *store, index dispatch.Index) (*dispatch.Dispatcher, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := dispatch.New(dispatch.Config{Content: dispatch.NewMemoryContent(), Puts: 2, Stages: dispatch.Stages{route.Text: s}, Index: index, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return d, dir
}

func TestReleaseUnstaged(t *testing.T) {
	const n, every, size = 10_000, 10, 1024
	s := heavyStore(n, every, size)
	index := dispatch.NewMemoryIndex(0)
	d, dir := unstagedDispatcher(t, s, index)
	var first dispatch.Event
	pr := probeStore(t, d, dir, func(ev *dispatch.Event) {
		if ev.Output != nil || ev.Part.Depth != 1 || ev.Part.SHA256 != sha(s.contents["shared"]) {
			return
		}
		if ev.Part.Where == "record 0" {
			first = *ev
			return
		}
		if ev.Part.Path != "" || ev.Reason != first.Reason || ev.Part.ContentType.Route != first.Part.ContentType.Route ||
			!reflect.DeepEqual(ev.Part.Flags, first.Part.Flags) {
			t.Errorf("%s: path %q, %q, %s, flags %v; the first: %q, %s, flags %v", ev.Part.Where,
				ev.Part.Path, ev.Reason, ev.Part.ContentType.Route, ev.Part.Flags, first.Reason, first.Part.ContentType.Route, first.Part.Flags)
		}
	})

	if pr.last.Outcome != dispatch.Prepared || pr.last.Children != n {
		t.Fatalf("the store: %s, %q, %d children", pr.last.Outcome, pr.last.Reason, pr.last.Children)
	}
	for i := range n {
		where := fmt.Sprintf("record %d", i)
		if got := pr.outcomes[where]; got != dispatch.NoProcessor {
			t.Fatalf("%s: %s, %q", where, got, pr.reasons[where])
		}
	}
	if distinct := n - n/every + 1; s.released() != distinct {
		t.Errorf("%d released, want each of the %d contents once", s.released(), distinct)
	}
	if !strings.Contains(first.Reason, "no stage") {
		t.Errorf("record 0: %q", first.Reason)
	}
	if pr.peakFiles > 3 || pr.peakBytes > 3*size {
		t.Errorf("the working directory held up to %d files, %d bytes", pr.peakFiles, pr.peakBytes)
	}
	t.Logf("peak %d files, %d bytes", pr.peakFiles, pr.peakBytes)

	leaf := &picky{}
	later, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages: dispatch.Stages{route.Text: heavyStore(1, 1, size), route.Unrouted: leaf}, Index: index, Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	events := dispatchAll(t, later, input(t, "later.txt", "later"), nil)
	if len(events) != 2 || events[0].Outcome != dispatch.Prepared || leaf.calls != 1 {
		t.Fatalf("with a stage registered: %d events, the content %s, %d prepared", len(events), events[0].Outcome, leaf.calls)
	}
}
