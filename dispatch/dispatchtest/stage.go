package dispatchtest

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/standards-lab/docproc/dispatch"
)

// Stage is the fixture stage: a stage that runs no tool, on any platform, whose part is lines of
// directives, so a test writes the tree of parts it dispatches as text. Register it for
// route.Text, which a part of plain text routes to.
//
// It first yields a text output in a file, named "lines", holding the part's line count, its Meta
// [dispatch.MetaCount] the count too. Then, line by line, a directive:
//
//   - "file NAME" finds a part named NAME, holding the content its world gives NAME, written into
//     the part's directory, where "line N", N the directive's line from 1.
//   - "record JSON" yields an inline record output of JSON, named "record N", its media type
//     application/json.
//   - "fail", "noproc", and "encrypted" end the stage with a failure, an error matching
//     dispatch.ErrNoProcessor, and one matching dispatch.ErrEncrypted.
//
// Any other line is text, and does nothing. Its identity is "fixture", version "1".
type Stage struct {
	world map[string]string

	mu    sync.Mutex
	calls map[string]int
}

// NewStage returns the fixture stage over world, the content a "file NAME" directive finds, by
// name; a name world lacks finds an empty part.
func NewStage(world map[string]string) *Stage {
	return &Stage{world: world, calls: map[string]int{}}
}

// Identity returns the fixture stage's identity.
func (s *Stage) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "fixture", Version: "1", Config: "fixture"}
}

// Calls returns how many times the stage prepared a part named name.
func (s *Stage) Calls(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[name]
}

// Prepare reads p's directives, as Stage says.
func (s *Stage) Prepare(_ context.Context, p dispatch.Part, dir string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		s.mu.Lock()
		s.calls[p.Name]++
		s.mu.Unlock()
		b, err := os.ReadFile(p.Path)
		if err != nil {
			yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w", err))
			return
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		count := strconv.Itoa(len(lines))
		out := filepath.Join(dir, "lines.txt")
		if err := os.WriteFile(out, []byte(count), 0o644); err != nil {
			yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w", err))
			return
		}
		o := dispatch.Output{
			Kind: dispatch.TextOutput, Name: "lines", MediaType: "text/plain", Path: out,
			Meta: map[string]string{dispatch.MetaCount: count},
		}
		if !yield(dispatch.OutputItem(o), nil) {
			return
		}
		for i, line := range lines {
			verb, arg, _ := strings.Cut(line, " ")
			n := strconv.Itoa(i + 1)
			switch verb {
			case "file":
				path := filepath.Join(dir, filepath.Base(arg))
				if err := os.WriteFile(path, []byte(s.world[arg]), 0o644); err != nil {
					yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w", err))
					return
				}
				if !yield(dispatch.PartItem(dispatch.Found{Path: path, Name: arg, Where: "line " + n}), nil) {
					return
				}
			case "record":
				o := dispatch.Output{
					Kind: dispatch.RecordOutput, Name: "record " + n, MediaType: "application/json", Value: []byte(arg),
				}
				if !yield(dispatch.OutputItem(o), nil) {
					return
				}
			case "fail":
				yield(dispatch.Item{}, errors.New("fixture stage: told to fail"))
				return
			case "noproc":
				yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w for this", dispatch.ErrNoProcessor))
				return
			case "encrypted":
				yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w content", dispatch.ErrEncrypted))
				return
			}
		}
	}
}
