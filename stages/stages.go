package stages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/standards-lab/docproc/audio"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/document"
	"github.com/standards-lab/docproc/email"
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

// State is what became of a route's stage at Build.
type State string

const (
	// Wired is a stage Build built and registered.
	Wired State = "wired"
	// Missing is a stage left out because a tool it needs is missing or too old, or because its
	// own constructor refused the tools it was given; its route has no processor yet.
	Missing State = "missing"
	// Excluded is a stage the caller left out; its route has no processor yet.
	Excluded State = "excluded"
)

// Tool is one tool Build tried to build: its program, the minimum processing's Spec declares,
// where it was found, and its version, or the error that left it unbuilt.
type Tool struct {
	Program string
	Minimum string
	// From is the path the program was found at, or, for a container tool, its image.
	From    string
	Version string
	Err     error
}

// Row is one route's line of the stage table: the stage's identity when it is wired, or why it
// isn't.
type Row struct {
	Route route.Route
	State State
	// Stage is the wired stage's identity.
	Stage dispatch.Identity
	// Tools names the wired stage's tools and their versions, as dispatch.Versions does, empty for
	// a stage that runs none.
	Tools  string
	Reason string
}

// Set is what Build made of the host: the stages to register, every tool it tried, in a fixed
// order, and one row per route, in [Routes] order.
type Set struct {
	Stages dispatch.Stages
	Tools  []Tool
	Rows   []Row
}

// spec is one tool's declaration: processing's Spec for it, and, for soffice, the image the
// container runner runs it in through the docker CLI.
type spec struct {
	tool.Spec
	image string
}

// specs are the tools a stage may need, in the order the tools table lists them.
func specs() []spec {
	return []spec{
		{Spec: document.GhostscriptSpec()},
		{Spec: document.QPDFSpec()},
		{Spec: document.PDFDetachSpec()},
		{Spec: picture.MagickSpec()},
		{Spec: picture.LibreOfficeSpec(), image: picture.LibreOfficeImage},
		{Spec: office.OfficeSpec()},
		{Spec: email.MailSpec()},
		{Spec: onenote.OneNoteSpec()},
		{Spec: sevenzip.SevenZipSpec()},
		{Spec: audio.FFmpegSpec()},
		{Spec: audio.FFprobeSpec()},
	}
}

// tools are the Tools Build built, by program.
type tools map[string]*tool.Tool

// stage is one route's wiring: the programs its stage needs, every one of them, and how to build
// it over them.
type stage struct {
	route route.Route
	needs []string
	build func(ctx context.Context, t tools) (dispatch.Stage, error)
}

// table is the ten routes' wiring, in [Routes] order. Each stage needs every tool it names: a
// stage processing could build over fewer, such as the archive stage without 7-Zip or the
// document stage without qpdf, is still left out, so a stage either runs as the table names it
// or not at all.
var table = []stage{
	{route.Document, []string{"gs", "qpdf", "pdfdetach"}, documentStage},
	{route.Office, []string{"docproc-ooxml", "soffice", "magick"}, officeStage},
	{route.VisionConverted, []string{"magick", "soffice"}, pictureStage},
	{route.Vision, nil, func(context.Context, tools) (dispatch.Stage, error) { return dispatch.NewVisionStage(), nil }},
	{route.Nested, []string{"docproc-mail"}, emailStage},
	{route.OneNote, []string{"docproc-onenote"}, onenoteStage},
	{route.Archive, []string{"7zz"}, archiveStage},
	{route.Audio, []string{"ffmpeg", "ffprobe"}, audioStage},
	{route.Video, []string{"ffmpeg", "ffprobe"}, videoStage},
	{route.Text, nil, func(context.Context, tools) (dispatch.Stage, error) { return dispatch.NewTextStage(), nil }},
}

// Routes returns the ten routes docproc wires a stage for, in the stage table's order.
func Routes() []route.Route {
	routes := make([]route.Route, len(table))
	for i, s := range table {
		routes[i] = s.route
	}
	return routes
}

// ErrRequest is the cause of a refused request: a route ParseRoutes doesn't know.
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

// joinRoutes joins routes with ", ".
func joinRoutes(routes []route.Route) string {
	names := make([]string, len(routes))
	for i, r := range routes {
		names[i] = string(r)
	}
	return strings.Join(names, ", ")
}

// Build builds every tool a stage outside exclude needs, found through lookup and checked
// against processing's minimum, then each route's stage over them, under ctx. It never fails: a
// tool it can't build leaves out every stage that needs it, and a stage whose constructor
// refuses its tools is left out too, each row saying why, so its route has no processor yet. A
// tool only excluded stages need is never looked up.
func Build(ctx context.Context, lookup Lookup, exclude []route.Route) Set {
	needed := map[string]bool{}
	for _, s := range table {
		if !slices.Contains(exclude, s.route) {
			for _, program := range s.needs {
				needed[program] = true
			}
		}
	}

	set := Set{Stages: dispatch.Stages{}}
	built, failed := tools{}, map[string]error{}
	for _, sp := range specs() {
		if !needed[sp.Program] {
			continue
		}
		t, from, err := buildTool(ctx, lookup, sp)
		row := Tool{Program: sp.Program, Minimum: sp.Minimum, From: from, Err: err}
		if err != nil {
			failed[sp.Program] = err
		} else {
			built[sp.Program] = t
			row.Version = t.Version().String()
		}
		set.Tools = append(set.Tools, row)
	}

	for _, s := range table {
		row := Row{Route: s.route}
		var reasons []string
		for _, program := range s.needs {
			if err := failed[program]; err != nil {
				reasons = append(reasons, err.Error())
			}
		}
		switch {
		case slices.Contains(exclude, s.route):
			row.State = Excluded
		case len(reasons) > 0:
			row.State, row.Reason = Missing, strings.Join(reasons, "; ")
		default:
			st, err := s.build(ctx, built)
			if err != nil {
				row.State, row.Reason = Missing, err.Error()
				break
			}
			row.State, row.Stage = Wired, st.Identity()
			var ts []*tool.Tool
			for _, program := range s.needs {
				ts = append(ts, built[program])
			}
			row.Tools = dispatch.Versions(ts...)
			set.Stages[s.route] = st
		}
		set.Rows = append(set.Rows, row)
	}
	return set
}

// buildTool finds sp's program through lookup and builds its Tool, which checks its version
// against sp's minimum: through the exec runner, or, for an image, through the container runner
// over the docker CLI lookup finds. It returns where the tool runs from, the path or the image,
// even when the build fails past the lookup.
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
	// A path with a separator is taken as given, and the Tool keeps the program's base name.
	s := sp.Spec
	s.Program = path
	t, err := tool.NewExec(ctx, s)
	return t, path, err
}

// documentStage builds the document stage: gs renders each page, one at a time, its text layer
// read, qpdf checks for a password and reads the AcroForm, and pdfdetach saves the embedded files.
func documentStage(_ context.Context, t tools) (dispatch.Stage, error) {
	gs, qt, dt := t["gs"], t["qpdf"], t["pdfdetach"]
	cfg := document.Config{Ghostscript: gs, QPDF: qt, PDFDetach: dt}
	render, err := document.NewGhostscript(cfg)
	if err != nil {
		return nil, err
	}
	cfg.Renderer = render
	prep := document.NewPreparer(cfg)
	q, err := document.NewQPDF(cfg)
	if err != nil {
		return nil, err
	}
	detach, err := document.NewDetacher(cfg)
	if err != nil {
		return nil, err
	}
	return dispatch.NewDocumentStage(dispatch.DocumentConfig{
		Preparer: prep, Version: dispatch.Versions(gs, qt, dt), Request: document.ImagesRequest{Text: true, Concurrency: 1},
		QPDF: q, PDFDetach: detach,
	})
}

// officeStage builds the office stage over office, LibreOffice in its pinned image, and magick.
func officeStage(ctx context.Context, t tools) (dispatch.Stage, error) {
	of, lo, mg := t["docproc-ooxml"], t["soffice"], t["magick"]
	c, err := office.NewConverter(ctx, office.Config{Office: of, LibreOffice: lo, Magick: mg})
	if err != nil {
		return nil, err
	}
	return dispatch.NewOfficeStage(dispatch.OfficeConfig{Converter: c, Version: dispatch.Versions(of, lo, mg)})
}

// pictureStage builds the picture stage over magick and LibreOffice in its pinned image.
func pictureStage(ctx context.Context, t tools) (dispatch.Stage, error) {
	mg, lo := t["magick"], t["soffice"]
	c, err := picture.NewConverter(ctx, picture.Config{Magick: mg, LibreOffice: lo})
	if err != nil {
		return nil, err
	}
	return dispatch.NewPictureStage(dispatch.PictureConfig{Converter: c, Version: dispatch.Versions(mg, lo)})
}

// emailStage builds the email stage over mail.
func emailStage(_ context.Context, t tools) (dispatch.Stage, error) {
	mail := t["docproc-mail"]
	r, err := email.NewReader(email.Config{Mail: mail})
	if err != nil {
		return nil, err
	}
	return dispatch.NewEmailStage(dispatch.EmailConfig{Reader: r, Version: dispatch.Versions(mail)})
}

// onenoteStage builds the onenote stage over onenote.
func onenoteStage(_ context.Context, t tools) (dispatch.Stage, error) {
	on := t["docproc-onenote"]
	r, err := onenote.NewReader(onenote.Config{OneNote: on})
	if err != nil {
		return nil, err
	}
	return dispatch.NewOneNoteStage(dispatch.OneNoteConfig{Reader: r, Version: dispatch.Versions(on)})
}

// archiveStage builds the archive stage with 7-Zip, for what archive/zip and archive/tar don't
// read.
func archiveStage(_ context.Context, t tools) (dispatch.Stage, error) {
	sz := t["7zz"]
	r, err := sevenzip.NewReader(sevenzip.Config{SevenZip: sz})
	if err != nil {
		return nil, err
	}
	return dispatch.NewArchiveStage(dispatch.ArchiveConfig{SevenZip: r, Version: dispatch.Versions(sz)})
}

// audioStage builds the audio stage over ffmpeg and ffprobe.
func audioStage(_ context.Context, t tools) (dispatch.Stage, error) {
	ff, fp := t["ffmpeg"], t["ffprobe"]
	s, err := audio.NewSplitter(audio.Config{FFmpeg: ff, FFprobe: fp})
	if err != nil {
		return nil, err
	}
	return dispatch.NewAudioStage(dispatch.AudioConfig{Splitter: s, Version: dispatch.Versions(ff, fp)})
}

// videoStage builds the video stage over ffmpeg and ffprobe.
func videoStage(_ context.Context, t tools) (dispatch.Stage, error) {
	ff, fp := t["ffmpeg"], t["ffprobe"]
	s, err := media.NewStreamer(media.Config{FFmpeg: ff, FFprobe: fp})
	if err != nil {
		return nil, err
	}
	return dispatch.NewVideoStage(dispatch.VideoConfig{Streamer: s, Version: dispatch.Versions(ff, fp)})
}

// WriteTable writes s as two tables to w: each tool Build tried, with its version, processing's
// minimum, and where it was found, or why it wasn't built; then each route's stage, its name and
// its tools' versions, or none, when wired, or its state and why.
func (s Set) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// tabwriter buffers every cell until Flush, so a write to w fails only there; err keeps the
	// first failure all the same.
	var err error
	row := func(cells ...string) {
		if err == nil {
			_, err = io.WriteString(tw, strings.Join(cells, "\t")+"\n")
		}
	}
	row("TOOL", "VERSION", "MINIMUM", "FROM")
	for _, t := range s.Tools {
		version, from := t.Version, t.From
		if t.Err != nil {
			version, from = "-", t.Err.Error()
		}
		row(t.Program, version, t.Minimum, from)
	}
	row()
	row("ROUTE", "STAGE", "STATE", "TOOLS")
	for _, r := range s.Rows {
		name, detail := "-", r.Reason
		if r.State == Wired {
			name, detail = r.Stage.Name, r.Tools
			if detail == "" {
				detail = "none"
			}
		}
		row(string(r.Route), name, string(r.State), detail)
	}
	return errors.Join(err, tw.Flush())
}
