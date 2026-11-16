// Package dispatchtest holds dispatch's store contracts as conformance suites, and the fixture
// stage, for every implementation of dispatch.Index and dispatch.Content to run, the module's own
// and a provider's, and for a host's tests to dispatch over. It imports dispatch, route, and
// inspect, and the standard library alone, and runs no tool.
//
// Index runs the index contract against a new index for each case: a claim, its token, its lease,
// a renewal, and a takeover once a lease runs out; the log appended, recorded, and read back in
// append order, less its local fields; a release or a takeover discarding the old token's
// appends; a wait on a claim; a drop; and a read of a log that its entry's replacement or drop
// ends with an error. Content runs the content store contract: a put all or nothing, a put of a
// present content that reads nothing, an open that misses with fs.ErrNotExist, and a name that
// isn't a SHA-256 refused.
//
// Its exported identifiers:
//
//   - [Index] and [Content], the conformance suites, each over a function that makes a new,
//     empty store for a test.
//   - [Stage], the fixture stage, made by [NewStage] over the content its directives find, and
//     its [Stage.Calls].
package dispatchtest
