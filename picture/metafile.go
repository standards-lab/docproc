package picture

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// pageWidth and pageHeight are, in pixels at 96 dpi, the page LibreOffice's Draw places a
// metafile on: A4, which metafileLocale makes its default page. The picture keeps its own size
// on the page, at its center, so rendering the page at a scale renders the picture at that scale.
const (
	pageWidth  = 794
	pageHeight = 1123
)

// metafileLocale is the locale the metafiles' soffice run is given, through its profile, so that
// Draw's default page is A4 whatever the paper of the system it runs on: its default page is the
// locale's paper, and without one, the system's, A4 in LibreOfficeImage and Letter under a US
// locale. A page of another shape than the pixel size asked for would stretch the picture.
const metafileLocale = "en-GB"

// metafileRegistry is the configuration the metafiles' soffice run starts its profile with:
// metafileLocale as LibreOffice's locale.
const metafileRegistry = `<?xml version="1.0" encoding="UTF-8"?>
<oor:items xmlns:oor="http://openoffice.org/2001/registry" xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<item oor:path="/org.openoffice.Setup/L10N"><prop oor:name="ooSetupSystemLocale" oor:op="fuse"><value>` + metafileLocale + `</value></prop></item>
</oor:items>
`

// metafileFilter returns soffice's --convert-to for metafiles rendered at scale: LibreOffice's
// Draw PNG export, with the page's pixel size, scale times its size at 96 dpi, as its filter
// options. Naming the filter, not only the extension, keeps a file soffice doesn't open as a
// picture, such as text named .emf, from being exported as a page of a text document; soffice
// fails on it instead.
func metafileFilter(scale int) string {
	return fmt.Sprintf(`png:draw_png_Export:{"PixelWidth":{"type":"long","value":"%d"},"PixelHeight":{"type":"long","value":"%d"}}`,
		scale*pageWidth, scale*pageHeight)
}

// metafilesToPNG converts metafiles, a batch's EMFs and WMFs, to PNG: it renders each at scale
// through one RunLibreOffice run in dir, each input named by its PNG's stem and its kind's
// extension, whose profile starts with metafileRegistry, so the page each is rendered on is A4 at
// scale times 96 dpi; then it trims each page to its picture through magick, writing the PNG into
// the directory of its own work holds for it, and moves each PNG to its Picture's PNG, appending
// it to written. soffice names each page for its input's stem, which no two of a batch share, and
// exits zero having written nothing for a picture it can't export, so a page that isn't there is
// an error too, as is a trim that fails.
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
		// -trim crops the page to the picture, the bounding box of what differs from the
		// corners' color, and +repage drops the crop's offset from the PNG; PNG in and out,
		// nothing is encoded lossily.
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
