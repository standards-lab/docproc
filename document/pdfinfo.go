package document

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/tool"
)

// PDFInfoMinimum is the oldest pdfinfo the package accepts: poppler 22.02, whose pdfinfo has
// -isodates, -custom, and -meta.
const PDFInfoMinimum = "22.02"

// PDFInfoSpec declares poppler's pdfinfo, minimum 22.02, for either runner's constructor. pdfinfo
// -v prints "pdfinfo version 26.10.0" and its copyright to stderr and exits 0. A caller that needs
// another program path or a KillAfter period sets them on the Spec returned.
func PDFInfoSpec() tool.Spec {
	return tool.Spec{
		Program:        "pdfinfo",
		VersionArgs:    []string{"-v"},
		VersionPattern: regexp.MustCompile(`pdfinfo version (\d+(?:\.\d+)*)`),
		Minimum:        PDFInfoMinimum,
	}
}

// ErrXMP is the cause of a Metadata whose XMP packet doesn't parse as XML; the metadata it
// returns beside the error holds the packet alone.
var ErrXMP = errors.New("the XMP packet doesn't parse")

// InfoReader reads a PDF's metadata through poppler's pdfinfo: its information dictionary, its
// XMP metadata stream, and its PDF version. Each Metadata is three pdfinfo runs, each reading the
// PDF by its absolute path in the PDF's directory. Its methods are safe for concurrent use.
type InfoReader struct {
	pdfinfo *tool.Tool
}

// NewInfoReader builds an InfoReader over cfg's PDFInfo; it reads no other field. A nil PDFInfo is
// the caller's mistake, and panics naming the field. It refuses one older than PDFInfoMinimum,
// whatever minimum its own Spec declared, with a *tool.VersionError wrapping tool.ErrTooOld.
func NewInfoReader(cfg Config) (*InfoReader, error) {
	if cfg.PDFInfo == nil {
		panic("document: Config.PDFInfo is nil")
	}
	if err := cfg.PDFInfo.AtLeast(PDFInfoMinimum); err != nil {
		return nil, err
	}
	return &InfoReader{pdfinfo: cfg.PDFInfo}, nil
}

const metadataLimit = 64 << 20

// Metadata returns the metadata of the PDF at pdf, as a metadata.PDF: its information dictionary,
// through pdfinfo -isodates -enc UTF-8 -custom, each standard key typed, each date in UTC, every
// other key kept as stored; its XMP packet, through pdfinfo -meta, kept whole, its common Dublin
// Core and XMP properties typed; and its PDF version, through pdfinfo's summary. A dictionary
// pdfinfo prints nothing of is no Info, and a PDF with no metadata stream no XMP.
//
// pdfinfo -custom prints each string value of the dictionary as "Key:" padded with spaces to 17
// columns and the value, a longer key followed by its value with no space; Metadata reads each line
// so, a line's value whole, leading spaces and all. A run that fails, or output past 64 MiB, fails
// with pdfinfo's message. A packet that doesn't parse returns the metadata, its XMP the packet
// alone, and an error wrapping ErrXMP.
func (r *InfoReader) Metadata(ctx context.Context, pdf string) (metadata.PDF, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return metadata.PDF{}, fmt.Errorf("document: pdfinfo: %w", err)
	}
	info, err := r.run(ctx, abs, "-isodates", "-enc", "UTF-8", "-custom")
	if err != nil {
		return metadata.PDF{}, err
	}
	packet, err := r.run(ctx, abs, "-meta")
	if err != nil {
		return metadata.PDF{}, err
	}
	summary, err := r.run(ctx, abs, "-enc", "UTF-8")
	if err != nil {
		return metadata.PDF{}, err
	}
	rec := metadata.PDF{Version: pdfVersion(summary), Info: parseInfo(info)}
	if len(packet) == 0 {
		return rec, nil
	}
	rec.XMP, err = parseXMP(packet)
	if err != nil {
		return rec, fmt.Errorf("document: pdfinfo: %s: %w: %w", filepath.Base(abs), ErrXMP, err)
	}
	return rec, nil
}

func (r *InfoReader) run(ctx context.Context, abs string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	stderr := &cli.LimitedBuffer{Limit: stderrLimit}
	if err := r.pdfinfo.Run(ctx, tool.Invocation{
		Args: append(args, abs), Dir: filepath.Dir(abs),
		Stdout: &capped{w: &stdout, limit: metadataLimit}, Stderr: stderr,
	}); err != nil {
		what := strings.Join(args, " ") + " " + filepath.Base(abs)
		if message := strings.Join(strings.Fields(stderr.String()), " "); message != "" {
			return nil, fmt.Errorf("document: pdfinfo: %s: %s: %w", what, message, err)
		}
		return nil, fmt.Errorf("document: pdfinfo: %s: %w", what, err)
	}
	return stdout.Bytes(), nil
}

const infoColumn = 17

func pdfVersion(summary []byte) string {
	for line := range strings.Lines(string(summary)) {
		if v, ok := strings.CutPrefix(line, "PDF version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseInfo(out []byte) *metadata.PDFInfo {
	text := strings.ToValidUTF8(string(out), "\uFFFD")
	info := &metadata.PDFInfo{}
	found := false
	for line := range strings.Lines(text) {
		key, value, ok := infoLine(strings.TrimSuffix(line, "\n"))
		if !ok {
			continue
		}
		found = true
		if !setInfo(info, key, value) {
			info.Custom = append(info.Custom, metadata.Property{Key: key, Value: value})
		}
	}
	if !found {
		return nil
	}
	return info
}

func infoLine(line string) (key, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		if i+1 >= infoColumn {
			return line[:i], line[i+1:], true
		}
		if strings.TrimLeft(line[i+1:min(len(line), infoColumn)], " ") == "" {
			return line[:i], line[min(len(line), infoColumn):], true
		}
	}
	return "", "", false
}

func setInfo(info *metadata.PDFInfo, key, value string) bool {
	text := map[string]*string{
		"Title": &info.Title, "Author": &info.Author, "Subject": &info.Subject,
		"Keywords": &info.Keywords, "Creator": &info.Creator, "Producer": &info.Producer,
	}
	if field, ok := text[key]; ok {
		*field = value
		return true
	}
	dates := map[string]*time.Time{"CreationDate": &info.CreationDate, "ModDate": &info.ModDate}
	if field, ok := dates[key]; ok {
		t, err := parseDate(value)
		if err != nil {
			return false
		}
		*field = t
		return true
	}
	return false
}

var dateLayouts = []string{
	"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04Z07:00", "2006-01-02T15:04",
	"2006-01-02", "2006-01", "2006",
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q isn't a date", s)
}

const (
	nsRDF   = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsXML   = "http://www.w3.org/XML/1998/namespace"
	nsDC    = "http://purl.org/dc/elements/1.1/"
	nsXMP   = "http://ns.adobe.com/xap/1.0/"
	nsPDF   = "http://ns.adobe.com/pdf/1.3/"
	nsXMPMM = "http://ns.adobe.com/xap/1.0/mm/"
)

type xmpNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []xmpNode  `xml:",any"`
	Text    string     `xml:",chardata"`
}

func parseXMP(packet []byte) (*metadata.XMP, error) {
	x := &metadata.XMP{Packet: strings.ToValidUTF8(string(packet), "\uFFFD")}
	var root xmpNode
	if err := xml.Unmarshal(packet, &root); err != nil {
		return &metadata.XMP{Packet: x.Packet}, err
	}
	root.descriptions(func(d xmpNode) {
		for _, a := range d.Attrs {
			setXMP(x, a.Name, []string{a.Value}, "")
		}
		for _, p := range d.Nodes {
			values, lang := p.values()
			setXMP(x, p.XMLName, values, lang)
		}
	})
	return x, nil
}

func (n xmpNode) descriptions(each func(xmpNode)) {
	if n.XMLName == (xml.Name{Space: nsRDF, Local: "Description"}) {
		each(n)
		return
	}
	for _, c := range n.Nodes {
		c.descriptions(each)
	}
}

func (n xmpNode) values() (values []string, defaultLang string) {
	for _, c := range n.Nodes {
		if c.XMLName.Space != nsRDF || (c.XMLName.Local != "Alt" && c.XMLName.Local != "Seq" && c.XMLName.Local != "Bag") {
			continue
		}
		for _, li := range c.Nodes {
			if li.XMLName != (xml.Name{Space: nsRDF, Local: "li"}) {
				continue
			}
			v := strings.TrimSpace(li.Text)
			values = append(values, v)
			for _, a := range li.Attrs {
				if a.Name == (xml.Name{Space: nsXML, Local: "lang"}) && a.Value == "x-default" && defaultLang == "" {
					defaultLang = v
				}
			}
		}
		return values, defaultLang
	}
	return []string{strings.TrimSpace(n.Text)}, ""
}

func setXMP(x *metadata.XMP, name xml.Name, values []string, defaultLang string) {
	values = nonEmpty(values)
	if len(values) == 0 {
		return
	}
	first := cmp.Or(defaultLang, values[0])
	switch name {
	case xml.Name{Space: nsDC, Local: "title"}:
		x.Title = first
	case xml.Name{Space: nsDC, Local: "description"}:
		x.Description = first
	case xml.Name{Space: nsDC, Local: "creator"}:
		x.Creators = values
	case xml.Name{Space: nsDC, Local: "subject"}:
		x.Subjects = values
	case xml.Name{Space: nsDC, Local: "language"}:
		x.Languages = values
	case xml.Name{Space: nsXMP, Local: "CreateDate"}:
		x.CreateDate, _ = parseDate(values[0])
	case xml.Name{Space: nsXMP, Local: "ModifyDate"}:
		x.ModifyDate, _ = parseDate(values[0])
	case xml.Name{Space: nsXMP, Local: "CreatorTool"}:
		x.CreatorTool = values[0]
	case xml.Name{Space: nsPDF, Local: "Producer"}:
		x.Producer = values[0]
	case xml.Name{Space: nsPDF, Local: "Keywords"}:
		x.Keywords = values[0]
	case xml.Name{Space: nsXMPMM, Local: "DocumentID"}:
		x.DocumentID = values[0]
	}
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
