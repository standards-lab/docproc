//go:build linux

package stages_test

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

type onenoteSettings struct {
	Stdout string            `json:"stdout"`
	Stderr string            `json:"stderr"`
	Exit   int               `json:"exit"`
	Media  map[string]string `json:"media"`
	HoldMS int               `json:"hold_ms,omitzero"`
	Signal string            `json:"signal,omitzero"`
	Limit  bool              `json:"limit,omitzero"`
	Home   string            `json:"home,omitzero"`
}

func envelopeStandin(minimum string) tooltest.Program {
	return func(r tooltest.Run) int { return envelopeRun(r, minimum) }
}

func envelopeRun(r tooltest.Run, minimum string) int {
	var s onenoteSettings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if home := cmp.Or(os.Getenv("HOME"), s.Home); s.Home != "" && home != s.Home {
		fmt.Fprintf(os.Stderr, "mise ERROR Config files in %s are not trusted.\n", filepath.Join(s.Home, ".config", "mise", "config.toml"))
		return 1
	}
	if len(r.Args) == 1 && r.Args[0] == "--version" {
		fmt.Printf("%s\nschema 2\n", minimum)
		return 0
	}
	if s.Limit {
		var l syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_AS, &l); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "docproc-onenote: warning: limit: %d\n", l.Cur)
	}
	time.Sleep(time.Duration(s.HoldMS) * time.Millisecond)
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
	if s.Signal != "" {
		err := syscall.Exec("/bin/sh", []string{"sh", "-c", "kill -s " + s.Signal + " $$"}, nil)
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return s.Exit
}

func standinOneNoteStage(t *testing.T, s onenoteSettings) (*stages.OneNoteStage, string) {
	t.Helper()
	spec := onenote.OneNoteSpec()
	link := tooltest.Link(t, t.TempDir(), "docproc-onenote", s)
	spec.Program = link
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := onenote.NewReader(onenote.Config{OneNote: tl})
	if err != nil {
		t.Fatal(err)
	}
	return stages.NewOneNoteStage(stages.OneNoteConfig{Reader: r, Version: stages.Versions(tl)}), link
}

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

func TestOneNoteStageRecords(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("a jpg"))
	jpg := hex.EncodeToString(sum[:]) + ".jpg"
	sum = sha256.Sum256([]byte("an svg"))
	svg := hex.EncodeToString(sum[:]) + ".svg"
	sum = sha256.Sum256([]byte("a recording"))
	rec := hex.EncodeToString(sum[:]) + ".bin"
	stage, link := standinOneNoteStage(t, onenoteSettings{
		Stdout: `{"line":"notebook","schema":"2","input":"package","name":"S"}
{"line":"section","id":"s","path":["S"]}
{"line":"page","id":"p","section":"s","level":1,"title":"T","modification_time":"2026-01-12T14:15:00Z","markdown":"# T","media":[{"kind":"image","file":"` + jpg + `","filename":"photo.jpg","size":5},{"kind":"embedded_file","filename":"gone.docx"},{"kind":"ink","file":"` + svg + `"},{"kind":"embedded_file","file":"` + rec + `","filename":"memo","size":11,"type":"audio"}]}
{"line":"skipped","kind":"section","name":"Locked.one","reason":"encrypted section","encrypted":true}
`,
		Stderr: "docproc-onenote: warning: /S, page p, embedded file 1: no data\n",
		Media:  map[string]string{jpg: "a jpg", svg: "an svg", rec: "a recording"},
	})
	items, err := prepare(t, stage)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	warnings := 0
	for _, it := range items {
		switch {
		case it.Output != nil:
			got = append(got, string(it.Output.Kind)+" "+it.Output.Name)
			if it.Output.Kind == dispatch.RecordOutput && it.Output.MediaType != dispatch.MediaOneNote {
				t.Errorf("%s's media type %q", it.Output.Name, it.Output.MediaType)
			}
		case it.Warning != nil:
			if w := *it.Warning; w != (dispatch.Warning{Tool: "docproc-onenote", Where: "/S, page p, embedded file 1", What: "no data"}) {
				t.Errorf("warning %+v", w)
			}
			warnings++
		case it.Part != nil && it.Part.Outcome != "":
			got = append(got, "unread "+it.Part.Name+" at "+it.Part.Where+": "+string(it.Part.Outcome))
			if it.Part.Path != "" || it.Part.Reason == "" {
				t.Errorf("unread child %+v: want no file and a reason", it.Part)
			}
		case it.Part != nil:
			got = append(got, "part "+it.Part.Name+" at "+it.Part.Where+" "+it.Part.MIME)
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
		"part photo.jpg at page p, media 1 ",
		"unread gone.docx at page p, media 2: failed",
		"part " + svg + " at page p, media 3 ",
		"part memo at page p, media 4 audio",
		"unread Locked.one at skipped section Locked.one: encrypted",
	}
	if !slices.Equal(got, want) || warnings != 1 {
		t.Errorf("items\n got %q\nwant %q; %d warnings, want 1", got, want, warnings)
	}
	runs := tooltest.Runs(t, link)
	if last := runs[len(runs)-1]; !slices.Contains(last.Args, "--file-only") {
		t.Errorf("docproc-onenote ran with %q, want --file-only", last.Args)
	}
}

func TestOneNoteStageRefusals(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]error{
		"encrypted section (a password-protected section); remove its password in OneNote first": dispatch.ErrEncrypted,
		cli.TableOfContents:                           dispatch.ErrReadElsewhere,
		"not a OneNote section, notebook, or package": nil,
	} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			stage, _ := standinOneNoteStage(t, onenoteSettings{Stderr: "docproc-onenote: error: " + reason + "\n", Exit: 3})
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
