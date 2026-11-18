package report

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
)

// dress sets what v's pages show of the run directory d beyond the manifest and record: each
// input's source as a file URL, each quarantined part's kept file, through d.Kept, and each
// output's way of showing, its content's file relative to the report directory report, itself
// relative to the run directory, and, for an output shown inline, its content, read whole from
// d's content store. A content the store misses is marked missing; any other failure to read it
// fails the render.
func dress(ctx context.Context, d *rundir.Dir, report string, v *model) error {
	for _, in := range v.Inputs {
		in.Source = sourceURL(in.Path)
	}
	for _, c := range v.Contents {
		if err := dressNodes(ctx, d, report, c.Roots); err != nil {
			return err
		}
	}
	return nil
}

// dressNodes dresses nodes and the parts found in them, as [dress] says.
func dressNodes(ctx context.Context, d *rundir.Dir, report string, nodes []*node) error {
	for _, n := range nodes {
		if quarantined(n) {
			kept, ok, err := d.Kept(n.Part.SHA256)
			if err != nil {
				return fmt.Errorf("report: %w", err)
			}
			if ok {
				n.Kept = kept
			}
		}
		for _, o := range n.Outputs {
			if err := dressOutput(ctx, d, report, o); err != nil {
				return err
			}
		}
		if err := dressNodes(ctx, d, report, n.Children); err != nil {
			return err
		}
	}
	return nil
}

// quarantined reports whether n's part is one a run may have kept: quarantined by this run or,
// for a seen part, by the run the index recorded, or named kept by its own line. Its SHA-256 must
// be one, so it names a kept file.
func quarantined(n *node) bool {
	if dispatch.ContentFile(n.Part.SHA256) == "" {
		return false
	}
	e := n.Event
	return e.Outcome == dispatch.Quarantined || e.Recorded == dispatch.Quarantined || e.Quarantine != ""
}

// dressOutput sets how o shows, as [dress] says.
func dressOutput(ctx context.Context, d *rundir.Dir, report string, o *output) error {
	o.Show = showOf(o)
	file := rundir.ContentPath(o.SHA256)
	if file == "" {
		o.Missing = true // a SHA-256 that isn't one names no content
		return nil
	}
	if o.Show != showText {
		rel, err := relative(report, file)
		if err != nil {
			return err
		}
		o.Href = rel
	}
	r, err := d.Content().Open(ctx, o.SHA256)
	if errors.Is(err, fs.ErrNotExist) {
		o.Missing = true // neither shown nor linked: the link would resolve to nothing
		return nil
	}
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer func() { _ = r.Close() }() // read only: a close error loses nothing
	if o.Show != showText {
		return nil // linked, not read
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("report: output %s: %w", o.SHA256, err)
	}
	// The page is UTF-8: a byte that isn't shows as the replacement character.
	o.Text = strings.ToValidUTF8(string(b), "\uFFFD")
	return nil
}

// showOf returns how o shows: a text, Markdown, or record output inline, a PNG or JPEG image as an
// img, a WAV audio as an audio player, and any other output as a link.
func showOf(o *output) string {
	switch o.Kind {
	case dispatch.TextOutput, dispatch.MarkdownOutput, dispatch.RecordOutput:
		return showText
	}
	mt, _, _ := mime.ParseMediaType(o.MediaType)
	switch {
	case o.Kind == dispatch.ImageOutput && (mt == "image/png" || mt == "image/jpeg"):
		return showImage
	case o.Kind == dispatch.AudioOutput && (mt == "audio/wav" || mt == "audio/x-wav" || mt == "audio/wave"):
		return showAudio
	}
	return showLink
}

// relative returns file, relative to the run directory, as a URL path relative to the report
// directory report, relative to the run directory too, so the run directory moves or archives
// whole and its pages' links still resolve.
func relative(report, file string) (string, error) {
	rel, err := filepath.Rel(filepath.FromSlash(report), file)
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}
	return (&url.URL{Path: filepath.ToSlash(rel)}).EscapedPath(), nil
}

// sourceURL returns the file URL of the absolute path p, an input's, as the page links it. It is
// built from p alone, each byte the URL can't hold escaped, so it is a file URL whatever p holds.
func sourceURL(p string) template.URL {
	return template.URL((&url.URL{Scheme: "file", Path: p}).String())
}
