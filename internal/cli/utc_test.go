package cli_test

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/standards-lab/docproc/internal/cli"
)

func TestUTC(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)
	var got time.Time
	if err := json.Unmarshal([]byte(`"2026-03-01T12:30:00+02:00"`), &got, cli.UTC); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("decoded %v in %v, want %v in UTC", got, got.Location(), want)
	}
	var rec struct {
		At time.Time `json:"at"`
	}
	if err := json.Unmarshal([]byte(`{"at":"2026-03-01T05:15:00-05:15"}`), &rec, cli.UTC); err != nil {
		t.Fatal(err)
	}
	if !rec.At.Equal(want) || rec.At.Location() != time.UTC {
		t.Errorf("decoded %v in %v, want %v in UTC", rec.At, rec.At.Location(), want)
	}
	kept := want.In(time.Local)
	if err := json.Unmarshal([]byte(`null`), &kept, cli.UTC); err != nil {
		t.Fatal(err)
	}
	if !kept.Equal(want) || kept.Location() != time.Local {
		t.Errorf("null changed the time to %v in %v", kept, kept.Location())
	}
}
