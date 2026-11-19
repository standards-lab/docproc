package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/stages"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
	"github.com/standards-lab/docproc/cmd/docproc/internal/apptest"
)

const gib = 1 << 30

// boundVars are the variables the bounds node reads, one per bound.
var boundVars = []string{
	"DOCPROC_TOOL_MEMORY",
	"DOCPROC_UNIT_TIMEOUT",
	"DOCPROC_STREAM_IDLE",
	"DOCPROC_MAX_PARTS",
	"DOCPROC_MAX_BYTES",
	"DOCPROC_MAX_DEPTH",
	"DOCPROC_BOMB_RATIO",
	"DOCPROC_BOMB_SIZE",
	"DOCPROC_ARCHIVE_ENTRIES",
	"DOCPROC_CLAIM_RELEASE",
	"DOCPROC_CONCURRENCY",
	"DOCPROC_PDF_DPI",
	"DOCPROC_PDF_TEXT",
	"DOCPROC_PDF_MAX_PIXELS",
	"DOCPROC_FRAME_MAX_SIDE",
	"DOCPROC_MAGICK_AREA",
	"DOCPROC_MAGICK_MEMORY",
	"DOCPROC_MAGICK_MAP",
	"DOCPROC_MAGICK_DISK",
	"DOCPROC_AUDIO_LIMIT",
}

// startingBounds returns docproc's starting values, the goal's table written out.
func startingBounds() app.Bounds {
	return app.Bounds{
		Stages: stages.Options{
			Memory:      2 * gib,
			Timeout:     10 * time.Minute,
			Idle:        2 * time.Minute,
			Inspect:     inspect.Options{BombRatio: 1000, BombSize: 1 * gib, Entries: 100_000},
			Concurrency: 1,
			Document:    document.ImagesRequest{DPI: 150, Text: true, MaxPixels: 64_000_000},
			Video:       media.Request{MaxSide: 2048},
			Magick:      picture.Limits{Area: 256_000_000, Memory: 1 * gib, Map: 2 * gib, Disk: 8 * gib},
			Audio:       audio.Request{Limit: 30 * time.Second},
		},
		Limits:  dispatch.Limits{Parts: 100_000, Bytes: 32 * gib, Depth: 16},
		Release: 10 * time.Second,
	}
}

// clearBounds empties every bound's variable for the test, so each reads its starting value
// whatever the environment holds.
func clearBounds(t *testing.T) {
	t.Helper()
	for _, name := range boundVars {
		t.Setenv(name, "")
	}
}

// buildBounds builds the bounds node of a fresh App, as a run that declares it does.
func buildBounds(t *testing.T) (app.Bounds, error) {
	t.Helper()
	a := app.New(cli.Streams{})
	sys, err := a.Graph().Build(a.Nodes().Bounds)
	if err != nil {
		return app.Bounds{}, err
	}
	return sys.Get(a.Nodes().Bounds), nil
}

func TestBounds_StartingValues(t *testing.T) {
	clearBounds(t)

	got, err := buildBounds(t)

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if want := startingBounds(); !reflect.DeepEqual(got, want) {
		t.Errorf("bounds = %+v\nwant %+v", got, want)
	}
}

// The library takes the starting values: stages.Build refuses options it can't take before it
// looks anything up, so a lookup that finds nothing leaves only that refusal to fail it.
func TestBounds_StartingValuesTakenByStages(t *testing.T) {
	clearBounds(t)
	b, err := buildBounds(t)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	none := func(program string) (string, error) { return "", errors.New(program + " not found") }

	if _, err := stages.Build(context.Background(), none, b.Stages); err != nil {
		t.Errorf("stages.Build(starting values) = %v, want nil", err)
	}
}

func TestBounds_EachVariableOverridesItsBound(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		set         func(*app.Bounds)
	}{
		{"DOCPROC_TOOL_MEMORY", "536870912", func(b *app.Bounds) { b.Stages.Memory = 512 << 20 }},
		{"DOCPROC_UNIT_TIMEOUT", "90s", func(b *app.Bounds) { b.Stages.Timeout = 90 * time.Second }},
		{"DOCPROC_STREAM_IDLE", "1m30s", func(b *app.Bounds) { b.Stages.Idle = 90 * time.Second }},
		{"DOCPROC_MAX_PARTS", "50", func(b *app.Bounds) { b.Limits.Parts = 50 }},
		{"DOCPROC_MAX_BYTES", "1048576", func(b *app.Bounds) { b.Limits.Bytes = 1 << 20 }},
		{"DOCPROC_MAX_DEPTH", "4", func(b *app.Bounds) { b.Limits.Depth = 4 }},
		{"DOCPROC_BOMB_RATIO", "250.5", func(b *app.Bounds) { b.Stages.Inspect.BombRatio = 250.5 }},
		{"DOCPROC_BOMB_SIZE", "1024", func(b *app.Bounds) { b.Stages.Inspect.BombSize = 1024 }},
		{"DOCPROC_ARCHIVE_ENTRIES", "10", func(b *app.Bounds) { b.Stages.Inspect.Entries = 10 }},
		{"DOCPROC_CLAIM_RELEASE", "500ms", func(b *app.Bounds) { b.Release = 500 * time.Millisecond }},
		{"DOCPROC_CONCURRENCY", "4", func(b *app.Bounds) { b.Stages.Concurrency = 4 }},
		{"DOCPROC_PDF_DPI", "300", func(b *app.Bounds) { b.Stages.Document.DPI = 300 }},
		{"DOCPROC_PDF_TEXT", "false", func(b *app.Bounds) { b.Stages.Document.Text = false }},
		{"DOCPROC_PDF_MAX_PIXELS", "1000", func(b *app.Bounds) { b.Stages.Document.MaxPixels = 1000 }},
		{"DOCPROC_FRAME_MAX_SIDE", "720", func(b *app.Bounds) { b.Stages.Video.MaxSide = 720 }},
		{"DOCPROC_MAGICK_AREA", "1000000", func(b *app.Bounds) { b.Stages.Magick.Area = 1_000_000 }},
		{"DOCPROC_MAGICK_MEMORY", "1024", func(b *app.Bounds) { b.Stages.Magick.Memory = 1024 }},
		{"DOCPROC_MAGICK_MAP", "2048", func(b *app.Bounds) { b.Stages.Magick.Map = 2048 }},
		{"DOCPROC_MAGICK_DISK", "0", func(b *app.Bounds) { b.Stages.Magick.Disk = 0 }},
		{"DOCPROC_AUDIO_LIMIT", "45s", func(b *app.Bounds) { b.Stages.Audio.Limit = 45 * time.Second }},
		{"DOCPROC_AUDIO_LIMIT", "4001ms", func(b *app.Bounds) { b.Stages.Audio.Limit = 4001 * time.Millisecond }},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			clearBounds(t)
			t.Setenv(tc.name, tc.value)

			got, err := buildBounds(t)

			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			want := startingBounds()
			tc.set(&want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("bounds = %+v\nwant %+v", got, want)
			}
		})
	}
}

// The node reads every one of boundVars.
func TestBounds_EveryVariableIsRead(t *testing.T) {
	for _, name := range boundVars {
		t.Run(name, func(t *testing.T) {
			clearBounds(t)
			// A value no bound takes proves the node reads the variable.
			t.Setenv(name, "-1")

			_, err := buildBounds(t)

			if err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("Build with %s=-1: %v, want an error naming %s", name, err, name)
			}
		})
	}
}

func TestBounds_BadValueNamesItsVariable(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"DOCPROC_TOOL_MEMORY", "2GiB"},
		{"DOCPROC_TOOL_MEMORY", "-1"},
		{"DOCPROC_UNIT_TIMEOUT", "10"},
		{"DOCPROC_STREAM_IDLE", "-2m"},
		{"DOCPROC_MAX_PARTS", "1e5"},
		{"DOCPROC_MAX_BYTES", "99999999999999999999"},
		{"DOCPROC_BOMB_RATIO", "NaN"},
		{"DOCPROC_BOMB_RATIO", "+Inf"},
		{"DOCPROC_BOMB_RATIO", "-3"},
		{"DOCPROC_CLAIM_RELEASE", "soon"},
		{"DOCPROC_CONCURRENCY", "1.5"},
		{"DOCPROC_PDF_TEXT", "maybe"},
		{"DOCPROC_FRAME_MAX_SIDE", "-2048"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			clearBounds(t)
			t.Setenv(tc.name, tc.value)

			_, err := buildBounds(t)

			if err == nil || !strings.Contains(err.Error(), tc.name+": ") {
				t.Errorf("Build: %v, want an error naming %s", err, tc.name)
			}
		})
	}
}

// The audio window's limit must be over twice the windows' 2 s overlap, so a window cut at the
// limit keeps audio of its own between its seams; zero, which the library reads as its own
// default, is refused with the rest rather than read as no bound, and the refusal says why.
func TestBounds_AudioLimitRefused(t *testing.T) {
	for _, value := range []string{"4s", "3s", "1ms", "0", "0s", "-30s", "30", "thirty"} {
		t.Run(value, func(t *testing.T) {
			clearBounds(t)
			t.Setenv("DOCPROC_AUDIO_LIMIT", value)

			_, err := buildBounds(t)

			if err == nil || !strings.Contains(err.Error(), "DOCPROC_AUDIO_LIMIT: ") {
				t.Errorf("Build: %v, want an error naming DOCPROC_AUDIO_LIMIT", err)
			}
		})
	}
	t.Run("message", func(t *testing.T) {
		clearBounds(t)
		t.Setenv("DOCPROC_AUDIO_LIMIT", "4s")

		_, err := buildBounds(t)

		want := "DOCPROC_AUDIO_LIMIT: 4s isn't over 4s, twice the 2s overlap between windows"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Build: %v, want %q", err, want)
		}
	})
}

// A bomb rule is set by its ratio and its size together: one of them zero alone fails, naming
// both variables, and both zero sets no rule.
func TestBounds_BombRuleTakesBothOrNeither(t *testing.T) {
	for _, zero := range []string{"DOCPROC_BOMB_RATIO", "DOCPROC_BOMB_SIZE"} {
		t.Run(zero, func(t *testing.T) {
			clearBounds(t)
			t.Setenv(zero, "0")

			_, err := buildBounds(t)

			if err == nil || !strings.Contains(err.Error(), "DOCPROC_BOMB_RATIO and DOCPROC_BOMB_SIZE") {
				t.Errorf("Build: %v, want an error naming both bomb variables", err)
			}
		})
	}
	t.Run("both", func(t *testing.T) {
		clearBounds(t)
		t.Setenv("DOCPROC_BOMB_RATIO", "0")
		t.Setenv("DOCPROC_BOMB_SIZE", "0")

		got, err := buildBounds(t)

		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if rule := got.Stages.Inspect.Bomb(); rule != (inspect.BombRule{}) {
			t.Errorf("bomb rule = %+v, want none", rule)
		}
	})
}

// Every bad variable is named at once, so an operator fixes them in one pass.
func TestBounds_EveryBadVariableNamed(t *testing.T) {
	clearBounds(t)
	t.Setenv("DOCPROC_MAX_DEPTH", "deep")
	t.Setenv("DOCPROC_MAGICK_MAP", "-1")

	_, err := buildBounds(t)

	for _, name := range []string{"DOCPROC_MAX_DEPTH", "DOCPROC_MAGICK_MAP"} {
		if err == nil || !strings.Contains(err.Error(), name+": ") {
			t.Errorf("Build: %v, want an error naming %s", err, name)
		}
	}
}

// A command that declares the bounds node fails, before it runs, on a bound it can't take, the
// variable named on its error line.
func TestRun_BadBoundFailsTheCommand(t *testing.T) {
	clearBounds(t)
	t.Setenv(shutdownTimeout, "")
	t.Setenv("DOCPROC_UNIT_TIMEOUT", "forever")
	a, stdout, stderr, built := newApp(t)
	ran := new(bool)
	a.Root().Add((&cli.Command{
		Name:    "probe",
		Summary: "Build the bounds",
		Run: func(context.Context, *cli.Invocation) error {
			*ran = true
			return nil
		},
	}).Use(a.Nodes().Bounds))

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *ran {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "docproc probe: bounds: DOCPROC_UNIT_TIMEOUT: "; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if log := built.Log(); !slices.Contains(log, "bounds") {
		t.Errorf("nodes built = %q, want bounds among them", log)
	}
}

// A run's stages are built under the audio window's limit: DOCPROC_AUDIO_LIMIT's, or 30 s when
// it is unset or empty.
func TestRun_AudioLimitReachesStages(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 30 * time.Second},
		{"45s", 45 * time.Second},
	} {
		t.Run("DOCPROC_AUDIO_LIMIT="+tc.value, func(t *testing.T) {
			in := write(t, t.TempDir(), "hello.txt", helloText)
			out := filepath.Join(t.TempDir(), "out")
			var got []time.Duration

			r := runCmd(t, context.Background(), func(a *app.App) {
				t.Setenv("DOCPROC_AUDIO_LIMIT", tc.value)
				apptest.Stages(a, func(ctx context.Context, opts stages.Options) (stages.Set, error) {
					got = append(got, opts.Audio.Limit)
					return stages.Build(ctx, apptest.NoTools, opts)
				})
			}, in, "--out", out)

			if r.code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitOK, r.stderr)
			}
			if !slices.Equal(got, []time.Duration{tc.want}) {
				t.Errorf("stages built with audio limits %v, want [%v]", got, tc.want)
			}
		})
	}
}

// An audio window's limit the run can't take fails it, naming DOCPROC_AUDIO_LIMIT, before any
// tool is looked up.
func TestRun_AudioLimitRefusedBeforeLookup(t *testing.T) {
	for _, value := range []string{"4s", "0", "thirty"} {
		t.Run(value, func(t *testing.T) {
			in := write(t, t.TempDir(), "hello.txt", helloText)
			out := filepath.Join(t.TempDir(), "out")
			lookup, looked := probes(apptest.NoTools)

			r := runCmd(t, context.Background(), func(a *app.App) {
				t.Setenv("DOCPROC_AUDIO_LIMIT", value)
				apptest.Tools(a, lookup)
			}, in, "--out", out)

			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d", r.code, process.ExitFailure)
			}
			if want := "docproc run: bounds: DOCPROC_AUDIO_LIMIT: "; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			if log := looked.Log(); len(log) != 0 {
				t.Errorf("tools looked up = %q, want none", log)
			}
		})
	}
}

// A command that doesn't declare the bounds node reads none of its variables.
func TestRun_VersionReadsNoBound(t *testing.T) {
	for _, name := range boundVars {
		t.Setenv(name, "unreadable")
	}
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"version"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if stdout.String() != "(devel)\n" {
		t.Errorf("stdout = %q, want %q", stdout, "(devel)\n")
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}
