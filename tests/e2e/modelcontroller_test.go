package e2e_test

import (
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type ModelControllerTestCtx struct {
	*ComponentTestCtx
}

func modelControllerTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.ModelController{})
	require.NoError(t, err)

	componentCtx := ModelControllerTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate deployment deletion recovery", componentCtx.ValidateDeploymentDeletionRecovery},
		{"Validate configmap deletion recovery", componentCtx.ValidateConfigMapDeletionRecovery},
		{"Validate service deletion recovery", componentCtx.ValidateServiceDeletionRecovery},
		{"Validate serviceaccount deletion recovery", componentCtx.ValidateServiceAccountDeletionRecovery},
		{"Validate rbac deletion recovery", componentCtx.ValidateRBACDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Increase the global eventually timeout
	reset := componentCtx.OverrideEventuallyTimeout(ct.TestTimeouts.mediumEventuallyTimeout, ct.TestTimeouts.defaultEventuallyPollInterval)
	defer reset() // Make sure it's reset after all tests run

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateComponentEnabled validates that the components are enabled with the correct states.
func (tc *ModelControllerTestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	// Define the test cases for checking component states
	testCases := []TestCase{
		{"ModelMeshServing enabled", func(t *testing.T) {
			t.Helper()
			tc.ValidateComponentDeployed(operatorv1.Managed, operatorv1.Removed, operatorv1.Removed, metav1.ConditionTrue)
		}},
		{"Kserve enabled", func(t *testing.T) {
			t.Helper()
			tc.ValidateComponentDeployed(operatorv1.Removed, operatorv1.Managed, operatorv1.Removed, metav1.ConditionTrue)
		}},
		{"Kserve and ModelMeshServing enabled", func(t *testing.T) {
			t.Helper()
			tc.ValidateComponentDeployed(operatorv1.Managed, operatorv1.Managed, operatorv1.Removed, metav1.ConditionTrue)
		}},
		{"ModelRegistry enabled", func(t *testing.T) {
			t.Helper()
			tc.ValidateComponentDeployed(operatorv1.Managed, operatorv1.Managed, operatorv1.Managed, metav1.ConditionTrue)
		}},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateComponentDisabled validates that the components are disabled.
func (tc *ModelControllerTestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	t.Run("Kserve ModelMeshServing and ModelRegistry disabled", func(t *testing.T) {
		t.Helper()
		tc.ValidateComponentDeployed(operatorv1.Removed, operatorv1.Removed, operatorv1.Removed, metav1.ConditionFalse)
	})
}

// ValidateComponentDeployed validates that the components are deployed with the correct management state.
func (tc *ModelControllerTestCtx) ValidateComponentDeployed(
	modelMeshState operatorv1.ManagementState,
	kserveState operatorv1.ManagementState,
	modelRegistryState operatorv1.ManagementState,
	status metav1.ConditionStatus,
) {
	// Ensure the components are updated with the correct states in DataScienceCluster.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.ModelMeshServingComponentName, modelMeshState),
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.KserveComponentName, kserveState),
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.ModelRegistryComponentName, modelRegistryState),
			),
		),
	)

	// Verify resources based on the desired status.
	if status == metav1.ConditionTrue {
		tc.verifyResourcesDeployed()
	} else {
		tc.verifyResourcesNotDeployed()
	}

	// Ensure ModelController condition matches the expected status in the DataScienceCluster.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, status)),
	)
}

// verifyResourcesDeployed ensures that the required resources are deployed.
func (tc *ModelControllerTestCtx) verifyResourcesDeployed() {
	// Ensure ModelController and related Deployments are deployed.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ModelController, types.NamespacedName{Name: componentApi.ModelControllerInstanceName}),
		WithCondition(
			And(
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
				jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady),
			),
		),
	)

	// Ensure the ModelController deployment exists.
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)
}

// verifyResourcesNotDeployed ensures that the required resources are not deployed.
func (tc *ModelControllerTestCtx) verifyResourcesNotDeployed() {
	// Ensure that the components are not deployed
	tc.EnsureResourceGone(WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}))
	tc.EnsureResourceGone(WithMinimalObject(gvk.ModelMeshServing, types.NamespacedName{Name: componentApi.ModelMeshServingInstanceName}))
	tc.EnsureResourceGone(WithMinimalObject(gvk.ModelController, types.NamespacedName{Name: componentApi.ModelControllerInstanceName}))
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)
}
