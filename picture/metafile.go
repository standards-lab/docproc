package picture

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

const (
	pageWidth  = 794
	pageHeight = 1123
)

const metafileLocale = "en-GB"

const metafileRegistry = `<?xml version="1.0" encoding="UTF-8"?>
<oor:items xmlns:oor="http://openoffice.org/2001/registry" xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<item oor:path="/org.openoffice.Setup/L10N"><prop oor:name="ooSetupSystemLocale" oor:op="fuse"><value>` + metafileLocale + `</value></prop></item>
</oor:items>
`

// See docs/api.md, "Running the tools".
func metafileFilter(scale int) string {
	return fmt.Sprintf(`png:draw_png_Export:{"PixelWidth":{"type":"long","value":"%d"},"PixelHeight":{"type":"long","value":"%d"}}`,
		scale*pageWidth, scale*pageHeight)
}

func (c *Converter) metafilesToPNG(ctx context.Context, dir string, scale int, metafiles []staged, written *[]string) error {
	var inputs []LibreOfficeInput
	for _, s := range metafiles {
		inputs = append(inputs, LibreOfficeInput{Path: s.Path, Name: s.name})
	}
	out, err := c.RunLibreOffice(ctx, LibreOfficeRun{
		Dir: dir, ConvertTo: metafileFilter(scale), Inputs: inputs, Registry: metafileRegistry,
	})
	if err != nil {
		return err
	}
	for _, s := range metafiles {
		page := filepath.Join(out.Dir, pngStem(s.PNG)+".png")
		if err := checkPNG(page); err != nil {
			return errors.Join(sofficeError(fmt.Errorf("%s: %w", s.name, err), out.Message), out.Remove())
		}
		args := slices.Concat([]string{"png:" + page, "-trim", "+repage"}, reproducible, []string{"png:" + s.png})
		if err := c.runMagick(ctx, dir, s.name, args, s.png); err != nil {
			return errors.Join(err, out.Remove())
		}
		if err := place(s, written); err != nil {
			return errors.Join(err, out.Remove())
		}
	}
	if err := out.Remove(); err != nil {
		return fmt.Errorf("picture: removing the libreoffice directory: %w", err)
	}
	return nil
}
