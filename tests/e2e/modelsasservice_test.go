package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type ModelsAsServiceTestCtx struct {
	*ComponentTestCtx
}

const (
	modelsAsServiceFieldName = "modelsAsService"

	maasGatewayNamespace = modelsasservice.DefaultGatewayNamespace
)

func modelsAsServiceTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewSubComponentTestCtx(t, &componentApi.ModelsAsService{}, componentApi.KserveKind, modelsAsServiceFieldName)
	require.NoError(t, err)

	componentCtx := ModelsAsServiceTestCtx{
		ComponentTestCtx: ct,
	}

	// Enable the parent component (KServe) so the DSC reconciliation path
	// that creates the maas-parameters ConfigMap is exercised.
	componentCtx.EnsureParentComponentEnabled(t)

	// Patch the DSC to enable MaaS without waiting for the ModelsAsServiceReady
	// condition, since maas-controller requires infrastructure (PostgreSQL, Gateway)
	// that is not set up in this lightweight test. The ConfigMap under test is
	// created by the operator's AppendOperatorInstallManifests during DSC
	// reconciliation, independently of maas-controller health.
	componentCtx.enableMaaSInDSCSpec(t)

	testCases := []TestCase{
		{"Validate maas-parameters ConfigMap payload-processing-namespace", componentCtx.ValidateMaaSParametersPayloadProcessingNamespace},
	}

	RunTestCases(t, testCases)
}

// enableMaaSInDSCSpec patches the DSC to set MaaS managementState to Managed
// and waits only for the spec to be persisted (not for the component to become Ready).
func (tc *ModelsAsServiceTestCtx) enableMaaSInDSCSpec(t *testing.T) {
	t.Helper()

	parentComponentName, _ := getComponentNameFromKind(tc.ParentKind)
	subComponentName := tc.SubComponentFieldName

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.%s.managementState = "%s"`, parentComponentName, subComponentName, operatorv1.Managed)),
		WithCondition(
			jq.Match(`.spec.components.%s.%s.managementState == "%s"`, parentComponentName, subComponentName, operatorv1.Managed),
		),
	)
}

// ValidateMaaSParametersPayloadProcessingNamespace verifies that the maas-parameters ConfigMap
// sets payload-processing-namespace to the application namespace, not the gateway namespace.
// This is the regression test for RHOAIENG-59726: the EnvoyFilter cluster_name FQDN must
// reference the namespace where payload-processing actually runs.
func (tc *ModelsAsServiceTestCtx) ValidateMaaSParametersPayloadProcessingNamespace(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Checking maas-parameters ConfigMap in namespace %s has correct payload-processing-namespace", tc.AppsNamespace)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      "maas-parameters",
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.data["payload-processing-namespace"] == "%s"`, tc.AppsNamespace),
				jq.Match(`.data["payload-processing-namespace"] != "%s"`, maasGatewayNamespace),
				jq.Match(`.data["app-namespace"] == "%s"`, tc.AppsNamespace),
			),
		),
		WithCustomErrorMsg(
			"maas-parameters ConfigMap payload-processing-namespace must be %q (app namespace), not %q (gateway namespace)",
			tc.AppsNamespace, maasGatewayNamespace,
		),
	)
}
