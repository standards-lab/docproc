// Package cli is the envelope reader: one definition of how docproc runs the system tools,
// docproc-ooxml, docproc-mail, and docproc-onenote, and reads the common envelope they write, so
// every reader reads a tool the same way. It also holds the helpers the readers, the document
// package's renderers, and audio's and media's FFmpeg runs share.
//
// The common envelope is the contract every docproc tool keeps, and this package holds it:
//
//   - Lines. stdout is JSON Lines and nothing else, one object per line, each naming its kind in
//     "line". The first line carries "schema", [Schema], and is the input's kind: docproc-ooxml's
//     "document", docproc-mail's "store", docproc-onenote's "notebook". A field with no value is
//     left out, and a line of a kind a reader doesn't know is kept as an unknown record, since the
//     contract grows by addition.
//   - Version. --version writes the version alone on its first line and "schema 2" on its second,
//     which [VersionPattern] matches, so a tool of another schema is refused at startup.
//   - stderr. One line each: `<tool>: warning: <where>: <what>`, and for exit codes 1 to 4
//     `<tool>: error: <reason>`. where never holds ": ", so a warning splits at its first three.
//     where and what are escaped, a backslash before a backslash, \n and \r for a line feed and a
//     carriage return, and, in where, `:\ ` for a colon and a space; [Read] inverts it.
//   - Exit codes. 0 read, skipped items included; 1 stdout or media not written; 2 usage; 3
//     refused before any output; 4 a fault of the tool's own.
//   - Refusal reasons. A reason beginning "encrypted" is an encrypted input, and docproc-onenote's
//     table-of-contents refusal is the fixed text [TableOfContents]; any other reason is prose for
//     a person, and nothing else is read from it.
//   - Media names. Given a media directory, a tool writes each distinct payload there once, before
//     the line that names it, as the lowercase hex SHA-256 of its bytes, then "." and the stored
//     extension, its case kept, when that is 1 to 8 ASCII letters and digits. A line names it in
//     "file", and the Markdown links it by that name.
//
// Each tool's README, tools/<format>/README.md, states its own lines and the rest of its contract.
// tools/envelope/cases.json holds the envelope's cases, which this package's tests, the Rust
// tools', and docproc-ooxml's read in place.
//
// [Read] runs a tool with its stdout and stderr on pipes. Read yields each line's kind and raw
// JSON, the reader decoding it, and checks that the first line is the reader's kind and carries
// [Schema]. stderr is parsed as it arrives: each warning is unescaped into a dispatch.Warning,
// yielded among the lines and always before the read's final error, and the run's message is the
// reason of the last error line. A run that exits nonzero, or dies by a signal, is one
// [*ExitError], which reuses the tool package's *tool.ExitError for the run's facts. A refusal's
// reason maps to a stage's class by [RefusalClass], the one mapping, and a line's media name
// resolves by [ResolveMedium], the media name rule.
//
// Its exported identifiers:
//
//   - [Schema] is the envelope's schema, "2"; [VersionPattern] matches a schema "2" --version,
//     for each reader's tool.Spec.
//   - [Read] runs a tool, as a [Run] describes, yielding each [Line]; [ErrSchema] is the error of
//     a first line of another kind or schema.
//   - [ExitError] is a run that exited nonzero or died by a signal; [ErrNotWritten], [ErrUsage],
//     [ErrRefused], and [ErrFailed] are exit codes 1 to 4, ErrFailed a signal's too.
//   - [Outcome] returns an exit code's error.
//   - [OwnFault] reports whether a signal is one a tool dies by through its own fault, the
//     content's failure, for stages' classification and document's shared gs processes alike.
//   - [RefusalClass] maps a refusal's reason to dispatch's class: one beginning "encrypted" to
//     dispatch.ErrEncrypted, onenote's [TableOfContents] to dispatch.ErrReadElsewhere;
//     [ExitError.Refusal] gives a run's, and the ExitError matches it.
//   - [ResolveMedium] resolves a line's media name to its [Medium], its file and SHA-256.
//   - [LimitedBuffer] keeps the first bytes written to it, for a tool's output read only in
//     part.
//   - [Spool] copies an input into a file in a working directory, for a tool that reads a path.
//   - [LineWriter] splits a program's stderr into lines as it arrives, keeping the last, each at
//     most [MaxLine], for audio's and media's FFmpeg runs.
//   - [WriteTemp] writes bytes to a hidden temporary file in a directory, for a file renamed into
//     place once whole, as audio's windows and media's windows and frames are.
//   - [UTC] is the decoding option that makes every time a reader decodes UTC.
package cli
