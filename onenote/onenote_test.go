//go:build linux

package onenote_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/tool"
)

// TestNewReader: a stand-in at the minimum, or a build past it, is accepted; an older one fails at
// construction, whether the Tool's Spec or NewReader refuses it, and one whose --version names
// another schema fails as the Tool is built; one the container runner built is refused with
// ErrRequest; a nil Tool panics, naming the field.
func TestNewReader(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.1.0", "0.1.0+8ea6b2d", "0.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			on := installStandin(t, version)
			tl, err := tool.NewExec(t.Context(), on.spec())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := onenote.NewReader(onenote.Config{OneNote: tl}); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("older", func(t *testing.T) {
		t.Parallel()
		on := installStandin(t, "0.0.9")
		_, err := tool.NewExec(t.Context(), on.spec())
		if !errors.Is(err, tool.ErrTooOld) {
			t.Fatalf("spec's own minimum: got %v, want ErrTooOld", err)
		}
		spec := on.spec()
		spec.Minimum = "0.0.1"
		tl, err := tool.NewExec(t.Context(), spec)
		if err != nil {
			t.Fatal(err)
		}
		_, err = onenote.NewReader(onenote.Config{OneNote: tl})
		var verr *tool.VersionError
		if !errors.Is(err, tool.ErrTooOld) || !errors.As(err, &verr) || verr.Found != "0.0.9" || verr.Minimum != onenote.OneNoteMinimum {
			t.Fatalf("NewReader: got %v, want a VersionError for 0.0.9 under %s", err, onenote.OneNoteMinimum)
		}
	})
	t.Run("schema 1", func(t *testing.T) {
		t.Parallel()
		on := installStandin(t, "0.1.0")
		on.set(func(s *onenoteSettings) { s.Schema = "1" })
		if _, err := tool.NewExec(t.Context(), on.spec()); !errors.Is(err, tool.ErrNoVersion) {
			t.Errorf("NewExec over a schema 1 docproc-onenote: got %v, want ErrNoVersion", err)
		}
	})
	t.Run("container", func(t *testing.T) {
		t.Parallel()
		on := installStandin(t, "0.1.0")
		docker := tooltest.Link(t, t.TempDir(), "docker", on.settings)
		image := tool.Image{Ref: "example.invalid/onenote:1@sha256:" + strings.Repeat("0", 64)}
		boxed, err := tool.NewContainer(t.Context(), docker, image, on.spec())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := onenote.NewReader(onenote.Config{OneNote: boxed}); !errors.Is(err, onenote.ErrRequest) {
			t.Errorf("NewReader over a container's docproc-onenote: got %v, want ErrRequest", err)
		}
	})
	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if r := recover(); r != "onenote: Config.OneNote is nil" {
				t.Errorf("recovered %v, want the panic naming Config.OneNote", r)
			}
		}()
		_, _ = onenote.NewReader(onenote.Config{})
		t.Error("NewReader returned")
	})
}

// mediaName returns the media name of content stored with ext, an extension with its dot or "".
func mediaName(content, ext string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:]) + ext
}

// The media names fullLines gives its page's image, recording, and ink.
var (
	mediaJPG = mediaName("a jpg", ".jpg")
	mediaMP3 = mediaName("an mp3", ".mp3")
	mediaSVG = mediaName("an svg", ".svg")
)

// fullLines are a notebook's lines with every field of schema "2" set, an unknown kind among them.
var fullLines = `{"line":"notebook","schema":"2","input":"notebook","name":"Work","id":"f1da443f-a65f-4513-b200-78d8a9910b8d","color":"#9bbbd2"}
{"line":"section_group","id":"11111111-2222-3333-4444-555555555555","path":["Group"]}
{"line":"section","id":"0575dd0a-5612-d746-b82b-983f7a80b282","parent":"11111111-2222-3333-4444-555555555555","path":["Group","Notes"],"color":"#8aa8e4"}
{"line":"page","id":"5eaf76b1-8dde-8745-a8a7-1a9865cc478a","section":"0575dd0a-5612-d746-b82b-983f7a80b282","level":2,"title":"Test Page","creation_time":"2020-10-27T10:47:23Z","modification_time":"2026-05-20T21:18:56Z","author":"Markus Siemens","markdown":"# Test Page\n\n![ink: Hallo](` + mediaSVG + `)","media":[{"kind":"image","file":"` + mediaJPG + `","filename":"a.jpg","size":5,"extension":"jpg"},{"kind":"embedded_file","file":"` + mediaMP3 + `","filename":"a.mp3","size":6,"type":"audio"},{"kind":"embedded_file","filename":"gone.docx"},{"kind":"ink","file":"` + mediaSVG + `"}],"ink_alternatives":[{"word":0,"alternatives":["Hallo","Hell"]},{"word":3,"alternatives":["x"]}]}
{"line":"skipped","kind":"section","name":"Encryption.one","reason":"encrypted section","encrypted":true}
{"line":"skipped","kind":"section","name":"Broken.one","reason":"Unexpected end of file"}
{"line":"outline","when":"later"}
`

// TestRead: the stand-in's lines are yielded in its order, each decoded into its typed record,
// every field of schema "2" set; a line of an unknown kind is kept with its JSON. docproc-onenote
// runs once, on the input made absolute, with no media directory, and the files the lines name
// have their SHA-256 and no Path; a medium with no file has neither.
func TestRead(t *testing.T) {
	r, on := standinReader(t)
	on.stdout(fullLines)
	t.Chdir(t.TempDir())
	seq := r.Read(t.Context(), onenote.Request{Input: "-notes.one"})
	if got := on.runs(); len(got) != 0 {
		t.Fatalf("runs %q before the sequence was ranged over", got)
	}
	recs, err := collect(seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 7 {
		t.Fatalf("%d records, want 7: %v", len(recs), recs)
	}
	at := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	five, six := int64(5), int64(6)
	want := []onenote.Record{
		&onenote.Notebook{Schema: "2", Input: "notebook", Name: "Work", ID: "f1da443f-a65f-4513-b200-78d8a9910b8d", Color: "#9bbbd2"},
		&onenote.SectionGroup{ID: "11111111-2222-3333-4444-555555555555", Path: []string{"Group"}},
		&onenote.Section{ID: "0575dd0a-5612-d746-b82b-983f7a80b282", Parent: "11111111-2222-3333-4444-555555555555", Path: []string{"Group", "Notes"}, Color: "#8aa8e4"},
		&onenote.Page{
			ID: "5eaf76b1-8dde-8745-a8a7-1a9865cc478a", Section: "0575dd0a-5612-d746-b82b-983f7a80b282", Level: 2, Title: "Test Page",
			CreationTime: at("2020-10-27T10:47:23Z"), ModificationTime: at("2026-05-20T21:18:56Z"), Author: "Markus Siemens",
			Markdown: "# Test Page\n\n![ink: Hallo](" + mediaSVG + ")",
			Media: []onenote.Medium{
				{Kind: onenote.ImageMedium, File: mediaJPG, Filename: "a.jpg", Size: &five, Extension: "jpg", SHA256: mediaJPG[:64]},
				{Kind: onenote.EmbeddedFileMedium, File: mediaMP3, Filename: "a.mp3", Size: &six, Type: "audio", SHA256: mediaMP3[:64]},
				{Kind: onenote.EmbeddedFileMedium, Filename: "gone.docx"},
				{Kind: onenote.InkMedium, File: mediaSVG, SHA256: mediaSVG[:64]},
			},
			InkAlternatives: []onenote.InkAlternative{{Word: 0, Alternatives: []string{"Hallo", "Hell"}}, {Word: 3, Alternatives: []string{"x"}}},
		},
		&onenote.Skipped{Kind: "section", Name: "Encryption.one", Reason: "encrypted section", Encrypted: true},
		&onenote.Skipped{Kind: "section", Name: "Broken.one", Reason: "Unexpected end of file"},
	}
	for i, w := range want {
		if !reflect.DeepEqual(recs[i], w) {
			t.Errorf("record %d: %+v\nwant %+v", i, recs[i], w)
		}
	}
	unknown, ok := recs[6].(*onenote.UnknownRecord)
	if !ok || unknown.Kind != "outline" || string(unknown.Raw) != `{"line":"outline","when":"later"}` {
		t.Errorf("unknown record %#v", recs[6])
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := on.runs(), [][]string{{filepath.Join(dir, "-notes.one")}}; !reflect.DeepEqual(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}
}

// TestReadMedia: a Request's media directory, made absolute, reaches docproc-onenote as --media
// before the input, and each medium's Path is its file there, the file docproc-onenote wrote; a
// medium with no file has no Path. A file name the media name rule can't have made ends the read
// with an error.
func TestReadMedia(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	on.set(func(s *onenoteSettings) { s.Media = []string{mediaJPG, mediaSVG} })
	on.stdout(`{"line":"notebook","schema":"2","input":"section","name":"S"}
{"line":"page","id":"p","media":[{"kind":"image","file":"` + mediaJPG + `"},{"kind":"image"},{"kind":"ink","file":"` + mediaSVG + `"}]}
`)
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	input := filepath.Join(dir, "in.one")
	recs, err := collect(r.Read(t.Context(), onenote.Request{Input: input, Media: media}))
	if err != nil {
		t.Fatal(err)
	}
	p := recs[1].(*onenote.Page)
	for _, i := range []int{0, 2} {
		m := p.Media[i]
		if m.Path != filepath.Join(media, m.File) {
			t.Errorf("%s: path %q, want it in %s", m.File, m.Path, media)
			continue
		}
		if b, err := os.ReadFile(m.Path); err != nil || string(b) != m.File {
			t.Errorf("%s: %q, %v; want the file docproc-onenote wrote", m.File, b, err)
		}
	}
	if m := p.Media[1]; m.Path != "" || m.SHA256 != "" {
		t.Errorf("a medium with no file: path %q, SHA-256 %q", m.Path, m.SHA256)
	}
	if got, want := on.runs(), [][]string{{"--media", media, input}}; !reflect.DeepEqual(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}

	for _, bad := range []string{"../escape.jpg", strings.ToUpper(mediaJPG), mediaJPG[:64] + ".toolongext"} {
		on.stdout(`{"line":"notebook","schema":"2","input":"section","name":"S"}
{"line":"page","id":"p","media":[{"kind":"image","file":"` + bad + `"}]}
`)
		recs, err := collect(r.Read(t.Context(), onenote.Request{Input: input, Media: media}))
		if err == nil || len(recs) != 1 {
			t.Errorf("file %q: %d records, %v; want the notebook and an error", bad, len(recs), err)
		}
	}
}

// TestReadFileOnly: a Request's FileOnly reaches docproc-onenote as --file-only, after the media
// directory and before the input; without it none is passed.
func TestReadFileOnly(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	dir := t.TempDir()
	input, media := filepath.Join(dir, "Open Notebook.onetoc2"), filepath.Join(dir, "m")
	for _, req := range []onenote.Request{
		{Input: input, Media: media, FileOnly: true},
		{Input: input, FileOnly: true},
		{Input: input},
	} {
		if _, err := collect(r.Read(t.Context(), req)); err != nil {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	want := [][]string{{"--media", media, "--file-only", input}, {"--file-only", input}, {input}}
	if got := on.runs(); !reflect.DeepEqual(got, want) {
		t.Errorf("runs %q, want %q", got, want)
	}
}

// TestReadWarnings: a read whose stderr holds warnings, with a section skipped, succeeds; each
// warning line is one Warning record, docproc-onenote's name its Tool, in order, its where
// unescaped.
func TestReadWarnings(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	on.stdout(`{"line":"notebook","schema":"2","input":"notebook","name":"N"}
{"line":"skipped","kind":"section","name":"Broken.one","reason":"Unexpected end of file"}
`)
	on.stderr("docproc-onenote: warning: /Broken: the section is skipped: Unexpected end of file\n" +
		"docproc-onenote: warning: /OneNote_RecycleBin: the recycle bin's sections and pages are not read\n" +
		"docproc-onenote: warning: /A:\\ B, page 1, image 2: no data\n")
	recs, err := collect(r.Read(t.Context(), onenote.Request{Input: "notebook.onetoc2"}))
	if err != nil {
		t.Fatal(err)
	}
	var warnings []onenote.Warning
	for _, rec := range recs {
		if w, ok := rec.(*onenote.Warning); ok {
			warnings = append(warnings, *w)
		}
	}
	if n := len(recs) - len(warnings); n != 2 {
		t.Errorf("%d records besides the warnings, want 2", n)
	}
	want := []onenote.Warning{
		{Tool: "docproc-onenote", Where: "/Broken", What: "the section is skipped: Unexpected end of file"},
		{Tool: "docproc-onenote", Where: "/OneNote_RecycleBin", What: "the recycle bin's sections and pages are not read"},
		{Tool: "docproc-onenote", Where: "/A: B, page 1, image 2", What: "no data"},
	}
	if !slices.Equal(warnings, want) {
		t.Errorf("warnings %+v\nwant %+v", warnings, want)
	}
}

// TestReadExit: exit codes 1 to 4 are a *cli.ExitError matching the code's error and no other,
// carrying docproc-onenote's reason and unwrapping to the Tool's *tool.ExitError; a warning before
// the error is still yielded. A refusal whose reason begins "encrypted" is classed Encrypted, the
// table of contents' fixed reason ReadElsewhere, and any other Unclassed.
func TestReadExit(t *testing.T) {
	t.Parallel()
	const encrypted = "encrypted section (a password-protected section, holding an [MS-ONESTORE] 2.5.19 encryption key); remove its password in OneNote first"
	cases := []struct {
		code    int
		want    error
		stderr  string
		msg     string
		refusal error
	}{
		{1, onenote.ErrNotWritten, errorPrefix + "writing stdout: Broken pipe\n", "writing stdout: Broken pipe", nil},
		{2, onenote.ErrUsage, errorPrefix + "unknown option --bogus\nusage: docproc-onenote [--media <dir>] [--file-only] <input>\n", "unknown option --bogus", nil},
		{3, onenote.ErrRefused, errorPrefix + "a ZIP archive; extract the OneDrive download first\n", "a ZIP archive; extract the OneDrive download first", nil},
		{3, onenote.ErrRefused, errorPrefix + encrypted + "\n", encrypted, dispatch.ErrEncrypted},
		{3, onenote.ErrRefused, errorPrefix + cli.TableOfContents + "\n", cli.TableOfContents, dispatch.ErrReadElsewhere},
		{4, onenote.ErrFailed, "docproc-onenote: warning: /: odd\n" + errorPrefix + "internal fault: the read thread can't start\n", "internal fault: the read thread can't start", nil},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.code)+" "+c.msg, func(t *testing.T) {
			t.Parallel()
			r, on := standinReader(t)
			on.stdout("")
			on.stderr(c.stderr)
			on.exit(c.code)
			recs, err := collect(r.Read(t.Context(), onenote.Request{Input: "in.one"}))
			exit, ok := errors.AsType[*cli.ExitError](err)
			if !ok || !errors.Is(err, c.want) {
				t.Fatalf("got %v, want a *cli.ExitError matching %v", err, c.want)
			}
			if _, ok := errors.AsType[*tool.ExitError](err); !ok {
				t.Errorf("%v doesn't unwrap to a *tool.ExitError", err)
			}
			for _, other := range []error{onenote.ErrNotWritten, onenote.ErrUsage, onenote.ErrRefused, onenote.ErrFailed} {
				if other != c.want && errors.Is(err, other) {
					t.Errorf("matches %v too", other)
				}
			}
			if exit.Code != c.code || exit.Message != c.msg || exit.Refusal() != c.refusal {
				t.Errorf("code %d, message %q, refusal %v; want %d, %q, %v", exit.Code, exit.Message, exit.Refusal(), c.code, c.msg, c.refusal)
			}
			for _, rec := range recs {
				if _, ok := rec.(*onenote.Warning); !ok {
					t.Errorf("record %+v, want warnings alone", rec)
				}
			}
			if c.code == 4 && (len(recs) != 1 || *recs[0].(*onenote.Warning) != onenote.Warning{Tool: "docproc-onenote", Where: "/", What: "odd"}) {
				t.Errorf("records %+v, want the warning", recs)
			}
		})
	}
}

// TestReadSchema: a notebook line of another schema, or of none, or a first line that isn't a
// notebook line, ends the read with ErrSchema, and nothing is yielded.
func TestReadSchema(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"schema 1":     `{"line":"notebook","schema":"1","input":"section"}` + "\n" + `{"line":"section","id":"s"}` + "\n",
		"none":         `{"line":"notebook","input":"section"}` + "\n",
		"not notebook": `{"line":"section","id":"s"}` + "\n",
		"empty":        "",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, on := standinReader(t)
			on.stdout(stdout)
			recs, err := collect(r.Read(t.Context(), onenote.Request{Input: "in.one"}))
			if !errors.Is(err, onenote.ErrSchema) {
				t.Fatalf("got %v, want ErrSchema", err)
			}
			if len(recs) != 0 {
				t.Errorf("records %v, want none", recs)
			}
		})
	}
}

// TestReadMalformed: output that isn't docproc-onenote's lines is an error, whatever was yielded
// before it.
func TestReadMalformed(t *testing.T) {
	t.Parallel()
	notebook := `{"line":"notebook","schema":"2"}` + "\n"
	cases := map[string]string{
		"no line":    notebook + `{"id":"s"}` + "\n",
		"not JSON":   notebook + "not JSON\n",
		"cut short":  notebook + `{"line":"section",`,
		"wrong type": notebook + `{"line":"page","level":"x"}` + "\n",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, on := standinReader(t)
			on.stdout(stdout)
			recs, err := collect(r.Read(t.Context(), onenote.Request{Input: "in.one"}))
			if err == nil {
				t.Fatal("want an error")
			}
			if len(recs) != 1 {
				t.Errorf("%d records, want the notebook alone", len(recs))
			}
		})
	}
}

// TestReadCancel: records are yielded while docproc-onenote runs, and a read whose context is
// cancelled while it runs stops it and ends with an error matching context.Canceled, never an
// ExitError, well before docproc-onenote would have exited.
func TestReadCancel(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	on.sleep()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	start := time.Now()
	var recs []onenote.Record
	var err error
	for rec, rerr := range r.Read(ctx, onenote.Request{Input: "in.one"}) {
		if rerr != nil {
			err = rerr
			break
		}
		recs = append(recs, rec)
		if len(recs) == 3 {
			cancel()
		}
	}
	if _, ok := errors.AsType[*cli.ExitError](err); !errors.Is(err, context.Canceled) || ok {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if len(recs) != 3 {
		t.Errorf("%d records before the cancel, want 3", len(recs))
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("took %v", d)
	}
}

// TestReadDeadline: a read whose deadline passes while docproc-onenote runs ends with an error
// matching context.DeadlineExceeded.
func TestReadDeadline(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	on.sleep()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	recs, err := collect(r.Read(ctx, onenote.Request{Input: "in.one"}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if len(recs) != 3 {
		t.Errorf("%d records, want 3", len(recs))
	}
}

// TestReadStop: a loop that stops early stops docproc-onenote, without waiting for it to exit, and
// nothing more is yielded.
func TestReadStop(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	on.sleep()
	start := time.Now()
	n := 0
	for _, err := range r.Read(t.Context(), onenote.Request{Input: "in.one"}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		break
	}
	if n != 1 {
		t.Errorf("%d records", n)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("took %v", d)
	}
}

// TestReadRefused: Read refuses an invalid request with an error matching ErrRequest before
// anything runs: an empty Input, and, from a working directory that has been removed, a relative
// Input or Media, which can't be made absolute. A cancelled context runs nothing either. An
// absolute Input with no Media is still read from there.
func TestReadRefused(t *testing.T) {
	r, on := standinReader(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(t.TempDir(), "in.one")
	cases := map[string]onenote.Request{
		"no input":       {Media: "m"},
		"relative input": {Input: "in.one"},
		"relative media": {Input: abs, Media: "m"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			recs, err := collect(r.Read(t.Context(), req))
			if !errors.Is(err, onenote.ErrRequest) || len(recs) != 0 {
				t.Errorf("got %d records, %v; want ErrRequest and none", len(recs), err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := collect(r.Read(ctx, onenote.Request{Input: abs})); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: got %v", err)
	}
	if got := on.runs(); len(got) != 0 {
		t.Errorf("runs %q, want none", got)
	}
	if _, err := collect(r.Read(t.Context(), onenote.Request{Input: abs})); err != nil {
		t.Errorf("absolute input: %v", err)
	}
}

// TestReadUTC: a time docproc-onenote writes with an offset is decoded as the same instant in UTC.
func TestReadUTC(t *testing.T) {
	t.Parallel()
	r, on := standinReader(t)
	on.stdout(`{"line":"notebook","schema":"2","input":"section","name":"S"}
{"line":"page","id":"p","creation_time":"2026-01-12T19:45:00+05:30","modification_time":"2026-01-12T19:45:00+05:30"}
`)
	recs, err := collect(r.Read(t.Context(), onenote.Request{Input: "in.one"}))
	if err != nil {
		t.Fatal(err)
	}
	p := recs[1].(*onenote.Page)
	want := time.Date(2026, 1, 12, 14, 15, 0, 0, time.UTC)
	for name, got := range map[string]time.Time{"creation": p.CreationTime, "modification": p.ModificationTime} {
		if !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("%s time %v, want %v in UTC", name, got, want)
		}
	}
}
