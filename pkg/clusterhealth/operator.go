package clusterhealth

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runOperatorSection collects operator deployment and pod status.
func runOperatorSection(ctx context.Context, c client.Client, op OperatorConfig) SectionResult[OperatorSection] {
	var out SectionResult[OperatorSection]
	_ = ctx
	_ = c
	_ = op
	// Stub: return empty data (Deployment nil, Pods nil/empty)
	out.Data.Pods = nil
	return out
}
