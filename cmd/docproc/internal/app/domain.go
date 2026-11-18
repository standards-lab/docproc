package app

import (
	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
)

// defineDomain defines the domain nodes on g into n: the services docproc's commands are built
// on, each constructed from the docproc library's packages over the infrastructure nodes it Uses.
// None is defined yet; the run command brings the first.
func defineDomain(g *graph.Graph, n *Nodes) {}

// mountDomain mounts docproc's commands at root, each from its command package under internal/,
// over the nodes it passes, each command declaring with Use the nodes it reads, so a run builds
// only what its command needs. None is mounted yet; version, which declares no nodes, is mounted
// by [New] itself.
func mountDomain(root *cli.Command, n *Nodes) {}
