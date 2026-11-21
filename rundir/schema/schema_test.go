package schema_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/rundir/schema"
)

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func roundTrip[T any](t *testing.T, v T, want string) {
	t.Helper()
	got := marshal(t, v)
	if got != want {
		t.Fatalf("Marshal =\n %s\nwant\n %s", got, want)
	}
	var back T
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, v) {
		t.Errorf("Unmarshal =\n %+v\nwant\n %+v", back, v)
	}
}

func TestTimeOf_UTCWhateverZone(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		in   time.Time
		want string
	}{
		{"utc whole second", time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC), `"2026-11-07T15:04:05.000000000Z"`},
		{"utc nanoseconds", time.Date(2026, 11, 7, 15, 4, 5, 120, time.UTC), `"2026-11-07T15:04:05.000000120Z"`},
		{"local", time.Date(2026, 11, 7, 20, 49, 5, 7, time.Local), `"2026-11-07T15:04:05.000000007Z"`},
		{"london summer", time.Date(2026, 7, 7, 16, 4, 5, 7, london), `"2026-07-07T15:04:05.000000007Z"`},
		{"fixed west", time.Date(2026, 11, 7, 10, 4, 5, 7, time.FixedZone("west", -5*3600)), `"2026-11-07T15:04:05.000000007Z"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm := schema.TimeOf(tc.in)

			if loc := tm.Time().Location(); loc != time.UTC {
				t.Errorf("TimeOf location = %v, want UTC", loc)
			}
			if !tm.Time().Equal(tc.in) {
				t.Errorf("TimeOf = %v, want the instant %v", tm.Time(), tc.in)
			}
			roundTrip(t, tm, tc.want)
		})
	}
}

func TestTimeOf_Now(t *testing.T) {
	tm := schema.TimeOf(time.Now())

	var back schema.Time
	if err := json.Unmarshal([]byte(marshal(t, tm)), &back); err != nil {
		t.Fatal(err)
	}
	if back != tm {
		t.Errorf("Unmarshal = %v, want %v", back, tm)
	}
}

func TestTime_UnmarshalConvertsToUTC(t *testing.T) {
	var tm schema.Time
	if err := json.Unmarshal([]byte(`"2026-11-07T20:49:05.5+05:45"`), &tm); err != nil {
		t.Fatal(err)
	}
	if want := schema.TimeOf(time.Date(2026, 11, 7, 15, 4, 5, 5e8, time.UTC)); tm != want {
		t.Errorf("Unmarshal = %v, want %v", tm, want)
	}
}

func TestTime_UnmarshalRefuses(t *testing.T) {
	for _, in := range []string{`12`, `"yesterday"`, `null`} {
		var tm schema.Time
		if err := json.Unmarshal([]byte(in), &tm); err == nil {
			t.Errorf("Unmarshal(%s) = nil, want an error", in)
		}
	}
}

func TestTime_Zero(t *testing.T) {
	if !(schema.Time{}).IsZero() {
		t.Error("Time{}.IsZero() = false, want true")
	}
	if !schema.TimeOf(time.Time{}).IsZero() {
		t.Error("TimeOf(zero).IsZero() = false, want true")
	}
}

var (
	modified = schema.TimeOf(time.Date(2026, 11, 1, 9, 30, 0, 250, time.FixedZone("east", 5*3600+45*60)))
	created  = schema.TimeOf(time.Date(2026, 10, 31, 8, 0, 0, 5, time.UTC))
	start    = schema.TimeOf(time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC))
	end      = schema.TimeOf(time.Date(2026, 11, 7, 15, 4, 6, 0, time.UTC))
)

func TestFile_Whole(t *testing.T) {
	f := schema.File{
		Name: "a.pdf", Arg: "/docs", Path: "sub/a.pdf", Size: 42, Mode: "-rw-r-----",
		Modified: modified, Created: created, Detected: "application/pdf",
	}

	roundTrip(t, f, `{"name":"a.pdf","arg":"/docs","path":"sub/a.pdf","size":42,"mode":"-rw-r-----",`+
		`"modified":"2026-11-01T03:45:00.000000250Z","created":"2026-10-31T08:00:00.000000005Z",`+
		`"detected":"application/pdf"}`)
}

func TestFile_OptionalAbsent(t *testing.T) {
	f := schema.File{Name: "a.txt", Arg: "/a.txt", Path: ".", Mode: "-rw-r--r--", Modified: modified}

	roundTrip(t, f, `{"name":"a.txt","arg":"/a.txt","path":".","size":0,"mode":"-rw-r--r--",`+
		`"modified":"2026-11-01T03:45:00.000000250Z"}`)
}

var input = dispatch.Part{
	ID: "/", Name: "a.zip", Size: 9, SHA256: "123",
	ContentType: dispatch.ContentType{Kind: route.ZIP, Route: route.Archive},
}

func TestEvent_InputPartLine(t *testing.T) {
	e := schema.Event{
		Kind:    schema.PartEvent,
		Input:   "/docs/a.zip",
		Part:    input,
		Stage:   dispatch.Identity{Name: "archive", Version: "1", Config: "c"},
		Outcome: dispatch.Prepared, Outputs: 2, Children: 3,
		File: &schema.File{Name: "a.zip", Arg: "/docs", Path: "a.zip", Size: 9, Mode: "-rw-r--r--", Modified: modified},
		Time: &schema.Span{Start: start, End: end},
	}

	roundTrip(t, e, `{"event":"part","input":"/docs/a.zip",`+
		`"part":{"id":"/","name":"a.zip","size":9,"sha256":"123","content_type":{"kind":"zip","route":"archive"},"depth":0},`+
		`"stage":{"name":"archive","version":"1","config":"c"},"outcome":"prepared","outputs":2,"children":3,`+
		`"file":{"name":"a.zip","arg":"/docs","path":"a.zip","size":9,"mode":"-rw-r--r--","modified":"2026-11-01T03:45:00.000000250Z"},`+
		`"time":{"start":"2026-11-07T15:04:05.000000000Z","end":"2026-11-07T15:04:06.000000000Z"}}`)
}

func TestEvent_QuarantinedLine(t *testing.T) {
	e := schema.Event{
		Kind:  schema.PartEvent,
		Input: "/docs/a.zip",
		Part: dispatch.Part{
			ID: "/x.exe", Name: "x.exe", Size: 4, SHA256: "456", Parent: "/", Depth: 1, Where: "x.exe",
			Flags: []inspect.Flag{{Check: inspect.Executable, Class: inspect.Active, Evidence: "PE"}},
		},
		Outcome:    dispatch.Quarantined,
		Quarantine: "quarantine/456",
	}

	roundTrip(t, e, `{"event":"part","input":"/docs/a.zip",`+
		`"part":{"id":"/x.exe","name":"x.exe","size":4,"sha256":"456","content_type":{},`+
		`"flags":[{"check":"executable","class":"active","evidence":"PE"}],"parent":"/","depth":1,"where":"x.exe"},`+
		`"outcome":"quarantined","quarantine":"quarantine/456"}`)
}

func TestEvent_OutputLine(t *testing.T) {
	e := schema.Event{
		Kind:  schema.OutputEvent,
		Input: "/docs/a.zip",
		Part:  input,
		Stage: dispatch.Identity{Name: "archive", Version: "1", Config: "c"},
		Output: &dispatch.Output{
			Kind: dispatch.TextOutput, Name: "listing", MediaType: "text/plain",
			Meta: map[string]string{dispatch.MetaCount: "3"}, SHA256: "789",
		},
	}

	roundTrip(t, e, `{"event":"output","input":"/docs/a.zip",`+
		`"part":{"id":"/","name":"a.zip","size":9,"sha256":"123","content_type":{"kind":"zip","route":"archive"},"depth":0},`+
		`"stage":{"name":"archive","version":"1","config":"c"},`+
		`"output":{"kind":"text","name":"listing","media_type":"text/plain","meta":{"count":"3"},"sha256":"789"}}`)
}

func TestEvent_WarningLine(t *testing.T) {
	e := schema.Event{
		Kind:    schema.WarningEvent,
		Input:   "/docs/a.zip",
		Part:    input,
		Warning: &dispatch.Warning{Tool: "archive", Where: "entry b", What: "truncated"},
		Outcome: dispatch.Seen, Recorded: dispatch.Prepared,
	}

	roundTrip(t, e, `{"event":"warning","input":"/docs/a.zip",`+
		`"part":{"id":"/","name":"a.zip","size":9,"sha256":"123","content_type":{"kind":"zip","route":"archive"},"depth":0},`+
		`"warning":{"tool":"archive","where":"entry b","what":"truncated"},"outcome":"seen","recorded":"prepared"}`)
}

func TestEvent_ErrorLine(t *testing.T) {
	e := schema.Event{
		Kind:  schema.ErrorEvent,
		Input: "/docs/gone.pdf",
		Err:   errors.New("open: permission denied"),
		Time:  &schema.Span{Start: start, End: end},
		Error: "open: permission denied",
	}
	want := `{"event":"error","input":"/docs/gone.pdf",` +
		`"time":{"start":"2026-11-07T15:04:05.000000000Z","end":"2026-11-07T15:04:06.000000000Z"},` +
		`"error":"open: permission denied"}`

	if got := marshal(t, e); got != want {
		t.Errorf("Marshal with Err =\n %s\nwant\n %s", got, want)
	}
	e.Err = nil
	roundTrip(t, e, want)
}

func TestManifest_Ended(t *testing.T) {
	m := schema.Manifest{
		Schema: schema.Version, Run: "0190e0b4-0000-7000-8000-000000000000",
		Docproc: schema.Build{Command: "v0.1.0", Library: "v0.2.0"},
		Start:   start, End: end, Workers: 4, Inputs: []string{"/docs"}, Collected: 17,
		Tools: []dispatch.Tool{
			{Program: "gs", Minimum: "10.0", From: "/usr/bin/gs", Version: "10.05.1"},
			{Program: "ffmpeg", Minimum: "6.0", Error: "missing"},
		},
		Stages: []dispatch.Row{
			{Route: route.Document, State: dispatch.Wired, Stage: dispatch.Identity{Name: "document", Version: "gs 10.05.1", Config: "c"}, Tools: "gs 10.05.1"},
			{Route: route.Audio, State: dispatch.Missing, Reason: "ffmpeg missing"},
		},
		Error: "store: disk full",
	}

	roundTrip(t, m, `{"schema":"2","run":"0190e0b4-0000-7000-8000-000000000000",`+
		`"docproc":{"command":"v0.1.0","library":"v0.2.0"},`+
		`"start":"2026-11-07T15:04:05.000000000Z","end":"2026-11-07T15:04:06.000000000Z",`+
		`"workers":4,"inputs":["/docs"],"collected":17,`+
		`"tools":[{"program":"gs","minimum":"10.0","from":"/usr/bin/gs","version":"10.05.1"},`+
		`{"program":"ffmpeg","minimum":"6.0","error":"missing"}],`+
		`"stages":[{"route":"document","state":"wired","stage":{"name":"document","version":"gs 10.05.1","config":"c"},"tools":"gs 10.05.1"},`+
		`{"route":"audio","state":"missing","reason":"ffmpeg missing"}],`+
		`"error":"store: disk full"}`)
}

func TestManifest_Running(t *testing.T) {
	m := schema.Manifest{
		Schema: schema.Version, Run: "r", Docproc: schema.Build{Command: "(devel)", Library: "(devel)"},
		Start: start, Workers: 1, Inputs: []string{"/a"}, Tools: []dispatch.Tool{}, Stages: []dispatch.Row{},
	}

	roundTrip(t, m, `{"schema":"2","run":"r","docproc":{"command":"(devel)","library":"(devel)"},`+
		`"start":"2026-11-07T15:04:05.000000000Z","workers":1,"inputs":["/a"],"collected":0,"tools":[],"stages":[]}`)
}

func TestManifest_ToolErrNeverMarshals(t *testing.T) {
	m := schema.Manifest{Tools: []dispatch.Tool{{Program: "gs", Minimum: "10.0", Error: "missing", Err: errors.New("missing")}}}

	want := `{"schema":"","run":"","docproc":{"command":"","library":""},"start":"0001-01-01T00:00:00.000000000Z",` +
		`"workers":0,"inputs":null,"collected":0,"tools":[{"program":"gs","minimum":"10.0","error":"missing"}],"stages":null}`
	if got := marshal(t, m); got != want {
		t.Errorf("Marshal =\n %s\nwant\n %s", got, want)
	}
}
