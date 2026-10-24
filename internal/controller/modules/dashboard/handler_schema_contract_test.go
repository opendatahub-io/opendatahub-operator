package dashboard_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"

	. "github.com/onsi/gomega"
)

// dashboardCRDSpecFields is the set of fields the Dashboard CRD
// (dashboards.components.platform.opendatahub.io/v1alpha1) declares in
// its spec. BuildModuleCR must only produce fields from this set.
//
// Source of truth: odh-dashboard api/v1alpha1/dashboard_types.go
//
// Keep in sync when the Dashboard CRD adds new spec fields.
var dashboardCRDSpecFields = map[string]bool{
	"managementState":        true,
	"components":             true,
	"notebooksNamespace":     true,
	"modelRegistryNamespace": true,
	"gateway":                true,
}

func TestBuildModuleCR_OnlyProducesCRDSchemaFields(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(
		operatorv1.Managed,
		operatorv1.Managed, "rhods-notebooks",
		operatorv1.Managed, "rhoai-model-registries",
	)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfig("gateway.example.com"))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")

	for field := range spec {
		g.Expect(dashboardCRDSpecFields).Should(HaveKey(field),
			"BuildModuleCR produced spec.%s which is not in the Dashboard CRD schema — "+
				"either add it to the CRD first (odh-dashboard api/v1alpha1/dashboard_types.go) "+
				"or remove it from BuildModuleCR", field)
	}
}
