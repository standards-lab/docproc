//go:build linux

package office_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

const (
	versionKey = "version"
	schemaKey  = "schema"
	logKey     = "log"
	exitKey    = "exit"
	stderrKey  = "stderr"
	stdoutKey  = "stdout"
	mediaKey   = "media"
	replayKey  = "replay"
	sleepKey   = "sleep"
)

const (
	sofficeVersionKey = "soffice-version"
	sofficeExitKey    = "soffice-exit"
	sofficeOutputKey  = "soffice-output"
	sofficeNothingKey = "soffice-nothing"
	sofficeSkipKey    = "soffice-skip"
)

const (
	magickVersionKey = "magick-version"
	magickExitKey    = "magick-exit"
	magickOutputKey  = "magick-output"
	magickRawKey     = "magick-raw"
)

const convertedPrefix = "converted: "

const pngSignature = "\x89PNG\r\n\x1a\n"

const standinDocument = `{"line":"document","schema":"2","format":"docx"}` + "\n"

func standinMedium(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".emf":
		return "\x01\x00\x00\x00" + strings.Repeat("\x00", 36) + " EMF" + name
	case ".wmf":
		return "\xd7\xcd\xc6\x9a" + name
	case ".tif", ".tiff":
		return "II*\x00" + name
	case ".png":
		return pngSignature + name
	case ".jpeg", ".jpg":
		return "\xff\xd8\xff" + name
	}
	return name
}

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{
		"docproc-ooxml": withSettings(standin),
		"soffice":       withSettings(sofficeStandin),
		"magick":        withSettings(magickStandin),
		"docker":        withSettings(dockerStandin),
	})
}

var settings map[string]string

func withSettings(program func(args []string) int) tooltest.Program {
	return func(r tooltest.Run) int {
		if err := r.Settings(&settings); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return program(r.Args)
	}
}

func setting(key string) string { return settings[key] }

func lookupSetting(key string) (string, bool) {
	v, ok := settings[key]
	return v, ok
}

func standin(args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		schema := setting(schemaKey)
		if schema == "" {
			schema = "2"
		}
		fmt.Printf("%s\nschema %s\n", setting(versionKey), schema)
		return 0
	}
	if len(args) != 4 || args[0] != "--media" || args[2] != "--" {
		fmt.Fprintf(os.Stderr, "stand-in: want --media <dir> -- <input>, got %q\n", args)
		return 2
	}
	media, path := args[1], args[3]
	input, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := appendLog(setting(logKey), "docproc-ooxml", args, string(input), ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if setting(sleepKey) != "" {
		time.Sleep(time.Minute)
	}
	if names := setting(mediaKey); names != "" {
		if err := os.MkdirAll(media, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		for name := range strings.SplitSeq(names, ",") {
			if err := os.WriteFile(filepath.Join(media, name), []byte(standinMedium(name)), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		}
	}
	if replay := setting(replayKey); replay != "" {
		return replayRun(replay, media)
	}
	fmt.Fprint(os.Stderr, setting(stderrKey))
	if code := setting(exitKey); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return n
	}
	out, set := lookupSetting(stdoutKey)
	if !set {
		out = standinDocument
	}
	if _, err := os.Stdout.WriteString(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

func replayRun(jsonl, media string) int {
	stem := strings.TrimSuffix(jsonl, ".jsonl")
	stdout, err := os.ReadFile(jsonl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	first, _, _ := bytes.Cut(stdout, []byte("\n"))
	var doc struct {
		Media []struct {
			File string `json:"file"`
		} `json:"media"`
	}
	if len(first) > 0 {
		if err := json.Unmarshal(first, &doc); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	for _, m := range doc.Media {
		if m.File == "" {
			continue
		}
		if err := os.MkdirAll(media, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if err := os.WriteFile(filepath.Join(media, m.File), []byte(m.File), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	stderr, err := os.ReadFile(stem + ".stderr")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	code := 0
	if exit, err := os.ReadFile(stem + ".exit"); err == nil {
		if code, err = strconv.Atoi(strings.TrimSpace(string(exit))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := os.Stdout.Write(stdout); err != nil {
		return 1
	}
	_, _ = os.Stderr.Write(stderr)
	return code
}

func sofficeStandin(args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(os.Stderr, "(process:20): dconf-CRITICAL **: unable to create directory '/.cache/dconf'")
		fmt.Printf("LibreOffice %s 680(Build:1)\n", setting(sofficeVersionKey))
		return 0
	}
	var outdir, format, profile string
	var inputs []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case strings.HasPrefix(a, "-env:UserInstallation=file://"):
			profile = strings.TrimPrefix(a, "-env:UserInstallation=file://")
		case a == "--outdir" && i+1 < len(args):
			i++
			outdir = args[i]
		case a == "--convert-to" && i+1 < len(args):
			i++
			format, _, _ = strings.Cut(args[i], ":")
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
	if err := appendLog(setting(logKey), "soffice", args, string(contents[0]), string(registry)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Fprint(os.Stderr, setting(sofficeOutputKey))
	if code := setting(sofficeExitKey); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return n
	}
	if setting(sofficeNothingKey) != "" {
		return 0
	}
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for i, path := range inputs {
		if filepath.Base(path) == setting(sofficeSkipKey) {
			continue
		}
		out := convertedPrefix + string(contents[i])
		if format == "png" {
			out = pngSignature + out
		}
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if err := os.WriteFile(filepath.Join(outdir, base+"."+format), []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	return 0
}

func magickStandin(args []string) int {
	if len(args) == 1 && args[0] == "-version" {
		fmt.Printf("Version: ImageMagick %s Q16-HDRI x86_64 https://imagemagick.org\n", setting(magickVersionKey))
		return 0
	}
	if slices.Equal(args, []string{"-list", "format", "null:"}) {
		fmt.Println("   Format  Module    Mode  Description")
		return 0
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "stand-in: want an input and an output")
		return 2
	}
	strip := func(arg string) string {
		if _, path, ok := strings.Cut(arg, ":"); ok {
			arg = path
		}
		return strings.TrimSuffix(arg, "[0]")
	}
	input, err := os.ReadFile(strip(args[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := appendLog(setting(logKey), "magick", args, string(input), ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Fprint(os.Stderr, setting(magickOutputKey))
	if code := setting(magickExitKey); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return n
	}
	out := convertedPrefix + strings.TrimPrefix(string(input), pngSignature)
	if setting(magickRawKey) == "" {
		out = pngSignature + out
	}
	if err := os.WriteFile(strip(args[len(args)-1]), []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

type standinRun struct {
	Program  string   `json:"program"`
	Dir      string   `json:"dir"`
	Args     []string `json:"args"`
	Raw      []byte   `json:"input"`
	Registry string   `json:"registry"`
	Input    string   `json:"-"`
}

func appendLog(file, program string, args []string, input, registry string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	line, err := json.Marshal(standinRun{Program: program, Dir: dir, Args: args, Raw: []byte(input), Registry: registry})
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

type standins struct {
	t        testing.TB
	links    map[string]string
	settings map[string]string
	log      string
}

func installStandin(t *testing.T, version string) *standins {
	t.Helper()
	s := &standins{t: t, links: map[string]string{}, log: filepath.Join(t.TempDir(), "runs.jsonl")}
	s.settings = map[string]string{versionKey: version, sofficeVersionKey: "26.8.1.1", magickVersionKey: "7.1.2-34", logKey: s.log}
	dir := t.TempDir()
	for _, name := range []string{"docproc-ooxml", "soffice", "magick"} {
		s.links[name] = tooltest.Link(t, dir, name, s.settings)
	}
	return s
}

func (s *standins) set(key, value string) {
	s.t.Helper()
	s.settings[key] = value
	for _, link := range s.links {
		tooltest.Set(s.t, link, s.settings)
	}
}

func (s *standins) setFor(t *testing.T, key, value string) {
	t.Helper()
	old, had := s.settings[key]
	s.set(key, value)
	t.Cleanup(func() {
		if had {
			s.set(key, old)
			return
		}
		s.unset(key)
	})
}

func (s *standins) unset(key string) {
	s.t.Helper()
	delete(s.settings, key)
	for _, link := range s.links {
		tooltest.Set(s.t, link, s.settings)
	}
}

func (s *standins) spec(spec tool.Spec) tool.Spec {
	s.t.Helper()
	link, ok := s.links[filepath.Base(spec.Program)]
	if !ok {
		s.t.Fatalf("no stand-in for %s", spec.Program)
	}
	spec.Program = link
	return spec
}

func (s *standins) runs() []standinRun {
	s.t.Helper()
	f, err := os.Open(s.log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var runs []standinRun
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		var r standinRun
		if err := json.Unmarshal(lines.Bytes(), &r); err != nil {
			s.t.Fatal(err)
		}
		r.Input = string(r.Raw)
		runs = append(runs, r)
	}
	if err := lines.Err(); err != nil {
		s.t.Fatal(err)
	}
	return runs
}

func standinConverter(t *testing.T) (*office.Converter, *standins) {
	t.Helper()
	s := installStandin(t, "0.1.0")
	o, err := tool.NewExec(t.Context(), s.spec(office.OfficeSpec()))
	if err != nil {
		t.Fatal(err)
	}
	c, err := office.NewConverter(t.Context(), office.Config{Office: o, Pictures: s.pictures()})
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func (s *standins) pictures() *picture.Converter {
	s.t.Helper()
	lo, err := tool.NewExec(s.t.Context(), s.spec(picture.LibreOfficeSpec()))
	if err != nil {
		s.t.Fatal(err)
	}
	mg, err := tool.NewExec(s.t.Context(), s.spec(picture.MagickSpec()))
	if err != nil {
		s.t.Fatal(err)
	}
	c, err := picture.NewConverter(s.t.Context(), picture.Config{Magick: mg, LibreOffice: lo})
	if err != nil {
		s.t.Fatal(err)
	}
	return c
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

func collect(t *testing.T, c *office.Converter, req office.Request) ([]office.Record, error) {
	t.Helper()
	var recs []office.Record
	for rec, err := range c.Convert(t.Context(), req) {
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func document(t *testing.T, recs []office.Record) *office.Document {
	t.Helper()
	if len(recs) == 0 {
		t.Fatal("no records")
	}
	doc, ok := recs[0].(*office.Document)
	if !ok {
		t.Fatalf("the first record is %T, not the Document", recs[0])
	}
	return doc
}

func writeDocument(t *testing.T, name, content string) string {
	t.Helper()
	if name == "" {
		name = "document"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mediaName(c, ext string) string { return strings.Repeat(c, 64) + ext }

func pictureLine(format, md string, files ...string) string {
	var media []string
	for _, f := range files {
		media = append(media, fmt.Sprintf(`{"kind":"picture","file":%q,"part":"/word/media/%s"}`, f, f))
	}
	line := fmt.Sprintf(`{"line":"document","schema":"2","format":%q,"markdown":%q`, format, md)
	if len(media) > 0 {
		line += `,"media":[` + strings.Join(media, ",") + `]`
	}
	return line + "}\n"
}
