// Package report is docproc's run report and its command, docproc report <run-dir> [--run <id>]:
// static HTML pages, rendered with html/template, written into a run's report directory,
// runs/<run>/report/, which rundir.Dir.ReportDir names. Its model, the pages and what they
// list, comes from the run's manifest and record alone; the pages then show the outputs from the
// run directory's content store and name its kept files.
//
// The report has an index page, index.html, and one page per distinct content the run's inputs
// hold, named by its SHA-256, <sha256>.html. The index lists every input, sorted by path, an
// input stopped with an error before its own event among them: its path, route, outcome, and
// time, and a link to its content's page where it has one; an input stopped before its own event
// has no SHA-256, so it has none. A content's page lists every input holding the content, each
// with its file metadata, outcome, and time, then the content's part tree and its outputs, once.
//
// The tree follows the record's part IDs: a part is a child of the part its parent ID names, when
// that part had its own line, and stays at the top of the tree otherwise, beside the input's own
// part. A content's tree is the first input's, by path, holding it. The input's own part heads
// the tree, and the other parts keep the order of their own lines, the order the stages finished
// them. A part with outputs or warnings but no line of its own shows as its lines name it, with
// no outcome. A part's lines repeated after its own line, as when a replay whose subtree the
// index no longer held whole prepares it again, start it afresh, so the part shows its last lines
// once.
//
// The index's header names the run, its start and end, or that it has not ended, the docproc build,
// its workers, its path arguments, how many inputs they named, the tools, the stage table, and the
// error that stopped the run; a summary counts the inputs, and their parts, by route and outcome,
// the routes in the stage table's order, any other after them by name, and each route's outcomes by
// name. A content's page links each input's source by its absolute path, as a file URL, and shows
// each part's record metadata: its ID, name, size, SHA-256, content type, route, stage, outcome,
// flags, warnings, its outputs' meta, and, for a quarantined part, its kept file in the run
// directory, rundir.Dir.Kept's, named and not linked.
//
// A text, Markdown, or record output shows inline, whole, read from the content store, Markdown as
// its source; every text is escaped, a byte that isn't UTF-8 shown as U+FFFD. A PNG or JPEG image
// shows as an img and a WAV window as an audio player, each at its content's file, and any other
// output is linked. An output whose content the store misses says so, as does one whose SHA-256
// isn't 64 lowercase hex digits, which names no page, content, or kept file, so no link or read
// reaches outside the run directory. Every link into the run directory is relative to the report
// directory, so the run directory moves or archives whole. The pages load nothing from elsewhere:
// no script, font, or style sheet, their style inline, following the reader's light or dark scheme.
//
// Rendering reads no clock: every time is the record's, in UTC, so the same run directory renders
// the same bytes in any local zone. An unended run renders the lines its record holds, marked not
// ended.
//
// The command renders the latest run of the run directory that ended, or the run --run names, one
// that never ended among them, and prints the index page's path. It reads the run directory
// alone, opened through rundir.Open, so it declares no node: it reads no configuration, builds no
// stage, and looks up no tool. A run directory that doesn't exist, a --run that isn't one of its
// runs, or no --run and no run that ended, exits 1, the error naming it.
//
// The package exports:
//
//   - [Commands], which returns the report command, declaring no node
//   - [Make], which renders a run's report and prints its index page's path, as the command
//     does and as docproc run --report does after its summary
//   - [Render], which renders a run of a run directory into its report directory, replacing
//     earlier pages whole
//   - [IndexPage], the index page's file name
package report
