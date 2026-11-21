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

func quarantined(n *node) bool {
	if dispatch.ContentFile(n.Part.SHA256) == "" {
		return false
	}
	e := n.Event
	return e.Outcome == dispatch.Quarantined || e.Recorded == dispatch.Quarantined || e.Quarantine != ""
}

func dressOutput(ctx context.Context, d *rundir.Dir, report string, o *output) error {
	o.Show = showOf(o)
	file := rundir.ContentPath(o.SHA256)
	if file == "" {
		o.Missing = true
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
		o.Missing = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer func() { _ = r.Close() }()
	if o.Show != showText {
		return nil
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("report: output %s: %w", o.SHA256, err)
	}
	o.Text = strings.ToValidUTF8(string(b), "\uFFFD")
	return nil
}

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

func relative(report, file string) (string, error) {
	rel, err := filepath.Rel(filepath.FromSlash(report), file)
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}
	return (&url.URL{Path: filepath.ToSlash(rel)}).EscapedPath(), nil
}

func sourceURL(p string) template.URL {
	return template.URL((&url.URL{Scheme: "file", Path: p}).String())
}
