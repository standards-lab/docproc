package stages

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/media"
	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/onenote"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/sevenzip"
	"github.com/standards-lab/docproc/tool"
)

// Lookup finds a program on the host, as exec.LookPath does: the path to run it by, or an error
// when the host has none. Build finds every tool through it, the docker CLI included, so a
// caller decides what the host holds.
type Lookup func(program string) (string, error)

// Options are what Build wires the stages under: the routes left out, the inspection options, the
// run bounds, and each stage's request. The zero value wires every route with no bound, each
// request at its own defaults. The library sets no bound of its own: each is none unless the host
// sets it.
type Options struct {
	// Exclude are the routes the host leaves out: each has no processor yet.
	Exclude []route.Route
	// Inspect are the inspection options, validated once: the archive and document stages' bomb
	// rule and the archive stage's Entries, in their identities, and Set.Inspect, for
	// dispatch.Config.Inspect, so one value exists. The bomb rule, none when BombRatio and
	// BombSize are zero, is the one an archive entry's, a gzip, bzip2, zstd, or xz stream's, and a
	// PDF's embedded file's written bytes may not pass; Entries bounds a 7-Zip listing; and Policy
	// is the one a 7-Zip listing's flags are weighed under, as inspect.CheckListing weighs them.
	Inspect inspect.Options
	// Timeout is a unit run's bound, on every run of a tool whose runs are units: gs, qpdf,
	// pdfdetach, pdftotext, pdfinfo, magick, soffice, docproc-ooxml, and ffprobe. Zero is none.
	Timeout time.Duration
	// Idle is a stream run's bound, on every run of a tool whose runs stream: ffmpeg,
	// docproc-mail, docproc-onenote, and 7zz, each ended once it writes nothing for this long.
	// Zero is none.
	Idle time.Duration
	// Memory is every tool's memory bound in bytes, tool.Spec.MemoryLimit: the exec runner's
	// RLIMIT_AS through prlimit, which must then be on PATH, and the container runner's --memory.
	// Zero is none.
	Memory int64
	// Env is added to every exec-run tool's fixed environment, tool.Spec.Env.
	Env []string
	// Concurrency is how many of a PDF's pages and an audio part's windows are held at once,
	// overriding the Document and Audio requests' own. Zero is 1, one at a time.
	Concurrency int
	// Document is the request every PDF's pages are prepared under, its DPI, Text, MaxPixels,
	// and PagesPerProcess; its Dir is each part's own.
	Document document.ImagesRequest
	// Picture is the request every converted picture is converted under, its MetafileScale and
	// Density; its Dir, Path, Kind, and Stem are each part's own.
	Picture picture.Request
	// Magick are ImageMagick's -limit values, which every magick run carries, the picture and
	// office stages' alike. The zero value passes none.
	Magick picture.Limits
	// Audio is the request every audio part's windows are cut under; its Dir is each part's own.
	Audio audio.Request
	// Video is the request every video part's windows and frames are taken under; its Dir is each
	// part's own.
	Video media.Request
}

func (o Options) check() error {
	refuse := func(format string, args ...any) error {
		return fmt.Errorf("stages: "+format+": %w", append(args, ErrRequest)...)
	}
	if err := o.Inspect.Validate(); err != nil {
		return refuse("%w", err)
	}
	switch {
	case o.Timeout < 0, o.Idle < 0:
		return refuse("a time bound is below zero")
	case o.Memory < 0:
		return refuse("memory bound %d is below zero", o.Memory)
	case o.Concurrency < 0:
		return refuse("concurrency %d is below zero", o.Concurrency)
	case o.Document.DPI < 0, o.Document.MaxPixels < 0, o.Document.PagesPerProcess < 0:
		return refuse("the document request's DPI, MaxPixels, or PagesPerProcess is below zero")
	case o.Picture.MetafileScale < 0, o.Picture.MetafileScale > picture.MaxMetafileScale, o.Picture.Density < 0:
		return refuse("the picture request's MetafileScale or Density is out of range")
	case min(o.Magick.Area, o.Magick.Width, o.Magick.Height, o.Magick.Memory, o.Magick.Map, o.Magick.Disk) < 0:
		return refuse("a magick limit is below zero")
	}
	if _, err := audio.NewCutter(o.Audio); err != nil {
		return refuse("%w", err)
	}
	if _, err := o.Video.Plan(); err != nil {
		return refuse("%w", err)
	}
	return nil
}

func (o Options) concurrency() int { return max(o.Concurrency, 1) }

// Set is what Build made of the host: the stages to register, every tool it tried, in a fixed
// order, one row per route, in [Routes] order, and the inspection options it validated, for
// dispatch.Config.Inspect.
type Set struct {
	Stages  dispatch.Stages
	Tools   []dispatch.Tool
	Rows    []dispatch.Row
	Inspect inspect.Options
}

type spec struct {
	tool.Spec
	image  string
	stream bool
}

func specs() []spec {
	return []spec{
		{Spec: document.GhostscriptSpec()},
		{Spec: document.QPDFSpec()},
		{Spec: document.PDFDetachSpec()},
		{Spec: document.PDFToTextSpec()},
		{Spec: document.PDFInfoSpec()},
		{Spec: picture.MagickSpec()},
		{Spec: picture.LibreOfficeSpec(), image: picture.LibreOfficeImage},
		{Spec: office.OfficeSpec()},
		{Spec: email.MailSpec(), stream: true},
		{Spec: onenote.OneNoteSpec(), stream: true},
		{Spec: sevenzip.SevenZipSpec(), stream: true},
		{Spec: audio.FFmpegSpec(), stream: true},
		{Spec: audio.FFprobeSpec()},
	}
}

type tools map[string]*tool.Tool

type stage struct {
	route route.Route
	needs []string
	build func(ctx context.Context, t tools, opts Options) (dispatch.Stage, error)
}

var table = []stage{
	{route.Document, []string{"gs", "qpdf", "pdfdetach", "pdftotext", "pdfinfo"}, buildDocument},
	{route.Office, []string{"docproc-ooxml", "soffice", "magick"}, buildOffice},
	{route.VisionConverted, []string{"magick", "soffice"}, buildPicture},
	{route.Vision, nil, func(context.Context, tools, Options) (dispatch.Stage, error) { return newVisionStage(), nil }},
	{route.Nested, []string{"docproc-mail"}, buildEmail},
	{route.OneNote, []string{"docproc-onenote"}, buildOneNote},
	{route.Archive, []string{"7zz"}, buildArchive},
	{route.Audio, []string{"ffmpeg", "ffprobe"}, buildAudio},
	{route.Video, []string{"ffmpeg", "ffprobe"}, buildVideo},
	{route.Text, nil, func(context.Context, tools, Options) (dispatch.Stage, error) { return newTextStage(), nil }},
}

// Routes returns the ten routes docproc wires a stage for, in the stage table's order.
func Routes() []route.Route {
	routes := make([]route.Route, len(table))
	for i, s := range table {
		routes[i] = s.route
	}
	return routes
}

// ErrRequest is the cause of a refused request: options Build can't take, or a route ParseRoutes
// doesn't know.
var ErrRequest = errors.New("invalid request")

// ParseRoutes returns the routes names name, in order, refusing a name that isn't one of
// [Routes] with an error wrapping ErrRequest.
func ParseRoutes(names []string) ([]route.Route, error) {
	known := Routes()
	routes := make([]route.Route, 0, len(names))
	for _, name := range names {
		r := route.Route(name)
		if !slices.Contains(known, r) {
			return nil, fmt.Errorf("stages: unknown route %q; the routes are %s: %w", name, joinRoutes(known), ErrRequest)
		}
		routes = append(routes, r)
	}
	return routes, nil
}

func joinRoutes(routes []route.Route) string {
	names := make([]string, len(routes))
	for i, r := range routes {
		names[i] = string(r)
	}
	return strings.Join(names, ", ")
}

// Build builds every tool a stage outside opts.Exclude requires, found through lookup and checked
// against its package's minimum, each under opts' run bounds, then each route's stage over them,
// under ctx. It refuses only options it can't take, an error wrapping ErrRequest, before anything
// is looked up. A tool it can't build leaves out every stage that requires it, and a stage whose
// constructor refuses its tools is left out too, each row saying why, so its route has no
// processor yet; a host that requires a route reads Set.Rows. A tool only excluded stages require
// is never looked up.
func Build(ctx context.Context, lookup Lookup, opts Options) (Set, error) {
	if err := opts.check(); err != nil {
		return Set{}, err
	}
	needed := map[string]bool{}
	for _, s := range table {
		if !slices.Contains(opts.Exclude, s.route) {
			for _, program := range s.needs {
				needed[program] = true
			}
		}
	}

	set := Set{Stages: dispatch.Stages{}, Inspect: opts.Inspect}
	built, failed := tools{}, map[string]error{}
	for _, sp := range specs() {
		if !needed[sp.Program] {
			continue
		}
		sp.MemoryLimit, sp.Env = opts.Memory, slices.Clone(opts.Env)
		if sp.stream {
			sp.Idle = opts.Idle
		} else {
			sp.Timeout = opts.Timeout
		}
		t, from, err := buildTool(ctx, lookup, sp)
		row := dispatch.Tool{Program: sp.Program, Minimum: sp.Minimum, From: from, Err: err}
		if err != nil {
			failed[sp.Program] = err
			row.Error = err.Error()
		} else {
			built[sp.Program] = t
			row.Version = t.VersionLine()
		}
		set.Tools = append(set.Tools, row)
	}

	for _, s := range table {
		row := dispatch.Row{Route: s.route}
		var reasons []string
		for _, program := range s.needs {
			if err := failed[program]; err != nil {
				reasons = append(reasons, err.Error())
			}
		}
		switch {
		case slices.Contains(opts.Exclude, s.route):
			row.State = dispatch.Excluded
		case len(reasons) > 0:
			row.State, row.Reason = dispatch.Missing, strings.Join(reasons, "; ")
		default:
			st, err := s.build(ctx, built, opts)
			if err != nil {
				row.State, row.Reason = dispatch.Missing, err.Error()
				break
			}
			row.State, row.Stage = dispatch.Wired, st.Identity()
			ts := make([]*tool.Tool, 0, len(s.needs))
			for _, program := range s.needs {
				ts = append(ts, built[program])
			}
			row.Tools = versions(ts...)
			set.Stages[s.route] = st
		}
		set.Rows = append(set.Rows, row)
	}
	return set, nil
}

func buildTool(ctx context.Context, lookup Lookup, sp spec) (*tool.Tool, string, error) {
	if sp.image != "" {
		docker, err := lookup("docker")
		if err != nil {
			return nil, "", fmt.Errorf("%s: docker not found", sp.Program)
		}
		t, err := tool.NewContainer(ctx, docker, tool.Image{Ref: sp.image}, sp.Spec)
		return t, sp.image, err
	}
	path, err := lookup(sp.Program)
	if err != nil {
		return nil, "", fmt.Errorf("%s: not found", sp.Program)
	}
	s := sp.Spec
	s.Program = path
	t, err := tool.NewExec(ctx, s)
	return t, path, err
}

func buildDocument(_ context.Context, t tools, opts Options) (dispatch.Stage, error) {
	gs, qt, dt, tt, it := t["gs"], t["qpdf"], t["pdfdetach"], t["pdftotext"], t["pdfinfo"]
	cfg := document.Config{Ghostscript: gs, QPDF: qt, PDFDetach: dt, PDFToText: tt, PDFInfo: it}
	render, err := document.NewGhostscript(cfg)
	if err != nil {
		return nil, err
	}
	cfg.Renderer = render
	if cfg.Text, err = document.NewTextReader(cfg); err != nil {
		return nil, err
	}
	q, err := document.NewQPDF(cfg)
	if err != nil {
		return nil, err
	}
	detach, err := document.NewDetacher(cfg)
	if err != nil {
		return nil, err
	}
	info, err := document.NewInfoReader(cfg)
	if err != nil {
		return nil, err
	}
	req := opts.Document
	req.Concurrency = opts.concurrency()
	return newDocumentStage(documentConfig{
		Preparer: document.NewPreparer(cfg), QPDF: q, PDFDetach: detach, PDFInfo: info,
		Version: versions(gs, qt, dt, tt, it), Request: req, Inspect: opts.Inspect, Memory: opts.Memory,
	}), nil
}

func pictures(ctx context.Context, t tools, opts Options) (*picture.Converter, error) {
	return picture.NewConverter(ctx, picture.Config{Magick: t["magick"], LibreOffice: t["soffice"], Limits: opts.Magick})
}

func buildOffice(ctx context.Context, t tools, opts Options) (dispatch.Stage, error) {
	of := t["docproc-ooxml"]
	p, err := pictures(ctx, t, opts)
	if err != nil {
		return nil, err
	}
	c, err := office.NewConverter(ctx, office.Config{Office: of, Pictures: p})
	if err != nil {
		return nil, err
	}
	return newOfficeStage(officeConfig{
		Converter: c, Version: versions(of, t["soffice"], t["magick"]),
		MetafileScale: opts.Picture.MetafileScale, Memory: opts.Memory,
	}), nil
}

func buildPicture(ctx context.Context, t tools, opts Options) (dispatch.Stage, error) {
	p, err := pictures(ctx, t, opts)
	if err != nil {
		return nil, err
	}
	return newPictureStage(pictureConfig{
		Converter: p, Version: versions(t["magick"], t["soffice"]), Request: opts.Picture, Memory: opts.Memory,
	}), nil
}

func buildEmail(_ context.Context, t tools, opts Options) (dispatch.Stage, error) {
	mail := t["docproc-mail"]
	r, err := email.NewReader(email.Config{Mail: mail})
	if err != nil {
		return nil, err
	}
	return newEmailStage(emailConfig{Reader: r, Version: versions(mail), Memory: opts.Memory}), nil
}

func buildOneNote(_ context.Context, t tools, opts Options) (dispatch.Stage, error) {
	on := t["docproc-onenote"]
	r, err := onenote.NewReader(onenote.Config{OneNote: on})
	if err != nil {
		return nil, err
	}
	return newOneNoteStage(onenoteConfig{Reader: r, Version: versions(on), Memory: opts.Memory}), nil
}

func buildArchive(_ context.Context, t tools, opts Options) (dispatch.Stage, error) {
	sz := t["7zz"]
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: sz})
	if err != nil {
		return nil, err
	}
	return newArchiveStage(archiveConfig{SevenZip: r, Version: versions(sz), Inspect: opts.Inspect, Memory: opts.Memory}), nil
}

func buildAudio(_ context.Context, t tools, opts Options) (dispatch.Stage, error) {
	ff, fp := t["ffmpeg"], t["ffprobe"]
	s, err := audio.NewSplitter(audio.Config{FFmpeg: ff, FFprobe: fp})
	if err != nil {
		return nil, err
	}
	req := opts.Audio
	req.Concurrency = opts.concurrency()
	return newAudioStage(audioConfig{Splitter: s, Version: versions(ff, fp), Request: req, Memory: opts.Memory}), nil
}

func buildVideo(_ context.Context, t tools, opts Options) (dispatch.Stage, error) {
	ff, fp := t["ffmpeg"], t["ffprobe"]
	s, err := media.NewStreamer(media.Config{FFmpeg: ff, FFprobe: fp})
	if err != nil {
		return nil, err
	}
	return newVideoStage(videoConfig{Streamer: s, Version: versions(ff, fp), Request: opts.Video, Memory: opts.Memory}), nil
}
