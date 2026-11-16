//go:build linux

package document_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

// settings are a stand-in's settings, which tooltest writes beside its link.
type settings struct {
	// Version is the version the stand-in's version command prints, in its program's form:
	// "7.1.2-34" for magick, "10.08.0" for gs.
	Version string `json:"version"`
	// Log names the file each run but a version command appends its working directory and
	// arguments to, one JSON object per line: tooltest's own run log holds no working directory.
	Log string `json:"log"`
	// Fail, when set, makes a magick or gs run write it to stderr and exit 1 after logging.
	Fail string `json:"fail,omitzero"`
	// Stdout, when set, is what the stand-in writes to stdout for a run in place of its own: gs's
	// page count or its page, or magick's copy of its stdin. It is bytes, so what isn't UTF-8,
	// such as a cut PNG signature, survives the settings' JSON.
	Stdout *[]byte `json:"stdout,omitzero"`
	// Stderr is what the stand-in gs writes to stderr for a run that succeeds, and what the
	// stand-in qpdf or pdfdetach writes to stderr for any run.
	Stderr string `json:"stderr,omitzero"`
	// Exit is the code the stand-in qpdf or pdfdetach exits with, once it has written its stdout
	// and stderr.
	Exit int `json:"exit,omitzero"`
}

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"magick":    withSettings(magick),
		"gs":        withSettings(gs),
		"qpdf":      withSettings(qpdf),
		"pdfdetach": withSettings(pdfdetach),
	})
}

// withSettings returns a tooltest.Program that reads its run's settings and runs program over
// them and the run's arguments.
func withSettings(program func(s settings, args []string) int) tooltest.Program {
	return func(r tooltest.Run) int {
		var s settings
		if err := r.Settings(&s); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return program(s, r.Args)
	}
}

// magick is the stand-in magick. With -version alone it prints a version line in magick's own
// form. Otherwise it appends its arguments to the log file, then fails when Fail is set, or
// else writes Stdout to stdout when it is set, and copies stdin to stdout when it
// isn't, so a corrected image is the image it was given.
func magick(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-version" {
		fmt.Printf("Version: ImageMagick %s Q16-HDRI x86_64 https://imagemagick.org\n", s.Version)
		return 0
	}
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	if s.Stdout != nil {
		if _, err := os.Stdout.Write(*s.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

// standinText is the stand-in gs's text layer for a page: lines as txtwrite lays them out.
const standinText = "      The Harbor Survey\n      The harbor survey began in the spring.\n"

// standinPage is the stand-in gs's page: the PNG signature and a tail that names it.
var standinPage = []byte("\x89PNG\r\n\x1a\nstand-in gs page")

// gs is the stand-in gs. With --version alone it prints the version alone, as gs does.
// Otherwise it appends its working directory and arguments to the log file, then fails when
// Fail is set, or else writes Stderr to stderr, and Stdout to stdout when it is set, and its own
// output when it isn't: a page count of 6 for a run
// with -dNODISPLAY, a page's text for a run with the txtwrite device, and standinPage for any
// other.
func gs(s settings, args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(s.Version)
		return 0
	}
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	fmt.Fprint(os.Stderr, s.Stderr)
	var out string
	switch {
	case s.Stdout != nil:
		out = string(*s.Stdout)
	case slices.Contains(args, "-dNODISPLAY"):
		out = "6\n"
	case slices.Contains(args, "-sDEVICE=txtwrite"):
		out = standinText
	default:
		out = string(standinPage)
	}
	if _, err := os.Stdout.WriteString(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

// qpdf is the stand-in qpdf. With --version alone it prints qpdf's two lines, its version and
// its license's. Otherwise it acts as scripted does.
func qpdf(s settings, args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Printf("qpdf version %s\nRun qpdf --copyright to see copyright and license information.\n", s.Version)
		return 0
	}
	return scripted(s, args)
}

// pdfdetach is the stand-in pdfdetach. With -v alone it prints its version and copyright to
// stderr and exits 99, as pdfdetach does. Otherwise it acts as scripted does.
func pdfdetach(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-v" {
		fmt.Fprintf(os.Stderr, "pdfdetach version %s\nCopyright 2005-2026 The Poppler Developers - http://poppler.freedesktop.org\n", s.Version)
		return 99
	}
	return scripted(s, args)
}

// scripted is a stand-in run that does what its settings say: it appends its working directory
// and arguments to the log file, writes Stdout to stdout and Stderr to stderr, and exits with
// Exit. A -o argument names the file the stand-in writes its
// stdout's value to in place of stdout, as pdfdetach -save does.
func scripted(s settings, args []string) int {
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	out := os.Stdout
	if i := slices.Index(args, "-o"); i >= 0 && i+1 < len(args) {
		f, err := os.OpenFile(args[i+1], os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	if s.Stdout != nil {
		if _, err := out.Write(*s.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	fmt.Fprint(os.Stderr, s.Stderr)
	return s.Exit
}

// standinRun is one logged run of a stand-in: its working directory and arguments.
type standinRun struct {
	Dir  string   `json:"dir"`
	Args []string `json:"args"`
}

// appendLog appends the working directory and args to file as one JSON object and a newline.
func appendLog(file string, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	line, err := json.Marshal(standinRun{Dir: dir, Args: args})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close())
}

// standin is a test's stand-in program: its link, in a directory of its own, and its settings.
type standin struct {
	t        *testing.T
	link     string
	settings settings
}

// newStandin links the stand-in as program in a directory of its own, reporting version and
// logging to a file of the test's.
func newStandin(t *testing.T, program, version string) *standin {
	t.Helper()
	s := settings{Version: version, Log: filepath.Join(t.TempDir(), "runs.jsonl")}
	return &standin{t: t, link: tooltest.Link(t, t.TempDir(), program, s), settings: s}
}

// set changes the stand-in's settings by f, for the runs that start after it.
func (st *standin) set(f func(*settings)) {
	st.t.Helper()
	f(&st.settings)
	tooltest.Set(st.t, st.link, st.settings)
}

// tool builds a Tool from spec through the exec runner, over the stand-in in place of spec's
// program.
func (st *standin) tool(spec tool.Spec) (*tool.Tool, error) {
	spec.Program = st.link
	return tool.NewExec(st.t.Context(), spec)
}

// mustTool is tool, failing the test on an error.
func (st *standin) mustTool(spec tool.Spec) *tool.Tool {
	st.t.Helper()
	tl, err := st.tool(spec)
	if err != nil {
		st.t.Fatal(err)
	}
	return tl
}

// runs returns every run of the stand-in, in order, the version command's left out.
func (st *standin) runs() []standinRun {
	t := st.t
	t.Helper()
	f, err := os.Open(st.settings.Log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var runs []standinRun
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		var r standinRun
		if err := json.Unmarshal(lines.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	return runs
}

// args returns the arguments of every run runs returns, in order.
func (st *standin) args() [][]string {
	st.t.Helper()
	var args [][]string
	for _, r := range st.runs() {
		args = append(args, r.Args)
	}
	return args
}

// standinMagick links the stand-in magick at version, builds the Tool from spec through the exec
// runner, and returns it with the stand-in.
func standinMagick(t *testing.T, version string, spec tool.Spec) (*tool.Tool, *standin) {
	t.Helper()
	st := newStandin(t, "magick", version)
	return st.mustTool(spec), st
}

// standinCorrector builds the picture package's corrections stage over the stand-in magick at
// 7.1.2-34, and returns it with the stand-in.
func standinCorrector(t *testing.T) (*picture.Corrector, *standin) {
	t.Helper()
	tl, st := standinMagick(t, "7.1.2-34", picture.MagickSpec())
	c, err := picture.NewCorrector(picture.Config{Magick: tl})
	if err != nil {
		t.Fatal(err)
	}
	return c, st
}

// wantPanic fails t unless f panics with want.
func wantPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != want {
			t.Errorf("panicked with %v, want %q", r, want)
		}
	}()
	f()
}
