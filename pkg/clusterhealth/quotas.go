package clusterhealth

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runQuotasSection collects resource quota usage across configured namespaces.
func runQuotasSection(ctx context.Context, c client.Client, ns NamespaceConfig) SectionResult[QuotasSection] {
	var out SectionResult[QuotasSection]
	_ = ctx
	_ = c
	_ = ns
	// Stub: return empty data
	out.Data.ByNamespace = make(map[string][]ResourceQuotaInfo)
	return out
}
