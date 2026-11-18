// Package rundir is cmd/docproc's run directory, read and written, under schema "2", whose shapes
// are package schema's:
//
//	rundir.json                    the run directory's schema, {"schema": "2"}
//	index/                         dispatch's FileIndex, shared by every run
//	content/<ab>/<sha256>          the content store, dispatch's FileContent, each content at
//	                               dispatch.ContentFile's layout, which [ContentPath] joins
//	quarantine/<sha256>            a quarantined part's file, kept once
//	runs/<run>/manifest.json       each run's schema.Manifest
//	runs/<run>/record.jsonl        and its record, one schema.Event per line
//	.work-*                        a running run's working directories, the caller's
//
// A run's id is a version 7 UUID, in its lowercase form, so the ids sort by when each run began.
// The content store's directory and a content's path in it are part of schema "2": a reader that
// isn't Go, such as a report's page linking a content's file, follows them.
//
// # One handle
//
// [Open] opens a run directory to read it, creating nothing; [Create] opens one to write it,
// making the directory, its rundir.json, the index, and the content store when they are absent.
// Each refuses a run directory of another schema, naming both versions, as it does one holding
// runs/ with no rundir.json, which schema "1" wrote; a root that is no run directory fails Open
// with an error matching fs.ErrNotExist. Every method that takes a run's id refuses one that isn't
// a run of the directory, a directory under runs/ named by a version 7 id and holding its
// manifest, so no caller checks an id first.
//
// # One writer
//
// One process writes a run directory: nothing here locks it, and [Dir.Begin] removes the .work-*
// directories at its root, which a killed run left. Every file is written whole, to a temporary
// name in its directory, synced, and renamed, but the record: it is appended, never renamed, each
// line written through as one write, with no fsync, as [Run.Write] makes it, so a run killed
// before [Run.End] loses at most its last line. A run whose manifest has no end is unended: it is
// still running, or it was killed. [Dir.Record] drops an unended run's last line when it has no
// newline, and says the run has not ended, matching [ErrNotEnded]; [Dir.Latest] names only a run
// that ended.
//
// The package exports:
//
//   - [Dir], from [Open] or [Create]: [Dir.Runs], the runs in the order they began,
//     [Dir.Latest], the latest that ended, [Dir.Manifest] and [Dir.Record], a run's manifest
//     and its lines, [Dir.Content], the content store, and [Dir.Kept], a quarantined part's kept
//     file
//   - [ContentPath], a content's file relative to the run directory
//   - [ErrNotEnded], which a record of an unended run yields last
//   - the writer's, on a Dir from Create: [Dir.Index], the index, [Dir.Begin], which begins a
//     [Run], and [Dir.Quarantine], which keeps a quarantined part's file
//   - [Run]: [Run.ID], its id, [Run.Write], which appends a record line, and [Run.End], which
//     records the run's end and its error
package rundir
