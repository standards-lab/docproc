package office

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

var exportFilters = map[string]string{
	"docx": "MS Word 2007 XML",
	"xlsx": "Calc MS Excel 2007 XML",
	"pptx": "Impress MS PowerPoint 2007 XML",
}

type conversion struct {
	from, to, infilter string
}

const (
	odfType       = "application/vnd.oasis.opendocument."
	odfTypeMax    = 128
	manifestName  = "META-INF/manifest.xml"
	manifestSpace = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"
)

var odfFormats = map[string]conversion{
	odfType + "text":                  {"odt", "docx", ""},
	odfType + "text-template":         {"ott", "docx", ""},
	odfType + "spreadsheet":           {"ods", "xlsx", ""},
	odfType + "spreadsheet-template":  {"ots", "xlsx", ""},
	odfType + "presentation":          {"odp", "pptx", ""},
	odfType + "presentation-template": {"otp", "pptx", ""},
	odfType + "graphics":              {"odg", "pptx", "impress8"},
	odfType + "graphics-template":     {"otg", "pptx", "impress8"},
}

func sourceFormat(path string, head []byte, name string) (conversion, error) {
	ext := route.Ext(name)
	switch route.Identify(head) {
	case route.RTF:
		return conversion{from: "rtf", to: "docx"}, nil
	case route.WordPerfect:
		return conversion{from: "wpd", to: "docx", infilter: "WordPerfect"}, nil
	case route.ODF:
		return odfFormat(path)
	case route.ZIP:
		if ext == ".xlsb" {
			return conversion{from: "xlsb", to: "xlsx"}, nil
		}
	case route.OLE2:
		if to := route.LegacyTarget(ext); to != "" {
			return conversion{from: strings.TrimPrefix(ext, "."), to: to}, nil
		}
	}
	return conversion{}, nil
}

func odfFormat(path string) (conversion, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return conversion{}, odfUnreadable(err)
	}
	defer func() { _ = z.Close() }()
	mediaType, err := odfMediaType(z)
	if err != nil {
		return conversion{}, odfUnreadable(err)
	}
	conv, ok := odfFormats[mediaType]
	if !ok {
		return conversion{}, fmt.Errorf("office: %w for an ODF package of type %s", dispatch.ErrNoProcessor, mediaType)
	}
	encrypted, err := odfEncrypted(z)
	if err != nil {
		return conversion{}, odfUnreadable(err)
	}
	if encrypted {
		return conversion{}, fmt.Errorf("office: an ODF package whose manifest holds encryption data: %w", dispatch.ErrEncrypted)
	}
	return conv, nil
}

func odfUnreadable(err error) error {
	var syntax *xml.SyntaxError
	if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, zip.ErrChecksum) || errors.As(err, &syntax) {
		return fmt.Errorf("office: an unreadable ODF package: %w: %w", dispatch.ErrRefused, err)
	}
	return fmt.Errorf("office: %w", err)
}

func odfMediaType(z *zip.ReadCloser) (string, error) {
	f, err := z.Open("mimetype")
	if err != nil {
		return "", fmt.Errorf("%w: %w", zip.ErrFormat, err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, odfTypeMax+1))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func odfEncrypted(z *zip.ReadCloser) (bool, error) {
	f, err := z.Open(manifestName)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	d := xml.NewDecoder(f)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if el, ok := tok.(xml.StartElement); ok && el.Name.Space == manifestSpace && el.Name.Local == "encryption-data" {
			return true, nil
		}
	}
}

func (c *Converter) toOOXML(ctx context.Context, dir, path string, conv conversion) (string, func() error, error) {
	out, err := c.pictures.RunLibreOffice(ctx, picture.LibreOfficeRun{
		Dir:       dir,
		ConvertTo: conv.to + ":" + exportFilters[conv.to],
		InFilter:  conv.infilter,
		Inputs:    []picture.LibreOfficeInput{{Path: path, Name: "document." + conv.from}},
	})
	if err != nil {
		return "", nil, fmt.Errorf("office: %w", err)
	}
	converted := filepath.Join(out.Dir, "document."+conv.to)
	info, err := os.Stat(converted)
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s isn't a regular file", converted)
	} else if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("soffice wrote no %s", conv.to)
	}
	if err != nil {
		err = fmt.Errorf("office: libreoffice: %w", err)
		if out.Message != "" {
			err = fmt.Errorf("%w: %s", err, out.Message)
		}
		return "", nil, errors.Join(err, out.Remove())
	}
	return converted, out.Remove, nil
}
