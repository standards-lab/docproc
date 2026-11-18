package schema

import (
	"encoding/json"
	"fmt"
	"time"
)

// timeLayout is RFC 3339 with nine digits of fraction, always written, so every time in the
// schema has the same width.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Time is a time as the schema writes it: a JSON string, RFC 3339, in UTC, with nine digits of
// nanoseconds, such as "2026-11-07T15:04:05.123456789Z". Its one constructor is [TimeOf], which
// converts to UTC, and a decoded Time is UTC too, so a Time never holds a zone and two that name
// the same instant are equal. The zero Time is no time, which an optional field leaves out.
type Time struct {
	t time.Time
}

// TimeOf returns t as a [Time]: in UTC, whatever its zone, with no monotonic reading.
func TimeOf(t time.Time) Time { return Time{t: t.UTC()} }

// Time returns t as a time.Time, in UTC.
func (t Time) Time() time.Time { return t.t }

// IsZero reports whether t is the zero time, which an optional field leaves out.
func (t Time) IsZero() bool { return t.t.IsZero() }

// String returns t as the schema writes it.
func (t Time) String() string { return t.t.Format(timeLayout) }

// MarshalJSON returns t as a JSON string, as [Time.String] writes it.
func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

// UnmarshalJSON reads t from a JSON string in RFC 3339, in any zone and with any number of
// fraction digits, converting it to UTC.
func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("schema: time %s is not a JSON string", b)
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	*t = TimeOf(parsed)
	return nil
}
