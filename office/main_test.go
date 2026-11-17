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

// The stand-ins are the test binary, run through a link named docproc-ooxml, soffice, or magick,
// acting on its arguments as docproc-ooxml, LibreOffice, or ImageMagick. A test's three links
// share one set of settings, a string by key, which each link names: a key that isn't set is
// unset, as an unset variable was.

// The stand-in docproc-ooxml's settings.
const (
	// versionKey is the version the stand-in's --version prints, before its schema line.
	versionKey = "version"
	// schemaKey, when set, is the schema the stand-in's --version prints in place of 2.
	schemaKey = "schema"
	// logKey names the file each run of any of the three appends its program, working directory,
	// arguments, and input to, one JSON object per line, so the log holds their runs in order.
	logKey = "log"
	// exitKey, when set, is the exit code of a run, which then writes nothing to stdout.
	exitKey = "exit"
	// stderrKey, when set, is what a run writes to stderr.
	stderrKey = "stderr"
	// stdoutKey, when set, is what a run that exits zero writes to stdout in place of
	// standinDocument.
	stdoutKey = "stdout"
	// mediaKey, when set, names the files, comma-separated, a run writes to its --media
	// directory, each holding its kind's signature, as standinMedium gives it, and its own name.
	mediaKey = "media"
	// replayKey, when set, is an expected output of docproc-ooxml's, a .jsonl file: a run writes
	// it to stdout and its .stderr beside it to stderr, and exits with the code its .exit beside
	// it holds, 0 without one, as docproc-ooxml did over the fixture.
	replayKey = "replay"
	// sleepKey, when set, makes a run sleep a minute after logging, for a test to cancel it.
	sleepKey = "sleep"
)

// The stand-in soffice's settings; it logs to logKey's file too.
const (
	// sofficeVersionKey is the version the stand-in soffice's --version prints, after the line
	// of desktop-library noise the real one writes to stderr in a container.
	sofficeVersionKey = "soffice-version"
	// sofficeExitKey, when set, is the exit code of a conversion, which then writes nothing.
	sofficeExitKey = "soffice-exit"
	// sofficeOutputKey, when set, is what a conversion writes to stderr.
	sofficeOutputKey = "soffice-output"
	// sofficeNothingKey, when set, makes a conversion exit zero having written nothing, as
	// soffice does when it can't export what it loaded.
	sofficeNothingKey = "soffice-nothing"
	// sofficeSkipKey, when set, names an input, by its base name, that a conversion writes
	// nothing for, as soffice does for one picture of a run it can't export.
	sofficeSkipKey = "soffice-skip"
)

// The stand-in magick's settings; it logs to logKey's file too.
const (
	// magickVersionKey is the version the stand-in magick's -version prints.
	magickVersionKey = "magick-version"
	// magickExitKey, when set, is the exit code of a conversion, which then writes nothing.
	magickExitKey = "magick-exit"
	// magickOutputKey, when set, is what a conversion writes to stderr.
	magickOutputKey = "magick-output"
	// magickRawKey, when set, makes a conversion write its output without PNG's signature.
	magickRawKey = "magick-raw"
)

// convertedPrefix begins the document the stand-in soffice writes, and the PNG either stand-in
// writes after PNG's signature: the prefix, then its input.
const convertedPrefix = "converted: "

// pngSignature begins every PNG, and the stand-ins' PNG output.
const pngSignature = "\x89PNG\r\n\x1a\n"

// standinDocument is the stand-in's stdout when stdoutKey isn't set: an empty docx's document
// line.
const standinDocument = `{"line":"document","schema":"2","format":"docx"}` + "\n"

// standinMedium returns the content the stand-in docproc-ooxml writes for the medium named name:
// the signature route.Identify names its extension's kind by, for an EMF, a WMF, a TIFF, a PNG, or
// a JPEG, and then name; name alone for any other.
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

// settings are the running stand-in's settings, which withSettings reads from its link.
var settings map[string]string

// withSettings returns a tooltest.Program that reads the link's settings into settings, then runs
// program on the run's arguments.
func withSettings(program func(args []string) int) tooltest.Program {
	return func(r tooltest.Run) int {
		if err := r.Settings(&settings); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return program(r.Args)
	}
}

// setting returns the stand-in's setting key, "" when it isn't set.
func setting(key string) string { return settings[key] }

// lookupSetting returns the stand-in's setting key and whether it is set.
func lookupSetting(key string) (string, bool) {
	v, ok := settings[key]
	return v, ok
}

// standin is the stand-in docproc-ooxml. With --version alone it prints the version, then
// `schema 2`, or schemaKey's schema, as docproc-ooxml does. Otherwise it takes its arguments as `--media <dir> --
// <input>`, appends its working directory, arguments, and its input file's content to the log
// file, sleeps when sleepKey is set, and writes the media mediaKey names into the media
// directory. Then it replays replayKey's expected output when it is set; or else writes
// stderrKey's value to stderr and exits with exitKey's code when it is set, or else writes
// stdoutKey's value, or standinDocument, to stdout and exits zero.
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

// replayRun writes the expected output jsonl to stdout and the .stderr beside it to stderr, and
// returns the exit code the .exit beside it holds, 0 when there is none. Each file its document
// line's media name is written into media holding its name alone, no signature, so the picture
// package converts none of them and the replay is the decoding alone.
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

// sofficeStandin is the stand-in soffice. With --version alone it writes a line of noise to
// stderr and the version line to stdout, as the real one does in a container. Otherwise its
// inputs are its arguments that are neither options nor their values. It appends its working
// directory, arguments, its first input's content, and the registrymodifications.xcu its
// -env:UserInstallation profile starts with, if any, to the log file; exits with
// sofficeExitKey's code, having written sofficeOutputKey's value to stderr, when it is set; exits
// zero having written nothing when sofficeNothingKey is set; and else writes each input's
// conversion into the directory after --outdir, named for its input's stem with the extension
// --convert-to names before its colon: convertedPrefix and the input, after PNG's signature for
// png. It writes nothing for the input sofficeSkipKey names.
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

// magickStandin is the stand-in magick. With -version alone it prints the version line the real one
// prints first, and with -list format null: a listing of no formats, which the picture package's
// Converter reads when it is built; neither is logged. Otherwise it takes its first argument as its
// input, with any format prefix and frame suffix, "tiff:path[0]", and its last as its output,
// "png:path". It appends its working directory, arguments, and its input's content to the log file;
// exits with magickExitKey's code, having written magickOutputKey's value to stderr, when it is
// set; and else writes PNG's signature, convertedPrefix, and its input, less any PNG signature it
// begins with, to the output, without the signature when magickRawKey is set. A metafile's PNG,
// which soffice and then magick wrote, holds convertedPrefix twice.
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

// standinRun is one logged run of a stand-in: its program, its working directory, its
// arguments, its input file's content, and, for soffice, the registrymodifications.xcu its
// profile started with, "" for none. The log holds the content as bytes, Raw, since a legacy
// document's isn't UTF-8; reading it sets Input from Raw.
type standinRun struct {
	Program  string   `json:"program"`
	Dir      string   `json:"dir"`
	Args     []string `json:"args"`
	Raw      []byte   `json:"input"`
	Registry string   `json:"registry"`
	Input    string   `json:"-"`
}

// appendLog appends program, the working directory, args, input, and registry to file as one
// JSON object and a newline.
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

// standins are a test's three stand-ins: their links and the settings they share.
type standins struct {
	t        testing.TB
	links    map[string]string
	settings map[string]string
	// log is the file the stand-ins log their runs to.
	log string
}

// installStandin links the test binary as docproc-ooxml, soffice, and magick in a directory of its
// own, the stand-in docproc-ooxml reporting version, the stand-in soffice 26.8.1.1, and
// the stand-in magick 7.1.2-34.
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

// set sets the stand-ins' setting key to value.
func (s *standins) set(key, value string) {
	s.t.Helper()
	s.settings[key] = value
	for _, link := range s.links {
		tooltest.Set(s.t, link, s.settings)
	}
}

// setFor sets the stand-ins' setting key to value until t ends, as t.Setenv set a variable,
// for a subtest of a test whose stand-ins its subtests share.
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

// unset unsets the stand-ins' setting key.
func (s *standins) unset(key string) {
	s.t.Helper()
	delete(s.settings, key)
	for _, link := range s.links {
		tooltest.Set(s.t, link, s.settings)
	}
}

// spec returns spec with its program the stand-in's link of the same name.
func (s *standins) spec(spec tool.Spec) tool.Spec {
	s.t.Helper()
	link, ok := s.links[filepath.Base(spec.Program)]
	if !ok {
		s.t.Fatalf("no stand-in for %s", spec.Program)
	}
	spec.Program = link
	return spec
}

// runs returns every run of any of the stand-ins, in order, the version commands left out.
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

// standinConverter builds a Converter over the stand-in docproc-ooxml at 0.1.0 and the picture
// package's Converter over the stand-in soffice at 26.8.1.1 and the stand-in magick at 7.1.2-34,
// each built from its Spec through the exec runner, and returns it with the stand-ins.
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

// pictures builds the picture package's Converter over the stand-in soffice and magick, each
// built from its Spec through the exec runner.
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

// collect ranges over a conversion's records and returns them with its error, the records
// yielded before it included.
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

// document returns the Document among recs, failing the test unless it is the first record.
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

// writeDocument writes content into a file of its own, outside any working directory, named
// name, "document" when it is "", and returns its path.
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

// mediaName returns a media name the envelope's rule could make: 64 of the hex digit c, then ext.
func mediaName(c, ext string) string { return strings.Repeat(c, 64) + ext }

// pictureLine returns a document line of format naming each of files as a picture, with md as
// its Markdown, and a line feed.
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
