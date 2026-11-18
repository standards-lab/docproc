// Package run is docproc's run command: docproc run <path>... --out <dir>. It collects its inputs
// from its path arguments, then builds docproc's stages over the host's tools through the stage
// builder its [Config] holds, printing the tools and the stage table, and dispatches every input on
// the dispatcher's pool over the run directory --out names, through package rundir: it begins a
// run, writes one record line, a schema.Event, per pool event, in order, and ends the run.
//
// A file argument is one input, and a directory yields each regular file under it, in lexical
// order, each file once. A named argument that is a symbolic link is followed; a link under a
// directory is not, and is no input. An argument that is neither a file nor a directory, or
// arguments that yield no input, fail the run before any tool is probed. --workers, at least 1
// with no ceiling, is how many inputs are dispatched at once, the host's CPUs by default; --exclude
// <route>, repeatable, leaves a route's stage out, and a route that isn't one of the stage
// table's is a usage error.
//
// The run's manifest names its docproc build, its workers, its path arguments, how many inputs
// they named, the tools the stage builder tried, and the stage table. Each output's content is in
// the run directory's content store, put there by the dispatcher, and a part the index has seen
// replays its log, its children's included, through the dispatcher, so every line is this run's
// own. An input's own part line, its last, carries its file's metadata, through os.Stat and
// statx's birth time where the kernel gives one, and its type as net/http.DetectContentType
// sniffs it, and its time, from its first event to its last, both taken on the pool's loop. A
// quarantined part's file is kept in the run directory, and its line names the kept file
// whenever it exists, whether this run or an earlier one kept it. An input whose dispatch stopped
// before its own event is an error line.
//
// As each input's own event arrives, the terminal prints one line: its path, route, outcome,
// outputs, children, and time, a failed part's error cut to its first line and at most 400 runes;
// the record keeps it whole. The closing summary counts the inputs by route and outcome, names
// the run, and, last, counts the parts that failed and the inputs stopped with an error, which
// leave the exit status 0. A failure of the index or the content store, a *dispatch.StoreError,
// stops the run, as the context ending does: the run's manifest ends with the error, and the
// command exits 1.
//
// With --report, once the summary is printed, the command renders the run's report through
// package report and prints its index's path, as docproc report does: after a run that ended, and
// after a failure of the index or the content store, which still exits 1; never after a
// cancellation. A render that fails after a run that ended exits 1, its error saying docproc
// report renders the pages again.
//
// The package exports:
//
//   - [Commands], which returns the run command over the [Config] node it declares
//   - [Config], what a run runs under, and [StageBuilder], which builds its stages
package run
