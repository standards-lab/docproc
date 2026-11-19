//go:build linux

package document_test

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/tool"
)

// settings are a stand-in's settings, which tooltest writes beside its link.
type settings struct {
	// Version is the version the stand-in's version command prints, in its program's form:
	// "10.08.0" for gs.
	Version string `json:"version"`
	// Log names the file each run but a version command appends its working directory and
	// arguments to, one JSON object per line: tooltest's own run log holds no working directory.
	Log string `json:"log"`
	// Fail, when set, makes a gs run write it to stderr and exit 1 after logging.
	Fail string `json:"fail,omitzero"`
	// Stdout, when set, is what the stand-in writes to stdout for a run in place of its own: gs's
	// page count or its page. It is bytes, so what isn't UTF-8, such as a cut PNG signature,
	// survives the settings' JSON.
	Stdout *[]byte `json:"stdout,omitzero"`
	// Pages, when set, is the stand-in gs's page count in place of 6.
	Pages int `json:"pages,omitzero"`
	// Sizes, when set, are the stand-in gs's pages' sizes, a line each, such as "612 792", which a
	// sizes run prints after the count of them; a sizes run prints "612 792" for each of Pages
	// when it isn't set.
	Sizes []string `json:"sizes,omitzero"`
	// FailPage, when set, makes the stand-in gs's render of that page write "told to fail" to
	// stderr and exit 1, in a run of many pages once the pages before it are written.
	FailPage int `json:"fail_page,omitzero"`
	// KillPage, when set, makes the stand-in gs's render send itself SIGKILL at that page, a
	// signal docproc didn't send, once the pages before it are written.
	KillPage int `json:"kill_page,omitzero"`
	// FailText, when set, makes the stand-in gs's txtwrite run fail as FailPage makes a render
	// fail, at that page.
	FailText int `json:"fail_text,omitzero"`
	// Portfolio, when set, makes the stand-in gs a PDF portfolio's, as gs reads one: a survey run,
	// whose PostScript asks for /Collection, prints true before the count; and a render or a text
	// run writes, in place of each page's, each of the two documents' page of that number, the
	// first document's pages, then the second's, as gs runs a portfolio's documents in turn over
	// the run's pages.
	Portfolio bool `json:"portfolio,omitzero"`
	// Delay holds, by page, how many milliseconds the stand-in gs's render of that page waits
	// before it writes the page.
	Delay map[int]int `json:"delay,omitzero"`
	// Stderr is what the stand-in gs writes to stderr for a run that succeeds, and what the
	// stand-in qpdf or pdfdetach writes to stderr for any run.
	Stderr string `json:"stderr,omitzero"`
	// Exit is the code the stand-in qpdf or pdfdetach exits with, once it has written its stdout
	// and stderr.
	Exit int `json:"exit,omitzero"`
}

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
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

// standinText returns the stand-in gs's text layer for page: lines as txtwrite lays them out,
// naming the page.
func standinText(page int) string {
	return fmt.Sprintf("      The Harbor Survey\n      Page %d of the harbor survey.\n", page)
}

// standinPage returns the stand-in gs's PNG of page at dpi: whole by its chunks, naming both.
func standinPage(page, dpi int) []byte {
	return tooltest.PNG(fmt.Sprintf("stand-in gs page %d at %d dpi", page, dpi))
}

// portfolioDocuments is how many documents the stand-in gs's portfolio embeds.
const portfolioDocuments = 2

// portfolioPage returns the stand-in gs's PNG of page of the portfolio's document doc, from 1, at
// dpi, as standinPage does a page's.
func portfolioPage(doc, page, dpi int) []byte {
	return tooltest.PNG(fmt.Sprintf("stand-in gs document %d page %d at %d dpi", doc, page, dpi))
}

// portfolioText returns the stand-in gs's text layer for page of the portfolio's document doc.
func portfolioText(doc, page int) string {
	return fmt.Sprintf("      Document %d\n      Page %d of document %d.\n", doc, page, doc)
}

// standinOutput is one page a stand-in gs render or text run writes: its PNG and its text, the
// page's own or, for a portfolio, a document's page, and the page of the run it is written for.
type standinOutput struct {
	page int
	png  func(dpi int) []byte
	text string
}

// outputs returns what a stand-in gs render or text run over pages writes, in order: each page's
// own, or, for a portfolio, each document's pages in turn.
func outputs(s settings, pages []int) []standinOutput {
	var out []standinOutput
	if !s.Portfolio {
		for _, page := range pages {
			out = append(out, standinOutput{page, func(dpi int) []byte { return standinPage(page, dpi) }, standinText(page)})
		}
		return out
	}
	for doc := 1; doc <= portfolioDocuments; doc++ {
		for _, page := range pages {
			out = append(out, standinOutput{page, func(dpi int) []byte { return portfolioPage(doc, page, dpi) }, portfolioText(doc, page)})
		}
	}
	return out
}

// argLimit is the longest argument gs takes, in bytes: a longer one is "Command too long", and gs
// exits 1, as gs 10.08.0 does past 2047.
const argLimit = 2047

// gs is the stand-in gs. With --version alone it prints the version alone, as gs does.
// Otherwise it appends its working directory and arguments to the log file, then refuses an
// argument past argLimit, as gs does, and fails when Fail is set, or else writes Stderr to
// stderr, and Stdout to stdout when it is set, and its own output when it isn't: for a run with
// -dNODISPLAY, whether the PDF is a portfolio when its PostScript asks for /Collection, then its
// page count, then its pages' sizes when its PostScript asks for them; for a run with the
// txtwrite device, each output's text, to stdout for an OutputFile of -, and otherwise to the
// OutputFile, its %d the output's place in the run from 1, failing at FailText; and for a render,
// each output's PNG in turn, after its page's Delay, failing at FailPage and killed at KillPage.
// A run's outputs are its pages', as outputs gives them, its pages its -sPageList, or its
// -dFirstPage alone. A page that fails writes "told to fail" to stderr and exits 1, the outputs
// before it written.
func gs(s settings, args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(s.Version)
		return 0
	}
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for _, a := range args {
		if len(a) > argLimit {
			fmt.Fprintf(os.Stderr, "Command too long: %s\n", a[:64])
			return 1
		}
	}
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	fmt.Fprint(os.Stderr, s.Stderr)
	pages := cmp.Or(s.Pages, 6)
	var out string
	switch {
	case s.Stdout != nil:
		out = string(*s.Stdout)
	case slices.Contains(args, "-dNODISPLAY"):
		script := args[len(args)-1]
		if strings.Contains(script, "/Collection") {
			out = fmt.Sprintf("%t\n", s.Portfolio)
		}
		if !strings.Contains(script, "/PageSize") {
			out += fmt.Sprintf("%d\n", pages)
			break
		}
		sizes := s.Sizes
		if sizes == nil {
			sizes = slices.Repeat([]string{"612 792"}, pages)
		}
		out += fmt.Sprintf("%d\n%s\n", len(sizes), strings.Join(sizes, "\n"))
	case slices.Contains(args, "-sDEVICE=txtwrite"):
		return standinTexts(s, args)
	default:
		dpi, _ := strconv.Atoi(strings.TrimPrefix(args[slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, "-r") })], "-r"))
		for _, o := range outputs(s, pageArgs(args)) {
			time.Sleep(time.Duration(s.Delay[o.page]) * time.Millisecond)
			if o.page == s.FailPage {
				fmt.Fprint(os.Stderr, "told to fail")
				return 1
			}
			if o.page == s.KillPage {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			}
			if _, err := os.Stdout.Write(o.png(dpi)); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		}
		return 0
	}
	if _, err := os.Stdout.WriteString(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

// standinTexts is the stand-in gs's txtwrite run, as gs says.
func standinTexts(s settings, args []string) int {
	var file string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-sOutputFile="); ok {
			file = v
		}
	}
	for i, o := range outputs(s, pageArgs(args)) {
		if o.page == s.FailText {
			fmt.Fprint(os.Stderr, "told to fail")
			return 1
		}
		var err error
		if file == "-" {
			_, err = os.Stdout.WriteString(o.text)
		} else {
			err = os.WriteFile(fmt.Sprintf(file, i+1), []byte(o.text), 0o600)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	return 0
}

// pageArgs returns the pages a gs run names: its -sPageList's, a page or a range of them, such
// as 3-7, between each comma, or its -dFirstPage alone, and none for a run that names neither.
func pageArgs(args []string) []int {
	var pages []int
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-dFirstPage="); ok {
			n, _ := strconv.Atoi(v)
			return []int{n}
		}
		if v, ok := strings.CutPrefix(a, "-sPageList="); ok {
			for f := range strings.SplitSeq(v, ",") {
				first, last, _ := strings.Cut(f, "-")
				from, _ := strconv.Atoi(first)
				to := from
				if last != "" {
					to, _ = strconv.Atoi(last)
				}
				for n := from; n <= to; n++ {
					pages = append(pages, n)
				}
			}
		}
	}
	return pages
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
