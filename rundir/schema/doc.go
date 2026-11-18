// Package schema is the run directory's schema "2": the shapes cmd/docproc writes as JSON, through
// encoding/json, and a later reader, such as cmd/docinfer, decodes, through package rundir. It is
// shapes alone, importing no docproc package but dispatch and inspect, so a reader never compiles
// the stages or the format packages beneath them. Each type's doc comment is its part of the
// contract: a field's JSON name, what it holds, and when it is absent. A field the comments don't
// call optional is always present, its zero value included.
//
// Schema "2" includes dispatch's shapes at dispatch.Version "1": a part, its content type, a
// stage's identity, an output, a warning, an inspection's flag, and the stage table's tool and
// row are dispatch's and inspect's, aliased or used as they are, so a break there is a break
// here.
//
// A time is a [Time]: RFC 3339, in UTC, with nine digits of nanoseconds, such as
// "2026-11-07T15:04:05.123456789Z". A writer makes one with [TimeOf], which converts any zone.
//
// A run directory holds, for each run, a [Manifest], runs/<run>/manifest.json, and a record,
// runs/<run>/record.jsonl, one [Event] per line. Every path to a file the run directory holds, a
// quarantined part's file among them, is relative to the run directory, with slashes, so the run
// directory can be moved whole; an output's content is found by its SHA-256 in the run
// directory's content store. An input's path and its argument, which docproc only reads, are
// absolute and cleaned, the manifest's inputs among them; a [File]'s "path" is relative to its
// "arg"; and a tool's path is the host's.
//
// The package exports:
//
//   - [Version], the schema's version, "2"
//   - [Time], a time as the schema writes it, and [TimeOf], which makes one
//   - [Part], [ContentType], [Stage], [Output], [Warning], and [Flag], the shapes it takes from
//     dispatch and inspect
//   - [File], an input's file metadata and its sniffed type
//   - [Event], a record line, of kind [PartEvent], [OutputEvent], [WarningEvent], or
//     [ErrorEvent], with its [Span]
//   - [Manifest], a run's manifest, with its [Build]
package schema

// Version is the schema's version, which a run directory and every run manifest name. It
// includes dispatch's shapes at dispatch.Version "1".
const Version = "2"
