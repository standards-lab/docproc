package app_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
	"github.com/standards-lab/docproc/cmd/docproc/internal/apptest"
)

var shutdownTimeout = lifecycle.NewEnv("DOCPROC").ShutdownTimeout

func newApp(t *testing.T) (a *app.App, stdout, stderr *bytes.Buffer, built *apptest.Recorder) {
	t.Helper()
	stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	a = app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr})
	return a, stdout, stderr, apptest.Builds(a)
}

func probe(a *app.App) *bool {
	ran := new(bool)
	a.Root().Add((&cli.Command{
		Name:    "probe",
		Summary: "Build the lifecycle config",
		Run: func(context.Context, *cli.Invocation) error {
			*ran = true
			return nil
		},
	}).Use(a.Nodes().LifecycleConfig))
	return ran
}

func TestRun_RootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv(shutdownTimeout, "not-a-duration")
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.Contains(stdout.String(), "version") {
				t.Errorf("stdout = %q, want the root help listing version", stdout)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"bogus"})

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d", code, process.ExitUsage)
	}
	if want := `docproc: unknown command "bogus"`; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestRun_UsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown root flag", []string{"--bogus"}, "docproc: "},
		{"version argument", []string{"version", "extra"}, "docproc version: "},
		{"unknown version flag", []string{"version", "--bogus"}, "docproc version: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(shutdownTimeout, "not-a-duration")
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), tc.args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.HasPrefix(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, tc.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

func TestRun_VersionHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			t.Setenv(shutdownTimeout, "not-a-duration")
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), []string{"version", flag})

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.Contains(stdout.String(), "Print the docproc version") {
				t.Errorf("stdout = %q, want the version command's help", stdout)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

func TestRun_Version(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"version"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if stdout.String() != "(devel)\n" {
		t.Errorf("stdout = %q, want %q", stdout, "(devel)\n")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestNew_IsCold(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")

	a, stdout, stderr, _ := newApp(t)

	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("New() wrote %q, %q, want nothing", stdout, stderr)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Replace after New() panicked: %v; want the graph unbuilt", r)
		}
	}()
	apptest.Halt(a)
}

func TestRun_CommandBuildsTheLifecycleConfig(t *testing.T) {
	t.Setenv(shutdownTimeout, "")
	a, _, stderr, built := newApp(t)
	ran := probe(a)

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if !*ran {
		t.Error("probe did not run")
	}
	if want := []string{"lifecycle config"}; !slices.Equal(built.Log(), want) {
		t.Errorf("nodes built = %q, want %q", built.Log(), want)
	}
}

func TestRun_InvalidShutdownTimeoutFailsTheCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, _, stderr, _ := newApp(t)
	ran := probe(a)

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *ran {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "docproc probe: lifecycle config: DOCPROC_SHUTDOWN_TIMEOUT"; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

func TestRun_HaltStopsTheCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, stdout, stderr, built := newApp(t)
	ran := probe(a)
	apptest.Halt(a)

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *ran {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "docproc probe: lifecycle config: " + apptest.ErrHalted.Error(); !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if want := []string{"lifecycle config"}; !slices.Equal(built.Log(), want) {
		t.Errorf("nodes built = %q, want %q", built.Log(), want)
	}
}

func TestRun_HaltLeavesVersion(t *testing.T) {
	a, stdout, stderr, built := newApp(t)
	apptest.Halt(a)

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
