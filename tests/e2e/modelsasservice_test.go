package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

type ModelsAsServiceTestCtx struct {
	*ComponentTestCtx
}

const (
	// Subcomponent field name in JSON (matches the field name in KserveCommonSpec).
	// This must match the JSON tag in KserveCommonSpec.ModelsAsService.
	modelsAsServiceFieldName = "modelsAsService"

	// Gateway constants from modelsasservice package.
	maasGatewayNamespace = modelsasservice.DefaultGatewayNamespace
	maasGatewayName      = modelsasservice.DefaultGatewayName

	// Gateway class for OpenShift default ingress controller.
	// Reference: https://github.com/opendatahub-io/models-as-a-service/blob/main/deployment/base/networking/maas/maas-gateway-api.yaml
	maasGatewayClassName = "openshift-default"
)

func modelsAsServiceTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewSubComponentTestCtx(t, &componentApi.ModelsAsService{}, componentApi.KserveKind, modelsAsServiceFieldName)
	require.NoError(t, err)

	componentCtx := ModelsAsServiceTestCtx{
		ComponentTestCtx: ct,
	}

	// Setup: Create the MaaS Gateway before running tests
	componentCtx.createMaaSGateway(t)

	// Note: per e2e convention, do not cleanup resources; leave state for debugging.

	testCases := []TestCase{
		{"Validate subcomponent enabled", componentCtx.ValidateSubComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		// TODO: Re-enable after https://issues.redhat.com/browse/RHOAIENG-47119 resolved
		// {"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate subcomponent releases", componentCtx.ValidateSubComponentReleases},
		// TODO: Re-enable after https://issues.redhat.com/browse/RHOAIENG-47119 resolved
		// {"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate subcomponent disabled", componentCtx.ValidateSubComponentDisabled},
	}

	RunTestCases(t, testCases)
}

// createMaaSGateway creates the maas-default-gateway Gateway resource required by ModelsAsService.
// The Gateway is based on the MaaS Gateway definition from:
// https://github.com/opendatahub-io/models-as-a-service/blob/main/deployment/base/networking/maas/maas-gateway-api.yaml
func (tc *ModelsAsServiceTestCtx) createMaaSGateway(t *testing.T) {
	t.Helper()
	t.Logf("Creating MaaS Gateway: %s/%s", maasGatewayNamespace, maasGatewayName)

	// First, ensure the namespace exists
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: maasGatewayNamespace}),
		WithCustomErrorMsg("Failed to create/ensure namespace %s for MaaS Gateway", maasGatewayNamespace),
	)

	// Get the cluster domain for the Gateway hostname
	clusterDomain, err := cluster.GetDomain(tc.Context(), tc.Client())
	require.NoError(t, err, "Failed to get cluster domain")

	hostname := fmt.Sprintf("maas.%s", clusterDomain)
	t.Logf("Using hostname for MaaS Gateway: %s", hostname)

	// Create the Gateway resource
	// Using testf.Transform to build the Gateway spec dynamically
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      maasGatewayName,
			Namespace: maasGatewayNamespace,
		}),
		WithMutateFunc(testf.TransformPipeline(
			// Set labels
			testf.Transform(`.metadata.labels = {
				"app.kubernetes.io/name": "maas",
				"app.kubernetes.io/instance": "%s",
				"app.kubernetes.io/component": "gateway",
				"opendatahub.io/managed": "false"
			}`, maasGatewayName),
			// Set annotations
			testf.Transform(`.metadata.annotations = {"opendatahub.io/managed": "false"}`),
			// Set the GatewayClass
			testf.Transform(`.spec.gatewayClassName = "%s"`, maasGatewayClassName),
			// Set the HTTP listener
			testf.Transform(`.spec.listeners = [
				{
					"name": "http",
					"hostname": "%s",
					"port": 80,
					"protocol": "HTTP",
					"allowedRoutes": {
						"namespaces": {
							"from": "All"
						}
					}
				}
			]`, hostname),
		)),
		WithCustomErrorMsg("Failed to create MaaS Gateway %s/%s", maasGatewayNamespace, maasGatewayName),
	)

	// Wait for the Gateway to exist
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      maasGatewayName,
			Namespace: maasGatewayNamespace,
		}),
		WithCustomErrorMsg("MaaS Gateway %s/%s should exist", maasGatewayNamespace, maasGatewayName),
	)

	t.Logf("MaaS Gateway %s/%s created successfully", maasGatewayNamespace, maasGatewayName)
}
