//go:build linux

package picture_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/tool"
)

var (
	tiffLE  = "II*\x00\x08\x00\x00\x00filler"
	tiffBE  = "MM\x00*\x00\x00\x00\x08filler"
	wmfKey  = "\xd7\xcd\xc6\x9a\x00\x00filler"
	svgData = `<?xml version="1.0"?>` + "\n" + `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`
	pngData = pngSignature + "\x00\x00\x00\rIHDR"
)

func emfData() string {
	b := make([]byte, 88)
	binary.LittleEndian.PutUint32(b, 1)
	binary.LittleEndian.PutUint32(b[4:], 108)
	copy(b[40:], " EMF")
	return string(b)
}

func wmfStandard() string {
	b := make([]byte, 18)
	binary.LittleEndian.PutUint16(b, 1)
	binary.LittleEndian.PutUint16(b[2:], 9)
	binary.LittleEndian.PutUint16(b[4:], 0x0300)
	return string(b)
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func writePicture(t *testing.T, content, name string) string {
	t.Helper()
	if name == "" {
		name = sha(content)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func unchanged(t *testing.T, path, content string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != content {
		t.Errorf("the original holds %q, %v; want it kept as %q", b, err, content)
	}
}

func programs(runs []standinRun) []string {
	var out []string
	for _, r := range runs {
		out = append(out, r.Program)
	}
	return out
}

func matchArgs(args, want []string) bool {
	if len(args) != len(want) {
		return false
	}
	for i, p := range want {
		if !regexp.MustCompile(`^` + p + `$`).MatchString(args[i]) {
			return false
		}
	}
	return true
}

func drawFilter(width, height int) string {
	return fmt.Sprintf(`png:draw_png_Export:{"PixelWidth":{"type":"long","value":"%d"},"PixelHeight":{"type":"long","value":"%d"}}`, width, height)
}

func own(dir string) string { return regexp.QuoteMeta(dir) + `/picture-[^/]+/[0-9]+/` }

func lo(dir string) string { return regexp.QuoteMeta(dir) + `/libreoffice-[^/]+` }

const enGB = `<item oor:path="/org.openoffice.Setup/L10N"><prop oor:name="ooSetupSystemLocale" oor:op="fuse"><value>en-GB</value></prop></item>`

func TestConvertSVG(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc, name string
		density    int
		want       string
	}{
		{"the default density", "diagram.xml", 0, "150"},
		{"a set density", "diagram.svg", 300, "300"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			path := writePicture(t, svgData, tc.name)
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "beside.png"), []byte(pngData), 0o600); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: route.SVG, Stem: "drawing", Density: tc.density})
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{filepath.Join(dir, "drawing.png")}; !slices.Equal(pngs, want) {
				t.Errorf("PNGs: got %q, want %q", pngs, want)
			}
			if b, err := os.ReadFile(pngs[0]); err != nil || string(b) != pngSignature+convertedPrefix+svgData {
				t.Errorf("PNG holds %q, %v", b, err)
			}
			unchanged(t, path, svgData)
			if got := entries(t, dir); !slices.Equal(got, []string{"drawing.png"}) {
				t.Errorf("working directory holds %q, want the PNG alone", got)
			}
			all := st.runs()
			if got := programs(all); !slices.Equal(got, []string{"magick"}) {
				t.Fatalf("runs: got %q, want magick alone", got)
			}
			want := []string{`-density`, tc.want, `-background`, `white`, `rsvg:` + own(dir) + `drawing\.svg`,
				`-alpha`, `remove`, `-alpha`, `off`, `-define`, `png:exclude-chunks=date,time`, `png:` + own(dir) + `drawing\.png`}
			if !matchArgs(all[0].Args, want) {
				t.Errorf("magick's arguments: got %q, want %q", all[0].Args, want)
			}
			if all[0].Dir != dir || all[0].Input != svgData {
				t.Errorf("magick ran in %s on %q; want in %s on the SVG", all[0].Dir, all[0].Input, dir)
			}
		})
	}
}

func TestConvertMetafile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc, content string
		kind          route.Kind
		ext           string
		scale         int
		width, height int
	}{
		{"EMF at the default scale", emfData(), route.EMF, ".emf", 0, 2382, 3369},
		{"placeable WMF at scale 1", wmfKey, route.WMF, ".wmf", 1, 794, 1123},
		{"standard WMF", wmfStandard(), route.WMF, ".wmf", 5, 3970, 5615},
		{"EMF at the largest scale", emfData(), route.EMF, ".emf", picture.MaxMetafileScale, 6352, 8984},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			path := writePicture(t, tc.content, "logo.bin")
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: tc.kind, Stem: "logo", MetafileScale: tc.scale})
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{filepath.Join(dir, "logo.png")}; !slices.Equal(pngs, want) {
				t.Errorf("PNGs: got %q, want %q", pngs, want)
			}
			if b, err := os.ReadFile(pngs[0]); err != nil || string(b) != pngSignature+convertedPrefix+convertedPrefix+tc.content {
				t.Errorf("PNG holds %q, %v", b, err)
			}
			unchanged(t, path, tc.content)
			if got := entries(t, dir); !slices.Equal(got, []string{"logo.png"}) {
				t.Errorf("working directory holds %q, want the PNG alone", got)
			}
			all := st.runs()
			if got := programs(all); !slices.Equal(got, []string{"soffice", "magick"}) {
				t.Fatalf("runs: got %q, want soffice and magick", got)
			}
			wantArgs := []string{
				`-env:UserInstallation=file://` + lo(dir) + `/profile`,
				`--headless`, `--norestore`,
				`--convert-to`, regexp.QuoteMeta(drawFilter(tc.width, tc.height)),
				`--outdir`, lo(dir) + `/out`,
				lo(dir) + `/in/logo` + regexp.QuoteMeta(tc.ext),
			}
			if !matchArgs(all[0].Args, wantArgs) {
				t.Errorf("soffice's arguments: got %q, want %q", all[0].Args, wantArgs)
			}
			if !regexp.MustCompile(`^`+lo(dir)+`$`).MatchString(all[0].Dir) || !strings.Contains(all[0].Registry, enGB) {
				t.Errorf("soffice ran in %s with profile %q; want in its own directory with the en-GB locale", all[0].Dir, all[0].Registry)
			}
			trim := []string{`png:` + lo(dir) + `/out/logo\.png`, `-trim`, `\+repage`, `-define`, `png:exclude-chunks=date,time`,
				`png:` + own(dir) + `logo\.png`}
			if !matchArgs(all[1].Args, trim) {
				t.Errorf("magick's trim arguments: got %q, want %q", all[1].Args, trim)
			}
		})
	}
}

func TestConvertRefused(t *testing.T) {
	t.Parallel()
	req := func(f func(*picture.Request)) func(dir, path string) picture.Request {
		return func(dir, path string) picture.Request {
			r := picture.Request{Dir: dir, Path: path, Kind: route.TIFF, Stem: "scan"}
			f(&r)
			return r
		}
	}
	for _, tc := range []struct {
		desc string
		req  func(dir, path string) picture.Request
		is   error
		want string
	}{
		{"a PNG", req(func(r *picture.Request) { r.Kind = route.PNG }), picture.ErrRequest, `picture: content: no converter for kind "png"`},
		{"no kind", req(func(r *picture.Request) { r.Kind = route.Unknown }), picture.ErrRequest, `no converter for kind ""`},
		{"no stem", req(func(r *picture.Request) { r.Stem = "" }), picture.ErrRequest, `picture: output stem "" isn't a plain name`},
		{"a stem with a path", req(func(r *picture.Request) { r.Stem = "../scan" }), picture.ErrRequest, `output stem "../scan" isn't a plain name`},
		{"a stem with a pattern", req(func(r *picture.Request) { r.Stem = "scan%d" }), picture.ErrRequest, `output stem "scan%d" isn't a plain name`},
		{"a stem with a frame", req(func(r *picture.Request) { r.Stem, r.Kind = "scan[1]", route.SVG }), picture.ErrRequest, `output stem "scan[1]" isn't a plain name`},
		{"a stem beginning with a dot", req(func(r *picture.Request) { r.Stem = ".scan" }), picture.ErrRequest, `output stem ".scan" isn't a plain name`},
		{"a scale above 8", req(func(r *picture.Request) { r.Kind, r.MetafileScale = route.EMF, 9 }), picture.ErrRequest, "picture: metafile scale 9 isn't from 1 to 8"},
		{"a scale below 0", req(func(r *picture.Request) { r.MetafileScale = -1 }), picture.ErrRequest, "picture: metafile scale -1 isn't from 1 to 8"},
		{"a negative density", req(func(r *picture.Request) { r.Kind, r.Density = route.SVG, -1 }), picture.ErrRequest, "picture: density -1 is negative"},
		{"a TIFF's negative density", req(func(r *picture.Request) { r.Density = -1 }), picture.ErrRequest, "picture: density -1 is negative"},
		{"no working directory", req(func(r *picture.Request) { r.Dir = "" }), picture.ErrRequest, "picture: no working directory"},
		{"a missing TIFF", req(func(r *picture.Request) { r.Path += ".missing" }), os.ErrNotExist, "picture: scan.tiff: open "},
		{"a missing SVG", req(func(r *picture.Request) { r.Path, r.Kind = r.Path+".missing", route.SVG }), os.ErrNotExist, "picture: scan.svg: open "},
		{"a missing EMF", req(func(r *picture.Request) { r.Path, r.Kind = r.Path+".missing", route.EMF }), os.ErrNotExist, "picture: libreoffice: scan.emf: open "},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			path := writePicture(t, tiffLE, "content")
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), tc.req(dir, path))
			if pngs != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, %v; want an error saying %q", pngs, err, tc.want)
			}
			if !errors.Is(err, tc.is) {
				t.Errorf("error %v doesn't match %v", err, tc.is)
			}
			if got := st.runs(); len(got) != 0 {
				t.Errorf("runs: got %q, want none", programs(got))
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
			unchanged(t, path, tiffLE)
		})
	}
}

func TestConvertFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc    string
		content string
		kind    route.Kind
		set     func(*settings)
		want    string
	}{
		{"soffice exits 1", emfData(), route.EMF,
			func(s *settings) { s.SofficeFail = "Error: source file could not be loaded\n" },
			"picture: libreoffice: tool: soffice: exit code 1: Error: source file could not be loaded"},
		{"soffice writes nothing", emfData(), route.EMF,
			func(s *settings) { s.SofficeNothing = true },
			"picture: libreoffice: pic.emf: no PNG written"},
		{"the trim exits 1", emfData(), route.EMF,
			func(s *settings) { s.Fail = "magick: insufficient image data\n" },
			"picture: magick: pic.emf: tool: magick: exit code 1: magick: insufficient image data"},
		{"magick exits 1 on an SVG", svgData, route.SVG,
			func(s *settings) { s.Fail = "magick: unable to read image data\n" },
			"picture: magick: pic.svg: tool: magick: exit code 1: magick: unable to read image data"},
		{"magick writes no PNG", svgData, route.SVG,
			func(s *settings) { s.Raw = true },
			"picture: magick: pic.svg: pic.png isn't a PNG"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			st.set(tc.set)
			path := writePicture(t, tc.content, "")
			dir := t.TempDir()
			pngs, err := c.ConvertPages(t.Context(), picture.Request{Dir: dir, Path: path, Kind: tc.kind, Stem: "pic"})
			if pngs != nil || err == nil {
				t.Fatalf("got %q, %v; want an error", pngs, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q doesn't say %q", err, tc.want)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
			unchanged(t, path, tc.content)
		})
	}
}

func TestConvertBatch(t *testing.T) {
	t.Parallel()
	c, st := standinConverter(t, picture.Limits{})
	dir := t.TempDir()
	out := t.TempDir()
	src := t.TempDir()
	pictures := []struct {
		name, content string
		kind          route.Kind
	}{
		{"a1.emf", emfData(), route.EMF},
		{"b2.tif", tiffLE, route.TIFF},
		{"c3.WMF", wmfKey, route.WMF},
		{"d4.svg", svgData, route.SVG},
		{"e5.tiff", "not a tiff", route.TIFF},
	}
	var batch []picture.Picture
	for _, p := range pictures {
		path := filepath.Join(src, p.name)
		if err := os.WriteFile(path, []byte(p.content), 0o600); err != nil {
			t.Fatal(err)
		}
		stem := strings.TrimSuffix(p.name, filepath.Ext(p.name))
		batch = append(batch, picture.Picture{Path: path, Kind: p.kind, PNG: filepath.Join(out, stem+".png")})
	}
	if err := c.ConvertBatch(t.Context(), picture.Batch{Dir: dir, MetafileScale: 1, Density: 72, Pictures: batch}); err != nil {
		t.Fatal(err)
	}
	for i, p := range pictures {
		want := pngSignature + convertedPrefix + p.content
		if p.kind == route.EMF || p.kind == route.WMF {
			want = pngSignature + convertedPrefix + convertedPrefix + p.content
		}
		if b, err := os.ReadFile(batch[i].PNG); err != nil || string(b) != want {
			t.Errorf("%s: PNG holds %q, %v; want %q", p.name, b, err, want)
		}
		unchanged(t, batch[i].Path, p.content)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("working directory holds %q, want nothing", got)
	}
	if got, want := entries(t, out), []string{"a1.png", "b2.png", "c3.png", "d4.png", "e5.png"}; !slices.Equal(got, want) {
		t.Errorf("PNG directory holds %q, want %q", got, want)
	}
	all := st.runs()
	if got, want := programs(all), []string{"magick", "magick", "magick", "soffice", "magick", "magick"}; !slices.Equal(got, want) {
		t.Fatalf("runs: got %q, want %q", got, want)
	}
	at := func(i int, name string) string {
		return regexp.QuoteMeta(dir) + `/picture-[^/]+/` + fmt.Sprint(i) + `/` + regexp.QuoteMeta(name)
	}
	if want := []string{`tiff:` + at(1, "b2.tiff") + `\[0\]`, `-define`, `png:exclude-chunks=date,time`, `png:` + at(1, "b2.png")}; !matchArgs(all[0].Args, want) {
		t.Errorf("b2's arguments: got %q, want %q", all[0].Args, want)
	}
	if !matchArgs(all[1].Args[:5], []string{`-density`, `72`, `-background`, `white`, `rsvg:` + at(3, "d4.svg")}) {
		t.Errorf("d4's arguments: got %q, want its copy read through rsvg at 72 dpi", all[1].Args)
	}
	if want := []string{`tiff:` + at(4, "e5.tiff") + `\[0\]`, `-define`, `png:exclude-chunks=date,time`, `png:` + at(4, "e5.png")}; !matchArgs(all[2].Args, want) {
		t.Errorf("e5's arguments: got %q, want %q", all[2].Args, want)
	}
	if got := all[3].Args[len(all[3].Args)-2:]; !matchArgs(got, []string{lo(dir) + `/in/a1\.emf`, lo(dir) + `/in/c3\.wmf`}) {
		t.Errorf("soffice's inputs: got %q, want a1's and c3's copies", got)
	}
	for i, name := range []string{"a1", "c3"} {
		want := []string{`png:` + lo(dir) + `/out/` + name + `\.png`, `-trim`, `\+repage`, `-define`, `png:exclude-chunks=date,time`, `png:` + at(2*i, name+".png")}
		if !matchArgs(all[4+i].Args, want) {
			t.Errorf("%s's trim: got %q, want %q", name, all[4+i].Args, want)
		}
	}
}

func TestConvertBatchRefused(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	path := filepath.Join(src, "a.tiff")
	if err := os.WriteFile(path, []byte(tiffLE), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		desc     string
		density  int
		pictures []picture.Picture
		want     string
	}{
		{"a PNG", 0, []picture.Picture{{Path: path, Kind: route.PNG, PNG: filepath.Join(src, "a.png")}},
			`picture: a.tiff: no converter for kind "png"`},
		{"a stem with a pattern", 0, []picture.Picture{{Path: path, Kind: route.TIFF, PNG: filepath.Join(src, "a%d.png")}},
			`picture: output stem "a%d" isn't a plain name`},
		{"two PNGs of one stem", 0, []picture.Picture{
			{Path: path, Kind: route.TIFF, PNG: filepath.Join(src, "a.png")},
			{Path: path, Kind: route.TIFF, PNG: filepath.Join(t.TempDir(), "a.png")},
		}, "picture: two pictures' PNGs are named a.png"},
		{"a negative density", -5, []picture.Picture{{Path: path, Kind: route.SVG, PNG: filepath.Join(src, "a.png")}},
			"picture: density -5 is negative"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			dir := t.TempDir()
			err := c.ConvertBatch(t.Context(), picture.Batch{Dir: dir, Density: tc.density, Pictures: tc.pictures})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !errors.Is(err, picture.ErrRequest) {
				t.Fatalf("got %v; want ErrRequest saying %q", err, tc.want)
			}
			if got := st.runs(); len(got) != 0 {
				t.Errorf("runs: got %q, want none", programs(got))
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q, want nothing", got)
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		c, st := standinConverter(t, picture.Limits{})
		dir := t.TempDir()
		if err := c.ConvertBatch(t.Context(), picture.Batch{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		if got := st.runs(); len(got) != 0 {
			t.Errorf("runs: got %q, want none", programs(got))
		}
		if got := entries(t, dir); len(got) != 0 {
			t.Errorf("working directory holds %q, want nothing", got)
		}
	})
}

func TestConvertBatchFailsRemovesPNGs(t *testing.T) {
	t.Parallel()
	c, st := standinConverter(t, picture.Limits{})
	st.set(func(s *settings) { s.SofficeNothing = true })
	src, out := t.TempDir(), t.TempDir()
	tiff, emf := filepath.Join(src, "a.tiff"), filepath.Join(src, "b.emf")
	for path, content := range map[string]string{tiff: tiffLE, emf: emfData()} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	err := c.ConvertBatch(t.Context(), picture.Batch{Dir: t.TempDir(), Pictures: []picture.Picture{
		{Path: tiff, Kind: route.TIFF, PNG: filepath.Join(out, "a.png")},
		{Path: emf, Kind: route.EMF, PNG: filepath.Join(out, "b.png")},
	}})
	if err == nil || !strings.Contains(err.Error(), "b.emf: no PNG written") {
		t.Fatalf("got %v, want soffice's missing page", err)
	}
	if got := entries(t, out); len(got) != 0 {
		t.Errorf("PNG directory holds %q, want nothing", got)
	}
}

func TestLimits(t *testing.T) {
	t.Parallel()
	limits := picture.Limits{Area: 4000000, Width: 8000, Height: 9000, Memory: 256 << 20, Disk: 1 << 30}
	want := []string{"-limit", "area", "4000000", "-limit", "width", "8000", "-limit", "height", "9000",
		"-limit", "memory", "268435456", "-limit", "disk", "1073741824"}
	for _, tc := range []struct {
		desc   string
		limits picture.Limits
		want   []string
	}{
		{"set", limits, want},
		{"zero", picture.Limits{}, nil},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, tc.limits)
			src := t.TempDir()
			var batch []picture.Picture
			for name, content := range map[string]string{"a.tiff": tiffLE, "b.svg": svgData, "c.webp": "RIFF\x10\x00\x00\x00WEBPVP8 ", "d.emf": emfData()} {
				path := filepath.Join(src, name)
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				kind := map[string]route.Kind{".tiff": route.TIFF, ".svg": route.SVG, ".webp": route.WebP, ".emf": route.EMF}[filepath.Ext(name)]
				batch = append(batch, picture.Picture{Path: path, Kind: kind, PNG: filepath.Join(t.TempDir(), strings.TrimSuffix(name, filepath.Ext(name))+".png")})
			}
			if err := c.ConvertBatch(t.Context(), picture.Batch{Dir: t.TempDir(), Pictures: batch}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ConvertPages(t.Context(), picture.Request{Dir: t.TempDir(), Path: batch[0].Path, Kind: route.TIFF, Stem: "pages"}); err != nil {
				t.Fatal(err)
			}
			listings := st.listings()
			if len(listings) != 1 || !slices.Equal(listings[0].Args, slices.Concat(tc.want, []string{"-list", "format", "null:"})) {
				t.Errorf("format listings: got %q, want one beginning %q", listings, tc.want)
			}
			magick := 0
			for _, r := range st.runs() {
				if r.Program == "soffice" {
					if slices.Contains(r.Args, "-limit") {
						t.Errorf("soffice's run carries -limit: %q", r.Args)
					}
					continue
				}
				magick++
				if !slices.Equal(r.Args[:len(tc.want)], tc.want) || slices.Contains(r.Args[len(tc.want):], "-limit") {
					t.Errorf("magick run %q doesn't begin with the limits %q alone", r.Args, tc.want)
				}
			}
			if magick != 5 {
				t.Errorf("%d magick runs, want 5", magick)
			}
		})
	}
}

func TestLimitsNegative(t *testing.T) {
	t.Parallel()
	st := newStandins(t, "7.1.2-34")
	c, err := picture.NewConverter(t.Context(), picture.Config{
		Magick:      st.mustTool(picture.MagickSpec()),
		LibreOffice: st.mustTool(picture.LibreOfficeSpec()),
		Limits:      picture.Limits{Map: -1},
	})
	if c != nil || !errors.Is(err, picture.ErrRequest) || !strings.Contains(err.Error(), "picture: magick's map limit -1 is negative") {
		t.Errorf("got %v, %v; want ErrRequest naming the map limit", c, err)
	}
	if got := st.listings(); len(got) != 0 {
		t.Errorf("format listings: got %q, want none", got)
	}
}

func TestRunLibreOffice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ registry, infilter string }{{"", ""}, {"<oor:items/>\n", "impress8"}} {
		registry := tc.registry
		t.Run(fmt.Sprintf("registry %q, infilter %q", registry, tc.infilter), func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			first, second := writePicture(t, "a document", "report.bin"), writePicture(t, "a memo", "memo")
			dir := t.TempDir()
			out, err := c.RunLibreOffice(t.Context(), picture.LibreOfficeRun{
				Dir: dir, ConvertTo: "png:writer_png_Export", InFilter: tc.infilter, Registry: registry,
				Inputs: []picture.LibreOfficeInput{{Path: first, Name: "report.doc"}, {Path: second, Name: "memo.rtf"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !regexp.MustCompile(`^` + lo(dir) + `/out$`).MatchString(out.Dir) {
				t.Errorf("output directory %s, want the run's own", out.Dir)
			}
			if got := entries(t, out.Dir); !slices.Equal(got, []string{"memo.png", "report.png"}) {
				t.Errorf("output holds %q, want each input's page", got)
			}
			all := st.runs()
			if len(all) != 1 || all[0].Program != "soffice" {
				t.Fatalf("runs: got %q, want soffice once", programs(all))
			}
			want := []string{`-env:UserInstallation=file://` + lo(dir) + `/profile`, `--headless`, `--norestore`}
			if tc.infilter != "" {
				want = append(want, `--infilter=`+tc.infilter)
			}
			want = append(want, `--convert-to`, `png:writer_png_Export`, `--outdir`, lo(dir)+`/out`, lo(dir)+`/in/report\.doc`, lo(dir)+`/in/memo\.rtf`)
			if !matchArgs(all[0].Args, want) {
				t.Errorf("soffice's arguments: got %q, want %q", all[0].Args, want)
			}
			if filepath.Dir(out.Dir) != all[0].Dir || all[0].Input != "a document" || all[0].Registry != registry {
				t.Errorf("soffice ran in %s on %q with registry %q; want in the run's directory on the copy with %q", all[0].Dir, all[0].Input, all[0].Registry, registry)
			}
			unchanged(t, first, "a document")
			unchanged(t, second, "a memo")
			if err := out.Remove(); err != nil {
				t.Fatal(err)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("working directory holds %q after Remove, want nothing", got)
			}
		})
	}
}

func TestRunLibreOfficeRefused(t *testing.T) {
	t.Parallel()
	in := picture.LibreOfficeInput{Name: "a.doc"}
	for _, tc := range []struct {
		desc string
		run  picture.LibreOfficeRun
		want string
	}{
		{"no working directory", picture.LibreOfficeRun{ConvertTo: "docx", Inputs: []picture.LibreOfficeInput{in}}, "picture: no working directory"},
		{"no convert-to", picture.LibreOfficeRun{Dir: "d", Inputs: []picture.LibreOfficeInput{in}}, "picture: libreoffice: no --convert-to"},
		{"no inputs", picture.LibreOfficeRun{Dir: "d", ConvertTo: "docx"}, "picture: libreoffice: no inputs"},
		{"a path", picture.LibreOfficeRun{Dir: "d", ConvertTo: "docx", Inputs: []picture.LibreOfficeInput{{Name: "x/a.doc"}}},
			`picture: libreoffice: input name "x/a.doc" isn't a file name`},
		{"no name", picture.LibreOfficeRun{Dir: "d", ConvertTo: "docx", Inputs: []picture.LibreOfficeInput{{}}},
			`picture: libreoffice: input name "" isn't a file name`},
		{"one stem", picture.LibreOfficeRun{Dir: "d", ConvertTo: "docx", Inputs: []picture.LibreOfficeInput{in, {Name: "a.rtf"}}},
			`picture: libreoffice: two inputs' names have the stem "a"`},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			c, st := standinConverter(t, picture.Limits{})
			dir := t.TempDir()
			if tc.run.Dir != "" {
				tc.run.Dir = dir
			}
			out, err := c.RunLibreOffice(t.Context(), tc.run)
			if out != nil || !errors.Is(err, picture.ErrRequest) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, %v; want ErrRequest saying %q", out, err, tc.want)
			}
			if got := st.runs(); len(got) != 0 || len(entries(t, dir)) != 0 {
				t.Errorf("runs %q, working directory %q; want neither", programs(got), entries(t, dir))
			}
		})
	}
	t.Run("soffice fails", func(t *testing.T) {
		t.Parallel()
		c, st := standinConverter(t, picture.Limits{}, func(s *settings) { s.SofficeFail = "Error: source file could not be loaded\n" })
		dir := t.TempDir()
		out, err := c.RunLibreOffice(t.Context(), picture.LibreOfficeRun{
			Dir: dir, ConvertTo: "docx", Inputs: []picture.LibreOfficeInput{{Path: writePicture(t, "x", ""), Name: "a.doc"}},
		})
		if out != nil || err == nil || !strings.Contains(err.Error(), "picture: libreoffice: tool: soffice: exit code 1: Error: source file could not be loaded") {
			t.Fatalf("got %v, %v; want soffice's error", out, err)
		}
		if got := entries(t, dir); len(got) != 0 || len(st.runs()) != 1 {
			t.Errorf("working directory holds %q after %d runs, want nothing after one", got, len(st.runs()))
		}
	})
}

func TestCheckMetafileScale(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]int{0: 3, 1: 1, 5: 5, 8: 8} {
		if got, err := picture.CheckMetafileScale(in); err != nil || got != want {
			t.Errorf("CheckMetafileScale(%d) = %d, %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []int{-1, 9} {
		if _, err := picture.CheckMetafileScale(in); !errors.Is(err, picture.ErrRequest) {
			t.Errorf("CheckMetafileScale(%d) returned %v, want ErrRequest", in, err)
		}
	}
}

func TestNewConverterVersion(t *testing.T) {
	t.Parallel()
	old := newStandins(t, "7.0.11-14", func(s *settings) { s.SofficeVersion = "7.6.4.1" })
	current := newStandins(t, "7.1.2-34")
	lenient := picture.MagickSpec()
	lenient.Minimum = "6"
	lo := picture.LibreOfficeSpec()
	lo.Minimum = "7"
	var verr *tool.VersionError
	_, err := picture.NewConverter(t.Context(), picture.Config{Magick: old.mustTool(lenient), LibreOffice: current.mustTool(lo)})
	if !errors.As(err, &verr) || verr.Minimum != picture.MagickMinimum || !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewConverter over magick 7.0.11-14 returned %v, want a VersionError of minimum %s", err, picture.MagickMinimum)
	}
	_, err = picture.NewConverter(t.Context(), picture.Config{Magick: current.mustTool(lenient), LibreOffice: old.mustTool(lo)})
	if !errors.As(err, &verr) || verr.Minimum != picture.LibreOfficeMinimum || !errors.Is(err, tool.ErrTooOld) {
		t.Errorf("NewConverter over soffice 7.6.4.1 returned %v, want a VersionError of minimum %s", err, picture.LibreOfficeMinimum)
	}
}

func TestNewConverterNil(t *testing.T) {
	t.Parallel()
	st := newStandins(t, "7.1.2-34")
	mg, so := st.mustTool(picture.MagickSpec()), st.mustTool(picture.LibreOfficeSpec())
	for want, cfg := range map[string]picture.Config{
		"picture: Config.Magick is nil":      {LibreOffice: so},
		"picture: Config.LibreOffice is nil": {Magick: mg},
	} {
		func() {
			defer func() {
				if r := recover(); r != want {
					t.Errorf("NewConverter panicked with %v, want %q", r, want)
				}
			}()
			_, _ = picture.NewConverter(t.Context(), cfg)
			t.Errorf("NewConverter returned, want a panic %q", want)
		}()
	}
}

func TestLibreOfficeSpec(t *testing.T) {
	t.Parallel()
	so, err := newStandins(t, "7.1.2-34").tool(picture.LibreOfficeSpec())
	if err != nil || so.Version().String() != "26.8.1.1" {
		t.Errorf("NewExec(LibreOfficeSpec()) = %v, %v; want soffice 26.8.1.1", so, err)
	}
}
