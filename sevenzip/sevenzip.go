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

// SevenZipMinimum is the oldest 7-Zip the package accepts: 26.06, the mise pin. It is raised to
// the pinned minor whenever a decoder's defect is fixed past it: RAR's CVE-2025-53816, fixed in
// 25.00, and xz's CVE-2026-14266, fixed in 26.02, are past 24.07, zstd's CVE-2024-11477 fixed in
// it, and 26.03 through 26.06 fix vulnerabilities they don't name.
const SevenZipMinimum = "26.06"

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

var formats = map[route.Kind]string{
	route.SevenZip: "7z",
	route.Zstd:     "zstd",
	route.XZ:       "xz",
	route.MSCF:     "cab",
	route.ZIP:      "zip",
	route.RAR4:     "rar",
	route.RAR5:     "rar5",
}

func stream(kind route.Kind) bool { return kind == route.Zstd || kind == route.XZ }

// ListRequest names the archive to list, the kind to read it as, and the bound on its items.
type ListRequest struct {
	// Path is the archive's file; it is only read.
	Path string
	// Kind is what route identified the archive as: route.SevenZip, route.Zstd, route.XZ,
	// route.MSCF, route.ZIP, route.RAR4, or route.RAR5. A RAR is read through 7zz's handler of
	// its version, rar or rar5, each refusing the other's. A RAR volume of a split set opens its
	// sibling volumes by name: a caller that would read one file alone refuses a volume first, as
	// route.RARVolume reports it.
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
	// for a zstd or xz stream's one item, and only then.
	Name string
}

var common = []string{"-p", "-sccUTF-8"}

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
// ErrEncrypted. A write to w that fails stops 7zz, and Extract returns once it has exited, with
// that write's error, which errors.Is and errors.As match, in preference to 7zz's exit at the
// broken pipe. An item the name matches none of writes nothing and is no error: the caller holds
// what it reads to the listing's size and CRC.
func (r *Reader) Extract(ctx context.Context, req ExtractRequest, w io.Writer) error {
	path, format, err := check(req.Path, req.Kind)
	if err != nil {
		return err
	}
	if (req.Name == "") != stream(req.Kind) {
		return fmt.Errorf("sevenzip: %w: a name is required for every kind but a zstd or xz stream, and none for either", ErrRequest)
	}
	args := append([]string{"e", "-so", "-spd", "-bd", "-t" + format}, common...)
	if req.Name != "" {
		args = append(args, "-i!"+req.Name)
	}
	args = append(args, "--", path)
	stderr := &cli.LimitedBuffer{Limit: stderrLimit}
	out := &failedWriter{w: w}
	err = r.run(ctx, args, out, stderr)
	if out.err != nil {
		err = out.err
	}
	if err != nil {
		return fmt.Errorf("sevenzip: extract %q from %s: %w", req.Name, filepath.Base(req.Path), err)
	}
	return nil
}

type failedWriter struct {
	w   io.Writer
	err error
}

func (f *failedWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil && f.err == nil {
		f.err = err
	}
	return n, err
}

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
