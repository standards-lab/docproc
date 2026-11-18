package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
)

// envPrefix is the prefix of every environment variable docproc reads its configuration from:
// go-core's config convention composes the rest, such as DOCPROC_SHUTDOWN_TIMEOUT.
const envPrefix = "DOCPROC"

// defineInfrastructure defines the infrastructure nodes on g into n: the lifecycle configuration
// alone, for now. It constructs nothing. Every value is read or constructed in its node's
// constructor, which runs only when a Build reaches the node, so configuration is read only for a
// command that declares a node, never in [New] and never for a run that builds nothing.
func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
}

// newLifecycleConfig reads the lifecycle configuration, the shutdown budget, from its default and
// DOCPROC_SHUTDOWN_TIMEOUT, and validates it. The dispatcher adds this node to every Build, so a
// command that declares any node reads it.
func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}
