package sevenzip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

// SevenZipMinimum is the oldest 7-Zip the package accepts: 24.07, the oldest release with no
// known defect in a decoder the package uses, its zstd decoder fixed of CVE-2024-11477's integer
// underflow. It is raised to the pinned minor whenever a decoder's defect is fixed past it.
const SevenZipMinimum = "24.07"

// SevenZipSpec declares 7-Zip's 7zz, minimum SevenZipMinimum, for either runner's constructor. 7zz with no
// arguments prints "7-Zip (z) 26.05 (x64) : Copyright ...", and the pattern takes the dotted
// numbers after the name. A caller that needs another program path or a KillAfter period sets
// them on the Spec returned.
func SevenZipSpec() tool.Spec {
	return tool.Spec{
		Program:        "7zz",
		VersionPattern: regexp.MustCompile(`7-Zip(?: \([a-z]+\))? (\d+(?:\.\d+)*)`),
		Minimum:        SevenZipMinimum,
	}
}

var (
	// ErrEncrypted is the cause of a listing whose headers are encrypted, so its items can't be
	// listed without the password, and of an extraction that needs a password: the package
	// gives none.
	ErrEncrypted = errors.New("encrypted")
	// ErrNotArchive is the cause of a listing or an extraction 7-Zip refuses because the file
	// can't be opened as the format the request names: it isn't one, or it is cut short.
	ErrNotArchive = errors.New("can't be opened as the archive format")
	// ErrRequest is the cause of a request refused before anything runs.
	ErrRequest = errors.New("invalid request")
)

// Config is what a Reader is built over: the Tool the caller built.
type Config struct {
	// SevenZip is 7-Zip's 7zz, typically built from SevenZipSpec; it is required.
	SevenZip *tool.Tool
}

// Reader lists and extracts archives through 7zz. Its methods are safe for concurrent use.
type Reader struct {
	sevenZip *tool.Tool
}

// NewReader builds a Reader over cfg's Tool. A nil SevenZip is the caller's mistake, and panics
// naming the field. It refuses one older than SevenZipMinimum, whatever minimum the Tool's own
// Spec declared, with a *tool.VersionError wrapping tool.ErrTooOld.
func NewReader(cfg Config) (*Reader, error) {
	if cfg.SevenZip == nil {
		panic("sevenzip: Config.SevenZip is nil")
	}
	if err := cfg.SevenZip.AtLeast(SevenZipMinimum); err != nil {
		return nil, err
	}
	return &Reader{sevenZip: cfg.SevenZip}, nil
}

// formats are the kinds the package reads, each with the format 7-Zip is told to read it as, its
// -t switch, so 7-Zip never guesses one from the content or the name.
var formats = map[route.Kind]string{
	// A 7z archive.
	route.SevenZip: "7z",
	// A zstd stream: one item, its decompressed content, unnamed.
	route.Zstd: "zstd",
	// A Microsoft cabinet, the container a OneNote package is.
	route.MSCF: "cab",
	// A ZIP, read through 7-Zip for an entry whose method archive/zip doesn't read.
	route.ZIP: "zip",
}

// ListRequest names the archive to list, the kind to read it as, and the bound on its items.
type ListRequest struct {
	// Path is the archive's file; it is only read.
	Path string
	// Kind is what route identified the archive as: route.SevenZip, route.Zstd, route.MSCF, or
	// route.ZIP.
	Kind route.Kind
	// Entries is the most items the listing may hold, directories among them, typically the
	// caller's inspect.Options.Entries; zero bounds none.
	Entries int
}

// ExtractRequest names one item to extract: the archive, the kind to read it as, and the item's
// listed name.
type ExtractRequest struct {
	// Path is the archive's file; it is only read.
	Path string
	// Kind is what route identified the archive as, as ListRequest's Kind.
	Kind route.Kind
	// Name is the item's Path in the archive's Listing, matched whole, never as a wildcard; empty
	// for a zstd stream's one item, and only then.
	Name string
}

// common are the switches every run takes: -p, an empty password, so 7zz never prompts for one,
// an encrypted item or header failing instead; and -sccUTF-8, its console output in UTF-8.
var common = []string{"-p", "-sccUTF-8"}

// stderrLimit is how much of 7zz's standard error a run keeps for its error.
const stderrLimit = 16 << 10

// List lists the archive req names, through one 7zz run of its technical listing, -slt, which
// reads the archive's headers and extracts nothing. The listing is parsed as 7zz writes it, and
// an item past req.Entries stops the parse and 7zz, refused with ErrEntries. A zstd stream's one item takes its size from
// the content size 7-Zip reports its frame header declares, in the archive's Method, when the
// item lists none. A listing 7zz ends with a warning, exit code
// 1, such as data after the archive's end, is a listing all the same. An archive whose headers are
// encrypted is refused with ErrEncrypted, one 7zz can't open as req's format with ErrNotArchive.
func (r *Reader) List(ctx context.Context, req ListRequest) (Listing, error) {
	path, format, err := check(req.Path, req.Kind)
	if err != nil {
		return Listing{}, err
	}
	if req.Entries < 0 {
		return Listing{}, fmt.Errorf("sevenzip: %w: entries %d", ErrRequest, req.Entries)
	}
	parser := &listingParser{limit: req.Entries}
	stderr := &cli.LimitedBuffer{Limit: stderrLimit}
	args := append([]string{"l", "-slt", "-t" + format}, common...)
	args = append(args, "--", path)
	runErr := r.run(ctx, args, parser, stderr)
	// The parse's own failure, a bound passed among them, is why 7zz stopped, when it failed.
	if parser.err == nil && runErr != nil {
		return Listing{}, fmt.Errorf("sevenzip: list %s: %w", filepath.Base(req.Path), runErr)
	}
	l, err := parser.end()
	if err != nil {
		return Listing{}, fmt.Errorf("sevenzip: list %s: %w", filepath.Base(req.Path), err)
	}
	if req.Kind == route.Zstd {
		zstdSize(&l)
	}
	return l, nil
}

// Extract writes the content of the item req names to w, through one 7zz run whose output is its
// standard output alone, -so: 7zz writes no file. The name is matched whole, -spd keeping it from
// being a wildcard and -i! from being a list file. An item that needs a password is refused with
// ErrEncrypted. A write to w that fails stops 7zz, and Extract returns once it has exited. An item
// the name matches none of writes nothing and is no error: the caller holds what it reads to the
// listing's size and CRC.
func (r *Reader) Extract(ctx context.Context, req ExtractRequest, w io.Writer) error {
	path, format, err := check(req.Path, req.Kind)
	if err != nil {
		return err
	}
	if (req.Name == "") != (req.Kind == route.Zstd) {
		return fmt.Errorf("sevenzip: %w: a name is required for every kind but a zstd stream, and none for it", ErrRequest)
	}
	args := append([]string{"e", "-so", "-spd", "-bd", "-t" + format}, common...)
	if req.Name != "" {
		args = append(args, "-i!"+req.Name)
	}
	args = append(args, "--", path)
	stderr := &cli.LimitedBuffer{Limit: stderrLimit}
	if err := r.run(ctx, args, w, stderr); err != nil {
		return fmt.Errorf("sevenzip: extract %q from %s: %w", req.Name, filepath.Base(req.Path), err)
	}
	return nil
}

// check refuses a request with no path or a kind the package doesn't read, and returns the path
// made absolute, so it never begins with a "-" or "@", and the format 7-Zip is told to read it as.
func check(path string, kind route.Kind) (string, string, error) {
	if path == "" {
		return "", "", fmt.Errorf("sevenzip: %w: no path", ErrRequest)
	}
	format, ok := formats[kind]
	if !ok {
		return "", "", fmt.Errorf("sevenzip: %w: kind %q", ErrRequest, kind)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("sevenzip: %w: %w", ErrRequest, err)
	}
	return abs, format, nil
}

// run runs 7zz with args, its stdout to stdout and its stderr to stderr, and returns its outcome:
// nil for exit code 0, or 1, a warning; and otherwise an error naming 7zz's message, matching
// ErrEncrypted or ErrNotArchive when the message says so, and the run's *tool.ExitError.
func (r *Reader) run(ctx context.Context, args []string, stdout io.Writer, stderr *cli.LimitedBuffer) error {
	err := r.sevenZip.Run(ctx, tool.Invocation{Args: args, Stdout: stdout, Stderr: stderr})
	exit, ok := errors.AsType[*tool.ExitError](err)
	switch {
	case err == nil:
		return nil
	case !ok:
		return err
	case exit.Code == 1:
		return nil
	}
	message := errorLines(stderr.String())
	switch {
	case strings.Contains(message, "Cannot open encrypted archive"), strings.Contains(message, "Wrong password"):
		return fmt.Errorf("%w: %s: %w", ErrEncrypted, message, err)
	case strings.Contains(message, "Cannot open the file as"):
		return fmt.Errorf("%w: %s: %w", ErrNotArchive, message, err)
	case message != "":
		return fmt.Errorf("%s: %w", message, err)
	}
	return err
}

// errorLines returns 7zz's stderr lines that say what went wrong, its "ERROR:" lines and those
// after an "ERRORS:" line, joined by "; ".
func errorLines(stderr string) string {
	var lines []string
	after := false
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			after = false
		case line == "ERRORS:":
			after = true
		case strings.HasPrefix(line, "ERROR:"), strings.HasPrefix(line, "Open ERROR:"), after:
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}
