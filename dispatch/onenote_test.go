//go:build linux

package dispatch_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/tool"
)

// onenoteSettings are the stand-in docproc-onenote's settings, which its link names.
type onenoteSettings struct {
	// Stdout and Stderr are what a run writes, Exit its exit code.
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
	// Media are the files a run writes into its --media directory, by name.
	Media map[string]string `json:"media"`
}

// onenoteStandin is the stand-in docproc-onenote: with --version alone it prints its minimum and
// schema 2; otherwise it writes its settings' Media into the directory after --media, then their
// Stdout and Stderr, and exits with their Exit.
func onenoteStandin(r tooltest.Run) int {
	var s onenoteSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(r.Args) == 1 && r.Args[0] == "--version" {
		fmt.Printf("%s\nschema 2\n", onenote.OneNoteMinimum)
		return 0
	}
	for i, arg := range r.Args {
		if arg != "--media" || i+1 >= len(r.Args) {
			continue
		}
		for name, content := range s.Media {
			if err := os.WriteFile(filepath.Join(r.Args[i+1], name), []byte(content), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}
	fmt.Print(s.Stdout)
	fmt.Fprint(os.Stderr, s.Stderr)
	return s.Exit
}

// standinOneNoteStage returns a onenote stage over the stand-in docproc-onenote, set to s.
func standinOneNoteStage(t *testing.T, s onenoteSettings) *dispatch.OneNoteStage {
	t.Helper()
	spec := onenote.OneNoteSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "docproc-onenote", s)
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := onenote.NewReader(onenote.Config{OneNote: tl})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := dispatch.NewOneNoteStage(dispatch.OneNoteConfig{Reader: r, Version: dispatch.Versions(tl)})
	if err != nil {
		t.Fatal(err)
	}
	return stage
}

// prepare runs stage's Prepare over a part in a directory of its own, returning its items and the
// error that ended them, if any.
func prepare(t *testing.T, stage dispatch.Stage) ([]dispatch.Item, error) {
	t.Helper()
	var items []dispatch.Item
	for it, err := range stage.Prepare(t.Context(), dispatch.Part{Path: filepath.Join(t.TempDir(), "in.one")}, t.TempDir()) {
		if err != nil {
			return items, err
		}
		items = append(items, it)
	}
	return items, nil
}

// TestOneNoteStageRecords: a page is its record, then its Markdown; each of its media whose file
// docproc-onenote wrote is a found part named by its filename, else its media name, at "page <id>,
// media <n>", carrying its SHA-256; a medium with no file is a record at its place, its Meta naming
// why; and the warnings are a last record, each "<where>: <what>".
func TestOneNoteStageRecords(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("a jpg"))
	jpg := hex.EncodeToString(sum[:]) + ".jpg"
	sum = sha256.Sum256([]byte("an svg"))
	svg := hex.EncodeToString(sum[:]) + ".svg"
	stage := standinOneNoteStage(t, onenoteSettings{
		Stdout: `{"line":"notebook","schema":"2","input":"section","name":"S"}
{"line":"section","id":"s","path":["S"]}
{"line":"page","id":"p","section":"s","level":1,"title":"T","modification_time":"2026-01-12T14:15:00Z","markdown":"# T","media":[{"kind":"image","file":"` + jpg + `","filename":"photo.jpg","size":5},{"kind":"embedded_file","filename":"gone.docx"},{"kind":"ink","file":"` + svg + `"}]}
`,
		Stderr: "docproc-onenote: warning: /S, page p, embedded file 1: no data\n",
		Media:  map[string]string{jpg: "a jpg", svg: "an svg"},
	})
	items, err := prepare(t, stage)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		switch {
		case it.Output != nil:
			got = append(got, string(it.Output.Kind)+" "+it.Output.Name)
			if it.Output.Name == "page p, media 2" && it.Output.Meta["reason"] == "" {
				t.Errorf("the medium with no file has no reason: %+v", it.Output.Meta)
			}
			if it.Output.Name == "warnings" && string(it.Output.Value) != `["/S, page p, embedded file 1: no data"]` {
				t.Errorf("warnings %s", it.Output.Value)
			}
		case it.Part != nil:
			got = append(got, "part "+it.Part.Name+" at "+it.Part.Where)
			if it.Part.SHA256 == "" || it.Part.Path == "" {
				t.Errorf("found part %+v: want its file and its SHA-256", it.Part)
			}
			if it.Part.Release != nil {
				it.Part.Release()
			}
		}
	}
	want := []string{
		"record notebook", "record section s", "record page p", "markdown page p",
		"part photo.jpg at page p, media 1",
		"record page p, media 2",
		"part " + svg + " at page p, media 3",
		"record warnings",
	}
	if !slices.Equal(got, want) {
		t.Errorf("items\n got %q\nwant %q", got, want)
	}
}

// TestOneNoteStageRefusals: a refusal internal/cli classes as encrypted ends the stage with
// ErrEncrypted, one it classes as read elsewhere, the table of contents', with ErrReadElsewhere,
// and any other with neither; each wraps the *cli.ExitError.
func TestOneNoteStageRefusals(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]error{
		"encrypted section (a password-protected section); remove its password in OneNote first": dispatch.ErrEncrypted,
		cli.TableOfContents:                           dispatch.ErrReadElsewhere,
		"not a OneNote section, notebook, or package": nil,
	} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			stage := standinOneNoteStage(t, onenoteSettings{Stderr: "docproc-onenote: error: " + reason + "\n", Exit: 3})
			items, err := prepare(t, stage)
			if len(items) != 0 {
				t.Errorf("items %+v, want none", items)
			}
			if _, ok := errors.AsType[*cli.ExitError](err); !ok || !errors.Is(err, onenote.ErrRefused) {
				t.Fatalf("got %v, want the refusal's *cli.ExitError", err)
			}
			for _, class := range []error{dispatch.ErrEncrypted, dispatch.ErrReadElsewhere} {
				if errors.Is(err, class) != (class == want) {
					t.Errorf("%v matches %v: %t", err, class, class != want)
				}
			}
		})
	}
}
