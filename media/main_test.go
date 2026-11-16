//go:build linux

package media_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/internal/mediatest"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/tool"
)

func TestMain(m *testing.M) {
	tooltest.Main(m, map[string]tooltest.Program{"ffmpeg": standin, "ffprobe": standin, "docker": dockerStandin})
}

// settings are the stand-ins' settings, which ffmpeg's, ffprobe's, and docker's links share.
type settings struct {
	// Version is the version ffmpeg's and ffprobe's -version print, after an n.
	Version string `json:"version"`
	// Probe is the report the stand-in ffprobe writes to stdout.
	Probe string `json:"probe,omitzero"`
	// PCM names a canonical 44-byte WAV whose samples the stand-in ffmpeg writes to its stdout,
	// when its arguments have an audio output.
	PCM string `json:"pcm,omitzero"`
	// Frames are the frames the stand-in ffmpeg takes, when its arguments have a video output.
	Frames []mediatest.Frame `json:"frames,omitzero"`
	// Gates, when set, are where the stand-in ffmpeg waits in its PCM.
	Gates []standinGate `json:"gates,omitzero"`
	// NoPNG makes the stand-in report its frames and write no PNG for them.
	NoPNG bool `json:"no_png,omitzero"`
	// Fail, when set, makes the stand-in ffmpeg, once it has written its output, write it and a
	// newline to stderr and exit 1.
	Fail string `json:"fail,omitzero"`
	// Hold is how many milliseconds the stand-in ffmpeg sleeps once it has written its output.
	Hold int `json:"hold,omitzero"`
	// Missing, when a or V, makes the stand-in ffmpeg refuse its map of that type, as ffmpeg does
	// a source without such a stream.
	Missing string `json:"missing,omitzero"`
	// DockerServer is the version the stand-in docker's daemon reports.
	DockerServer string `json:"docker_server,omitzero"`
}

// run is one stand-in run, as the links' logs record it.
type run = tooltest.Logged

// standinGate is a point in the stand-in ffmpeg's PCM it waits at: once it has written At bytes,
// it writes no more until a file exists at Path, and fails if none does within gateWait.
type standinGate struct {
	At   int    `json:"at"`
	Path string `json:"path"`
}

// gateWait is how long the stand-in ffmpeg waits at a gate before it fails: long enough for any
// window the PCM written has fixed to reach the test, under the race detector.
const gateWait = 20 * time.Second

// standin is the stand-in run as ffmpeg or ffprobe.
func standin(r tooltest.Run) int {
	var s settings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 1 && args[0] == "-version" {
		fmt.Printf("%s version n%s Copyright (c) 2000-2026 the FFmpeg developers\n", r.Program, s.Version)
		return 0
	}
	if r.Program == "ffprobe" {
		fmt.Print(s.Probe)
		return 0
	}
	if slices.Contains(args, "pipe:0") {
		if err := logStdin(r.Link); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if s.Missing != "" {
		fmt.Fprintln(os.Stderr, "Stream map '' matches no streams.")
		fmt.Fprintln(os.Stderr, "To ignore this, add a trailing '?' to the map.")
		fmt.Fprintf(os.Stderr, "Failed to set value '0:%s:0' for option 'map': Invalid argument\n", s.Missing)
		fmt.Fprintln(os.Stderr, "Error parsing options for output file pipe:1.")
		return 1
	}
	if err := standinRun(s, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	time.Sleep(time.Duration(s.Hold) * time.Millisecond)
	if s.Fail != "" {
		fmt.Fprintln(os.Stderr, s.Fail)
		return 1
	}
	return 0
}

// stdinSuffix ends the name of the file beside the ffmpeg link that holds the SHA-256 of what
// each of its runs whose input is pipe:0 read from its stdin, a line a run.
const stdinSuffix = ".stdin"

// logStdin reads the run's stdin whole and appends its SHA-256 to the file beside link.
func logStdin(link string) error {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(in)
	f, err := os.OpenFile(link+stdinSuffix, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(hex.EncodeToString(sum[:]) + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// standinRun is the stand-in ffmpeg's run: the PCM of s.PCM's WAV to stdout, when its arguments
// have an audio output, half a second at a time, waiting at s.Gates; and s.Frames, when they have
// a video output, its last argument the image2 pattern, each reported and written before the PCM
// from its time on.
func standinRun(s settings, args []string) error {
	var pcm []byte
	gates := s.Gates
	if slices.Contains(args, "pipe:1") {
		data, err := os.ReadFile(s.PCM)
		if err != nil {
			return err
		}
		pcm = data[44:]
	} else {
		gates = nil
	}
	var frames []mediatest.Frame
	pattern := ""
	if slices.Contains(args, "image2") {
		frames = s.Frames
		pattern = args[len(args)-1]
	}
	return mediatest.Stream(pcm, frames,
		func(at, end int) error {
			var err error
			gates, err = writeGated(pcm, at, end, gates)
			return err
		},
		func(i int) error {
			mediatest.Report(i, frames[i])
			if s.NoPNG {
				return nil
			}
			return mediatest.WritePNG(pattern, i, frames[i])
		})
}

// writeGated writes pcm's bytes from at to end to stdout, waiting at each of the gates, in order,
// whose byte is in that range, and returns the gates left.
func writeGated(pcm []byte, at, end int, gates []standinGate) ([]standinGate, error) {
	for len(gates) > 0 && gates[0].At <= end {
		g := gates[0]
		if _, err := os.Stdout.Write(pcm[at:g.At]); err != nil {
			return nil, err
		}
		at = g.At
		for deadline := time.Now().Add(gateWait); ; time.Sleep(5 * time.Millisecond) {
			if _, err := os.Stat(g.Path); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("the gate at byte %d, %s, was never opened", g.At, g.Path)
			}
		}
		gates = gates[1:]
	}
	_, err := os.Stdout.Write(pcm[at:end])
	return gates, err
}

// dockerStandin is the stand-in docker CLI: version prints s.DockerServer; image inspect finds
// every image present; stop and rm do nothing; and run replaces itself with the stand-in its
// --entrypoint names, its sibling link, on the arguments after the image, so the run is that
// stand-in's, in the CLI's process, with its streams.
func dockerStandin(r tooltest.Run) int {
	var s settings
	if err := r.Settings(&s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	args := r.Args
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(s.DockerServer)
		return 0
	case "image":
		fmt.Println("sha256:" + strings.Repeat("0", 64))
		return 0
	case "stop", "rm":
		return 0
	case "run":
	default:
		fmt.Fprintf(os.Stderr, "docker: unknown command %q\n", args[0])
		return 2
	}
	entrypoint, i := "", 1
	for ; i < len(args) && strings.HasPrefix(args[i], "--"); i++ {
		if v, ok := strings.CutPrefix(args[i], "--entrypoint="); ok {
			entrypoint = v
		}
	}
	if i == len(args) {
		fmt.Fprintln(os.Stderr, "docker: run: no image")
		return 125
	}
	link := filepath.Join(filepath.Dir(r.Link), entrypoint)
	err := syscall.Exec(link, append([]string{link}, args[i+1:]...), os.Environ())
	fmt.Fprintln(os.Stderr, err)
	return 125
}

// standins are a test's stand-in ffmpeg, ffprobe, and docker: three links in a directory of their
// own, sharing one settings.
type standins struct {
	t        testing.TB
	settings settings
	// dir is the links' directory.
	dir string
	// since is how many runs of each link a mark left out of runs.
	since map[string]int
}

// links names the stand-ins' links, in the order runs lists their runs.
var links = []string{"ffprobe", "docker", "ffmpeg"}

// installStandins links the stand-ins as ffmpeg, ffprobe, and docker in a directory of their own,
// ffmpeg's and ffprobe's version version and the stand-in docker daemon's 29.0.1.
func installStandins(t testing.TB, version string) *standins {
	t.Helper()
	s := &standins{t: t, settings: settings{Version: version, DockerServer: "29.0.1"}, dir: t.TempDir(), since: map[string]int{}}
	for _, name := range links {
		tooltest.Link(t, s.dir, name, s.settings)
	}
	return s
}

// link returns the path of the stand-in named name.
func (s *standins) link(name string) string { return filepath.Join(s.dir, name) }

// set changes the stand-ins' settings through change, for every run after it.
func (s *standins) set(change func(*settings)) {
	s.t.Helper()
	change(&s.settings)
	for _, name := range links {
		tooltest.Set(s.t, s.link(name), s.settings)
	}
}

// mark leaves the runs so far out of runs.
func (s *standins) mark() {
	s.t.Helper()
	for _, name := range links {
		s.since[name] = len(tooltest.Runs(s.t, s.link(name)))
	}
}

// runs returns the stand-ins' runs since the last mark: ffprobe's, then docker's runs of a
// container, then ffmpeg's, its runs inside docker's included, each in the order they began, and
// no version command. A probe precedes ffmpeg's run, and docker's run of ffmpeg becomes it, so
// that is the order they ran in; the logs, one a link, don't order runs across links.
func (s *standins) runs() []run {
	s.t.Helper()
	var out []run
	for _, name := range links {
		for _, r := range tooltest.Runs(s.t, s.link(name))[s.since[name]:] {
			switch {
			case slices.Equal(r.Args, []string{"-version"}):
			case name == "docker" && (len(r.Args) == 0 || r.Args[0] != "run" || r.Args[len(r.Args)-1] == "-version"):
			default:
				out = append(out, r)
			}
		}
	}
	return out
}

// stdins returns the SHA-256 of what each ffmpeg run whose input was pipe:0 read from its stdin,
// in order.
func (s *standins) stdins() []string {
	s.t.Helper()
	data, err := os.ReadFile(s.link("ffmpeg") + stdinSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.Fields(string(data))
}

// config builds the stand-ins through the exec runner, each Spec's minimum lowered to 1 so any
// version passes the runner, leaving the Streamer's own minimum to decide.
func (s *standins) config() media.Config {
	s.t.Helper()
	build := func(spec tool.Spec) *tool.Tool {
		spec.Program = s.link(spec.Program)
		spec.Minimum = "1"
		spec.KillAfter = time.Second
		tl, err := tool.NewExec(s.t.Context(), spec)
		if err != nil {
			s.t.Fatal(err)
		}
		return tl
	}
	return media.Config{FFmpeg: build(audio.FFmpegSpec()), FFprobe: build(audio.FFprobeSpec())}
}

// containerStreamer builds a Streamer over the stand-ins through the container runner, the
// stand-in docker running them, its ffmpeg's image joining network, none when empty.
func (s *standins) containerStreamer(network string) *media.Streamer {
	s.t.Helper()
	build := func(spec tool.Spec) *tool.Tool {
		spec.Minimum = "1"
		spec.KillAfter = time.Second
		tl, err := tool.NewContainer(s.t.Context(), s.link("docker"), tool.Image{Ref: audio.FFmpegImage, Network: network}, spec)
		if err != nil {
			s.t.Fatal(err)
		}
		return tl
	}
	st, err := media.NewStreamer(media.Config{FFmpeg: build(audio.FFmpegSpec()), FFprobe: build(audio.FFprobeSpec())})
	if err != nil {
		s.t.Fatal(err)
	}
	return st
}

// standinStreamer installs the stand-ins at 9.0.2 and builds a Streamer over them.
func standinStreamer(t testing.TB) (*media.Streamer, *standins) {
	t.Helper()
	st := installStandins(t, "9.0.2")
	s, err := media.NewStreamer(st.config())
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}
