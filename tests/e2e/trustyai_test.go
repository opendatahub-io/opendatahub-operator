package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	trustyaiModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trustyai"
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
	dummyPrometheusName                = "dummy-prometheus"
)

// trustyAITestSuite runs the complete TrustyAI component test suite.
// This includes dependency validation tests specific to TrustyAI's KServe requirements.
func trustyAITestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewModuleTestCtx(t, trustyaiModule.NewHandler())
	require.NoError(t, err)

	componentCtx := TrustyAITestCtx{
		ComponentTestCtx: ct,
	}
	componentCtx.createDummyPrometheus(t)

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate module enabled", componentCtx.ValidateModuleEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate platform release", componentCtx.ValidatePlatformRelease},
		{"Validate MCP guardrails mode", componentCtx.ValidateMCPGuardrailsMode},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
		{"Validate module disabled", componentCtx.ValidateModuleDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// createDummyPrometheus provides an instance for environments such as CRC,
// where no default Prometheus instance exists, so the TrustyAI tests can run.
func (tc *TrustyAITestCtx) createDummyPrometheus(t *testing.T) {
	t.Helper()

	key := types.NamespacedName{
		Name:      dummyPrometheusName,
		Namespace: tc.AppsNamespace,
	}

	prometheuses := &monitoringv1.PrometheusList{}
	err := tc.Client().List(tc.Context(), prometheuses)
	switch {
	case err == nil && len(prometheuses.Items) > 0:
		return
	case meta.IsNoMatchError(err):
		t.Logf("Prometheus CRD not installed; skipping dummy Prometheus %q", key)
		return
	case err == nil:
		// No Prometheus instance exists; create the test instance below.
	default:
		require.NoError(t, err, "failed to list Prometheus instances")
	}

	err = tc.Client().Create(tc.Context(), &monitoringv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: monitoringv1.PrometheusSpec{
			CommonPrometheusFields: monitoringv1.CommonPrometheusFields{
				Paused: true,
			},
		},
	})

	switch {
	case err == nil:
	case k8serr.IsAlreadyExists(err):
		return
	default:
		require.NoError(t, err, "failed to create dummy Prometheus %q", key)
	}

	t.Cleanup(func() {
		err := tc.Client().Delete(tc.Context(), &monitoringv1.Prometheus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		})

		if k8serr.IsNotFound(err) {
			return
		}

		require.NoError(t, err, "failed to delete test-created Prometheus %q", key)
	})
}

// ValidateUpdateDeploymentsResources verifies ODS-2664 / RHOAIENG-101065
// allowlist-preserve for trustyai-service-operator-controller-manager: without
// opendatahub.io/managed=true, replica edits must stick across module
// reconcile (companion to trustyai-service-operator#1081).
//
// manifestDefaultReplicas matches the TrustyAI service-operator manager.yaml
// default (replicas: 1). The managed=true assertion must compare against that
// manifest default, not whatever replica count happened to be live at test
// start — otherwise a pre-drifted Deployment makes the revert check flaky.
func (tc *TrustyAITestCtx) ValidateUpdateDeploymentsResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	const (
		manifestDefaultReplicas = 1
		patchedReplicas         = 2
	)

	deploymentName := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      trustyAIServiceOperatorDeployment,
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, deploymentName),
	)

	// Unmanaged (no opendatahub.io/managed=true): preserve the patched value.
	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Deployment, deploymentName),
		WithMutateFunc(testf.Transform(`.spec.replicas = %d`, patchedReplicas)),
		WithCondition(jq.Match(`.spec.replicas == %d`, patchedReplicas)),
		WithCustomErrorMsg("TrustyAI module operator should preserve unmanaged service operator Deployment replicas"),
	)

	// Explicitly managed: module reapplies the manifest default.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Deployment, deploymentName),
		WithMutateFunc(testf.Transform(
			`.metadata.annotations["opendatahub.io/managed"] = "true" | .spec.replicas = %d`,
			patchedReplicas,
		)),
		WithCondition(jq.Match(
			`.metadata.annotations["opendatahub.io/managed"] == "true" and .spec.replicas == %d`,
			patchedReplicas,
		)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, deploymentName),
		WithCondition(jq.Match(`.spec.replicas == %d`, manifestDefaultReplicas)),
		WithCustomErrorMsg("TrustyAI module operator should revert replicas to the manifest default when opendatahub.io/managed=true"),
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
