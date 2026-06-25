package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type RhoaiMcpTestCtx struct {
	*ComponentTestCtx
}

func rhoaiMcpTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.RhoaiMcp{})
	require.NoError(t, err)

	componentCtx := RhoaiMcpTestCtx{
		ComponentTestCtx: ct,
	}

	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate operator deployment env vars", componentCtx.ValidateOperatorDeploymentEnvVars},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate spec projection update", componentCtx.ValidateSpecProjectionUpdate},
		{"Validate SSA idempotency", componentCtx.ValidateSSAIdempotency},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	RunTestCases(t, testCases)
}

// ValidateSpec ensures that the RhoaiMcp CR spec matches the DSC configuration.
func (tc *RhoaiMcpTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	tc.SkipIfXKSCluster(t)

	dsc := tc.FetchDataScienceCluster()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.RhoaiMcp, types.NamespacedName{Name: componentApi.RhoaiMcpInstanceName}),
		WithCondition(And(
			jq.Match(`.spec.transport == "%s"`, dsc.Spec.Components.RhoaiMcp.Transport),
			jq.Match(`.spec.authMode == "%s"`, dsc.Spec.Components.RhoaiMcp.AuthMode),
			jq.Match(`.spec.readOnlyMode == %t`, dsc.Spec.Components.RhoaiMcp.ReadOnlyMode),
			jq.Match(`.spec.enableDangerousOperations == %t`, dsc.Spec.Components.RhoaiMcp.EnableDangerousOperations),
		)),
	)
}

// ValidateOperatorDeploymentEnvVars verifies that the RhoaiMcp operator Deployment
// has RELATED_IMAGE_* and APPLICATIONS_NAMESPACE environment variables injected by the platform.
func (tc *RhoaiMcpTestCtx) ValidateOperatorDeploymentEnvVars(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithCondition(
			HaveEach(And(
				// At least one container has APPLICATIONS_NAMESPACE env var set
				jq.Match(`.spec.template.spec.containers[].env[] | select(.name == "APPLICATIONS_NAMESPACE") | .value != ""`),
				// At least one RELATED_IMAGE_* env var exists
				jq.Match(`[.spec.template.spec.containers[].env[] | select(.name | startswith("RELATED_IMAGE_"))] | length > 0`),
			)),
		),
		WithCustomErrorMsg("RhoaiMcp operator deployment should have RELATED_IMAGE_* and APPLICATIONS_NAMESPACE env vars"),
	)
}

// ValidateSpecProjectionUpdate patches the DSC to change a RhoaiMcp field and verifies
// the RhoaiMcp CR spec is updated accordingly. This tests that SSA projection works on
// updates, not just initial creation. Follows the TrustyAI ValidateMCPGuardrailsMode pattern.
func (tc *RhoaiMcpTestCtx) ValidateSpecProjectionUpdate(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc.SkipIfXKSCluster(t)

	// Enable readOnlyMode on the DSC
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.rhoaiMcp.readOnlyMode = true`)),
	)

	// Validate RhoaiMcp CR spec reflects the change and component stays Ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.RhoaiMcp, types.NamespacedName{Name: componentApi.RhoaiMcpInstanceName}),
		WithCondition(
			And(
				jq.Match(`.spec.readOnlyMode == true`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, status.ConditionTypeReady),
			),
		),
		WithCustomErrorMsg("RhoaiMcp should reflect readOnlyMode=true and stay Ready after DSC patch"),
	)

	// Restore readOnlyMode to false on the DSC
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.rhoaiMcp.readOnlyMode = false`)),
	)

	// Validate RhoaiMcp CR spec reflects the restoration and component stays Ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.RhoaiMcp, types.NamespacedName{Name: componentApi.RhoaiMcpInstanceName}),
		WithCondition(
			And(
				jq.Match(`(.spec.readOnlyMode // false) == false`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, status.ConditionTypeReady),
			),
		),
		WithCustomErrorMsg("RhoaiMcp should return to readOnlyMode=false and stay Ready after DSC restoration"),
	)
}

// ValidateSSAIdempotency verifies that a no-op reconcile does not bump the RhoaiMcp CR's
// metadata.generation, ensuring the platform's SSA apply is not causing spurious updates.
func (tc *RhoaiMcpTestCtx) ValidateSSAIdempotency(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc.SkipIfXKSCluster(t)

	// Record the current generation of the RhoaiMcp CR
	cr := tc.EnsureResourceExists(
		WithMinimalObject(gvk.RhoaiMcp, types.NamespacedName{Name: componentApi.RhoaiMcpInstanceName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, status.ConditionTypeReady)),
	)
	generation := cr.GetGeneration()

	// Trigger a DSC reconcile by re-applying the same management state (no-op for the module CR spec)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.rhoaiMcp.managementState = "Managed"`)),
		WithCondition(jq.Match(`.metadata.generation == .status.observedGeneration`)),
	)

	// Verify that the RhoaiMcp CR generation has not changed
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.RhoaiMcp, types.NamespacedName{Name: componentApi.RhoaiMcpInstanceName}),
		WithCondition(
			And(
				jq.Match(`.metadata.generation == %d`, generation),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, status.ConditionTypeReady),
			),
		),
		WithCustomErrorMsg("RhoaiMcp CR generation should not change after a no-op reconcile (was %d)", generation),
	)
}

// ValidateCRDReinstated ensures that RhoaiMcp CRDs are not deleted when the component
// is disabled, and are properly reinstated when re-enabled.
func (tc *RhoaiMcpTestCtx) ValidateCRDReinstated(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	crds := []CRD{
		{Name: "rhoaimcps.components.platform.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}
