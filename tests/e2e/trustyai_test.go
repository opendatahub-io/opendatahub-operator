package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// TrustyAITestCtx extends ComponentTestCtx with TrustyAI-specific test functionality.
// TrustyAI has unique dependency requirements (KServe CRDs) that need special handling.
type TrustyAITestCtx struct {
	*ComponentTestCtx
}

const (
	trustyAIPartOfLabel                = "app.kubernetes.io/part-of"
	trustyAIModuleControllerDeployment = "trustyai-operator-module-controller-manager"
	trustyAIServiceOperatorDeployment  = "trustyai-service-operator-controller-manager"
)

// trustyAITestSuite runs the complete TrustyAI component test suite.
// This includes dependency validation tests specific to TrustyAI's KServe requirements.
func trustyAITestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewModuleTestCtx(t, gvk.TrustyAI, componentApi.TrustyAIInstanceName)
	require.NoError(t, err)

	componentCtx := TrustyAITestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate platform release", componentCtx.ValidatePlatformRelease},
		{"Validate MCP guardrails mode", componentCtx.ValidateMCPGuardrailsMode},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateUpdateDeploymentsResources verifies that the TrustyAI module operator
// reconciles changes to its managed service operator Deployment.
func (tc *TrustyAITestCtx) ValidateUpdateDeploymentsResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	deploymentName := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      trustyAIServiceOperatorDeployment,
	}

	deployment := tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, deploymentName),
	)

	replicas := ExtractAndExpectValue[int](tc.g, *deployment, `.spec.replicas`, Not(BeNil()))

	updatedReplicas := replicas + 1
	if replicas > 1 {
		updatedReplicas = 1
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Deployment, deploymentName),
		WithMutateFunc(testf.Transform(`.spec.replicas = %d`, updatedReplicas)),
		WithCondition(jq.Match(`.spec.replicas == %d`, updatedReplicas)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, deploymentName),
		WithCondition(jq.Match(`.spec.replicas == %d`, replicas)),
		WithCustomErrorMsg("TrustyAI module operator should reconcile the service operator Deployment replicas"),
	)
}

// ValidateOperandsOwnerReferences ensures TrustyAI operand deployments are owned by the TrustyAI CR.
// Overrides parent implementation to use TrustyAI-specific labels from the modular architecture.
func (tc *TrustyAITestCtx) ValidateOperandsOwnerReferences(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	if tc.IsXKS() {
		t.Skip("Skipping test because operand ownership by the TrustyAI CR is not enforced on XKS")
	}

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.Set{
				trustyAIPartOfLabel: componentApi.TrustyAIComponentName,
			}.AsSelector(),
		}),
		WithCondition(
			HaveEach(
				jq.Match(
					`any(.metadata.ownerReferences[]?; .kind == "%s" and .name == "%s")`,
					componentApi.TrustyAIKind,
					componentApi.TrustyAIInstanceName,
				),
			),
		),
		WithCustomErrorMsg("TrustyAI operand Deployments should be owned by the TrustyAI CR"),
	)
}

// ValidateComponentEnabled validates TrustyAI component with required KServe dependency.
// Unlike other components, TrustyAI requires KServe CRDs to be present before it can start.
// This method ensures the dependency is satisfied before running standard component validation.
func (tc *TrustyAITestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	// TrustyAI requires Kserve CRDs, so enable Kserve first
	tc.setKserveState(operatorv1.Managed, true)

	// Call the parent component validation
	tc.ComponentTestCtx.ValidateComponentEnabled(t)
}

// ValidateAllDeletionRecovery verifies that resources managed by both layers of the
// TrustyAI module architecture are recreated after deletion.
func (tc *TrustyAITestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	savedOpts := tc.DefaultResourceOpts
	tc.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(tc.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { tc.DefaultResourceOpts = savedOpts }()

	testCases := []TestCase{
		{"Service deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.validateTrustyAIResourceDeletionRecovery(t, gvk.Service)
		}},
		{"Deployment deletion recovery", tc.validateTrustyAIDeploymentDeletionRecovery},
	}

	RunTestCases(t, testCases)
}

func (tc *TrustyAITestCtx) validateTrustyAIResourceDeletionRecovery(t *testing.T, resourceGVK schema.GroupVersionKind) {
	t.Helper()

	resources := tc.FetchResources(
		WithMinimalObject(resourceGVK, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.Set{
				trustyAIPartOfLabel: componentApi.TrustyAIComponentName,
			}.AsSelector(),
		}),
	)
	require.NotEmpty(t, resources, "TrustyAI %s resources should exist", resourceGVK.Kind)

	for _, resource := range resources {
		t.Run(resourceGVK.Kind+"_"+resource.GetName(), func(t *testing.T) {
			t.Helper()

			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(resourceGVK, types.NamespacedName{
					Namespace: resource.GetNamespace(),
					Name:      resource.GetName(),
				}),
			)
		})
	}
}

func (tc *TrustyAITestCtx) validateTrustyAIDeploymentDeletionRecovery(t *testing.T) {
	t.Helper()

	for _, deploymentName := range []string{
		trustyAIServiceOperatorDeployment,
		trustyAIModuleControllerDeployment,
	} {
		t.Run("deployment_"+deploymentName, func(t *testing.T) {
			t.Helper()

			nn := types.NamespacedName{Namespace: tc.AppsNamespace, Name: deploymentName}
			tc.EnsureResourceDeletedThenRecreated(WithMinimalObject(gvk.Deployment, nn))
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, nn),
				WithCondition(jq.Match(
					`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
					status.ConditionTypeAvailable,
					metav1.ConditionTrue,
				)),
				WithCustomErrorMsg("Recreated TrustyAI Deployment should become Available"),
			)
		})
	}
}

// ValidateComponentDisabled validates TrustyAI component removal and cleans up its KServe dependency.
func (tc *TrustyAITestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	// Disable TrustyAI while its dependency is still available.
	tc.ComponentTestCtx.ValidateComponentDisabled(t)

	for _, deploymentName := range []string{
		trustyAIServiceOperatorDeployment,
		trustyAIModuleControllerDeployment,
	} {
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(gvk.Deployment, types.NamespacedName{
				Namespace: tc.AppsNamespace,
				Name:      deploymentName,
			}),
			WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
		)
	}

	// Clean up KServe only after TrustyAI has been removed.
	tc.setKserveState(operatorv1.Removed, false)
}

// ValidateMCPGuardrailsMode toggles TrustyAI MCPGuardrailsMode on the DataScienceCluster and
// checks that the TrustyAI CR spec and Ready condition stay consistent with the DSC.
func (tc *TrustyAITestCtx) ValidateMCPGuardrailsMode(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// Enable MCP Guardrails mode on the DSC
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.trustyai.mcpGuardrailsMode = true`)),
	)

	// Validate TrustyAI CR spec and Ready condition
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.TrustyAI, types.NamespacedName{Name: componentApi.TrustyAIInstanceName}),
		WithCondition(
			And(
				jq.Match(`.spec.mcpGuardrailsMode == true`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, status.ConditionTypeReady),
			),
		),
		WithCustomErrorMsg("TrustyAI should expose mcpGuardrailsMode and stay Ready when MCP guardrails mode is enabled on the DSC"),
	)

	// Disable MCP Guardrails mode on the DSC
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.trustyai.mcpGuardrailsMode = false`)),
	)

	// Validate TrustyAI CR spec and Ready condition
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.TrustyAI, types.NamespacedName{Name: componentApi.TrustyAIInstanceName}),
		WithCondition(
			And(
				jq.Match(`(.spec.mcpGuardrailsMode // false) == false`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, status.ConditionTypeReady),
			),
		),
		WithCustomErrorMsg("TrustyAI should return to Ready after MCP guardrails mode is disabled on the DSC"),
	)
}

// setKserveState manages KServe component lifecycle for TrustyAI dependency testing.
// Uses extended timeouts because KServe initialization involves complex CRD installation
// and feature gate configuration that can be slow in CI environments.
func (tc *TrustyAITestCtx) setKserveState(state operatorv1.ManagementState, shouldExist bool) {
	// TODO: remove timeout override once we understand why Kserve takes so long in CI
	savedOpts := tc.DefaultResourceOpts
	tc.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { tc.DefaultResourceOpts = savedOpts }()

	tc.UpdateComponentStateInDataScienceClusterWithKind(state, gvk.Kserve.Kind)

	if shouldExist {
		tc.ValidateComponentCondition(
			gvk.Kserve,
			componentApi.KserveInstanceName,
			status.ConditionTypeReady,
		)
	} else {
		tc.DeleteResource(
			WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
			WithWaitForDeletion(true),
		)
	}
}
