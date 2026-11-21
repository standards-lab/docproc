package app

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/stages"
)

const envPrefix = "DOCPROC"

func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.Bounds = g.Define("bounds", newBounds)
}

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
	// Stages carry the run bounds, the inspection options, and the PDF, audio, video, and
	// ImageMagick bounds; every other option is the zero value, so every route is wired and every
	// other request is at its own defaults.
	Stages stages.Options
	// Limits are each dispatch's caps.
	Limits dispatch.Limits
	// Release bounds a claim's release once a dispatch's context has ended:
	// dispatch.Config.Release.
	Release time.Duration
}

const gib = 1 << 30

const (
	bombRatioVar = "bomb ratio"
	bombSizeVar  = "bomb size"
)

// See docs/command.md, "Configuration".
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
			Audio:       audio.Request{Limit: 30 * time.Second},
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
		readEnv(&b.Stages.Audio.Limit, "audio limit", parseAudioLimit),
	}
	if (b.Stages.Inspect.BombRatio == 0) != (b.Stages.Inspect.BombSize == 0) {
		errs = append(errs, fmt.Errorf("%s and %s: set both above zero, or both to zero for no bomb rule",
			config.EnvName(envPrefix, bombRatioVar), config.EnvName(envPrefix, bombSizeVar)))
	}
	return b, errors.Join(errs...)
}

func readEnv[T any](dest *T, bound string, parse func(string) (T, error)) error {
	var v *T
	err := config.SetFromEnv(&v, config.EnvName(envPrefix, bound), parse)
	if v != nil {
		*dest = *v
	}
	return err
}

func parseCount[T int | int64 | uint64](s string) (T, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	switch {
	case errors.Is(err, strconv.ErrRange), err == nil && int64(T(n)) != n:
		return 0, fmt.Errorf("%s is out of range", s)
	case err != nil:
		return 0, fmt.Errorf("%q is not a whole number", s)
	case n < 0:
		return 0, fmt.Errorf("%d is below zero", n)
	}
	return T(n), nil
}

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

func parseAudioLimit(s string) (time.Duration, error) {
	d, err := parseDuration(s)
	if err != nil {
		return 0, err
	}
	if _, err := audio.NewPlanner(audio.Request{Limit: d}); d == 0 || err != nil {
		return 0, fmt.Errorf("%v isn't over %v, twice the %v overlap between windows",
			d, 2*audio.DefaultOverlap, audio.DefaultOverlap)
	}
	return d, nil
}

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
