// Package tooltest holds the unit tier's stand-in tool, which every test package that runs a
// tool runs in place of the real program, and the one helper that reads the system tools' data
// under tools/ in place. No unit test runs a real program or reads ~/docs: a package's tests
// build their Tools over stand-in links, and the integration tier, under the integration build
// tag, runs the real tools.
//
// A stand-in is the test binary itself, linked under a program's name. Link makes the link and
// writes the stand-in's settings into a file the link names, beside it, so a run of the link finds
// its own settings, and tests with links of their own run in parallel, with no variable of the
// process's environment between them. A package's TestMain calls Main with the programs its
// stand-ins act as: in a process started through a link, Main logs the run, then runs the program
// the link's settings name and exits with its code; in the test binary itself, it sets a zone
// that isn't UTC, so a time a package returns or writes in the local zone fails its test, and runs
// the tests. Every package's TestMain calls Main, with no programs when it runs no tool.
//
// The tools' goldens and the envelope's shared cases are read in place under tools/, never copied.
// tools/ holds a go.mod of its own, which keeps it out of the base module's zip, so a test run
// from the zip finds no tools tree: Tools skips its test then, naming the tree it didn't find; in
// the repository, where the tree is present, a file Tools doesn't find fails the test.
//
// It builds untagged, so the unit tier proves it, and no released file imports it.
//
// Its exported identifiers:
//
//   - [Main] runs a package's tests, or, in a process started through a link, the stand-in.
//   - [Program] is what a stand-in does as one program, given its [Run].
//   - [Run] is one run of a stand-in: its program, its link, its arguments, and its settings.
//   - [Link] links the test binary as a program, with its settings, and returns the link's path.
//   - [Set] replaces a link's settings.
//   - [Runs] returns the runs a link's log holds, in order; [Logged] is one of them.
//   - [Zone] is the zone Main makes local, which isn't UTC.
//   - [PNG] is a stand-in's PNG, whole by its chunks, naming a text.
//   - [GSPages] returns the pages a gs run's arguments name, its page list's or its first page.
//   - [Tools] returns the path of a file or directory under tools/, skipping when the tree is
//     absent and failing when the file is.
package tooltest
