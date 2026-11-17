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

// exportFilters are LibreOffice's export filters to the OOXML formats, by the format's
// extension; naming the filter, not only the extension, keeps a template's export from choosing
// a template format.
var exportFilters = map[string]string{
	"docx": "MS Word 2007 XML",
	"xlsx": "Calc MS Excel 2007 XML",
	"pptx": "Impress MS PowerPoint 2007 XML",
}

// legacyHeadLen is how much of a document legacyFormat reads: an OLE2 compound file's signature,
// the legacy binary formats' and an encrypted OOXML package's, and an RTF's, which is shorter.
const legacyHeadLen = 8

// legacyFormat reports whether the document whose first bytes are head is one LibreOffice
// converts before docproc-ooxml runs: an RTF, as the route package identifies it by its signature,
// whatever its file name, name, says, converted to docx as a .doc is; or an OLE2 compound file, as
// the route package identifies it, whose name has a legacy extension, in any case, converted to the
// OOXML format route.LegacyTarget gives. It returns the format converted from, "rtf" or the legacy
// extension without its dot and lowercased, and the OOXML format it converts to; "" for both when
// the document goes to docproc-ooxml as it is.
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

// toOOXML converts the legacy document or RTF at path, whose format is from, to the OOXML format
// to through the picture package's LibreOffice runner, in dir, the working directory, which
// copies it in as "document" and from's extension, the name LibreOffice reads its format by. It
// returns the converted document's path and a function that removes it with the run's directory,
// which the caller runs when its call ends; a failed conversion removes them itself. soffice
// exits zero having written nothing when it can't export the document it loaded, so a converted
// document that isn't there is an error too.
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
