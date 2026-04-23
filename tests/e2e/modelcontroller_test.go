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

const (
	// wvaDeploymentName is the name of the WVA controller manager deployment.
	wvaDeploymentName = "workload-variant-autoscaler-controller-manager"
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
		{"Validate WVA deployment when enabled", componentCtx.ValidateWVADeployment},
		{"Validate WVA ConfigMap is configurable and recovers from deletion", componentCtx.ValidateWVAConfigMapUserConfigurable},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
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

	skipUnless(t, Smoke, Tier1)

	// Ensure Kserve and ModelRegistry components are set as Managed in DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.KserveComponentName, operatorv1.Managed),
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.ModelRegistryComponentName, operatorv1.Managed),
			),
		),
	)

	// Ensure ModelController resources are deployed.
	tc.verifyResourcesDeployed()

	// Ensure ModelController condition matches the expected status in the DataScienceCluster.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionTrue)),
	)
}

// ValidateWVADeployment validates that the WVA deployment exists when WVA is set to Managed.
func (tc *ModelControllerTestCtx) ValidateWVADeployment(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	// Ensure WVA is set to Managed in DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.components.%s.wva.managementState = "%s"`, componentApi.KserveComponentName, operatorv1.Managed),
			),
		),
	)

	// Ensure WVA deployment exists and is ready.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      wvaDeploymentName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.ModelController.Kind),
				jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
			),
		),
	)
}

// ValidateWVAConfigMapUserConfigurable validates user can update CM workload-variant-autoscaler-saturation-scaling-config
// which wont get reconciled by Operator. Also validates that if deleted, it gets recreated with default data.
func (tc *ModelControllerTestCtx) ValidateWVAConfigMapUserConfigurable(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	const (
		wvaConfigMapName          = "workload-variant-autoscaler-saturation-scaling-config"
		queueSpareTriggerOriginal = "3" // we can start with this accordig to current default and update or remove test later
		queueSpareTriggerModified = "2"
	)

	// Ensure WVA ConfigMap exists first with original value
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      wvaConfigMapName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			jq.Match(`.data.default | contains("queueSpareTrigger: %s")`, queueSpareTriggerOriginal),
		),
	)

	// Modify the ConfigMap data to change queueSpareTrigger from 3 to 2
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      wvaConfigMapName,
			Namespace: tc.AppsNamespace,
		}),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.data.default |= sub("queueSpareTrigger: %s"; "queueSpareTrigger: %s")`, queueSpareTriggerOriginal, queueSpareTriggerModified),
			),
		),
	)

	// Verify the change persists and is NOT reconciled back by the operator
	// The ConfigMap should remain modified, proving it's user-configurable
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      wvaConfigMapName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			jq.Match(`.data.default | contains("queueSpareTrigger: %s")`, queueSpareTriggerModified),
		),
	)

	// Test if CM gets deleted by user it gets recreated by the operator with default
	tc.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      wvaConfigMapName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			jq.Match(`.data.default | contains("queueSpareTrigger: %s")`, queueSpareTriggerOriginal),
		),
	)
}

// ValidateComponentDisabled validates that the components are disabled.
func (tc *ModelControllerTestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	// Ensure Kserve, WVA, and ModelRegistry components are set as Removed in DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.components.%s.wva.managementState = "%s"`, componentApi.KserveComponentName, operatorv1.Removed),
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.KserveComponentName, operatorv1.Removed),
				testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.ModelRegistryComponentName, operatorv1.Removed),
			),
		),
	)

	// Ensure ModelController resources are removed.
	tc.verifyResourcesNotDeployed()

	// Ensure ModelController condition matches the expected status in the DataScienceCluster.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionFalse)),
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

	// Ensure WVA deployment is removed when component is disabled
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      wvaDeploymentName,
			Namespace: tc.AppsNamespace,
		}),
	)
}
