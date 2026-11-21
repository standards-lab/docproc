//go:build linux

package picture_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

type settings struct {
	MagickVersion  string  `json:"magick_version"`
	SofficeVersion string  `json:"soffice_version"`
	Log            string  `json:"log"`
	Fail           string  `json:"fail,omitzero"`
	Raw            bool    `json:"raw,omitzero"`
	SofficeFail    string  `json:"soffice_fail,omitzero"`
	SofficeNothing bool    `json:"soffice_nothing,omitzero"`
	Formats        *string `json:"formats,omitzero"`
	ListFail       string  `json:"list_fail,omitzero"`
	Pages          int     `json:"pages,omitzero"`
	Gap            bool    `json:"gap,omitzero"`
}

const fullListing = `   Format  Module    Mode  Description
-------------------------------------------------------------------------------
     AVCI  HEIC      rw+   AVC Image File Format (1.23.6)
     AVIF  HEIC      rw+   AV1 Image File Format (1.23.6)
     HEIC  HEIC      rw+   High Efficiency Image Format (1.23.6)
     HEIF  HEIC      rw+   High Efficiency Image Format (1.23.6)
     TIFF* TIFF      rw+   Tagged Image File Format (LIBTIFF, Version 4.7.2)
             Compression options: None, Fax/Group3, Group4, JBIG, JPEG, LERC, LZW, LZMA, RLE, ZIP, ZSTD, WEBP
     WEBP* WEBP      rw+   WebP Image Format (libwebp 1.6.0 [0210])
`

const convertedPrefix = "converted: "

const pngSignature = "\x89PNG\r\n\x1a\n"

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"magick":  withSettings(magickStandin),
		"soffice": withSettings(sofficeStandin),
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

func filePath(arg string) (string, bool) {
	if _, rest, ok := strings.Cut(arg, ":"); ok {
		arg = rest
	}
	arg = strings.TrimSuffix(arg, "[0]")
	return arg, filepath.IsAbs(arg)
}

func magickStandin(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-version" {
		fmt.Printf("Version: ImageMagick %s Q16-HDRI x86_64 https://imagemagick.org\n", s.MagickVersion)
		return 0
	}
	if listing(args) {
		if err := appendLog(s.Log, "magick", args, "", ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if s.ListFail != "" {
			fmt.Fprint(os.Stderr, s.ListFail)
			return 1
		}
		listing := fullListing
		if s.Formats != nil {
			listing = *s.Formats
		}
		fmt.Print(listing)
		return 0
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "stand-in: want an input and an output")
		return 2
	}
	var input []byte
	for _, a := range args[:len(args)-1] {
		if path, ok := filePath(a); ok {
			var err error
			if input, err = os.ReadFile(path); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			break
		}
	}
	if err := appendLog(s.Log, "magick", args, string(input), ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	out := convertedPrefix + strings.TrimPrefix(string(input), pngSignature)
	if !s.Raw {
		out = pngSignature + out
	}
	path, _ := filePath(args[len(args)-1])
	if slices.Contains(args, "+adjoin") {
		return magickPages(s, path, out)
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

func magickPages(s settings, pattern, out string) int {
	n := max(s.Pages, 1)
	for i := range n {
		if i == 1 && s.Gap {
			continue
		}
		page := fmt.Sprintf("%s page %d", out, i+1)
		if err := os.WriteFile(fmt.Sprintf(pattern, i), []byte(page), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	return 0
}

func listing(args []string) bool {
	n := len(args)
	return n >= 3 && args[n-3] == "-list" && args[n-2] == "format" && args[n-1] == "null:"
}

func sofficeStandin(s settings, args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(os.Stderr, "(process:20): dconf-CRITICAL **: unable to create directory '/.cache/dconf'")
		fmt.Printf("LibreOffice %s 680(Build:1)\n", s.SofficeVersion)
		return 0
	}
	var outdir, profile string
	var inputs []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case strings.HasPrefix(a, "-env:UserInstallation=file://"):
			profile = strings.TrimPrefix(a, "-env:UserInstallation=file://")
		case (a == "--outdir" || a == "--convert-to") && i+1 < len(args):
			i++
			if a == "--outdir" {
				outdir = args[i]
			}
		case !strings.HasPrefix(a, "-"):
			inputs = append(inputs, a)
		}
	}
	if len(inputs) == 0 {
		fmt.Fprintln(os.Stderr, "stand-in: no input")
		return 2
	}
	contents := make([][]byte, len(inputs))
	for i, path := range inputs {
		var err error
		if contents[i], err = os.ReadFile(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	registry, err := os.ReadFile(filepath.Join(profile, "user", "registrymodifications.xcu"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := appendLog(s.Log, "soffice", args, string(contents[0]), string(registry)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if s.SofficeFail != "" {
		fmt.Fprint(os.Stderr, s.SofficeFail)
		return 1
	}
	if s.SofficeNothing {
		return 0
	}
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for i, path := range inputs {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		out := pngSignature + convertedPrefix + string(contents[i])
		if err := os.WriteFile(filepath.Join(outdir, base+".png"), []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	return 0
}

type standinRun struct {
	Program  string   `json:"program"`
	Dir      string   `json:"dir"`
	Args     []string `json:"args"`
	Input    string   `json:"input"`
	Registry string   `json:"registry"`
}

func appendLog(log, program string, args []string, input, registry string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	line, err := json.Marshal(standinRun{Program: program, Dir: dir, Args: args, Input: input, Registry: registry})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close())
}

type standins struct {
	t        *testing.T
	magick   string
	soffice  string
	settings settings
}

func newStandins(t *testing.T, magickVersion string, opts ...func(*settings)) *standins {
	t.Helper()
	s := settings{MagickVersion: magickVersion, SofficeVersion: "26.8.1.1", Log: filepath.Join(t.TempDir(), "runs.jsonl")}
	for _, opt := range opts {
		opt(&s)
	}
	dir := t.TempDir()
	return &standins{
		t:        t,
		magick:   tooltest.Link(t, dir, "magick", s),
		soffice:  tooltest.Link(t, dir, "soffice", s),
		settings: s,
	}
}

func (st *standins) set(f func(*settings)) {
	st.t.Helper()
	f(&st.settings)
	tooltest.Set(st.t, st.magick, st.settings)
	tooltest.Set(st.t, st.soffice, st.settings)
}

func (st *standins) tool(spec tool.Spec) (*tool.Tool, error) {
	switch spec.Program {
	case "magick":
		spec.Program = st.magick
	case "soffice":
		spec.Program = st.soffice
	}
	return tool.NewExec(st.t.Context(), spec)
}

func (st *standins) mustTool(spec tool.Spec) *tool.Tool {
	st.t.Helper()
	tl, err := st.tool(spec)
	if err != nil {
		st.t.Fatal(err)
	}
	return tl
}

func (st *standins) runs() []standinRun {
	st.t.Helper()
	return slices.DeleteFunc(st.all(), func(r standinRun) bool { return r.Program == "magick" && listing(r.Args) })
}

func (st *standins) listings() []standinRun {
	st.t.Helper()
	return slices.DeleteFunc(st.all(), func(r standinRun) bool { return r.Program != "magick" || !listing(r.Args) })
}

func (st *standins) all() []standinRun {
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
	lines.Buffer(nil, 1<<20)
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

func standinConverter(t *testing.T, limits picture.Limits, opts ...func(*settings)) (*picture.Converter, *standins) {
	t.Helper()
	st := newStandins(t, "7.1.2-34", opts...)
	c, err := picture.NewConverter(t.Context(), picture.Config{
		Magick:      st.mustTool(picture.MagickSpec()),
		LibreOffice: st.mustTool(picture.LibreOfficeSpec()),
		Limits:      limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, st
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}
