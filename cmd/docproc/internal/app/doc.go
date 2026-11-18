// Package app is docproc's composition root. [New] describes the program's dependencies as one
// go-core graph and builds the command tree on go-cli-sdk over it; [App.Run] runs that tree, as a
// go-cli-sdk cli.Program, on the process arguments. It was started from go-cli-sdk-template's
// template/v0.3.0. Each architecture layer has one file, which defines the layer's nodes in its
// define function and owns its mount, so the layer files are the architecture's layer list:
//
//   - app.go: [App], [New], and [Nodes], which call the layer files' define and mount functions
//     in layer order, lowest first;
//   - infrastructure.go: defineInfrastructure, the configuration, read from DOCPROC_ variables:
//     the lifecycle configuration and [Bounds], a run's bounds at docproc's starting values;
//   - domain.go: defineDomain and mountDomain, the services over docproc's library, the stage
//     builder, stages.Build over the host's tools, and the run config over it and [Bounds], and
//     the commands, from the command packages under internal/, that run them: run, from
//     internal/run;
//   - version.go: the version command, which declares no nodes.
//
// [Nodes] holds one handle per node: each define function fills its own part of one Nodes
// value, and its constructors read the lower layers' nodes from it with Use. Each field's node is
// named for the field, so no comment names it; graph_test.go checks that rule with go-core's
// graphtest.CheckNames, and checks that every leaf of the tree, as Command.Leaves yields it,
// builds its nodes without dropping an order between layers. Defining the nodes constructs
// nothing. A command declares the nodes it needs with Use; the dispatcher builds them, with the
// lifecycle configuration node, only when the command runs, starts what was built layer by
// layer, and shuts it down in reverse when the command returns. A run that builds nothing, such
// as help, a usage error, or version, reads no configuration and looks up no tool; help and a
// usage error exit 2.
//
// The App publishes its composition: [App.Graph], [App.Nodes], and [App.Root] return the graph,
// a handle on each of its nodes, and the command tree. The program itself never calls them; they
// are for a caller that observes what a run builds, Replaces a node's constructor with a
// substitute, or adds a command over the nodes, before the App runs, as internal/apptest's
// fixtures do.
package app
