//go:build linux

package stages_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

// oneSection is the head of a OneNote section, by which route routes a part to the onenote
// stage.
const oneSection = "\xe4\x52\x5c\x7b\x8c\xd8\xa7\x4d\xae\xb1\x53\x78\xd0\x29\x96\xd3\x00\x00\x00\x00\x00\x00\x00\x00"

// testMemory is the memory bound the classification's stand-in runs under, tooltest's room for a
// stand-in, a test binary that may be race-built, to start.
const testMemory = tooltest.Memory

// onenoteOnly is every route but route.OneNote, for a Build that wires the onenote stage alone.
func onenoteOnly() []route.Route {
	return slices.DeleteFunc(stages.Routes(), func(r route.Route) bool { return r == route.OneNote })
}

// standinSet returns Build's Set under opts over a lookup that finds docproc-onenote as the
// stand-in, set to s, and no other program, with the stand-in's link.
func standinSet(t *testing.T, s onenoteSettings, opts stages.Options) (stages.Set, string) {
	t.Helper()
	link := tooltest.Link(t, t.TempDir(), "docproc-onenote", s)
	lookup := func(program string) (string, error) {
		if program == "docproc-onenote" {
			return link, nil
		}
		return "", fmt.Errorf("%s isn't here", program)
	}
	set, err := stages.Build(t.Context(), lookup, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range set.Tools {
		if tl.Program == "docproc-onenote" && tl.Error != "" {
			t.Fatalf("the stand-in docproc-onenote was refused: %s", tl.Error)
		}
	}
	return set, link
}

// dispatchSet dispatches a OneNote section through set's stages, with set's inspection, over a
// new index, and returns the section's own event and whether the index records its entry.
func dispatchSet(t *testing.T, set stages.Set) (dispatched, bool) {
	t.Helper()
	index := dispatch.NewMemoryIndex(0)
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2, Stages: set.Stages, Inspect: set.Inspect, Index: index, Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	events := dispatchAll(t, d, input(t, "in.one", oneSection), nil)
	ev := events[len(events)-1]
	_, recorded, err := index.Lookup(t.Context(), dispatch.Key{SHA256: ev.Part.SHA256, Stage: ev.Stage})
	if err != nil {
		t.Fatal(err)
	}
	return ev, recorded
}

// TestClassification dispatches a OneNote section through Build's Set over the stand-in
// docproc-onenote, under an 8 GiB memory bound and an idle bound, once for each line of
// the package doc's table: a tool's own exit, 3 and 4, its own fatal signal, its reason naming the
// signal and the bound, its own time bound, and output that doesn't parse are the content's,
// failed and recorded; exit 1 and 2, and a SIGKILL or SIGTERM docproc didn't send, are the run's,
// failed and never recorded; and a refusal internal/cli classes is encrypted or read elsewhere.
// The idle bound, 5 s, runs from the run's start, the stand-in's start included, so it is far
// past any start on a loaded machine; only the case that holds its run reaches it.
func TestClassification(t *testing.T) {
	t.Parallel()
	const notebook = `{"line":"notebook","schema":"2","input":"section","name":"S"}` + "\n"
	refused := func(reason string) onenoteSettings {
		return onenoteSettings{Stderr: "docproc-onenote: error: " + reason + "\n", Exit: 3}
	}
	for _, c := range []struct {
		name     string
		settings onenoteSettings
		outcome  dispatch.Outcome
		recorded bool
		reason   string
	}{
		{"exit 3, refused", refused("not a OneNote section, notebook, or package"), dispatch.Failed, true, "refused"},
		{"exit 4, the tool's fault", onenoteSettings{Stdout: notebook, Stderr: "docproc-onenote: error: internal\n", Exit: 4}, dispatch.Failed, true, "failed: internal"},
		{"SIGSEGV", onenoteSettings{Stdout: notebook, Signal: "SEGV"}, dispatch.Failed, true, "killed by SIGSEGV under a 8 GiB memory bound"},
		{"SIGABRT", onenoteSettings{Stdout: notebook, Signal: "ABRT"}, dispatch.Failed, true, "killed by SIGABRT under a 8 GiB memory bound"},
		{"SIGBUS", onenoteSettings{Signal: "BUS"}, dispatch.Failed, true, "killed by SIGBUS under a 8 GiB memory bound"},
		{"SIGFPE", onenoteSettings{Signal: "FPE"}, dispatch.Failed, true, "killed by SIGFPE under a 8 GiB memory bound"},
		{"SIGILL", onenoteSettings{Signal: "ILL"}, dispatch.Failed, true, "killed by SIGILL under a 8 GiB memory bound"},
		{"its own time bound", onenoteSettings{HoldMS: 60000}, dispatch.Failed, true, "timed out"},
		{"output that doesn't parse", onenoteSettings{Stdout: notebook + `{"line":"page","id":` + "\n"}, dispatch.Failed, true, ""},
		{"exit 1, not written", onenoteSettings{Stderr: "docproc-onenote: error: writing stdout: Broken pipe\n", Exit: 1}, dispatch.Failed, false, "not written"},
		{"exit 2, usage", onenoteSettings{Stderr: "docproc-onenote: error: unknown option\n", Exit: 2}, dispatch.Failed, false, "usage"},
		{"SIGKILL docproc didn't send", onenoteSettings{Stdout: notebook, Signal: "KILL"}, dispatch.Failed, false, "killed by SIGKILL"},
		{"SIGTERM docproc didn't send", onenoteSettings{Signal: "TERM"}, dispatch.Failed, false, "killed by SIGTERM"},
		{"encrypted", refused("encrypted section (a password-protected section); remove its password in OneNote first"), dispatch.Encrypted, true, "encrypted"},
		{"read elsewhere", refused(cli.TableOfContents), dispatch.ReadElsewhere, true, cli.TableOfContents},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			set, _ := standinSet(t, c.settings, stages.Options{Exclude: onenoteOnly(), Memory: testMemory, Idle: 5 * time.Second})
			ev, recorded := dispatchSet(t, set)
			if ev.Outcome != c.outcome || recorded != c.recorded || !strings.Contains(ev.Reason, c.reason) {
				t.Errorf("%s, recorded %t, %q; want %s, recorded %t, %q in the reason", ev.Outcome, recorded, ev.Reason, c.outcome, c.recorded, c.reason)
			}
			if c.outcome == dispatch.Failed && errors.Is(ev.Err, dispatch.ErrRefused) != c.recorded {
				t.Errorf("err %v: ErrRefused %t, want %t", ev.Err, errors.Is(ev.Err, dispatch.ErrRefused), c.recorded)
			}
		})
	}
}

// TestClassificationNoBound: a death by the tool's own signal with no memory bound names the
// signal alone, and a stage built under no bound passes no prlimit.
func TestClassificationNoBound(t *testing.T) {
	t.Parallel()
	set, link := standinSet(t, onenoteSettings{Signal: "ABRT"}, stages.Options{Exclude: onenoteOnly()})
	ev, recorded := dispatchSet(t, set)
	if ev.Outcome != dispatch.Failed || !recorded || !strings.Contains(ev.Reason, "killed by SIGABRT") || strings.Contains(ev.Reason, "memory bound") {
		t.Errorf("%s, recorded %t, %q", ev.Outcome, recorded, ev.Reason)
	}
	if runs := tooltest.Runs(t, link); len(runs) != 2 || runs[1].Program != "docproc-onenote" {
		t.Errorf("runs %+v", runs)
	}
}

// TestClassify: the facts classify reads apart from a run, case by case: the context's end and a
// failed start are the run's; a timed-out run is the content's whatever its exit; 7-Zip's command
// line error is the run's and its out of memory the content's, soffice's 77, a fatal error at its
// start, is the run's and its 1 the content's, while any other tool's every nonzero exit is the
// content's; JSON that doesn't parse and a format package's content errors
// are the content's; and a signal names the bound in the largest unit that holds it whole.
func TestClassify(t *testing.T) {
	t.Parallel()
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	exit := func(program string, code int) error { return &tool.ExitError{Program: program, Code: code} }
	for _, c := range []struct {
		name    string
		ctx     context.Context
		err     error
		memory  int64
		content bool
		reason  string
	}{
		{"the context ended", ended, exit("gs", 1), 0, false, ""},
		{"a canceled run", t.Context(), fmt.Errorf("tool: gs: %w", context.Canceled), 0, false, ""},
		{"a failed start", t.Context(), &os.PathError{Op: "fork/exec", Path: "/gone", Err: os.ErrNotExist}, 0, false, ""},
		{"timed out after exit 1", t.Context(), &tool.ExitError{Program: "docproc-mail", Code: 1, TimedOut: true}, 0, true, ""},
		{"gs's exit 1", t.Context(), exit("gs", 1), 0, true, ""},
		{"qpdf's exit 2", t.Context(), exit("qpdf", 2), 0, true, ""},
		{"7zz's command line error", t.Context(), exit("7zz", 7), 0, false, ""},
		{"7zz's out of memory", t.Context(), exit("7zz", 8), 0, true, ""},
		{"soffice's fatal error at its start", t.Context(), exit("soffice", 77), 0, false, ""},
		{"soffice's exit 1", t.Context(), exit("soffice", 1), 0, true, ""},
		{"docproc-ooxml's usage", t.Context(), exit("docproc-ooxml", 2), 0, false, ""},
		{"a signal under 512 MiB", t.Context(), &tool.ExitError{Program: "docproc-ooxml", Code: -1, Signal: "SIGABRT", Message: "libssl"}, 512 << 20,
			true, "docproc-ooxml: killed by SIGABRT under a 512 MiB memory bound: libssl"},
		{"a signal under an odd bound", t.Context(), &tool.ExitError{Program: "gs", Code: -1, Signal: "SIGSEGV"}, 1000, true, "gs: killed by SIGSEGV under a 1000 bytes memory bound"},
		{"SIGPIPE", t.Context(), &tool.ExitError{Program: "gs", Code: -1, Signal: "SIGPIPE"}, 0, false, ""},
		{"JSON that doesn't parse", t.Context(), fmt.Errorf("document: %w", &jsontext.SyntacticError{}), 0, true, ""},
		{"no audio", t.Context(), fmt.Errorf("audio: %w", audio.ErrNoAudio), 0, true, ""},
		{"not an archive", t.Context(), fmt.Errorf("sevenzip: %w", sevenzip.ErrNotArchive), 0, true, ""},
		{"docproc's side of the pipe", t.Context(), errors.New("write |1: broken pipe"), 0, false, ""},
	} {
		content, reason := stages.Classify(c.ctx, c.err, c.memory)
		if content != c.content || c.reason != "" && reason != c.reason {
			t.Errorf("%s: content %t, %q; want %t, %q", c.name, content, reason, c.content, c.reason)
		}
	}
	if err := stages.PartErr(t.Context(), exit("gs", 1), 0); !errors.Is(err, dispatch.ErrRefused) {
		t.Errorf("partErr of the content's: %v, want ErrRefused", err)
	}
	for _, named := range []error{dispatch.ErrEncrypted, dispatch.ErrReadElsewhere, dispatch.ErrNoProcessor} {
		if err := stages.PartErr(t.Context(), named, 0); err != named {
			t.Errorf("partErr(%v) = %v, want it as it is", named, err)
		}
	}
}

// TestBuildBounds: Build hands each tool its run bound by the tool's kind, a stream tool, such as
// docproc-onenote, Idle: a Timeout leaves its long-held run alone, while Idle ends it; and Memory
// reaches the tool, the stand-in run under a RLIMIT_AS of the bound.
func TestBuildBounds(t *testing.T) {
	t.Parallel()
	hold := onenoteSettings{HoldMS: 1500, Stdout: `{"line":"notebook","schema":"2","input":"section","name":"S"}` + "\n"}
	set, _ := standinSet(t, hold, stages.Options{Exclude: onenoteOnly(), Timeout: 200 * time.Millisecond})
	if ev, _ := dispatchSet(t, set); ev.Outcome != dispatch.Prepared {
		t.Errorf("a stream tool under a unit run's Timeout: %s, %q; want prepared", ev.Outcome, ev.Reason)
	}
	set, _ = standinSet(t, hold, stages.Options{Exclude: onenoteOnly(), Idle: 200 * time.Millisecond})
	if ev, recorded := dispatchSet(t, set); ev.Outcome != dispatch.Failed || !recorded || !strings.Contains(ev.Reason, "timed out") {
		t.Errorf("a stream tool under Idle: %s, recorded %t, %q; want failed, recorded, timed out", ev.Outcome, recorded, ev.Reason)
	}
	set, _ = standinSet(t, onenoteSettings{Limit: true, Stdout: hold.Stdout}, stages.Options{Exclude: onenoteOnly(), Memory: testMemory})
	ev, _ := dispatchSet(t, set)
	if want := (dispatch.Warning{Tool: "docproc-onenote", Where: "limit", What: strconv.Itoa(testMemory)}); !slices.Contains(ev.Warnings, want) {
		t.Errorf("the bounded stand-in's warnings %+v, want its RLIMIT_AS the bound", ev.Warnings)
	}
}
