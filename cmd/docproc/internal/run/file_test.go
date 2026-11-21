package run_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docproc/cmd/docproc/internal/app"
	"github.com/standards-lab/docproc/cmd/docproc/internal/apptest"
	"github.com/standards-lab/docproc/cmd/docproc/internal/run"
)

func inputFile(t *testing.T) (*schema.File, string) {
	t.Helper()
	t.Setenv("DOCPROC_SHUTDOWN_TIMEOUT", "")
	t.Setenv("DOCPROC_TOOL_MEMORY", "0")
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	apptest.Tools(a, apptest.NoTools)
	if code := a.Run(context.Background(), []string{"run", path, "--out", out}); code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr.String())
	}
	dir, err := rundir.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	id, err := dir.Latest()
	if err != nil {
		t.Fatal(err)
	}
	for e, err := range dir.Record(id) {
		if err != nil {
			t.Fatal(err)
		}
		if e.Kind == schema.PartEvent && e.Part.Depth == 0 {
			return e.File, path
		}
	}
	t.Fatal("no input line")
	return nil, ""
}

func TestFile_BirthTimeGiven(t *testing.T) {
	born := time.Date(2026, 11, 1, 9, 30, 0, 250, time.FixedZone("east", 5*3600+45*60))
	var probed string
	run.SetBirth(t, func(path string) (time.Time, error) {
		probed = path
		return born, nil
	})
	f, path := inputFile(t)
	if probed != path {
		t.Errorf("probed %q, want the input %q", probed, path)
	}
	if f == nil || f.Created != schema.TimeOf(born) {
		t.Errorf("file = %+v, want created %v", f, schema.TimeOf(born))
	}
}

func TestFile_NoBirthTime(t *testing.T) {
	run.SetBirth(t, func(string) (time.Time, error) { return time.Time{}, nil })
	f, _ := inputFile(t)
	if f == nil || !f.Created.IsZero() || f.Modified.IsZero() || f.Size != 2 || f.Detected == "" {
		t.Errorf("file = %+v, want no created time beside its other fields", f)
	}
}

func TestFile_ProbeFails(t *testing.T) {
	run.SetBirth(t, func(path string) (time.Time, error) {
		return time.Time{}, &os.PathError{Op: "statx", Path: path, Err: errors.New("refused")}
	})
	if f, _ := inputFile(t); f != nil {
		t.Errorf("file = %+v, want none", f)
	}
}

func TestFile_HostBirthTime(t *testing.T) {
	before := time.Now().Add(-time.Second)
	f, _ := inputFile(t)
	if f == nil {
		t.Fatal("no file")
	}
	if c := f.Created; !c.IsZero() && (c.Time().Before(before) || c.Time().After(time.Now())) {
		t.Errorf("created = %v, want a time during the test, or none", c)
	}
}
