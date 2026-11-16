//go:build linux

package picture_test

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
	"strings"
	"testing"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

// settings are the stand-ins' settings, which tooltest writes beside each link: both links of a
// test's stand-ins hold the same settings.
type settings struct {
	// MagickVersion is the version the stand-in magick's -version prints; SofficeVersion the
	// version the stand-in soffice's --version prints, after the line of desktop-library noise
	// the real one writes to stderr in a container.
	MagickVersion  string `json:"magick_version"`
	SofficeVersion string `json:"soffice_version"`
	// Log names the file each run appends its program, working directory, arguments, and input
	// to, one JSON object per line: what tooltest's own run log doesn't hold.
	Log string `json:"log"`
	// Fail, when set, makes a magick run write it to stderr and exit 1 after logging.
	Fail string `json:"fail,omitzero"`
	// Stdout, when set, is what a magick run that writes to stdout writes in place of its copy of
	// stdin.
	Stdout *string `json:"stdout,omitzero"`
	// Raw makes a magick run that writes a file write it without PNG's signature.
	Raw bool `json:"raw,omitzero"`
	// SofficeFail, when set, makes a soffice run write it to stderr and exit 1 after logging.
	SofficeFail string `json:"soffice_fail,omitzero"`
	// SofficeNothing makes a soffice run exit zero having written nothing, as soffice does when
	// it can't export what it loaded.
	SofficeNothing bool `json:"soffice_nothing,omitzero"`
	// Formats, when set, is the format listing the stand-in magick's -list format prints in place
	// of fullListing; ListFail, when set, makes it write ListFail to stderr and exit 1.
	Formats  *string `json:"formats,omitzero"`
	ListFail string  `json:"list_fail,omitzero"`
	// Pages is how many pages a pages run of the stand-in magick writes: 1 when it is zero.
	Pages int `json:"pages,omitzero"`
	// Gap makes a pages run leave out its second page, as a run that failed partway might.
	Gap bool `json:"gap,omitzero"`
}

// fullListing is the stand-in magick's format listing by default: the lines ImageMagick 7.1.2-34
// prints for the formats the Converter checks, with a header, a TIFF line, and a line of
// continuation around them, each readable.
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

// convertedPrefix begins what either stand-in writes for a picture after PNG's signature: the
// prefix, then its input.
const convertedPrefix = "converted: "

// pngSignature begins every PNG, and the stand-ins' PNG output.
const pngSignature = "\x89PNG\r\n\x1a\n"

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"magick":  withSettings(magickStandin),
		"soffice": withSettings(sofficeStandin),
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

// filePath returns the path an argument of magick's names, its format prefix, "tiff:", and
// frame suffix, "[0]", taken off, and whether it names one: an absolute path.
func filePath(arg string) (string, bool) {
	if _, rest, ok := strings.Cut(arg, ":"); ok {
		arg = rest
	}
	arg = strings.TrimSuffix(arg, "[0]")
	return arg, filepath.IsAbs(arg)
}

// magickStandin is the stand-in magick. With -version alone it prints the version line the real one
// prints first, and with -list format its format listing, the settings' Formats or fullListing,
// or fails when ListFail is set; neither is logged in its own log. A run whose first argument is png:-, the
// corrections stage's, reads stdin: it logs its arguments, then fails when Fail is set, or else
// writes Stdout when it is set, and copies stdin when it isn't, so a corrected image is
// the image it was given. Any other run, a conversion's, reads its input, its first argument that
// names an absolute path, "tiff:path[0]", "rsvg:path", or "png:path", and writes its last,
// "png:path": it logs its arguments and its input's content, then fails when Fail is set, or
// else writes PNG's signature, convertedPrefix, and its input, less any PNG signature it begins
// with, without the signature when Raw is set; a pages run, one with +adjoin, writes that to
// each page, as magickPages says. A metafile's PNG, which soffice and then magick wrote, holds
// convertedPrefix twice.
func magickStandin(s settings, args []string) int {
	if len(args) == 1 && args[0] == "-version" {
		fmt.Printf("Version: ImageMagick %s Q16-HDRI x86_64 https://imagemagick.org\n", s.MagickVersion)
		return 0
	}
	if len(args) == 2 && args[0] == "-list" && args[1] == "format" {
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
	if args[0] == "png:-" {
		return magickStdout(s, args)
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

// magickPages writes a pages run's pages, the settings' Pages of them, 1 when it is zero: each to
// pattern, a path relative to the run's working directory with %d for the page's index from 0,
// holding out and "page N" for its number from 1, the second left out when Gap is set.
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

// magickStdout is the stand-in magick's run that reads stdin, as magickStandin says.
func magickStdout(s settings, args []string) int {
	if err := appendLog(s.Log, "magick", args, "", ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if s.Fail != "" {
		fmt.Fprint(os.Stderr, s.Fail)
		return 1
	}
	if s.Stdout != nil {
		if _, err := os.Stdout.WriteString(*s.Stdout); err != nil {
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

// sofficeStandin is the stand-in soffice. With --version alone it writes a line of noise to
// stderr and the version line to stdout, as the real one does in a container. Otherwise its
// inputs are its arguments that are neither options nor their values. It logs its arguments, its
// first input's content, and the registrymodifications.xcu its -env:UserInstallation profile
// starts with; exits 1, having written SofficeFail to stderr, when it is set; exits zero having
// written nothing when SofficeNothing is set; and else writes each input's page
// into the directory after --outdir, named for its input's stem and .png: PNG's signature,
// convertedPrefix, and the input.
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

// standinRun is one logged run of a stand-in: its program, its working directory, its
// arguments, its input file's content, "" for a run that reads stdin, and, for soffice, the
// registrymodifications.xcu its profile started with.
type standinRun struct {
	Program  string   `json:"program"`
	Dir      string   `json:"dir"`
	Args     []string `json:"args"`
	Input    string   `json:"input"`
	Registry string   `json:"registry"`
}

// appendLog appends program, the working directory, args, input, and registry to log as one JSON
// object and a newline.
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

// standins are a test's stand-in magick and soffice: their links, in a directory of their own,
// and the settings both hold.
type standins struct {
	t        *testing.T
	magick   string
	soffice  string
	settings settings
}

// newStandins links the stand-ins as magick and soffice in a directory of their own, the magick
// reporting magickVersion and the soffice 26.8.1.1, logging to a file of the test's, with each of
// opts applied to their settings.
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

// set changes the stand-ins' settings by f, for the runs that start after it.
func (st *standins) set(f func(*settings)) {
	st.t.Helper()
	f(&st.settings)
	tooltest.Set(st.t, st.magick, st.settings)
	tooltest.Set(st.t, st.soffice, st.settings)
}

// tool builds a Tool from spec through the exec runner, over the stand-in its Program names.
func (st *standins) tool(spec tool.Spec) (*tool.Tool, error) {
	switch spec.Program {
	case "magick":
		spec.Program = st.magick
	case "soffice":
		spec.Program = st.soffice
	}
	return tool.NewExec(st.t.Context(), spec)
}

// mustTool is tool, failing the test on an error.
func (st *standins) mustTool(spec tool.Spec) *tool.Tool {
	st.t.Helper()
	tl, err := st.tool(spec)
	if err != nil {
		st.t.Fatal(err)
	}
	return tl
}

// runs returns every run of either stand-in, in order, the version commands and format listings
// left out.
func (st *standins) runs() []standinRun {
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

// args returns the arguments of every run runs returns, in order.
func (st *standins) args() [][]string {
	st.t.Helper()
	var args [][]string
	for _, r := range st.runs() {
		args = append(args, r.Args)
	}
	return args
}

// standinMagick links the stand-ins with magick at version, opts applied to their settings,
// builds the magick Tool from spec through the exec runner, and returns it with the stand-ins.
func standinMagick(t *testing.T, version string, spec tool.Spec, opts ...func(*settings)) (*tool.Tool, *standins) {
	t.Helper()
	st := newStandins(t, version, opts...)
	return st.mustTool(spec), st
}

// standinCorrector builds the corrections stage over the stand-in magick at 7.1.2-34, and
// returns it with the stand-ins.
func standinCorrector(t *testing.T) (*picture.Corrector, *standins) {
	t.Helper()
	tl, st := standinMagick(t, "7.1.2-34", picture.MagickSpec())
	c, err := picture.NewCorrector(picture.Config{Magick: tl})
	if err != nil {
		t.Fatal(err)
	}
	return c, st
}

// standinConverter builds a Converter over the stand-in magick at 7.1.2-34 and the stand-in
// soffice at 26.8.1.1, each when asked, built from its Spec through the exec runner, opts applied
// to their settings first, and returns it with the stand-ins.
func standinConverter(t *testing.T, libreoffice, magick bool, opts ...func(*settings)) (*picture.Converter, *standins) {
	t.Helper()
	st := newStandins(t, "7.1.2-34", opts...)
	var cfg picture.Config
	if libreoffice {
		cfg.LibreOffice = st.mustTool(picture.LibreOfficeSpec())
	}
	if magick {
		cfg.Magick = st.mustTool(picture.MagickSpec())
	}
	c, err := picture.NewConverter(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, st
}

// entries returns the names in dir, in name order.
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
