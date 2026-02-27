package clusterhealth

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runDSCISection collects DSCI (DSCInitialization) status conditions.
func runDSCISection(ctx context.Context, c client.Client, nn types.NamespacedName) SectionResult[CRConditionsSection] {
	var out SectionResult[CRConditionsSection]
	_ = ctx
	_ = c
	_ = nn
	// Stub: return empty data
	out.Data.Name = nn.Name
	out.Data.Conditions = nil
	return out
}

// runDSCSection collects DSC (DataScienceCluster) status conditions.
func runDSCSection(ctx context.Context, c client.Client, nn types.NamespacedName) SectionResult[CRConditionsSection] {
	var out SectionResult[CRConditionsSection]
	_ = ctx
	_ = c
	_ = nn
	// Stub: return empty data
	out.Data.Name = nn.Name
	out.Data.Conditions = nil
	return out
}
