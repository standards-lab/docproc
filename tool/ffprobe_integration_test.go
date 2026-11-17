//go:build integration && linux

package tool_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/tool"
)

// ffmpegImage is linuxserver/ffmpeg's 9.0.2 CLI image, build ls89, pinned by its index digest.
const ffmpegImage = "linuxserver/ffmpeg:9.0.2-cli-ls89@sha256:" +
	"822d04f0b2d0c4847430f5cc90f20ea48be866bb502ece63fdeb729400bf5361"

// ffprobeSpec declares ffprobe, minimum 9.0. ffprobe prints "ffprobe version n9.0.2 ..." on the
// host and "ffprobe version 9.0.2 ..." in the image; the pattern skips the n.
func ffprobeSpec() tool.Spec {
	return tool.Spec{
		Program:        "ffprobe",
		VersionArgs:    []string{"-version"},
		VersionPattern: regexp.MustCompile(`ffprobe version n?(\d+(?:\.\d+)*)`),
		Minimum:        "9.0",
	}
}

// ffprobeTool is ffprobe built through one runner, with the runner's checks on a run: running
// blocks until a run started after the build has its program in place, and leftover checks that
// the runs left nothing behind.
type ffprobeTool struct {
	*tool.Tool
	running  func(t *testing.T)
	leftover func(t *testing.T)
}

// ffprobeRunners are the runners ffprobe runs through: the host's ffprobe through the exec
// runner, and ffprobe in the pinned image through the container runner, which pulls the image
// first when it isn't present. The runner is the only difference between the runs the tests
// below compare.
var ffprobeRunners = map[string]func(t *testing.T) ffprobeTool{
	"exec": func(t *testing.T) ffprobeTool {
		tl, err := tool.NewExec(t.Context(), ffprobeSpec())
		if err != nil {
			t.Fatal(err)
		}
		return ffprobeTool{Tool: tl, running: func(*testing.T) {}, leftover: func(*testing.T) {}}
	},
	"container": func(t *testing.T) ffprobeTool {
		// A run's container name is the runner's own, so the runner's docker CLI is a script that
		// logs each run's --name before it runs the host's docker; the checks are on the
		// containers so named alone, whatever other packages' tests run at the same time.
		docker, names := loggingDocker(t)
		tl, err := tool.NewContainer(t.Context(), docker, tool.Image{Ref: ffmpegImage}, ffprobeSpec())
		if err != nil {
			t.Fatal(err)
		}
		if got := tl.Name(); got != "ffprobe" {
			t.Errorf("Name() = %q, want ffprobe", got)
		}
		return ffprobeTool{
			Tool: tl,
			running: func(t *testing.T) {
				t.Helper()
				deadline := time.Now().Add(30 * time.Second)
				for len(containers(t, names(t), true)) == 0 {
					if time.Now().After(deadline) {
						t.Fatal("no container of the run's was running after 30 s")
					}
					time.Sleep(50 * time.Millisecond)
				}
			},
			leftover: func(t *testing.T) {
				t.Helper()
				run := names(t)
				if len(run) == 0 {
					t.Fatal("no docker run was logged")
				}
				if left := containers(t, run, false); len(left) != 0 {
					t.Errorf("containers %q outlived the run", left)
				}
			},
		}
	},
}

// loggingDocker returns a docker CLI that appends the --name of each docker run to a log, then
// runs the host's docker on its arguments, and a function returning the names logged so far.
func loggingDocker(t *testing.T) (string, func(t *testing.T) []string) {
	t.Helper()
	real, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "names")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = run ]; then\n" +
		"  for arg; do\n" +
		"    case $arg in --name=*) printf '%s\\n' \"${arg#--name=}\" >>'" + log + "' ;; esac\n" +
		"  done\n" +
		"fi\n" +
		"exec '" + real + "' \"$@\"\n"
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return docker, func(t *testing.T) []string {
		t.Helper()
		b, err := os.ReadFile(log)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		return strings.Fields(string(b))
	}
}

// containers returns the IDs of the containers named names: the running ones only, or with
// running false, every one.
func containers(t *testing.T, names []string, running bool) []string {
	t.Helper()
	var ids []string
	for _, name := range names {
		args := []string{"ps", "--quiet", "--no-trunc", "--filter=name=^" + name + "$"}
		if !running {
			args = append(args, "--all")
		}
		out, err := exec.CommandContext(t.Context(), "docker", args...).Output()
		if err != nil {
			t.Fatalf("docker ps: %v", err)
		}
		ids = append(ids, strings.Fields(string(out))...)
	}
	return ids
}

// probeArgs are ffprobe's arguments for a JSON report of input's format and streams.
func probeArgs(input string) []string {
	return []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams", input}
}

// probeStream is the part of a stream in ffprobe's JSON report that the tests compare.
type probeStream struct {
	CodecName  string `json:"codec_name"`
	SampleRate string `json:"sample_rate"`
}

// probe is the part of ffprobe's JSON report that the tests compare.
type probe struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
	Streams []probeStream `json:"streams"`
}

func (p probe) equal(q probe) bool {
	return p.Format == q.Format && slices.Equal(p.Streams, q.Streams)
}

// toneProbe is testdata/tone.wav's report: one second of 16 kHz 16-bit PCM in a WAV.
const toneProbe = `{"format":{"format_name":"wav","duration":"1.000000"},` +
	`"streams":[{"codec_name":"pcm_s16le","sample_rate":"16000"}]}`

func TestFFprobeVersion(t *testing.T) {
	for name, runner := range ffprobeRunners {
		t.Run(name, func(t *testing.T) {
			tl := runner(t)
			t.Logf("%s %s", tl.Name(), tl.Version())
		})
	}
}

// TestFFprobeProbesTone checks that ffprobe, given the same invocation through both runners,
// reports the same format and streams for the fixed tone, and that the report is the tone's.
func TestFFprobeProbesTone(t *testing.T) {
	var want probe
	if err := json.Unmarshal([]byte(toneProbe), &want); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]probe{}
	for name, runner := range ffprobeRunners {
		t.Run(name, func(t *testing.T) {
			tl := runner(t)
			var stdout bytes.Buffer
			start := time.Now()
			err := tl.Run(t.Context(), tool.Invocation{Args: probeArgs("tone.wav"), Dir: dir, Stdout: &stdout})
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			var p probe
			if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
				t.Fatalf("ffprobe's report %q: %v", stdout.String(), err)
			}
			if !p.equal(want) {
				t.Errorf("ffprobe reported %+v, want %+v", p, want)
			}
			got[name] = p
			t.Logf("%s %s in %v: %+v", tl.Name(), tl.Version(), elapsed, p)
			tl.leftover(t)
		})
	}
	if e, c := got["exec"], got["container"]; len(got) == 2 && !e.equal(c) {
		t.Errorf("the exec runner reported %+v, the container runner %+v", e, c)
	}
}

// TestFFprobeCancelHeldStdin checks that ffprobe, reading a stdin the test holds open, is
// cancelled mid-run through both runners: Run returns the context's error, and the run leaves no
// container behind. The test writes only the tone's 44-byte header, so ffprobe waits on stdin for
// the samples until the cancel. The cancel comes once the run is under way: the header's write
// has returned, so Run has started its program and is copying stdin, and, through the container
// runner, the run's container is running.
func TestFFprobeCancelHeldStdin(t *testing.T) {
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	tone, err := os.ReadFile(filepath.Join(dir, "tone.wav"))
	if err != nil {
		t.Fatal(err)
	}
	for name, runner := range ffprobeRunners {
		t.Run(name, func(t *testing.T) {
			tl := runner(t)
			stdin, held := io.Pipe()
			defer func() { _ = held.Close() }()
			wrote := make(chan error, 1)
			go func() {
				_, err := held.Write(tone[:44])
				wrote <- err
			}()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := runAsync(ctx, tl.Tool, tool.Invocation{Args: probeArgs("pipe:0"), Dir: dir, Stdin: stdin, Stdout: io.Discard})
			select {
			case err := <-wrote:
				if err != nil {
					t.Fatalf("writing the header: %v", err)
				}
			case err := <-done:
				t.Fatalf("Run returned %v before the cancel, want it waiting on stdin", err)
			case <-time.After(30 * time.Second):
				t.Fatal("Run hadn't read the header after 30 s")
			}
			tl.running(t)
			select {
			case err := <-done:
				t.Fatalf("Run returned %v before the cancel, want it waiting on stdin", err)
			default:
			}
			start := time.Now()
			cancel()
			elapsed, err := awaitRun(t, done, start, 15*time.Second)
			assertContextError(t, err, context.Canceled)
			if elapsed > 10*time.Second {
				t.Errorf("Run took %v after cancel, want well under 10 s", elapsed)
			}
			t.Logf("Run returned %v after cancel: %v", elapsed, err)
			tl.leftover(t)
		})
	}
}
