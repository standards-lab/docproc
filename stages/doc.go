// Package stages wires docproc's stages: it builds the tools each of processing's ten routes
// needs, each found through a [Lookup] the caller gives and checked against processing's
// minimum, and the route's stage over them, the way processing's own tests wire them. A tool it
// can't build, missing or too old, leaves out the stages that need it, and a caller may exclude a
// route; either way the route has no processor yet. [Set.WriteTable] prints what was built.
//
// Its exported identifiers:
//
//   - [Lookup] is how a program is found on the host.
//   - [Build] builds the tools and stages, and [Set] is what it built.
//   - [Tool] is one tool Build tried, and [Row] one route's stage.
//   - [State] and its values, [Wired], [Missing], and [Excluded], are a row's state.
//   - [Routes] returns the ten routes in table order, and [ParseRoutes] parses route names,
//     refusing an unknown one with [ErrRequest], a refused request's cause.
//   - [Set.WriteTable] writes the tools and stages as tables.
package stages
