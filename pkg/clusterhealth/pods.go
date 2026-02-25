package clusterhealth

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runPodsSection collects pod phases and container states across configured namespaces.
func runPodsSection(ctx context.Context, c client.Client, ns NamespaceConfig) SectionResult[PodsSection] {
	var out SectionResult[PodsSection]
	_ = ctx
	_ = c
	_ = ns
	// Stub: return empty data
	out.Data.ByNamespace = make(map[string][]PodInfo)
	return out
}
