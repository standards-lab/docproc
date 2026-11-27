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

type settings struct {
	Version   string            `json:"version"`
	Log       string            `json:"log"`
	Fail      string            `json:"fail,omitzero"`
	Stdout    *[]byte           `json:"stdout,omitzero"`
	Pages     int               `json:"pages,omitzero"`
	Sizes     []string          `json:"sizes,omitzero"`
	FailPage  int               `json:"fail_page,omitzero"`
	KillPage  int               `json:"kill_page,omitzero"`
	FailText  int               `json:"fail_text,omitzero"`
	Portfolio bool              `json:"portfolio,omitzero"`
	Delay     map[int]int       `json:"delay,omitzero"`
	Leave     bool              `json:"leave,omitzero"`
	Message   string            `json:"message,omitzero"`
	Stderr    string            `json:"stderr,omitzero"`
	Exit      int               `json:"exit,omitzero"`
	Info      map[string]string `json:"info,omitzero"`
}

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"gs":        withSettings(gs),
		"qpdf":      withSettings(qpdf),
		"pdfdetach": withSettings(pdfdetach),
		"pdftotext": withSettings(pdftotext),
		"pdfinfo":   withSettings(pdfinfo),
		"docker":    withSettings(docker),
	})
}

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

func standinText(page int) string {
	return fmt.Sprintf("The Harbor Survey\n\nPage %d of the harbor survey.\n\n", page)
}

func standinPage(page, dpi int) []byte {
	return tooltest.PNG(fmt.Sprintf("stand-in gs page %d at %d dpi", page, dpi))
}

const portfolioDocuments = 2

func portfolioPage(doc, page, dpi int) []byte {
	return tooltest.PNG(fmt.Sprintf("stand-in gs document %d page %d at %d dpi", doc, page, dpi))
}

type standinOutput struct {
	page int
	png  func(dpi int) []byte
}

func outputs(s settings, args []string) []standinOutput {
	pages := tooltest.GSPages(args)
	var out []standinOutput
	if !s.Portfolio || strings.Contains(args[len(args)-1], " dopdfpages ") {
		for _, page := range pages {
			out = append(out, standinOutput{page, func(dpi int) []byte { return standinPage(page, dpi) }})
		}
		return out
	}
	for doc := 1; doc <= portfolioDocuments; doc++ {
		for _, page := range pages {
			out = append(out, standinOutput{page, func(dpi int) []byte { return portfolioPage(doc, page, dpi) }})
		}
	}
	return out
}

const argLimit = 2047

func gs(s settings, args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(s.Version)
		return 0
	}
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if s.Leave {
		f, err := os.CreateTemp(os.Getenv("TMPDIR"), "gs_")
		if err == nil {
			err = f.Close()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
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
		sizes := s.Sizes
		if sizes == nil {
			sizes = slices.Repeat([]string{"612 792"}, pages)
		}
		if strings.Contains(script, "/PageSize") {
			pages = len(sizes)
		}
		out = fmt.Sprintf("%d\n", pages)
		if pages > 0 && strings.Contains(script, "/Collection") {
			out += fmt.Sprintf("%t\n", s.Portfolio)
		}
		if strings.Contains(script, "/PageSize") {
			for _, size := range sizes {
				out += size + "\n"
			}
		}
	default:
		dpi, _ := strconv.Atoi(strings.TrimPrefix(args[slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, "-r") })], "-r"))
		messages := os.Stdout
		if slices.Contains(args, "-sstdout=%stderr") {
			messages = os.Stderr
		}
		if _, err := messages.WriteString(s.Message); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		for _, o := range outputs(s, args) {
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

func qpdf(s settings, args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Printf("qpdf version %s\nRun qpdf --copyright to see copyright and license information.\n", s.Version)
		return 0
	}
	return scripted(s, args)
}

func pdfdetach(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-v" {
		fmt.Fprintf(os.Stderr, "pdfdetach version %s\nCopyright 2005-2026 The Poppler Developers - http://poppler.freedesktop.org\n", s.Version)
		return 99
	}
	return scripted(s, args)
}

func pdftotext(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-v" {
		fmt.Fprintf(os.Stderr, "pdftotext version %s\nCopyright 2005-2026 The Poppler Developers - http://poppler.freedesktop.org\n", s.Version)
		return 0
	}
	page, err := strconv.Atoi(args[slices.Index(args, "-f")+1])
	if err != nil || s.Stdout != nil || s.Exit != 0 {
		return scripted(s, args)
	}
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if page == s.FailText {
		fmt.Fprint(os.Stderr, "told to fail")
		return 1
	}
	if _, err := os.Stdout.WriteString(standinText(page) + "\f"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

func pdfinfo(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-v" {
		fmt.Fprintf(os.Stderr, "pdfinfo version %s\nCopyright 2005-2026 The Poppler Developers - http://poppler.freedesktop.org\n", s.Version)
		return 0
	}
	if s.Info == nil {
		return scripted(s, args)
	}
	if err := appendLog(s.Log, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	mode := "summary"
	switch {
	case slices.Contains(args, "-custom"):
		mode = "custom"
	case slices.Contains(args, "-meta"):
		mode = "meta"
	}
	if _, err := os.Stdout.WriteString(s.Info[mode]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

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

type standinRun struct {
	Dir  string   `json:"dir"`
	Args []string `json:"args"`
}

func docker(s settings, args []string) int {
	if len(args) == 0 || args[0] != "run" {
		if len(args) > 0 && args[0] == "version" {
			fmt.Println("27.0.0")
		}
		return 0
	}
	ref := slices.IndexFunc(args[1:], func(a string) bool { return !strings.HasPrefix(a, "-") }) + 1
	for _, a := range args[1:ref] {
		if dir, ok := strings.CutPrefix(a, "--workdir="); ok {
			if err := os.Chdir(dir); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		}
	}
	return gs(s, args[ref+1:])
}

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

type standin struct {
	t        *testing.T
	link     string
	settings settings
}

func newStandin(t *testing.T, program, version string) *standin {
	t.Helper()
	s := settings{Version: version, Log: filepath.Join(t.TempDir(), "runs.jsonl")}
	return &standin{t: t, link: tooltest.Link(t, t.TempDir(), program, s), settings: s}
}

func (st *standin) set(f func(*settings)) {
	st.t.Helper()
	f(&st.settings)
	tooltest.Set(st.t, st.link, st.settings)
}

func (st *standin) tool(spec tool.Spec) (*tool.Tool, error) {
	spec.Program = st.link
	return tool.NewExec(st.t.Context(), spec)
}

func (st *standin) mustTool(spec tool.Spec) *tool.Tool {
	st.t.Helper()
	tl, err := st.tool(spec)
	if err != nil {
		st.t.Fatal(err)
	}
	return tl
}

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

func wantPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != want {
			t.Errorf("panicked with %v, want %q", r, want)
		}
	}()
	f()
}
