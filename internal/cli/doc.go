// Package cli is the envelope reader: one definition of how docproc runs the system tools,
// docproc-ooxml, docproc-mail, and docproc-onenote, and reads the common envelope they write
// (context/layers.md, "Common envelope"), so every reader reads a tool the same way. It also holds
// the helpers the readers, the document package's renderers, and audio's and media's FFmpeg runs
// share.
//
// [Read] runs a tool with its stdout and stderr on pipes. stdout is JSON Lines: Read yields each
// line's kind and raw JSON, the reader decoding it, and checks that the first line is the
// reader's kind and carries [Schema]. stderr is parsed as it arrives: `<tool>: warning: <where>:
// <what>` is split at its first three ": " and unescaped into a dispatch.Warning, yielded among
// the lines and always before the read's final error, and the run's message is the reason of the
// last `<tool>: error: <reason>` line. The exit codes are the envelope's: 0 read; 1 stdout or media
// not written; 2 usage; 3 refused before any output; 4 a fault of the tool's own. A run that
// exits nonzero, or dies by a signal, is one [*ExitError], which reuses the tool package's
// *tool.ExitError for the run's facts. A refusal's reason maps to a stage's class by
// [RefusalClass], the one mapping, and a line's media name resolves by [ResolveMedium], the
// envelope's media name rule. Its tests read the envelope's shared cases,
// tools/envelope/cases.json, in place.
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
