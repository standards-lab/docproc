package picture

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// DefaultMetafileScale is the scale an EMF or WMF is rendered at when a request leaves its
// MetafileScale zero: 3, three times its size at 96 dpi.
const DefaultMetafileScale = 3

// MaxMetafileScale is the largest MetafileScale a request may ask for: 8, at which the page each
// metafile is rendered on is 6352x8984 pixels, some 57 million, before the trim.
const MaxMetafileScale = 8

// DefaultDensity is the density an SVG is rendered at when a request leaves its Density zero, in
// dots per inch: 150, the DPI a PDF's pages are rendered at for vision. An SVG's CSS pixel is a
// 96th of an inch, so an SVG 400 pixels wide is 625 pixels wide as PNG.
const DefaultDensity = 150

// ErrRequest is the cause of every refused request: a MetafileScale below zero or above
// MaxMetafileScale, a negative Density, an empty working directory, a picture of a kind the
// package doesn't convert, an output stem that isn't one, two pictures of a batch whose PNGs
// share a stem, a negative limit, and a LibreOfficeRun missing what it needs.
var ErrRequest = errors.New("invalid request")

// CheckMetafileScale returns the scale a request's MetafileScale asks for, DefaultMetafileScale
// when it is zero, or an error wrapping ErrRequest when it is below zero or above
// MaxMetafileScale.
// A caller that validates a request before anything runs, as office's Convert does, calls it.
func CheckMetafileScale(scale int) (int, error) {
	s := cmp.Or(scale, DefaultMetafileScale)
	if s < 1 || s > MaxMetafileScale {
		return 0, fmt.Errorf("picture: metafile scale %d isn't from 1 to %d: %w", scale, MaxMetafileScale, ErrRequest)
	}
	return s, nil
}

func checkDensity(density int) (int, error) {
	if density < 0 {
		return 0, fmt.Errorf("picture: density %d is negative: %w", density, ErrRequest)
	}
	return cmp.Or(density, DefaultDensity), nil
}

// Config is what a Converter is built over: the Tools the caller built, both required, and
// magick's limits.
type Config struct {
	// Magick is ImageMagick's magick, typically built from MagickSpec. It converts TIFF, SVG,
	// HEIC, WebP, and AVIF to PNG and trims an EMF's or WMF's rendered page to its picture. It is
	// required.
	Magick *tool.Tool
	// LibreOffice is LibreOffice's soffice, typically built from LibreOfficeSpec through the
	// container runner in LibreOfficeImage. It renders EMF and WMF to PNG, with Magick, and runs
	// every RunLibreOffice. It is required.
	LibreOffice *tool.Tool
	// Limits are the -limit options every magick run carries; the zero value passes none.
	Limits Limits
}

// Converter converts pictures to PNG, the one form a vision model decodes them in: TIFF, EMF,
// WMF, SVG, HEIC, WebP, and AVIF. Its methods are safe for concurrent use.
type Converter struct {
	magick      *tool.Tool
	libreoffice *tool.Tool
	limits      []string
	readers     map[route.Kind]bool
}

// NewConverter builds a Converter over cfg's Tools. A nil Magick or LibreOffice is the caller's
// mistake, and panics naming the field; a negative limit is an error matching ErrRequest. It
// refuses a Magick older than MagickMinimum and a LibreOffice older than LibreOfficeMinimum,
// whatever minimum the Tool's own Spec declared, with a *tool.VersionError wrapping
// tool.ErrTooOld. It runs magick's format listing, magick -list format, once, under ctx, with
// cfg's limits, and keeps which of HEIC, WebP, and AVIF it reads: a kind it doesn't read is
// refused with ErrNoReader. A listing that fails is an error, and no Converter is built.
func NewConverter(ctx context.Context, cfg Config) (*Converter, error) {
	if cfg.Magick == nil {
		panic("picture: Config.Magick is nil")
	}
	if cfg.LibreOffice == nil {
		panic("picture: Config.LibreOffice is nil")
	}
	limits, err := cfg.Limits.args()
	if err != nil {
		return nil, err
	}
	if err := cfg.Magick.AtLeast(MagickMinimum); err != nil {
		return nil, err
	}
	if err := cfg.LibreOffice.AtLeast(LibreOfficeMinimum); err != nil {
		return nil, err
	}
	readers, err := listReaders(ctx, cfg.Magick, limits)
	if err != nil {
		return nil, err
	}
	return &Converter{magick: cfg.Magick, libreoffice: cfg.LibreOffice, limits: limits, readers: readers}, nil
}

// Request is one picture's conversion to PNG, such as a standalone picture's.
type Request struct {
	// Dir is the working directory, which the caller owns: the picture is copied into a directory
	// of its own made in it, picture-*, which is removed when the conversion ends, and the PNGs
	// are written into it and left. It is required; an empty Dir is refused rather than taken as
	// the system's temporary directory.
	Dir string `json:"-"`
	// Path is the picture's file, which is read and never changed.
	Path string `json:"-"`
	// Kind is how the picture is read, as the caller identified it, whatever its name and
	// content: route.TIFF, route.EMF, route.WMF, route.SVG, route.HEIC, route.WebP, or
	// route.AVIF. A picture of the wrong kind fails in its converter; a kind of no other is
	// refused.
	Kind route.Kind `json:"-"`
	// Stem names the PNGs: Dir's Stem and .png, or for a TIFF each page's Stem, "-page", its
	// number from 1, and .png. It is required, and is a plain name of ASCII letters, digits, '.',
	// '-', and '_', beginning with a letter or digit, such as the picture's SHA-256, so magick
	// reads no part of it as a format, a frame, or a pattern.
	Stem string `json:"-"`
	// MetafileScale is the scale an EMF or WMF is rendered at, a multiple of its size at 96 dpi:
	// zero is DefaultMetafileScale, 3, and it may be at most MaxMetafileScale, 8. Every other
	// kind ignores it.
	MetafileScale int `json:"metafile_scale"`
	// Density is the density an SVG is rendered at, in dots per inch: zero is DefaultDensity,
	// 150. Every other kind ignores it.
	Density int `json:"density"`
}

var kinds = map[route.Kind]struct{ ext, name string }{
	route.TIFF: {".tiff", "a TIFF"},
	route.EMF:  {".emf", "an EMF"},
	route.WMF:  {".wmf", "a WMF"},
	route.SVG:  {".svg", "an SVG"},
	route.HEIC: {".heic", "a HEIC"},
	route.WebP: {".webp", "a WebP"},
	route.AVIF: {".avif", "an AVIF"},
}

func metafile(kind route.Kind) bool { return kind == route.EMF || kind == route.WMF }

// See docs/api.md, "Running the tools".
var stemPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func checkStem(stem string) error {
	if !stemPattern.MatchString(stem) {
		return fmt.Errorf("picture: output stem %q isn't a plain name: %w", stem, ErrRequest)
	}
	return nil
}

// ConvertPages converts the picture at req.Path, read as req.Kind, to PNG: a TIFF to one PNG for
// each of its pages, its images in the order the file holds them, in one magick run, named by
// req.Stem, "-page", the page's number from 1, and .png; any other kind to the one PNG named by
// req.Stem and .png, through ConvertBatch, as a batch of one: an EMF or WMF rendered at
// req.MetafileScale through soffice and trimmed to its picture through magick; an SVG rendered at
// req.Density through magick's librsvg and flattened onto white; a HEIC's primary image, or a
// WebP's or AVIF's first frame, through magick, turned upright by its stored orientation. It
// returns the PNGs' absolute paths in req.Dir, in page order, replacing an earlier conversion's of
// the same names. Every PNG is written without the time of the run, as every conversion is, so a
// picture converted twice is the same bytes.
//
// A kind the package doesn't convert, a stem that isn't one, an empty req.Dir, a MetafileScale
// below zero or above MaxMetafileScale, and a negative Density are each an error matching
// ErrRequest before anything runs, as a HEIC, WebP, or AVIF whose reader magick lacks is one
// matching ErrNoReader. A run that fails, or leaves a gap in a TIFF's pages, is an error naming
// the picture's copy, and the PNGs it wrote are removed.
func (c *Converter) ConvertPages(ctx context.Context, req Request) ([]string, error) {
	if err := checkStem(req.Stem); err != nil {
		return nil, err
	}
	if req.Kind != route.TIFF {
		dir, err := workingDir(req.Dir)
		if err != nil {
			return nil, err
		}
		png := filepath.Join(dir, req.Stem+".png")
		err = c.ConvertBatch(ctx, Batch{
			Dir:           dir,
			MetafileScale: req.MetafileScale,
			Density:       req.Density,
			Pictures:      []Picture{{Path: req.Path, Kind: req.Kind, PNG: png}},
		})
		if err != nil {
			return nil, err
		}
		return []string{png}, nil
	}
	if _, err := CheckMetafileScale(req.MetafileScale); err != nil {
		return nil, err
	}
	if _, err := checkDensity(req.Density); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := workingDir(req.Dir)
	if err != nil {
		return nil, err
	}
	return c.tiffPages(ctx, dir, req)
}

func workingDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("picture: no working directory: %w", ErrRequest)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("picture: %w", err)
	}
	return abs, nil
}

// Batch is several pictures' conversion to PNG at once, such as an office document's: its EMFs
// and WMFs are rendered in one soffice run.
type Batch struct {
	// Dir is the working directory, which the caller owns and the runners mount: the pictures are
	// copied into a directory made in it, picture-*, which is removed when the batch ends. It is
	// required; an empty Dir is refused.
	Dir string
	// MetafileScale is the scale the batch's EMFs and WMFs are rendered at, as Request's.
	MetafileScale int
	// Density is the density the batch's SVGs are rendered at, as Request's.
	Density int
	// Pictures are the batch's pictures, converted in order, but for the EMFs and WMFs, which are
	// rendered together after the rest.
	Pictures []Picture
}

// Picture is one picture of a Batch.
type Picture struct {
	// Path is the picture's file, which is read and never changed.
	Path string
	// Kind is how the picture is read, whatever its content: route.TIFF, route.EMF, route.WMF,
	// route.SVG, route.HEIC, route.WebP, or route.AVIF. A picture of the wrong kind fails in its
	// converter.
	Kind route.Kind
	// PNG is the path its PNG is written to, replacing any file there. Its name's stem, the name
	// less its extension, names the picture's copy and, in an error, the picture; it is a plain
	// name, as Request's Stem is, and no two of a batch's pictures may share it.
	PNG string
}

// ConvertBatch converts b's pictures to PNG, each in a directory of its own: each picture is
// copied, under its PNG's stem and its kind's extension, into a fresh directory made for it in a
// picture-* directory in b.Dir, which holds nothing else, so an SVG's relative references find
// nothing, and its converter reads the copy; a metafile's copy is RunLibreOffice's, in its own
// directory. A TIFF's first image is converted through magick, read as TIFF whatever its content,
// one run each. An SVG is rendered through magick's librsvg coder at b.Density and flattened onto
// white, one run each; librsvg runs no script and fetches nothing
// over a network, and reads no file the SVG names outside its directory, which holds the copy
// alone. A HEIC's primary image, or a WebP's or AVIF's first frame, is converted through magick,
// read through the coder its kind names whatever its content and turned upright by its stored
// orientation, one run each. Every EMF and WMF is rendered in one RunLibreOffice run, whose profile gives
// LibreOffice a British English locale, so Draw places each on an A4 page at b.MetafileScale times
// 96 dpi, and each page is trimmed to its picture through magick, one run each. Each writes PNG, so
// nothing is encoded lossily, and without the time of the run, so a picture converted twice is the
// same bytes both times. Each PNG is written beside its copy and then moved to its Picture's PNG,
// so no PNG is written in part. The picture-* directory is removed when ConvertBatch returns,
// however it ends.
//
// Every magick run carries the Converter's limits. A picture of no converted kind, a PNG stem that
// isn't a plain name, two pictures that share a PNG stem, an empty b.Dir, a MetafileScale
// CheckMetafileScale refuses, and a negative Density are each an error matching ErrRequest before
// anything runs, and a HEIC, WebP, or AVIF whose reader magick lacks one matching ErrNoReader. A
// conversion
// that fails, or writes no PNG, is an error naming the picture by its copy's name, with the
// converter's message, and the PNGs the batch wrote are removed. When ctx ends a run, the error
// matches ctx's, as every Tool run's does.
func (c *Converter) ConvertBatch(ctx context.Context, b Batch) error {
	scale, err := CheckMetafileScale(b.MetafileScale)
	if err != nil {
		return err
	}
	density, err := checkDensity(b.Density)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := workingDir(b.Dir)
	if err != nil {
		return err
	}
	stems := map[string]bool{}
	for _, p := range b.Pictures {
		name := filepath.Base(p.Path)
		k, ok := kinds[p.Kind]
		switch {
		case !ok:
			return fmt.Errorf("picture: %s: no converter for kind %q: %w", name, p.Kind, ErrRequest)
		case raster(p.Kind) && !c.readers[p.Kind]:
			return fmt.Errorf("picture: %s is %s picture: %w", name, k.name, ErrNoReader)
		}
		stem := pngStem(p.PNG)
		if err := checkStem(stem); err != nil {
			return err
		}
		if stems[stem] {
			return fmt.Errorf("picture: two pictures' PNGs are named %s: %w", filepath.Base(p.PNG), ErrRequest)
		}
		stems[stem] = true
	}
	if len(b.Pictures) == 0 {
		return nil
	}
	work, err := os.MkdirTemp(dir, "picture-*")
	if err != nil {
		return fmt.Errorf("picture: picture directory: %w", err)
	}
	var written []string
	err = c.convertBatch(ctx, dir, work, scale, density, b.Pictures, &written)
	if rerr := os.RemoveAll(work); rerr != nil {
		err = errors.Join(err, fmt.Errorf("picture: removing the picture directory: %w", rerr))
	}
	if err != nil {
		for _, png := range written {
			err = errors.Join(err, os.Remove(png))
		}
		return err
	}
	return nil
}

func pngStem(png string) string {
	base := filepath.Base(png)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

type staged struct {
	Picture
	copied, name, png string
}

func (c *Converter) convertBatch(ctx context.Context, dir, work string, scale, density int, pictures []Picture, written *[]string) error {
	var metafiles []staged
	for i, p := range pictures {
		if metafile(p.Kind) {
			s, err := ownDir(work, i, p)
			if err != nil {
				return err
			}
			metafiles = append(metafiles, s)
			continue
		}
		s, err := stage(work, i, p)
		if err != nil {
			return err
		}
		args := tiffArgs(s)
		switch {
		case p.Kind == route.SVG:
			args = svgArgs(s, density)
		case raster(p.Kind):
			args = rasterArgs(s)
		}
		if err := c.runMagick(ctx, dir, s.name, args, s.png); err != nil {
			return err
		}
		if err := place(s, written); err != nil {
			return err
		}
	}
	if len(metafiles) == 0 {
		return nil
	}
	return c.metafilesToPNG(ctx, dir, scale, metafiles, written)
}

func ownDir(work string, i int, p Picture) (staged, error) {
	own := filepath.Join(work, strconv.Itoa(i))
	if err := os.Mkdir(own, 0o700); err != nil {
		return staged{}, fmt.Errorf("picture: %w", err)
	}
	stem := pngStem(p.PNG)
	name := stem + kinds[p.Kind].ext
	return staged{Picture: p, copied: filepath.Join(own, name), name: name, png: filepath.Join(own, stem+".png")}, nil
}

func stage(work string, i int, p Picture) (staged, error) {
	s, err := ownDir(work, i, p)
	if err != nil {
		return staged{}, err
	}
	if err := copyFile(p.Path, s.copied); err != nil {
		return staged{}, fmt.Errorf("picture: %s: %w", s.name, err)
	}
	return s, nil
}

func place(s staged, written *[]string) error {
	if err := os.Rename(s.png, s.PNG); err != nil {
		return fmt.Errorf("picture: %s: %w", s.name, err)
	}
	*written = append(*written, s.PNG)
	return nil
}

// See docs/api.md, "Running the tools".
var reproducible = []string{"-define", "png:exclude-chunks=date,time"}

func tiffArgs(s staged) []string {
	return slices.Concat([]string{"tiff:" + s.copied + "[0]"}, reproducible, []string{"png:" + s.png})
}

func svgArgs(s staged, density int) []string {
	return slices.Concat(
		[]string{"-density", strconv.Itoa(density), "-background", "white", "rsvg:" + s.copied, "-alpha", "remove", "-alpha", "off"},
		reproducible, []string{"png:" + s.png})
}

const magickOutputLimit = 64 << 10

func (c *Converter) runMagick(ctx context.Context, dir, name string, args []string, png string) error {
	out := &cli.LimitedBuffer{Limit: magickOutputLimit}
	err := c.magick.Run(ctx, tool.Invocation{
		Args:   slices.Concat(c.limits, args),
		Dir:    dir,
		Stdout: out,
		Stderr: out,
	})
	if err == nil {
		err = checkPNG(png)
	}
	if err != nil {
		err = fmt.Errorf("picture: magick: %s: %w", name, err)
		if msg := firstLine(out.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

var errNotWritten = errors.New("no PNG written")

func checkPNG(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return errNotWritten
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s isn't a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, len(route.PNGSignature))
	if _, err := io.ReadFull(f, head); err != nil || route.Identify(head) != route.PNG {
		return fmt.Errorf("%s isn't a PNG", filepath.Base(path))
	}
	return nil
}

func firstLine(out string) string {
	for line := range strings.Lines(out) {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
