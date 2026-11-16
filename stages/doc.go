// Package stages wires docproc's stages: it builds the tools each of processing's ten routes
// needs, each found through a [Lookup] the caller gives and checked against processing's
// minimum, and the route's stage over them, the way processing's own tests wire them. A tool it
// can't build, missing or too old, leaves out the stages that need it, and a caller may exclude a
// route; either way the route has no processor yet. [Set.WriteTable] prints what was built.
//
// The package exports:
//
//   - [Lookup], how a program is found on the host
//   - [Build], which builds the tools and stages, and [Set], what it built
//   - [Tool], one tool Build tried, and [Row], one route's stage
//   - [State] and its values, [Wired], [Missing], and [Excluded]
//   - [Routes], the ten routes in table order, and [ParseRoutes], which refuses an unknown one
//   - [Set.WriteTable], the tools and stages as tables
package stages
