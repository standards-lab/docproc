package office

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

var exportFilters = map[string]string{
	"docx": "MS Word 2007 XML",
	"xlsx": "Calc MS Excel 2007 XML",
	"pptx": "Impress MS PowerPoint 2007 XML",
}

const legacyHeadLen = 8

func legacyFormat(head []byte, name string) (from, to string) {
	kind := route.Identify(head)
	if kind == route.RTF {
		return "rtf", "docx"
	}
	ext := route.Ext(name)
	to = route.LegacyTarget(ext)
	if to == "" || kind != route.OLE2 {
		return "", ""
	}
	return strings.TrimPrefix(ext, "."), to
}

func (c *Converter) toOOXML(ctx context.Context, dir, path, from, to string) (string, func() error, error) {
	out, err := c.pictures.RunLibreOffice(ctx, picture.LibreOfficeRun{
		Dir:       dir,
		ConvertTo: to + ":" + exportFilters[to],
		Inputs:    []picture.LibreOfficeInput{{Path: path, Name: "document." + from}},
	})
	if err != nil {
		return "", nil, fmt.Errorf("office: %w", err)
	}
	converted := filepath.Join(out.Dir, "document."+to)
	info, err := os.Stat(converted)
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s isn't a regular file", converted)
	} else if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("soffice wrote no %s", to)
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
