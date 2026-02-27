package clusterhealth

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runNodesSection collects node conditions and resource allocation.
func runNodesSection(ctx context.Context, c client.Client, ns NamespaceConfig) SectionResult[NodesSection] {
	var out SectionResult[NodesSection]
	_ = ctx
	_ = c
	_ = ns
	// Stub: return empty data (Deployment nil, Pods nil/empty)
	out.Data.Nodes = nil
	return out
}
