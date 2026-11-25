package route_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/route"
)

var testdataRoutes = map[string]route.Route{
	".pdf":  route.Document,
	".docx": route.Office, ".xlsx": route.Office, ".pptx": route.Office,
	".doc": route.Office, ".xls": route.Office, ".ppt": route.Office,
	".wav":  route.Audio,
	".svg":  route.VisionConverted,
	".json": route.Text, ".md": route.Text, ".txt": route.Text,
	".sh": route.Text, ".typ": route.Text,
}

func TestFileTestdata(t *testing.T) {
	n := 0
	for _, dir := range []string{"../office/testdata", "../document/testdata", "../picture/testdata", "../tool/testdata"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			want, ok := testdataRoutes[filepath.Ext(path)]
			if !ok {
				return nil
			}
			n++
			got, why, err := route.File(path, d.Name(), "")
			if err != nil {
				return err
			}
			if got != want || (filepath.Ext(path) == ".sh" && why != `"#!" line, a script`) {
				t.Errorf("%s: route %q (%s), want %q", path, got, why, want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n == 0 {
		t.Error("no testdata routed")
	}
}
