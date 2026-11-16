package stages_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/stages"
)

// TestMain runs the package's tests in a local zone that isn't UTC; they run no tool.
func TestMain(m *testing.M) {
	tooltest.Main(m, nil)
}

// TestParseRoutes: every route Routes names parses, in the order given, and a name that is no
// route is refused, matching ErrRequest and naming it.
func TestParseRoutes(t *testing.T) {
	t.Parallel()
	known := stages.Routes()
	names := make([]string, 0, len(known))
	for _, r := range slices.Backward(known) {
		names = append(names, string(r))
	}
	got, err := stages.ParseRoutes(names)
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(known)
	slices.Reverse(want)
	if !slices.Equal(got, want) {
		t.Errorf("parsed %v, want %v", got, want)
	}
	_, err = stages.ParseRoutes([]string{string(known[0]), "telepathy"})
	if !errors.Is(err, stages.ErrRequest) || !strings.Contains(err.Error(), `"telepathy"`) {
		t.Errorf("an unknown route: %v, want ErrRequest naming it", err)
	}
}
