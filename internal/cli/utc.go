package cli

import (
	"encoding/json/v2"
	"time"
)

// UTC is the option a reader decodes a tool's line with, json.Unmarshal(raw, rec, UTC), so every
// time.Time it decodes is UTC, whatever offset the line wrote it with: every time the module
// returns is UTC. The instant is kept; only its zone changes. A null leaves the time as it was.
var UTC = json.WithUnmarshalers(json.UnmarshalFunc(func(data []byte, t *time.Time) error {
	if string(data) == "null" {
		return nil
	}
	var parsed time.Time
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*t = parsed.UTC()
	return nil
}))
