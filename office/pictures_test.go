//go:build linux

package office_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

// pictureConverter builds a Converter over the stand-in docproc-ooxml at 0.1.0 and, when asked, the
// stand-in soffice and the stand-in magick, each built from its Spec through the exec runner, and
// returns it with the stand-ins.
func pictureConverter(t *testing.T, libreoffice, magick bool) (*office.Converter, *standins) {
	t.Helper()
	ins := installStandin(t, "0.1.0")
	var cfg office.Config
	var err error
	if cfg.Office, err = tool.NewExec(t.Context(), ins.spec(office.OfficeSpec())); err != nil {
		t.Fatal(err)
	}
	if libreoffice {
		if cfg.LibreOffice, err = tool.NewExec(t.Context(), ins.spec(picture.LibreOfficeSpec())); err != nil {
			t.Fatal(err)
		}
	}
	if magick {
		if cfg.Magick, err = tool.NewExec(t.Context(), ins.spec(picture.MagickSpec())); err != nil {
			t.Fatal(err)
		}
	}
	c, err := office.NewConverter(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, ins
}

// programs returns the programs of runs, in order.
func programs(runs []standinRun) []string {
	var out []string
	for _, r := range runs {
		out = append(out, r.Program)
	}
	return out
}

// h returns a stem the envelope's media name rule could make: 64 of the hex digit c.
func h(c string) string { return strings.Repeat(c, 64) }

// TestConvertPictures: with both converters, each picture of a convertible kind, by its stored
// file's signature, is copied into a directory of its own and each TIFF goes to magick, one run
// each, reading its first image as TIFF and writing PNG; every EMF and WMF goes to soffice in one
// run, in document order, with its profile and its output in a directory of its own in the working
// directory, the profile starting with the British English locale that makes Draw's page A4, and
// the Draw PNG export rendering that page at the default scale, 3; then each page to magick, one
// run each, trimmed to the picture, PNG to PNG. Each converted picture's PNG is its stored name's
// stem and .png, beside the stored file, which stays, and is its medium's Path, Converted; PNG,
// JPEG, GIF, and BMP, a kind the standard names no converter for, and an embedded package are
// handed on as stored. Only the media directory is left.
func TestConvertPictures(t *testing.T) {
	t.Parallel()
	c, ins := pictureConverter(t, true, true)
	names := []string{h("1") + ".emf", h("2") + ".WMF", h("3") + ".tiff", h("4") + ".png", h("5") + ".jpeg",
		h("6") + ".gif", h("7") + ".bmp", h("8") + ".TIF", h("9") + ".svg", h("0") + ".emf"}
	embedded := h("e") + ".emf"
	ins.set(mediaKey, strings.Join(append(slices.Clone(names), embedded), ","))
	line := pictureLine("docx", "", names...)
	line = strings.Replace(line, `]}`, `,{"kind":"embedded","file":"`+embedded+`"}]}`, 1)
	ins.set(stdoutKey, line)
	dir := t.TempDir()
	recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	doc := document(t, recs)
	media := filepath.Dir(doc.Media[0].Path)
	at := func(name string) string { return filepath.Join(media, name) }
	converted := map[string]bool{names[0]: true, names[1]: true, names[2]: true, names[7]: true, names[9]: true}
	for i, m := range doc.Media {
		name := append(slices.Clone(names), embedded)[i]
		want, content := at(name), standinMedium(name)
		if converted[name] {
			want = at(name[:64] + ".png")
			switch strings.ToLower(filepath.Ext(name)) {
			case ".emf", ".wmf":
				content = pngSignature + convertedPrefix + convertedPrefix + standinMedium(name)
			default:
				content = pngSignature + convertedPrefix + standinMedium(name)
			}
		}
		if m.File != name || m.Path != want || m.Converted != converted[name] {
			t.Errorf("%s: handed on as %s, converted %t; want %s", name, m.Path, m.Converted, want)
		}
		if b, err := os.ReadFile(m.Path); err != nil || string(b) != content {
			t.Errorf("%s: handed on %q, %v; want %q", name, b, err, content)
		}
		if b, err := os.ReadFile(at(name)); err != nil || string(b) != standinMedium(name) {
			t.Errorf("%s: the stored file holds %q, %v", name, b, err)
		}
	}
	if got := entries(t, dir); !slices.Equal(got, []string{filepath.Base(media)}) {
		t.Errorf("working directory holds %q, want the media directory alone", got)
	}

	all := ins.runs()
	if got, want := programs(all), []string{"docproc-ooxml", "magick", "magick", "soffice", "magick", "magick", "magick"}; !slices.Equal(got, want) {
		t.Fatalf("runs: got %q, want %q", got, want)
	}
	// Each picture is converted from a copy in a directory of its own, numbered by its place in
	// the picture package's batch, under its stem and its kind's extension.
	own := func(i int, name string) string {
		return regexp.QuoteMeta(dir) + `/picture-[^/]+/` + fmt.Sprint(i) + `/` + regexp.QuoteMeta(name)
	}
	for i, tiff := range []string{h("3"), h("8")} {
		mg := all[1+i]
		n := map[string]int{h("3"): 2, h("8"): 3}[tiff]
		if want := []string{"tiff:" + own(n, tiff+".tiff") + `\[0\]`, "-define", "png:exclude-chunks=date,time", "png:" + own(n, tiff+".png")}; !matchArgs(mg.Args, want) {
			t.Errorf("magick's arguments: got %q, want %q", mg.Args, want)
		}
		if mg.Dir != dir {
			t.Errorf("magick's working directory: got %s, want %s", mg.Dir, dir)
		}
	}
	sr := all[3]
	if sr.Dir != dir {
		t.Errorf("soffice's working directory: got %s, want %s", sr.Dir, dir)
	}
	work := regexp.QuoteMeta(dir) + `/libreoffice-[^/]+`
	wantArgs := []string{
		`-env:UserInstallation=file://` + work + `/profile`,
		`--headless`, `--norestore`,
		`--convert-to`, regexp.QuoteMeta(drawFilter(2382, 3369)),
		`--outdir`, work + `/out`,
		own(0, h("1")+".emf"), own(1, h("2")+".wmf"), own(4, h("0")+".emf"),
	}
	if !matchArgs(sr.Args, wantArgs) {
		t.Errorf("soffice's arguments: got %q, want %q", sr.Args, wantArgs)
	}
	if !strings.Contains(sr.Registry, `<item oor:path="/org.openoffice.Setup/L10N"><prop oor:name="ooSetupSystemLocale" oor:op="fuse"><value>en-GB</value></prop></item>`) {
		t.Errorf("soffice's profile started with %q, want the en-GB locale", sr.Registry)
	}
	for i, stem := range []string{h("1"), h("2"), h("0")} {
		mg := all[4+i]
		n := map[string]int{h("1"): 0, h("2"): 1, h("0"): 4}[stem]
		want := []string{`png:` + work + `/out/` + stem + `\.png`, `-trim`, `\+repage`, `-define`, `png:exclude-chunks=date,time`, `png:` + own(n, stem+".png")}
		if !matchArgs(mg.Args, want) {
			t.Errorf("magick's trim arguments: got %q, want %q", mg.Args, want)
		}
		if mg.Dir != dir {
			t.Errorf("magick's working directory: got %s, want %s", mg.Dir, dir)
		}
	}
}

// drawFilter returns soffice's --convert-to for the Draw PNG export of a page width by height
// pixels.
func drawFilter(width, height int) string {
	return fmt.Sprintf(`png:draw_png_Export:{"PixelWidth":{"type":"long","value":"%d"},"PixelHeight":{"type":"long","value":"%d"}}`, width, height)
}

// TestConvertPicturesScale: a Request's MetafileScale sets the page's pixel size soffice renders
// a metafile at, the scale times A4 at 96 dpi, 794x1123: the default, 3, when zero, and up to 8.
// A negative scale, or one above 8, is refused before anything runs.
func TestConvertPicturesScale(t *testing.T) {
	t.Parallel()
	emf := h("1") + ".emf"
	for _, tc := range []struct {
		scale         int
		width, height int
	}{
		{0, 2382, 3369},
		{1, 794, 1123},
		{5, 3970, 5615},
		{picture.MaxMetafileScale, 6352, 8984},
	} {
		t.Run(fmt.Sprint(tc.scale), func(t *testing.T) {
			c, ins := pictureConverter(t, true, true)
			ins.set(mediaKey, emf)
			ins.set(stdoutKey, pictureLine("docx", "", emf))
			if _, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: t.TempDir(), MetafileScale: tc.scale}); err != nil {
				t.Fatal(err)
			}
			all := ins.runs()
			if got := programs(all); !slices.Equal(got, []string{"docproc-ooxml", "soffice", "magick"}) {
				t.Fatalf("runs: got %q, want docproc-ooxml, soffice, and magick", got)
			}
			if i := slices.Index(all[1].Args, "--convert-to"); i < 0 || all[1].Args[i+1] != drawFilter(tc.width, tc.height) {
				t.Errorf("soffice's arguments: got %q, want --convert-to %s", all[1].Args, drawFilter(tc.width, tc.height))
			}
		})
	}
	for _, scale := range []int{-1, picture.MaxMetafileScale + 1} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			c, ins := pictureConverter(t, true, true)
			ins.set(mediaKey, emf)
			ins.set(stdoutKey, pictureLine("docx", "", emf))
			dir := t.TempDir()
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: dir, MetafileScale: scale})
			want := fmt.Sprintf("office: picture: metafile scale %d isn't from 1 to 8", scale)
			if len(recs) != 0 || !errors.Is(err, office.ErrRequest) || !strings.HasPrefix(err.Error(), want) {
				t.Fatalf("got %d records, %v; want %q, matching ErrRequest", len(recs), err, want)
			}
			if got := ins.runs(); len(got) != 0 {
				t.Errorf("runs: got %q, want none", programs(got))
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
		})
	}
}

// TestConvertPicturesNoConverter: a picture whose converter the Converter wasn't given, for EMF
// and WMF either of soffice and magick, is handed on as stored, and the conversion succeeds;
// nothing runs for it, and its links in the Markdown stay as docproc-ooxml wrote them. A
// converter given converts its own kinds.
func TestConvertPicturesNoConverter(t *testing.T) {
	t.Parallel()
	emf, wmf, tiff, png := h("1")+".emf", h("2")+".wmf", h("3")+".tiff", h("4")+".png"
	link := func(names ...string) string {
		var parts []string
		for _, n := range names {
			parts = append(parts, "!["+n[:1]+"]("+n+")")
		}
		return strings.Join(parts, " ") + "\n"
	}
	md := link(emf, wmf, tiff, png)
	for _, tc := range []struct {
		desc                string
		libreoffice, magick bool
		converted           []string
		programs            []string
		markdown            string
	}{
		{"neither", false, false, nil, []string{"docproc-ooxml"}, md},
		{"libreoffice", true, false, nil, []string{"docproc-ooxml"}, md},
		{"magick", false, true, []string{tiff},
			[]string{"docproc-ooxml", "magick"},
			link(emf, wmf, h("3")+".png", png)},
		{"both", true, true, []string{emf, wmf, tiff},
			[]string{"docproc-ooxml", "magick", "soffice", "magick", "magick"},
			link(h("1")+".png", h("2")+".png", h("3")+".png", png)},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			c, ins := pictureConverter(t, tc.libreoffice, tc.magick)
			ins.set(mediaKey, strings.Join([]string{emf, wmf, tiff, png}, ","))
			ins.set(stdoutKey, pictureLine("docx", md, emf, wmf, tiff, png))
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			doc := document(t, recs)
			for _, m := range doc.Media {
				stored := filepath.Join(filepath.Dir(doc.Media[0].Path), m.File)
				if slices.Contains(tc.converted, m.File) {
					if !m.Converted || m.Path != strings.TrimSuffix(stored, filepath.Ext(stored))+".png" {
						t.Errorf("%s: handed on as %s, converted %t; want converted", m.File, m.Path, m.Converted)
					}
				} else if m.Converted || m.Path != stored {
					t.Errorf("%s: handed on as %s, converted %t; want as stored", m.File, m.Path, m.Converted)
				}
			}
			if got := programs(ins.runs()); !slices.Equal(got, tc.programs) {
				t.Errorf("runs: got %q, want %q", got, tc.programs)
			}
			if doc.Markdown != tc.markdown {
				t.Errorf("Markdown:\ngot  %q\nwant %q", doc.Markdown, tc.markdown)
			}
		})
	}
}

// TestConvertPicturesFails: soffice exiting nonzero, soffice writing no PNG for one of its
// pictures, magick exiting nonzero, and magick writing something that isn't a PNG, in a TIFF's
// conversion or a metafile's trim, each fail the conversion before the Document is yielded, with
// the converter's own message or the picture's name, and leave nothing in the working directory.
func TestConvertPicturesFails(t *testing.T) {
	t.Parallel()
	noise := "(process:22): dconf-CRITICAL **: unable to create directory '/.cache/dconf'\nFontconfig error: No writable cache directories\n"
	emf, wmf, tiff, png := h("1")+".emf", h("2")+".wmf", h("3")+".tiff", h("4")+".png"
	all := []string{emf, wmf, tiff, png}
	metafiles := []string{emf, wmf, png}
	for _, tc := range []struct {
		desc  string
		media []string
		env   map[string]string
		want  string
	}{
		{"soffice exits 1", all,
			map[string]string{sofficeExitKey: "1", sofficeOutputKey: noise + "Error: Please verify input parameters... (SfxBaseModel::impl_store failed)\n"},
			"office: picture: libreoffice: tool: soffice: exit code 1: Error: Please verify input parameters... (SfxBaseModel::impl_store failed)"},
		{"soffice skips a picture", all,
			map[string]string{sofficeSkipKey: wmf, sofficeOutputKey: noise},
			"office: picture: libreoffice: " + wmf + ": no PNG written: Fontconfig error: No writable cache directories"},
		{"magick exits 1", all,
			map[string]string{magickExitKey: "1", magickOutputKey: "magick: improper image header `c3.tiff' @ error/tiff.c/TIFFErrors/608.\n"},
			"office: picture: magick: " + tiff + ": tool: magick: exit code 1: magick: improper image header `c3.tiff' @ error/tiff.c/TIFFErrors/608."},
		{"magick writes no PNG", all,
			map[string]string{magickRawKey: "1"},
			"office: picture: magick: " + tiff + ": " + h("3") + ".png isn't a PNG"},
		{"the trim exits 1", metafiles,
			map[string]string{magickExitKey: "1", magickOutputKey: "magick: insufficient image data in file `a1.png' @ error/png.c/ReadPNGImage/4361.\n"},
			"office: picture: magick: " + emf + ": tool: magick: exit code 1: magick: insufficient image data in file `a1.png' @ error/png.c/ReadPNGImage/4361."},
		{"the trim writes no PNG", metafiles,
			map[string]string{magickRawKey: "1"},
			"office: picture: magick: " + emf + ": " + h("1") + ".png isn't a PNG"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			c, ins := pictureConverter(t, true, true)
			ins.set(mediaKey, strings.Join(tc.media, ","))
			ins.set(stdoutKey, pictureLine("docx", "", tc.media...))
			for k, v := range tc.env {
				ins.set(k, v)
			}
			dir := t.TempDir()
			recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: dir})
			if len(recs) != 0 || err == nil {
				t.Fatalf("got %d records, %v; want an error alone", len(recs), err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q doesn't say %q", err, tc.want)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
		})
	}
}

// TestConvertRewritesLinks: every record's Markdown has each occurrence of a converted picture's
// stored name, a name repeated included, swapped for its PNG's, and a picture handed on as stored
// keeps its links. A stored .tif and .tiff of one stem, the same bytes under two extensions, are
// converted once and each swapped whole, the longer name never read as the shorter and a suffix.
// Each medium keeps docproc-ooxml's name in File.
func TestConvertRewritesLinks(t *testing.T) {
	t.Parallel()
	c, ins := pictureConverter(t, true, true)
	emf, tif, tiff, png := h("1")+".emf", h("3")+".tif", h("3")+".tiff", h("4")+".png"
	ins.set(mediaKey, strings.Join([]string{emf, tif, tiff, png}, ","))
	md := "![a](" + emf + ")\n\nagain ![](" + emf + "), and ![](" + emf + ")\n\n" +
		"![t](" + tiff + ") ![t](" + tif + ")\n\n![p](" + png + ") ![p](" + png + ")\n"
	ins.set(stdoutKey, pictureLine("xlsx", "", emf, tif, tiff, png)+
		`{"line":"sheet","name":"S","markdown":"# S\n"}`+"\n"+
		`{"line":"rows","markdown":`+strconv.Quote(md)+`}`+"\n")
	recs, err := collect(t, c, office.Request{Path: writeDocument(t, "", "doc"), Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records: got %d, want 3", len(recs))
	}
	want := "![a](" + h("1") + ".png)\n\nagain ![](" + h("1") + ".png), and ![](" + h("1") + ".png)\n\n" +
		"![t](" + h("3") + ".png) ![t](" + h("3") + ".png)\n\n![p](" + png + ") ![p](" + png + ")\n"
	if got := recs[2].(*office.Rows).Markdown; got != want {
		t.Errorf("Markdown:\ngot  %q\nwant %q", got, want)
	}
	if got := programs(ins.runs()); !slices.Equal(got, []string{"docproc-ooxml", "magick", "soffice", "magick"}) {
		t.Errorf("runs: got %q, want docproc-ooxml, one magick, soffice, and its trim", got)
	}
	doc := document(t, recs)
	for i, name := range []string{emf, tif, tiff, png} {
		if doc.Media[i].File != name {
			t.Errorf("medium %d: File %s, want docproc-ooxml's %s", i, doc.Media[i].File, name)
		}
	}
	pngPath := filepath.Join(filepath.Dir(doc.Media[0].Path), h("3")+".png")
	for _, m := range doc.Media[1:3] {
		if m.Path != pngPath || !m.Converted {
			t.Errorf("%s: handed on as %s, converted %t; want %s", m.File, m.Path, m.Converted, pngPath)
		}
	}
}

// TestNewConverterMagick: MagickSpec reads magick's version line, and refuses an older magick at
// the Tool's construction; the constructor refuses one a laxer Spec built, whatever minimum the
// Tool's own Spec declared.
func TestNewConverterMagick(t *testing.T) {
	t.Parallel()
	ins := installStandin(t, "0.1.0")
	o, err := tool.NewExec(t.Context(), ins.spec(office.OfficeSpec()))
	if err != nil {
		t.Fatal(err)
	}
	mg, err := tool.NewExec(t.Context(), ins.spec(picture.MagickSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if got := mg.Version().String(); got != "7.1.2" {
		t.Errorf("version: got %s, want 7.1.2", got)
	}

	ins.set(magickVersionKey, "6.9.13-17")
	if _, err := tool.NewExec(t.Context(), ins.spec(picture.MagickSpec())); !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewExec over 6.9.13: got %v, want ErrTooOld", err)
	}
	lax := ins.spec(picture.MagickSpec())
	lax.Minimum = "6.0"
	old, err := tool.NewExec(t.Context(), lax)
	if err != nil {
		t.Fatal(err)
	}
	_, err = office.NewConverter(t.Context(), office.Config{Office: o, Magick: old})
	var verr *tool.VersionError
	if !errors.As(err, &verr) || !errors.Is(err, tool.ErrTooOld) {
		t.Fatalf("NewConverter over 6.9.13: got %v, want a VersionError wrapping ErrTooOld", err)
	}
	if verr.Program != "magick" || verr.Found != "6.9.13" || verr.Minimum != picture.MagickMinimum {
		t.Errorf("VersionError: %+v", verr)
	}
}
