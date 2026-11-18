// Package integration is docproc's built-binary integration tier: the harness that runs the
// command as the built binary, and, under the integration build tag, the suite that asserts its
// behavior through the interfaces a terminal uses: its arguments, its output streams, its exit
// code, its signals, and the run directory it writes, read back through rundir. It was started
// from go-cli-sdk-template's template/v0.3.0 integration package and is built on go-core's
// processtest; nothing in the runtime exists for the tests' sake.
//
// The harness builds and starts the binary itself, since processtest falls short of this tier: its
// Main builds the program to a path it keeps to itself, its Launch starts the program with no
// arguments, and its Run bounds a run by processtest.Failsafe, which a run over every fixture can
// outlast. The harness builds the binary as Main does, starts it with its arguments, and bounds
// each run by [Bound], which such a run fits, so a test can kill docproc partway through a run it
// started.
//
//   - [Main] is the suite's TestMain: it clears the DOCPROC_ environment and builds the command
//     once per run, with the race detector so the program runs under it too.
//   - [Start] starts the built program with its arguments as a [Process], which a test polls with
//     [Process.Exited], waits on with [Process.Wait], or kills with [Process.Kill], each run
//     bounded by [Bound]; [Run] runs it once and returns its processtest.Result.
//   - [Fixtures] returns the repository's committed fixtures, the stages tier's golden's list,
//     read from the repository tree the module sits in by [ReadFixtures]: absent, matching
//     [ErrNoFixtures], the test skips, naming what is missing; a file the list names that is
//     missing fails it.
//
// The tier runs from `mise run test:integration`, over the docproc tools built from the checkout,
// first on PATH, and the host's other system tools. The harness is untagged, so the unit tier
// type-checks it and tests its fixture list on every check; the suite files carry the integration
// tag.
package integration
