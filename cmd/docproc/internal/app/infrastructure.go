package app

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/stages"
)

// envPrefix is the prefix of every environment variable docproc reads its configuration from:
// go-core's config convention composes the rest, such as DOCPROC_SHUTDOWN_TIMEOUT.
const envPrefix = "DOCPROC"

// defineInfrastructure defines the infrastructure nodes on g into n: the lifecycle configuration
// and the run's bounds. It constructs nothing. Every value is read or constructed in its node's
// constructor, which runs only when a Build reaches the node, so configuration is read only for a
// command that declares a node, never in [New] and never for a run that builds nothing.
func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.Bounds = g.Define("bounds", newBounds)
}

// newLifecycleConfig reads the lifecycle configuration, the shutdown budget, from its default and
// DOCPROC_SHUTDOWN_TIMEOUT, and validates it. The dispatcher adds this node to every Build, so a
// command that declares any node reads it.
func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

// Bounds are a run's bounds, in the shapes the library takes them, so a run hands them on as
// they are: Stages to stages.Build, and Limits and Release to the dispatch.Config, with the
// stages.Set's Inspect, these options validated, as its Inspect. The library sets no bound of
// its own; each here starts at docproc's starting value, which its variable overrides.
type Bounds struct {
	// Stages carry the run bounds, the inspection options, and the PDF, video, and ImageMagick
	// bounds; every other option is the zero value, so every route is wired and every other
	// request is at its own defaults.
	Stages stages.Options
	// Limits are each dispatch's caps.
	Limits dispatch.Limits
	// Release bounds a claim's release once a dispatch's context has ended:
	// dispatch.Config.Release.
	Release time.Duration
}

// gib is a gibibyte, in bytes.
const gib = 1 << 30

// bombRatioVar and bombSizeVar are the bomb rule's bounds, which name their variables both in
// their own reads and in the rule's, which takes the two together.
const (
	bombRatioVar = "bomb ratio"
	bombSizeVar  = "bomb size"
)

// newBounds reads the run's bounds from their starting values and their DOCPROC_ variables. A
// value it can't take fails the node, naming its variable, every such variable at once, before
// any tool is looked up, since nothing else in the node does any I/O.
//
// Each bound's variable is named for what it bounds, under envPrefix. A duration is read as a
// Go duration, such as 90s or 10m; a size as a count of bytes; a count, a ratio, or a pixel
// count as a decimal number; and a switch as strconv.ParseBool reads it. An unset or empty
// variable leaves its bound at its starting value. No value may be negative, and zero is what
// the library makes of a zero: no bound, unless the variable says otherwise.
//
//   - DOCPROC_TOOL_MEMORY, 2 GiB: every tool's memory bound in bytes, Stages.Memory, the exec
//     runner's RLIMIT_AS, which needs prlimit on PATH when it isn't zero. docproc-ooxml fails
//     now and then below 512 MiB, and docproc-onenote reserves a 256 MiB stack against it.
//   - DOCPROC_UNIT_TIMEOUT, 10m: a unit run's bound, Stages.Timeout, on gs, qpdf, pdfdetach,
//     magick, soffice, docproc-ooxml, and ffprobe.
//   - DOCPROC_STREAM_IDLE, 2m: a stream run's bound, Stages.Idle, ending ffmpeg, docproc-mail,
//     docproc-onenote, or 7zz once it writes nothing for this long.
//   - DOCPROC_MAX_PARTS, 100000: the most parts one dispatch admits, the input among them,
//     Limits.Parts.
//   - DOCPROC_MAX_BYTES, 32 GiB: the most bytes one dispatch's parts may come to, Limits.Bytes.
//   - DOCPROC_MAX_DEPTH, 16: the deepest part one dispatch admits, the input at depth 0,
//     Limits.Depth.
//   - DOCPROC_BOMB_RATIO, 1000: the bomb rule's ratio, expanded to compressed size,
//     Stages.Inspect.BombRatio. It is zero exactly when DOCPROC_BOMB_SIZE is, which applies no
//     bomb rule.
//   - DOCPROC_BOMB_SIZE, 1 GiB: the bomb rule's expanded size in bytes, Stages.Inspect.BombSize.
//   - DOCPROC_ARCHIVE_ENTRIES, 100000: the most entries a ZIP or a tar may hold,
//     Stages.Inspect.Entries.
//   - DOCPROC_CLAIM_RELEASE, 10s: the bound on a claim's release once a dispatch's context has
//     ended, Release.
//   - DOCPROC_CONCURRENCY, 1: how many of a PDF's pages and an audio part's windows are held at
//     once, Stages.Concurrency; zero is 1.
//   - DOCPROC_PDF_DPI, 150: the DPI a PDF's pages are rendered at, Stages.Document.DPI; zero is
//     document.DefaultDPI.
//   - DOCPROC_PDF_TEXT, true: whether a PDF page's text layer is read, Stages.Document.Text.
//   - DOCPROC_PDF_MAX_PIXELS, 64000000: the most pixels a page's PNG may hold,
//     Stages.Document.MaxPixels; a page that would hold more is rendered at a lower DPI.
//   - DOCPROC_FRAME_MAX_SIDE, 2048: the largest side a video frame is written at, in pixels,
//     Stages.Video.MaxSide.
//   - DOCPROC_MAGICK_AREA, 256000000: the most pixels one image may hold in ImageMagick's pixel
//     cache, Stages.Magick.Area, -limit area.
//   - DOCPROC_MAGICK_MEMORY, 1 GiB: the most bytes of heap the pixel cache may take,
//     Stages.Magick.Memory, -limit memory.
//   - DOCPROC_MAGICK_MAP, 2 GiB: the most bytes of the pixel cache that may be memory-mapped,
//     Stages.Magick.Map, -limit map.
//   - DOCPROC_MAGICK_DISK, 8 GiB: the most bytes of the pixel cache that may be on disk,
//     Stages.Magick.Disk, -limit disk.
func newBounds(*graph.Scope) (Bounds, error) {
	b := Bounds{
		Stages: stages.Options{
			Memory:  2 * gib,
			Timeout: 10 * time.Minute,
			Idle:    2 * time.Minute,
			Inspect: inspect.Options{BombRatio: 1000, BombSize: 1 * gib, Entries: 100_000},

			Concurrency: 1,
			Document:    document.ImagesRequest{DPI: 150, Text: true, MaxPixels: 64_000_000},
			Video:       media.Request{MaxSide: 2048},
			Magick:      picture.Limits{Area: 256_000_000, Memory: 1 * gib, Map: 2 * gib, Disk: 8 * gib},
		},
		Limits:  dispatch.Limits{Parts: 100_000, Bytes: 32 * gib, Depth: 16},
		Release: 10 * time.Second,
	}

	errs := []error{
		readEnv(&b.Stages.Memory, "tool memory", parseCount[int64]),
		readEnv(&b.Stages.Timeout, "unit timeout", parseDuration),
		readEnv(&b.Stages.Idle, "stream idle", parseDuration),
		readEnv(&b.Limits.Parts, "max parts", parseCount[int]),
		readEnv(&b.Limits.Bytes, "max bytes", parseCount[int64]),
		readEnv(&b.Limits.Depth, "max depth", parseCount[int]),
		readEnv(&b.Stages.Inspect.BombRatio, bombRatioVar, parseRatio),
		readEnv(&b.Stages.Inspect.BombSize, bombSizeVar, parseCount[uint64]),
		readEnv(&b.Stages.Inspect.Entries, "archive entries", parseCount[int]),
		readEnv(&b.Release, "claim release", parseDuration),
		readEnv(&b.Stages.Concurrency, "concurrency", parseCount[int]),
		readEnv(&b.Stages.Document.DPI, "pdf dpi", parseCount[int]),
		readEnv(&b.Stages.Document.Text, "pdf text", strconv.ParseBool),
		readEnv(&b.Stages.Document.MaxPixels, "pdf max pixels", parseCount[int64]),
		readEnv(&b.Stages.Video.MaxSide, "frame max side", parseCount[int]),
		readEnv(&b.Stages.Magick.Area, "magick area", parseCount[int64]),
		readEnv(&b.Stages.Magick.Memory, "magick memory", parseCount[int64]),
		readEnv(&b.Stages.Magick.Map, "magick map", parseCount[int64]),
		readEnv(&b.Stages.Magick.Disk, "magick disk", parseCount[int64]),
	}
	// The bomb rule is set by both its values or by neither, as inspect.Options.Validate has it;
	// a rule half off names both variables, since either may be the one meant.
	if (b.Stages.Inspect.BombRatio == 0) != (b.Stages.Inspect.BombSize == 0) {
		errs = append(errs, fmt.Errorf("%s and %s: set both above zero, or both to zero for no bomb rule",
			config.EnvName(envPrefix, bombRatioVar), config.EnvName(envPrefix, bombSizeVar)))
	}
	return b, errors.Join(errs...)
}

// readEnv sets *dest from the variable bound names under envPrefix, read by parse, and leaves it
// when the variable is unset or empty. Its error names the variable.
func readEnv[T any](dest *T, bound string, parse func(string) (T, error)) error {
	name := config.EnvName(envPrefix, bound)
	s := os.Getenv(name)
	if s == "" {
		return nil
	}
	v, err := parse(s)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*dest = v
	return nil
}

// parseCount reads a decimal count, a size in bytes among them, refusing a negative one.
func parseCount[T int | int64 | uint64](s string) (T, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return 0, fmt.Errorf("%s is out of range", s)
	case err != nil:
		return 0, fmt.Errorf("%q is not a whole number", s)
	case n < 0:
		return 0, fmt.Errorf("%d is below zero", n)
	}
	return T(n), nil
}

// parseDuration reads a Go duration, refusing a negative one.
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	switch {
	case err != nil:
		return 0, fmt.Errorf("%q is not a duration, such as 90s or 10m", s)
	case d < 0:
		return 0, fmt.Errorf("%s is below zero", d)
	}
	return d, nil
}

// parseRatio reads a finite ratio, refusing a negative one.
func parseRatio(s string) (float64, error) {
	r, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil, math.IsNaN(r), math.IsInf(r, 0):
		return 0, fmt.Errorf("%q is not a finite number", s)
	case r < 0:
		return 0, fmt.Errorf("%g is below zero", r)
	}
	return r, nil
}
