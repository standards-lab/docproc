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

const ffmpegImage = "linuxserver/ffmpeg:9.0.2-cli-ls89@sha256:" +
	"822d04f0b2d0c4847430f5cc90f20ea48be866bb502ece63fdeb729400bf5361"

func ffprobeSpec() tool.Spec {
	return tool.Spec{
		Program:        "ffprobe",
		VersionArgs:    []string{"-version"},
		VersionPattern: regexp.MustCompile(`ffprobe version n?(\d+(?:\.\d+)*)`),
		Minimum:        "9.0",
	}
}

type ffprobeTool struct {
	*tool.Tool
	running  func(t *testing.T)
	leftover func(t *testing.T)
}

var ffprobeRunners = map[string]func(t *testing.T) ffprobeTool{
	"exec": func(t *testing.T) ffprobeTool {
		tl, err := tool.NewExec(t.Context(), ffprobeSpec())
		if err != nil {
			t.Fatal(err)
		}
		return ffprobeTool{Tool: tl, running: func(*testing.T) {}, leftover: func(*testing.T) {}}
	},
	"container": func(t *testing.T) ffprobeTool {
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

func probeArgs(input string) []string {
	return []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams", input}
}

type probeStream struct {
	CodecName  string `json:"codec_name"`
	SampleRate string `json:"sample_rate"`
}

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
