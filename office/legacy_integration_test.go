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
)

var legacyTargets = map[string]string{".doc": "docx", ".xls": "xlsx", ".ppt": "pptx"}

func containerConverter(t *testing.T) *office.Converter {
	t.Helper()
	return fullConverter(t)
}

func TestLegacyHost(t *testing.T) {
	c := realConverter(t)
	paths, err := filepath.Glob(filepath.Join("testdata", "legacy", "generated", "*.*"))
	if err != nil {
		t.Fatal(err)
	}
	paths = slices.DeleteFunc(paths, func(p string) bool { return legacyTargets[filepath.Ext(p)] == "" })
	if len(paths) == 0 {
		t.Fatal("no legacy fixtures")
	}
	for _, path := range append(paths, filepath.Join("testdata", "rtf", "memo.rtf")) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			start := time.Now()
			res := convertPath(t, c, path)
			ext := filepath.Ext(path)
			from, to := strings.TrimPrefix(ext, "."), legacyTargets[ext]
			if ext == ".rtf" {
				to = "docx"
			}
			if res.doc.Format != to || res.doc.ConvertedFrom != from {
				t.Fatalf("document %s, converted from %q; want %s from %s", res.doc.Format, res.doc.ConvertedFrom, to, from)
			}
			if strings.TrimSpace(res.markdown) == "" {
				t.Errorf("no Markdown")
			}
			t.Logf("recorded: %s to %s in %d ms, %d B of Markdown", filepath.Base(path), to, time.Since(start).Milliseconds(), len(res.markdown))
		})
	}
}

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

type layered struct {
	Layers []struct {
		Size int64 `json:"size"`
	} `json:"layers"`
}

type tally struct{ want, got, kept int }

func (t *tally) String() string {
	return fmt.Sprintf("reference %4d  model %4d  kept %4d  lost %4d  added %4d", t.want, t.got, t.kept, t.want-t.kept, t.got-t.kept)
}

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

var orderedItem = regexp.MustCompile(`^\d+\. `)

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

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
