package stages_test

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
)

func prepareText(t *testing.T, ctx context.Context, name string, content []byte) (dispatch.Output, string, string, []string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var outputs []dispatch.Output
	var warnings []string
	for item, err := range stages.NewTextStage().Prepare(ctx, dispatch.Part{Path: path, Name: name}, t.TempDir()) {
		switch {
		case err != nil:
			return dispatch.Output{}, "", path, nil, err
		case item.Warning != nil:
			if w := *item.Warning; w.Tool != "text" || w.Where != "text" || len(outputs) == 0 {
				t.Errorf("%s: warning %+v", name, w)
			}
			warnings = append(warnings, item.Warning.What)
		case item.Output == nil:
			t.Fatalf("%s: the stage found a part", name)
		default:
			outputs = append(outputs, *item.Output)
		}
	}
	if len(outputs) != 1 {
		t.Fatalf("%s: %d outputs, want 1", name, len(outputs))
	}
	o := outputs[0]
	if o.Kind != dispatch.TextOutput || o.Name != "text" || o.MediaType != "text/plain" || o.Value != nil {
		t.Errorf("%s: output %s %q %q, %d bytes inline", name, o.Kind, o.Name, o.MediaType, len(o.Value))
	}
	b, err := os.ReadFile(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	return o, string(b), path, warnings, nil
}

func TestTextStage(t *testing.T) {
	const fallback = "no byte order mark and not UTF-8: decoded as windows-1252, which may not be the text's encoding"
	utf8 := map[string]string{"encoding": "utf-8"}
	bom := func(charset string) map[string]string {
		return map[string]string{"encoding": charset, "encoding_by": "bom"}
	}
	cp1252 := map[string]string{"encoding": "windows-1252", "encoding_by": "fallback"}
	cases := []struct {
		name, content, want string
		meta                map[string]string
		warnings            []string
	}{
		{"ascii", "a,b\r\n1,2\n", "a,b\r\n1,2\n", utf8, nil},
		{"empty", "", "", utf8, nil},
		{"utf-8 storing U+FFFD", "café �\n", "café �\n", utf8, nil},
		{"utf-8 mark", "\xef\xbb\xbfcafé\r\n", "café\r\n", bom("utf-8"), nil},
		{"utf-8 mark, invalid byte", "\xef\xbb\xbfa\xffb�", "a�b�", bom("utf-8"),
			[]string{"1 U+FFFD replacing what utf-8 can't decode"}},
		{"utf-16le mark", "\xff\xfec\x00\xe9\x00\r\x00\n\x00", "cé\r\n", bom("utf-16le"), nil},
		{"utf-16be mark", "\xfe\xff\x00c\x00\xe9\x30\x42", "céあ", bom("utf-16be"), nil},
		{"utf-16le mark, unpaired surrogate and odd byte", "\xff\xfe\x00\xd8a\x00\xfd\xffb",
			"�a��", bom("utf-16le"),
			[]string{"2 U+FFFD replacing what utf-16le can't decode"}},
		{"utf-32le mark", "\xff\xfe\x00\x00c\x00\x00\x00\x00\xf6\x01\x00", "c\U0001f600", bom("utf-32le"), nil},
		{"utf-32be mark, past U+10FFFF", "\x00\x00\xfe\xff\x00\x00\x00c\x00\x11\x00\x00", "c�", bom("utf-32be"),
			[]string{"1 U+FFFD replacing what utf-32be can't decode"}},
		{"windows-1252", "caf\xe9 \x80 \x93q\x94\r\n", "café € “q”\r\n", cp1252,
			[]string{fallback}},
		{"windows-1252, undefined bytes", "a\x81b\x9d", "a�b�", cp1252,
			[]string{fallback, "2 U+FFFD replacing what windows-1252 can't decode"}},
		{"utf-8 then a stray byte", "café\xff", "cafÃ©ÿ", cp1252, []string{fallback}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, got, path, warnings, err := prepareText(t, t.Context(), "a.txt", []byte(c.content))
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("text %q, want %q", got, c.want)
			}
			if !maps.Equal(o.Meta, c.meta) {
				t.Errorf("meta %q, want %q", o.Meta, c.meta)
			}
			if !slices.Equal(warnings, c.warnings) {
				t.Errorf("warnings %q, want %q", warnings, c.warnings)
			}
			if pass := maps.Equal(c.meta, utf8); (o.Path == path) != pass {
				t.Errorf("output at %s, the part at %s", o.Path, path)
			}
		})
	}
}

func TestTextStageVersion(t *testing.T) {
	if v := stages.NewTextStage().Identity().Version; v != "3" {
		t.Errorf("version %q, want 3", v)
	}
}

func TestTextStageChunks(t *testing.T) {
	split := strings.Repeat("a", 64<<10-1) + "é\n"
	o, got, _, _, err := prepareText(t, t.Context(), "split.txt", []byte(split))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"encoding": "utf-8"}; !maps.Equal(o.Meta, want) || got != split {
		t.Errorf("split rune: %q, %d bytes of %d", o.Meta, len(got), len(split))
	}
	late := strings.Repeat("a", 200<<10) + "\xe9"
	o, got, _, _, err = prepareText(t, t.Context(), "late.txt", []byte(late))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"encoding": "windows-1252", "encoding_by": "fallback"}; !maps.Equal(o.Meta, want) || got != late[:len(late)-1]+"é" {
		t.Errorf("late byte: %q, %d bytes", o.Meta, len(got))
	}
}

func TestTextStageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, _, err := prepareText(t, ctx, "a.txt", []byte("caf\xe9")); !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want context.Canceled", err)
	}
}

func TestTextDispatch(t *testing.T) {
	work := t.TempDir()
	d, err := dispatch.New(dispatch.Config{
		Content: dispatch.NewMemoryContent(), Puts: 2,
		Stages: dispatch.Stages{route.Text: stages.NewTextStage()},
		Index:  dispatch.NewMemoryIndex(0), Dir: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"notes.csv": "caf\xe9;1\r\n", "notes": "\xff\xfec\x00a\x00f\x00\xe9\x00",
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var text string
		events := dispatchAll(t, d, path, func(ev *dispatch.Event) {
			if ev.Output != nil {
				b, err := os.ReadFile(ev.Output.Path)
				if err != nil {
					t.Fatal(err)
				}
				text = string(b)
			}
		})
		ev := events[len(events)-1]
		if ev.Part.ContentType.Route != route.Text || ev.Outcome != dispatch.Prepared || ev.Stage != stages.NewTextStage().Identity() || len(ev.Outputs) != 1 {
			t.Fatalf("%s: %s %s by %s, %d outputs", name, ev.Part.ContentType.Route, ev.Outcome, ev.Stage, len(ev.Outputs))
		}
		if want := "café"; !strings.HasPrefix(text, want) {
			t.Errorf("%s: text %q, want it to begin %q", name, text, want)
		}
	}
	wantEmpty(t, work)
}
