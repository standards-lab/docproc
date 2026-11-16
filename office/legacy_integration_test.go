//go:build integration && linux

package office_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/tool"
)

// legacyTargets are the OOXML formats the legacy fixtures' extensions convert to.
var legacyTargets = map[string]string{".doc": "docx", ".xls": "xlsx", ".ppt": "pptx"}

// containerConverter builds a Converter over the real office the mise pins, through the exec
// runner, and soffice in the default image, LibreOfficeImage, through the container runner,
// which pulls the image when it isn't present.
func containerConverter(t *testing.T) *office.Converter {
	t.Helper()
	o, err := tool.NewExec(t.Context(), office.OfficeSpec())
	if err != nil {
		t.Fatalf("office, which mise pins, run the tier through mise run integration: %v", err)
	}
	lo, err := tool.NewContainer(t.Context(), "", tool.Image{Ref: picture.LibreOfficeImage}, picture.LibreOfficeSpec())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("soffice %s in %s", lo.Version(), picture.LibreOfficeImage)
	c, err := office.NewConverter(t.Context(), office.Config{Office: o, LibreOffice: lo})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestLegacyRoundTrip converts every legacy fixture through soffice in the default image, then
// docproc-ooxml, twice each, and asserts only that each conversion succeeds as a Document of the
// OOXML format the legacy one converts to, which ConvertedFrom names. It records, in its log,
// the image's footprint, each conversion's wall time, first run and repeat, and what the round
// trip loses per element kind: for a generated fixture against docproc-ooxml's records of its
// OOXML original in testdata/ooxml, for a real one against its hand-written truth.
func TestLegacyRoundTrip(t *testing.T) {
	c := containerConverter(t)
	logFootprint(t)

	generated, err := filepath.Glob(filepath.Join("testdata", "legacy", "generated", "*.*"))
	if err != nil {
		t.Fatal(err)
	}
	generated = slices.DeleteFunc(generated, func(p string) bool { return legacyTargets[filepath.Ext(p)] == "" })
	byFormat := map[string]map[string]*tally{}
	for _, path := range generated {
		t.Run(filepath.Base(path), func(t *testing.T) {
			res := convertLegacy(t, c, path)
			original := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + "." + res.doc.Format
			want := elements(convertPath(t, c, filepath.Join("testdata", "ooxml", original)))
			got := elements(res)
			format := strings.TrimPrefix(filepath.Ext(path), ".")
			if byFormat[format] == nil {
				byFormat[format] = map[string]*tally{}
			}
			logLoss(t, filepath.Base(path), "original", want, got, byFormat[format])
		})
	}
	for _, format := range slices.Sorted(maps.Keys(byFormat)) {
		for _, kind := range slices.Sorted(maps.Keys(byFormat[format])) {
			t.Logf("loss: %-5s %-12s %s", format, kind, byFormat[format][kind])
		}
	}

	truths, err := filepath.Glob(filepath.Join("testdata", "legacy", "real", "*.truth.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range truths {
		var truth struct {
			File     string              `json:"file"`
			Elements map[string][]string `json:"elements"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &truth); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Run(truth.File, func(t *testing.T) {
			res := convertLegacy(t, c, filepath.Join("testdata", "legacy", "real", truth.File))
			want := map[string][]string{}
			for kind, keys := range truth.Elements {
				for _, k := range keys {
					want[kind] = append(want[kind], normalize(k))
				}
			}
			logLoss(t, truth.File, "truth", want, elements(res), nil)
		})
	}
}

// convertLegacy converts the legacy fixture at path, in a working directory of its own, twice,
// logging each run's wall time, and fails unless both succeed with a Document of the OOXML format
// its extension converts to. It returns the first conversion.
func convertLegacy(t *testing.T, c *office.Converter, path string) converted {
	t.Helper()
	ext := filepath.Ext(path)
	var first *converted
	var times []time.Duration
	for range 2 {
		start := time.Now()
		res := convertPath(t, c, path)
		times = append(times, time.Since(start))
		if res.doc.Format != legacyTargets[ext] || res.doc.ConvertedFrom != strings.TrimPrefix(ext, ".") {
			t.Fatalf("document %s, converted from %q; want %s from %s", res.doc.Format, res.doc.ConvertedFrom, legacyTargets[ext], ext)
		}
		for _, w := range res.warnings {
			t.Logf("warning: %s: %s", w.Where, w.What)
		}
		if first == nil {
			first = &res
		}
	}
	t.Logf("footprint: %-34s first %5d ms  repeat %5d ms", filepath.Base(path), times[0].Milliseconds(), times[1].Milliseconds())
	return *first
}

// logFootprint logs the default image's size: compressed, the sum of its layers for this
// machine's architecture as the registry's manifest gives them, and on disk, as docker image
// inspect gives it. Neither is asserted; a size docker can't give is logged as unknown.
func logFootprint(t *testing.T) {
	t.Helper()
	ref := picture.LibreOfficeImage
	disk, err := exec.CommandContext(t.Context(), "docker", "image", "inspect", "--format={{.Size}}", ref).Output()
	if err != nil {
		t.Logf("footprint: on disk unknown: %v", err)
	} else {
		t.Logf("footprint: image on disk %s bytes", strings.TrimSpace(string(disk)))
	}
	out, err := exec.CommandContext(t.Context(), "docker", "manifest", "inspect", "--verbose", ref).Output()
	if err != nil {
		t.Logf("footprint: compressed unknown: %v", err)
		return
	}
	var manifests []struct {
		Descriptor struct {
			Platform struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform"`
		} `json:"Descriptor"`
		Docker *layered `json:"SchemaV2Manifest"`
		OCI    *layered `json:"OCIManifest"`
	}
	if err := json.Unmarshal(out, &manifests); err != nil {
		t.Logf("footprint: compressed unknown: %v", err)
		return
	}
	for _, m := range manifests {
		p := m.Descriptor.Platform
		if p.OS != "linux" || p.Architecture != runtime.GOARCH {
			continue
		}
		layers := m.Docker
		if layers == nil {
			layers = m.OCI
		}
		if layers == nil {
			break
		}
		var size int64
		for _, l := range layers.Layers {
			size += l.Size
		}
		t.Logf("footprint: image compressed %d bytes (linux/%s, %d layers)", size, p.Architecture, len(layers.Layers))
		return
	}
	t.Logf("footprint: compressed unknown: no linux/%s manifest", runtime.GOARCH)
}

// layered is an image manifest's layers, as docker manifest inspect --verbose gives them.
type layered struct {
	Layers []struct {
		Size int64 `json:"size"`
	} `json:"layers"`
}

// tally is one element kind's loss: the reference's elements, the model's, and those they share,
// counted with multiplicity.
type tally struct{ want, got, kept int }

func (t *tally) String() string {
	return fmt.Sprintf("reference %4d  model %4d  kept %4d  lost %4d  added %4d", t.want, t.got, t.kept, t.want-t.kept, t.got-t.kept)
}

// logLoss logs, for each element kind in want or got, how many of want's elements got keeps, by
// their keys counted with multiplicity, then each element lost and each added, and adds each
// kind's counts to sum when it isn't nil. ref names the reference in the log.
func logLoss(t *testing.T, file, ref string, want, got map[string][]string, sum map[string]*tally) {
	t.Helper()
	kinds := map[string]bool{}
	for k := range want {
		kinds[k] = true
	}
	for k := range got {
		kinds[k] = true
	}
	for _, kind := range slices.Sorted(maps.Keys(kinds)) {
		n := tally{want: len(want[kind]), got: len(got[kind])}
		left := map[string]int{}
		for _, k := range got[kind] {
			left[k]++
		}
		var lost []string
		for _, k := range want[kind] {
			if left[k] > 0 {
				left[k]--
				n.kept++
			} else {
				lost = append(lost, k)
			}
		}
		t.Logf("loss: %-34s %-12s %s against the %s", file, kind, &n, ref)
		for _, k := range lost {
			t.Logf("loss:   lost %s %q", kind, k)
		}
		for _, k := range got[kind] {
			if left[k] > 0 {
				left[k]--
				t.Logf("loss:   added %s %q", kind, k)
			}
		}
		if sum != nil {
			if sum[kind] == nil {
				sum[kind] = &tally{}
			}
			sum[kind].want += n.want
			sum[kind].got += n.got
			sum[kind].kept += n.kept
		}
	}
}

// elements returns a conversion's elements by kind, each as a key that a hand-written truth can
// give, its whitespace collapsed, read from its records: from a Sheet and its Rows, a sheet as its
// name, its table as "sheet", and each cell as its sheet, reference, and value, "Sheet1!A1=text",
// and its formula as a "formula" too; from the Document's Markdown, a heading as its level and
// text, "1:Title", a pptx's "# Slide n" a slide as its number, a GFM table as its rows and widest
// row's cells, "2x3", a top-level list as ordered or unordered and its item count, "unordered:3",
// and any other line a paragraph, its Markdown escapes kept; and each medium as its content type,
// or "linked". It reads the Markdown, not a model, so a key it can't read the way a truth gives
// it is logged as lost, never failed.
func elements(res converted) map[string][]string {
	out := map[string][]string{}
	add := func(kind, key string) { out[kind] = append(out[kind], normalize(key)) }
	var sheet string
	tabled := false
	for _, rec := range res.recs {
		switch r := rec.(type) {
		case *office.Sheet:
			sheet, tabled = r.Name, false
			add("sheet", r.Name)
		case *office.Rows:
			if len(r.Rows) > 0 && !tabled {
				add("table", "sheet")
				tabled = true
			}
			for _, row := range r.Rows {
				for _, cell := range row.Cells {
					if cell.Value != nil {
						add("cell", sheet+"!"+cell.Ref+"="+cellValue(cell.Value))
					}
					if cell.Formula != "" {
						add("formula", sheet+"!"+cell.Ref+"="+cell.Formula)
					}
				}
			}
		}
	}
	for _, m := range res.doc.Media {
		if m.File == "" {
			add("media", "linked")
		} else {
			add("media", m.ContentType)
		}
	}
	markdownElements(res.doc.Format, res.doc.Markdown, add)
	return out
}

// markdownElements adds the elements of md, a Document's Markdown of format, by kind through
// add, as elements describes.
func markdownElements(format, md string, add func(kind, key string)) {
	var table [][]string
	list, items := "", 0
	flush := func() {
		if len(table) > 0 {
			width := 0
			for _, row := range table {
				width = max(width, len(row))
			}
			add("table", fmt.Sprintf("%dx%d", len(table), width))
			table = nil
		}
		if items > 0 {
			add("list", list+":"+strconv.Itoa(items))
			list, items = "", 0
		}
	}
	for line := range strings.Lines(md) {
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "|"):
			if strings.HasPrefix(line, "| ---") {
				continue
			}
			table = append(table, strings.Split(strings.Trim(line, "|"), " | "))
			continue
		case strings.HasPrefix(line, "- "), orderedItem.MatchString(line):
			kind := "unordered"
			if !strings.HasPrefix(line, "- ") {
				kind = "ordered"
			}
			if items > 0 && kind != list {
				flush()
			}
			list = kind
			items++
			add("paragraph", orderedItem.ReplaceAllString(strings.TrimPrefix(line, "- "), ""))
			continue
		case strings.HasPrefix(line, " "):
			continue
		}
		flush()
		switch {
		case strings.TrimSpace(line) == "", strings.HasPrefix(line, "!["), strings.HasPrefix(line, "[^"):
		case format == "pptx" && strings.HasPrefix(line, "# Slide "):
			add("slide", strings.TrimPrefix(line, "# Slide "))
		case strings.HasPrefix(line, "#"):
			level := len(line) - len(strings.TrimLeft(line, "#"))
			add("heading", strconv.Itoa(level)+":"+strings.TrimSpace(line[level:]))
		default:
			add("paragraph", line)
		}
	}
	flush()
}

// orderedItem matches an ordered list item's marker.
var orderedItem = regexp.MustCompile(`^\d+\. `)

// cellValue returns a sheet cell's value as text: a number in its shortest form, a boolean as
// TRUE or FALSE, and text, an error, or a date as given.
func cellValue(v any) string {
	switch v := v.(type) {
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strings.ToUpper(strconv.FormatBool(v))
	case string:
		return v
	}
	return fmt.Sprint(v)
}

// normalize collapses a key's runs of whitespace to single spaces and trims it.
func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
