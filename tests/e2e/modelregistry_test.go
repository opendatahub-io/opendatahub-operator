package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
)

type ModelRegistryTestCtx struct {
	*ComponentTestCtx
}

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.ModelRegistry{})
	require.NoError(t, err)

	componentCtx := ModelRegistryTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate deployment", componentCtx.ValidateDeployment},
		// TODO: Disabled until these tests have been hardened (RHOAIENG-27721)
		// {"Validate deployment deletion recovery", componentCtx.ValidateDeploymentDeletionRecovery},
		// {"Validate configmap deletion recovery", componentCtx.ValidateConfigMapDeletionRecovery},
		// {"Validate service deletion recovery", componentCtx.ValidateServiceDeletionRecovery},
		// {"Validate serviceaccount deletion recovery", componentCtx.ValidateServiceAccountDeletionRecovery},
		// {"Validate rbac deletion recovery", componentCtx.ValidateRBACDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateSpec checks the ModelRegistry spec against the DataScienceCluster instance.
func (tc *ModelRegistryTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ModelRegistry, types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}),

		// Validate that the registriesNamespace in ModelRegistry matches the corresponding value in DataScienceCluster spec.
		WithCondition(jq.Match(`.spec.registriesNamespace == "%s"`, dsc.Spec.Components.ModelRegistry.RegistriesNamespace)),

		// Validate that the modelCatalog state in ModelRegistry matches the corresponding value in DataScienceCluster spec.
		WithCondition(jq.Match(`.spec.modelCatalog.managementState == "%s"`, dsc.Spec.Components.ModelRegistry.ModelCatalog.ManagementState)),
	)
}

// ValidateCRDReinstated ensures that required CRDs are reinstated if deleted.
func (tc *ModelRegistryTestCtx) ValidateCRDReinstated(t *testing.T) {
	t.Helper()

	crds := []CRD{
		{Name: "modelregistries.modelregistry.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

// ValidateDeployment checks the ModelRegistry deployment against the DataScienceCluster instance.
func (tc *ModelRegistryTestCtx) ValidateDeployment(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	mcEnabled := "false"
	if dsc.Spec.Components.ModelRegistry.ModelCatalog.ManagementState == operatorv1.Managed {
		mcEnabled = "true"
	}

	// Validate that the ENABLE_MODEL_CATALOG env var in ModelRegistry matches the corresponding value in DataScienceCluster spec.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace, Name: modelregistry.LegacyComponentName + "-controller-manager"}),
		WithCondition(jq.Match(`.spec.template.spec.containers[] | select(.name=="manager") | .env[] | select(.name == "ENABLE_MODEL_CATALOG") | .value == %q`, mcEnabled)),
	)
}
