//go:build linux

package cli_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

// standinTool links the stand-in as name with settings and builds its Tool through the exec
// runner, its version checked by cli.VersionPattern.
func standinTool(t *testing.T, name string, settings standinSettings) *tool.Tool {
	t.Helper()
	link := tooltest.Link(t, t.TempDir(), name, settings)
	tl, err := tool.NewExec(t.Context(), tool.Spec{
		Program: link, VersionArgs: []string{"--version"}, VersionPattern: cli.VersionPattern,
		Minimum: "0.1.0", KillAfter: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// read ranges over a run of tl whose first line is a "document", returning its lines' kinds,
// its warnings, and its final error; it fails the test on a warning that follows the error.
func read(t *testing.T, tl *tool.Tool) (kinds []string, warnings []dispatch.Warning, err error) {
	t.Helper()
	for line, lerr := range cli.Read(t.Context(), cli.Run{Tool: tl, Args: []string{"input"}, First: "document"}) {
		switch {
		case err != nil:
			t.Fatalf("an item after the final error %v: %+v, %v", err, line, lerr)
		case lerr != nil:
			err = lerr
		case line.Warning != nil:
			warnings = append(warnings, *line.Warning)
		default:
			kinds = append(kinds, line.Kind)
		}
	}
	return kinds, warnings, err
}

// TestReadLines: each stdout line is yielded with its kind and its JSON, as written.
func TestReadLines(t *testing.T) {
	t.Parallel()
	stdout := `{"line":"document","schema":"2","format":"xlsx"}` + "\n" +
		`{"line":"sheet","name":"A"}` + "\n" +
		`{"line":"rows","rows":[{"row":1}]}` + "\n"
	tl := standinTool(t, "tool", standinSettings{Stdout: stdout})
	var raws []string
	var kinds []string
	for line, err := range cli.Read(t.Context(), cli.Run{Tool: tl, First: "document"}) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, line.Kind)
		raws = append(raws, string(line.Raw)+"\n")
	}
	if want := []string{"document", "sheet", "rows"}; !slices.Equal(kinds, want) {
		t.Errorf("kinds %q, want %q", kinds, want)
	}
	if got := strings.Join(raws, ""); got != stdout {
		t.Errorf("raw %q, want %q", got, stdout)
	}
}

// TestReadSchema: a first line of another kind or schema, or none from a run that exited 0, is
// ErrSchema; a line that isn't a JSON object with a kind is an error, not ErrSchema.
func TestReadSchema(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, stdout string
		schema       bool
	}{
		{"schema 1", `{"line":"document","schema":"1"}` + "\n", true},
		{"no schema", `{"line":"document"}` + "\n", true},
		{"another kind", `{"line":"store","schema":"2"}` + "\n", true},
		{"no line", "", true},
		{"not JSON", `{"line":"document","schema":"2"}` + "\nnot JSON\n", false},
		{"no kind", `{"line":"document","schema":"2"}` + "\n{\"x\":1}\n", false},
		{"an array", "[1]\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := read(t, standinTool(t, "tool", standinSettings{Stdout: c.stdout}))
			if err == nil || errors.Is(err, cli.ErrSchema) != c.schema {
				t.Fatalf("got %v, want an error matching ErrSchema %t", err, c.schema)
			}
			if !strings.HasPrefix(err.Error(), "cli: tool: ") {
				t.Errorf("error %q doesn't name the tool", err)
			}
		})
	}
}

// TestReadWarnings: each shared case's stderr line is a warning with its where and what, the
// escape inverted, the tool named; a control character escaped by its code point reads back; a
// line that fits no warning is one whose what is the line alone; a blank line is none.
func TestReadWarnings(t *testing.T) {
	t.Parallel()
	cases := readCases(t)
	var stderr strings.Builder
	var want []dispatch.Warning
	for _, c := range cases.Warnings {
		stderr.WriteString(c.Line + "\n")
		want = append(want, dispatch.Warning{Tool: "tool", Where: c.Where, What: c.What})
	}
	stderr.WriteString("tool: warning: tab\\u{9}here:\\ \\u{1b}[0m: bell\\u{7} and next line\\u{85}\n")
	want = append(want, dispatch.Warning{Tool: "tool", Where: "tab\there: \x1b[0m", What: "bell\a and next line\u0085"})
	stderr.WriteString("\n" + "thread 'main' panicked at src/main.rs:1:1\n" + "tool: warning: no where\n" + "tool: warning: ends unended")
	want = append(want,
		dispatch.Warning{Tool: "tool", What: "thread 'main' panicked at src/main.rs:1:1"},
		dispatch.Warning{Tool: "tool", What: "tool: warning: no where"},
		dispatch.Warning{Tool: "tool", What: "tool: warning: ends unended"},
	)
	tl := standinTool(t, "tool", standinSettings{Stdout: `{"line":"document","schema":"2"}` + "\n", Stderr: stderr.String()})
	_, warnings, err := read(t, tl)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(warnings, want) {
		t.Errorf("warnings\n%q\nwant\n%q", warnings, want)
	}
}

// TestReadExitCodes: each shared exit code 1 to 4 is an *ExitError matching its own error and no
// other's, unwrapping to the run's *tool.ExitError, with no reason no refusal class; 0 is none.
func TestReadExitCodes(t *testing.T) {
	t.Parallel()
	all := []error{cli.ErrNotWritten, cli.ErrUsage, cli.ErrRefused, cli.ErrFailed}
	for _, c := range readCases(t).ExitCodes {
		t.Run(fmt.Sprint(c.Code), func(t *testing.T) {
			t.Parallel()
			_, _, err := read(t, standinTool(t, "tool", standinSettings{Exit: c.Code}))
			if c.Code == 0 {
				if !errors.Is(err, cli.ErrSchema) {
					t.Fatalf("exit 0 with no line: %v, want ErrSchema", err)
				}
				return
			}
			exit, ok := errors.AsType[*cli.ExitError](err)
			if !ok || exit.Code != c.Code {
				t.Fatalf("%s: got %v, want an *ExitError of code %d", c.Meaning, err, c.Code)
			}
			if _, ok := errors.AsType[*tool.ExitError](err); !ok {
				t.Errorf("%v doesn't unwrap to a *tool.ExitError", err)
			}
			if exit.Refusal() != nil {
				t.Errorf("%v with no reason: refusal class %v", err, exit.Refusal())
			}
			for i, target := range all {
				if errors.Is(err, target) != (i+1 == c.Code) {
					t.Errorf("errors.Is(code %d, %v) = %t", c.Code, target, i+1 != c.Code)
				}
			}
		})
	}
}

// TestReadErrors: each shared case's error line gives its reason as the run's message, and the
// run's message is the last error line, whatever follows it.
func TestReadErrors(t *testing.T) {
	t.Parallel()
	cases := readCases(t)
	for _, c := range cases.Errors {
		t.Run(c.Reason, func(t *testing.T) {
			t.Parallel()
			tl := standinTool(t, "tool", standinSettings{Stderr: "tool: error: an earlier reason\n" + c.Line + "\ntool: warning: w: after\n", Exit: 4})
			_, warnings, err := read(t, tl)
			exit, ok := errors.AsType[*cli.ExitError](err)
			if !ok || exit.Message != c.Reason {
				t.Fatalf("got %v, want an *ExitError with message %q", err, c.Reason)
			}
			if want := "tool: failed: " + c.Reason; err.Error() != want {
				t.Errorf("error %q, want %q", err, want)
			}
			if len(warnings) != 1 || warnings[0].What != "after" {
				t.Errorf("warnings %q", warnings)
			}
		})
	}
}

// TestReadWarnsThenFails: a run that warns past 1 MiB and then fails keeps every warning, each
// yielded before the final error, and its last error line's message.
func TestReadWarnsThenFails(t *testing.T) {
	t.Parallel()
	const count = 12000 // 12,000 lines of about 120 bytes: past 1 MiB
	tl := standinTool(t, "tool", standinSettings{
		Stdout:   `{"line":"document","schema":"2"}` + "\n",
		Warnings: count,
		Stderr:   "tool: error: the first fault\ntool: error: the last fault\n",
		Exit:     4,
	})
	kinds, warnings, err := read(t, tl)
	if len(kinds) != 1 {
		t.Errorf("lines %q", kinds)
	}
	size := 0
	for i, w := range warnings {
		if w.Where != fmt.Sprintf("line %d", i+1) {
			t.Fatalf("warning %d is %q", i+1, w.Where)
		}
		size += len(w.Where) + len(w.What)
	}
	if len(warnings) != count || size < 1<<20 {
		t.Errorf("%d warnings of %d bytes, want %d past 1 MiB", len(warnings), size, count)
	}
	exit, ok := errors.AsType[*cli.ExitError](err)
	if !ok || exit.Message != "the last fault" || !errors.Is(err, cli.ErrFailed) {
		t.Errorf("got %v, want the last error line's message", err)
	}
}

// TestReadSignal: a death by a signal is an *ExitError matching ErrFailed, its signal named in its
// message.
func TestReadSignal(t *testing.T) {
	t.Parallel()
	_, _, err := read(t, standinTool(t, "tool", standinSettings{Kill: true}))
	exit, ok := errors.AsType[*cli.ExitError](err)
	if !ok || exit.Signal != "SIGKILL" || !errors.Is(err, cli.ErrFailed) || errors.Is(err, cli.ErrRefused) {
		t.Fatalf("got %v, want an *ExitError for SIGKILL matching ErrFailed alone", err)
	}
	if !strings.Contains(err.Error(), "killed by SIGKILL") {
		t.Errorf("error %q doesn't name the signal", err)
	}
}

// TestReadRefusals: the tools' refusal goldens, read in place, are refusals classed by their
// reasons: onenote's table of contents ReadElsewhere, an encrypted input of either tool
// Encrypted, and a legacy document Unclassed.
func TestReadRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tool, golden string
		want         error
	}{
		{"docproc-onenote", "onenote/tests/golden/interop/SUT/Open Notebook.onetoc2.file-only.stderr", dispatch.ErrReadElsewhere},
		{"docproc-onenote", "onenote/tests/golden/interop/SUT/Encryption.one.stderr", dispatch.ErrEncrypted},
		{"docproc-ooxml", "ooxml/test/Docproc.Ooxml.Tests/Expected/generated/refused/encrypted.libreoffice.stderr", dispatch.ErrEncrypted},
		{"docproc-ooxml", "ooxml/test/Docproc.Ooxml.Tests/Expected/generated/refused/legacy.libreoffice.stderr", nil},
	} {
		t.Run(c.golden, func(t *testing.T) {
			t.Parallel()
			golden := tooltest.Tools(t, c.golden)
			_, warnings, err := read(t, standinTool(t, c.tool, standinSettings{StderrFile: golden, Exit: 3}))
			if !errors.Is(err, cli.ErrRefused) || len(warnings) != 0 {
				t.Fatalf("got %v and %q, want a refusal alone", err, warnings)
			}
			if exit, ok := errors.AsType[*cli.ExitError](err); !ok || exit.Refusal() != c.want {
				t.Errorf("%v: refusal class %v, want %v", err, exit.Refusal(), c.want)
			}
		})
	}
}

// TestReadStops: a caller that stops ranging ends the run, which would otherwise sleep a minute,
// and Read waits for it.
func TestReadStops(t *testing.T) {
	t.Parallel()
	tl := standinTool(t, "tool", standinSettings{Stdout: `{"line":"document","schema":"2"}` + "\n", Sleep: true})
	began := time.Now()
	for line, err := range cli.Read(t.Context(), cli.Run{Tool: tl, First: "document"}) {
		if err != nil || line.Kind != "document" {
			t.Fatalf("got %+v, %v", line, err)
		}
		break
	}
	if took := time.Since(began); took > 30*time.Second {
		t.Errorf("the stopped read took %v", took)
	}
}

// TestReadContext: a context's end ends the run with the context's error, not an *ExitError.
func TestReadContext(t *testing.T) {
	t.Parallel()
	tl := standinTool(t, "tool", standinSettings{Stdout: `{"line":"document","schema":"2"}` + "\n", Sleep: true})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var err error
	for line, lerr := range cli.Read(ctx, cli.Run{Tool: tl, First: "document"}) {
		if line.Kind == "document" {
			cancel()
		}
		if lerr != nil {
			err = lerr
		}
	}
	if _, ok := errors.AsType[*cli.ExitError](err); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// TestReadNilTool: a Run with no Tool panics by name.
func TestReadNilTool(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != "cli: Run.Tool is nil" {
			t.Errorf("recovered %v", r)
		}
	}()
	cli.Read(t.Context(), cli.Run{})
	t.Error("Read returned")
}
