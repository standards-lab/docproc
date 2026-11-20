package dispatchtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
)

// Stage is the fixture stage: a stage that runs no tool, on any 64-bit platform, whose part is
// lines of directives, so a test writes the tree of parts it dispatches as text. Register it for
// route.Text, which a part of plain text routes to.
//
// It first yields a text output in a file, named "lines", holding the part's line count, its Meta
// [dispatch.MetaCount] the count too. Then, line by line, a directive, N the directive's line from
// 1:
//
//   - "file NAME" finds a part named NAME, holding the content its world gives NAME, written into
//     the part's directory, where "line N"; "sum NAME" finds it claiming its content's SHA-256,
//     and "lie NAME" claiming another's.
//   - "unread NAME" finds an unread child named NAME, where "line N", encrypted, with no file.
//   - "record JSON" yields an inline record output of JSON, named "record N", its media type
//     application/json.
//   - "message ID" yields an inline text output named "message ID", a store's message, its
//     Item.After ID, a decimal cursor. A part whose After is set is read from the line after the
//     message it names, nothing before it yielded, "lines" among it, as mail resumes a store; a
//     cursor no message line names ends the stage with an error.
//   - "warn TEXT" yields a warning of TEXT, its tool "fixture", where "line N".
//   - "flag CLASS CHECK" raises a flag about the part, of CHECK and CLASS, its evidence "line N".
//     The flags of the part's first lines, before any other directive, are raised before the
//     "lines" output, so the dispatcher weighs them before the part's first output.
//   - "fail", "noproc", "encrypted", and "refused" end the stage with a failure and errors
//     matching dispatch.ErrNoProcessor, dispatch.ErrEncrypted, and dispatch.ErrRefused.
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
		from := 0
		if p.After != 0 {
			cursor := "message " + strconv.FormatUint(p.After, 10)
			i := slices.Index(lines, cursor)
			if i < 0 {
				yield(dispatch.Item{}, fmt.Errorf("fixture stage: no %q to resume after", cursor))
				return
			}
			from = i + 1
		}
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
		if from == 0 {
			// The leading flags, before the first output.
			for ; from < len(lines) && strings.HasPrefix(lines[from], "flag "); from++ {
				if !s.directive(lines[from], from+1, dir, yield) {
					return
				}
			}
			if !yield(dispatch.OutputItem(o), nil) {
				return
			}
		}
		for i := from; i < len(lines); i++ {
			if !s.directive(lines[i], i+1, dir, yield) {
				return
			}
		}
	}
}

// directive yields what line, the part's line n, directs, reporting whether the stage goes on.
func (s *Stage) directive(line string, n int, dir string, yield func(dispatch.Item, error) bool) bool {
	verb, arg, _ := strings.Cut(line, " ")
	where := "line " + strconv.Itoa(n)
	switch verb {
	case "file", "sum", "lie":
		path := filepath.Join(dir, filepath.Base(arg))
		if err := os.WriteFile(path, []byte(s.world[arg]), 0o644); err != nil {
			yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w", err))
			return false
		}
		f := dispatch.Found{Path: path, Name: arg, Where: where}
		switch verb {
		case "sum":
			f.SHA256 = sha(s.world[arg])
		case "lie":
			f.SHA256 = sha("not " + s.world[arg])
		}
		return yield(dispatch.PartItem(f), nil)
	case "unread":
		return yield(dispatch.PartItem(dispatch.Found{
			Name: arg, Where: where, Outcome: dispatch.Encrypted, Reason: "fixture stage: an encrypted entry",
		}), nil)
	case "record":
		return yield(dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.RecordOutput, Name: "record " + strconv.Itoa(n), MediaType: "application/json", Value: []byte(arg),
		}), nil)
	case "message":
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			yield(dispatch.Item{}, fmt.Errorf("fixture stage: message: %w", err))
			return false
		}
		item := dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.TextOutput, Name: line, MediaType: "text/plain", Value: []byte(line),
		})
		item.After = id
		return yield(item, nil)
	case "warn":
		return yield(dispatch.WarningItem(dispatch.Warning{Tool: "fixture", Where: where, What: arg}), nil)
	case "flag":
		class, check, _ := strings.Cut(arg, " ")
		return yield(dispatch.FlagItem(inspect.Flag{Check: inspect.Check(check), Class: inspect.Class(class), Evidence: where}), nil)
	case "fail":
		yield(dispatch.Item{}, errors.New("fixture stage: told to fail"))
		return false
	case "noproc":
		yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w for this", dispatch.ErrNoProcessor))
		return false
	case "encrypted":
		yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w content", dispatch.ErrEncrypted))
		return false
	case "refused":
		yield(dispatch.Item{}, fmt.Errorf("fixture stage: %w: told to refuse", dispatch.ErrRefused))
		return false
	}
	return true
}

// sha returns content's SHA-256, lowercase hex.
func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
