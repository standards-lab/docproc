// Package report is docproc's run report: static HTML pages, rendered with html/template from a
// run's manifest and record alone, written into the run's report directory,
// runs/<run>/report/, which rundir.Dir.ReportDir names.
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
// part. A content's tree is the first input's, by path, holding it.
//
// Rendering reads no clock: every time is the record's, in UTC, so the same run directory renders
// the same bytes in any local zone. An unended run renders the lines its record holds, marked not
// ended.
//
// The package exports:
//
//   - [Render], which renders a run of a run directory into its report directory, replacing
//     earlier pages whole
//   - [IndexPage], the index page's file name
package report
