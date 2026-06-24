package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
)

type DAGOrderingTestCtx struct {
	*TestContext
}

type componentBatch struct {
	name       string
	runlevel   int
	components []componentEntry
}

type componentEntry struct {
	name     string
	gvk      schema.GroupVersionKind
	internal bool // components whose CR may not exist in the test (webhook-blocked, auto-created, etc.)
}

// dagBatches mirrors the runlevel assignments from cmd/main.go.
// The ordering here defines the expected provisioning order.
var dagBatches = []componentBatch{
	{
		name:     "Batch20",
		runlevel: 20,
		components: []componentEntry{
			{name: componentApi.DashboardComponentName, gvk: gvk.Dashboard},
			{name: componentApi.DataSciencePipelinesComponentName, gvk: gvk.DataSciencePipelines},
			{name: componentApi.ModelRegistryComponentName, gvk: gvk.ModelRegistry},
			{name: componentApi.RayComponentName, gvk: gvk.Ray},
			{name: componentApi.TrainerComponentName, gvk: gvk.Trainer},
			{name: componentApi.TrainingOperatorComponentName, gvk: gvk.TrainingOperator},
			{name: componentApi.WorkbenchesComponentName, gvk: gvk.Workbenches},
		},
	},
	{
		name:     "Batch31",
		runlevel: 31,
		components: []componentEntry{
			{name: componentApi.KserveComponentName, gvk: gvk.Kserve},
			{name: componentApi.KueueComponentName, gvk: gvk.Kueue, internal: true},
		},
	},
	{
		name:     "Batch32",
		runlevel: 32,
		components: []componentEntry{
			{name: componentApi.FeastOperatorComponentName, gvk: gvk.FeastOperator},
			{name: componentApi.MLflowOperatorComponentName, gvk: gvk.MLflowOperator},
			{name: componentApi.OGXComponentName, gvk: gvk.OGX},
			{name: componentApi.SparkOperatorComponentName, gvk: gvk.SparkOperator},
		},
	},
	{
		name:     "Batch33",
		runlevel: 33,
		components: []componentEntry{
			{name: componentApi.ModelControllerComponentName, gvk: gvk.ModelController},
			{name: componentApi.ModelsAsServiceComponentName, gvk: gvk.ModelsAsService, internal: true},
			{name: componentApi.TrustyAIComponentName, gvk: gvk.TrustyAI},
		},
	},
}

// dscComponentFields lists the JSON field names in DSC spec.components
// that have a managementState field. DataSciencePipelines uses "aipipelines"
// as its field name.
// Kueue is excluded: a validating webhook rejects managementState=Managed
// for Kueue because it no longer manages deployments directly.
var dscComponentFields = []string{
	"dashboard",
	"workbenches",
	"aipipelines",
	"kserve",
	"ray",
	"modelregistry",
	"trainingoperator",
	"trustyai",
	"feastoperator",
	"ogx",
	"mlflowoperator",
	"trainer",
	"sparkoperator",
}

// extensionGVKs lists the explicitly-enabled Extension components.
// ModelController and ModelsAsService are excluded — they are internal
// components auto-created by the operator and may not have CRs.
var extensionGVKs = []schema.GroupVersionKind{
	gvk.Kserve,
	gvk.FeastOperator,
	gvk.MLflowOperator,
	gvk.OGX,
	gvk.SparkOperator,
}

const dagQuotaName = "dag-test-restrictive-quota"

func dagOrderingTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	tc.SkipIfXKSCluster(t)

	ctx := DAGOrderingTestCtx{TestContext: tc}

	t.Log("Ensuring clean slate: setting all components to Removed")
	ctx.setAllRemoved(t)

	// Tests are ordered to minimise expensive enable/disable cycles.
	// Each test's end state feeds the next test's start state:
	//   InTreeGates (→Removed) → AdminAckGates (→Removed) →
	//   PartialEnablement (→Removed) → DAGConvergence (→AllReady) →
	//   BatchOrdering (→AllReady, read-only) →
	//   PlatformReady (→AllReady, read-only) →
	//   DeployBlocked (→AllReady after recovery) →
	//   DAGCleanup (→Removed) → RunlevelGating (→Removed)
	testCases := []TestCase{
		{"Validate in-tree gates block and unblock provisioning", ctx.ValidateInTreeGates},
		{"Validate admin ack gates block and unblock provisioning", ctx.ValidateAdminAckGates},
		{"Validate partial enablement", ctx.ValidatePartialEnablement},
		{"Validate DAG convergence", ctx.ValidateDAGConvergence},
		{"Validate batch ordering", ctx.ValidateBatchOrdering},
		{"Validate PlatformReady condition on component CRs", ctx.ValidatePlatformReady},
		{"Validate runlevel gating blocks resource deployment", ctx.ValidateRunlevelGatingBlocksDeployment},
		{"Validate DAG cleanup", ctx.ValidateDAGCleanup},
		{"Validate runlevel gating and recovery", ctx.ValidateRunlevelGatingAndRecovery},
	}

	RunTestCases(t, testCases)
}

// ValidateDAGConvergence enables all components simultaneously and waits
// for ProvisioningProgress=True (the DAG walked all batches). It tracks
// ProvisioningProgress reason transitions and asserts that
// AwaitingReadiness is observed during the convergence process.
// It intentionally does NOT wait for ComponentsReady — individual
// component health is tested elsewhere.
func (tc *DAGOrderingTestCtx) ValidateDAGConvergence(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	t.Log("Patching DSC to set all components to Managed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(allComponentsTransform("Managed")),
	)
	t.Log("Patch applied, waiting for convergence while tracking reason transitions")

	observedReasons := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	converged := false
	for !converged {
		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for convergence; observed reasons: %v", observedReasons)
		case <-time.After(5 * time.Second):
		}

		dsc := tc.FetchResource(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		)
		if dsc == nil {
			continue
		}

		conditions, _, _ := unstructured.NestedSlice(dsc.Object, "status", "conditions")
		for _, c := range conditions {
			cond, ok := c.(map[string]any)
			if !ok {
				continue
			}
			condType, _ := cond["type"].(string)
			reason, _ := cond["reason"].(string)
			condStatus, _ := cond["status"].(string)

			if condType == status.ConditionTypeProvisioningProgress {
				if reason != "" && !observedReasons[reason] {
					t.Logf("Observed ProvisioningProgress reason transition: %s (status=%s)", reason, condStatus)
					observedReasons[reason] = true
				}
			}

			if condType == status.ConditionTypeProvisioningProgress &&
				condStatus == string(metav1.ConditionTrue) {
				converged = true
			}
		}
	}

	t.Logf("All observed ProvisioningProgress reasons: %v", observedReasons)

	require.True(t, observedReasons[status.AwaitingReadinessReason],
		"Expected AwaitingReadiness reason to appear during DAG convergence, got: %v", observedReasons)
}

// ValidateBatchOrdering fetches component CRs created during DAG convergence
// and asserts that components in earlier runlevel batches have earlier
// creationTimestamps than those in later batches.
func (tc *DAGOrderingTestCtx) ValidateBatchOrdering(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	type batchTimestamps struct {
		name     string
		earliest metav1.Time
		latest   metav1.Time
		count    int
		skipped  []string
	}

	var results []batchTimestamps

	for _, batch := range dagBatches {
		bt := batchTimestamps{name: batch.name}

		for _, comp := range batch.components {
			instanceName := tc.GetInstanceName(comp.gvk)
			u := tc.FetchResource(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: instanceName}),
			)
			if u == nil {
				bt.skipped = append(bt.skipped, comp.name)
				t.Logf("Component %s CR not found, skipping from ordering check", comp.name)
				continue
			}

			ts := u.GetCreationTimestamp()
			if ts.IsZero() {
				bt.skipped = append(bt.skipped, comp.name)
				t.Logf("Component %s has zero creationTimestamp, skipping", comp.name)
				continue
			}

			if bt.count == 0 {
				bt.earliest = ts
				bt.latest = ts
			} else {
				if ts.Before(&bt.earliest) {
					bt.earliest = ts
				}
				if bt.latest.Before(&ts) {
					bt.latest = ts
				}
			}
			bt.count++
		}

		if bt.count > 0 {
			results = append(results, bt)
			t.Logf("Batch %s: %d CRs, earliest=%s, latest=%s",
				bt.name, bt.count, bt.earliest.Format(time.RFC3339), bt.latest.Format(time.RFC3339))
		} else {
			t.Logf("Batch %s: no CRs found (skipped: %s)", bt.name, strings.Join(bt.skipped, ", "))
		}

		if len(bt.skipped) > 0 {
			t.Logf("Batch %s: skipped components: %s", bt.name, strings.Join(bt.skipped, ", "))
		}
	}

	require.GreaterOrEqual(t, len(results), 2,
		"Need at least 2 batches with component CRs to verify ordering")

	for i := range len(results) - 1 {
		curr := results[i]
		next := results[i+1]

		require.False(t,
			next.earliest.Before(&curr.latest),
			"Batch %q latest CR (%s) should not be after batch %q earliest CR (%s) — DAG ordering violated",
			curr.name, curr.latest.Format(time.RFC3339),
			next.name, next.earliest.Format(time.RFC3339),
		)

		t.Logf("Ordering OK: batch %q (latest %s) -> batch %q (earliest %s)",
			curr.name, curr.latest.Format(time.RFC3339),
			next.name, next.earliest.Format(time.RFC3339))
	}
}

// ValidatePlatformReady verifies that every component CR has the
// PlatformReady condition set to True after DAG convergence, confirming
// that the in-memory runlevel tracker allowed each component's
// precondition to pass.
func (tc *DAGOrderingTestCtx) ValidatePlatformReady(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	for _, batch := range dagBatches {
		for _, comp := range batch.components {
			if comp.internal {
				t.Logf("Skipping internal component %s (CR may not exist)", comp.name)
				continue
			}

			instanceName := tc.GetInstanceName(comp.gvk)

			tc.EnsureResourceExists(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: instanceName}),
				WithCondition(jq.Match(
					`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
					precondition.PlatformReadyConditionType, metav1.ConditionTrue,
				)),
				WithEventuallyTimeout(30*time.Second),
				WithEventuallyPollingInterval(5*time.Second),
				WithCustomErrorMsg("Component %s should have %s=True condition", comp.name, precondition.PlatformReadyConditionType),
			)
			t.Logf("Component %s: %s=True (correct)", comp.name, precondition.PlatformReadyConditionType)
		}
	}
}

// ValidateRunlevelGatingBlocksDeployment proves that the RunlevelGate
// action prevents SSA from applying resources while a component's
// runlevel hasn't been cleared. It uses a "canary resource" approach:
//
//  1. All components are deployed and healthy (from prior tests)
//  2. Apply a zero-pod quota + delete Ray pods to block batch 20
//  3. Restart the operator (resets the in-memory RunlevelTracker)
//  4. Verify PlatformReady=False on a batch 31+ component (gate active)
//  5. Delete a non-critical resource (ClusterRoleBinding) owned by that component
//  6. Consistently verify the resource stays deleted (SSA/deploy did NOT run)
//  7. Remove the quota → DAG advances → deploy runs
//  8. Verify the canary resource is recreated by SSA
func (tc *DAGOrderingTestCtx) ValidateRunlevelGatingBlocksDeployment(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	canaryGVK := gvk.ClusterRoleBinding
	canaryNN := types.NamespacedName{Name: "kserve-proxy-rolebinding"}
	kserveInstanceName := tc.GetInstanceName(gvk.Kserve)

	t.Log("Verifying canary ClusterRoleBinding exists before test")
	canary := tc.FetchResource(WithMinimalObject(canaryGVK, canaryNN))
	require.NotNil(t, canary, "Canary %s must exist before test", canaryNN.Name)

	t.Log("Applying zero-pod quota to block batch 20 deployments")
	tc.createDAGQuota()

	t.Log("Deleting Ray pods (quota prevents replacements, making batch 20 unhealthy)")
	tc.deletePodsForComponent(t, componentApi.RayComponentName)

	rayInstanceName := tc.GetInstanceName(gvk.Ray)
	t.Log("Waiting for Ray CR to report unhealthy before restarting operator")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Ray, types.NamespacedName{Name: rayInstanceName}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "Ready" and .status == "False")`,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(5*time.Second),
	)

	t.Log("Restarting operator to reset RunlevelTracker (simulates upgrade)")
	tc.rolloutRestartOperator(t)

	t.Log("Waiting for DSC ProvisioningProgress=False (confirms new operator with reset tracker has taken over)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Waiting for PlatformReady=False on Kserve (batch 31 gated)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: kserveInstanceName}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			precondition.PlatformReadyConditionType, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Confirmed: Kserve PlatformReady=False (gate active, deploy skipped)")

	t.Log("Deleting canary resource while gate is active")
	tc.DeleteResource(
		WithMinimalObject(canaryGVK, canaryNN),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)

	t.Log("Triggering a Kserve reconcile by annotating the CR")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: kserveInstanceName}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			ann := obj.GetAnnotations()
			if ann == nil {
				ann = map[string]string{}
			}
			ann["e2e-test/reconcile-trigger"] = time.Now().Format(time.RFC3339)
			obj.SetAnnotations(ann)
			return nil
		}),
	)

	t.Log("Waiting for Kserve to reconcile and re-assert PlatformReady=False (proves controller ran with SkipDeploy)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: kserveInstanceName}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s" and .reason == "RunlevelNotCleared")`,
			precondition.PlatformReadyConditionType, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(60*time.Second),
		WithEventuallyPollingInterval(5*time.Second),
	)

	t.Log("Verifying canary is still deleted (controller reconciled but deploy was skipped)")
	u := tc.FetchResource(WithMinimalObject(canaryGVK, canaryNN))
	require.Nil(t, u,
		"Canary %s must remain deleted — controller reconciled but SkipDeploy prevented SSA", canaryNN.Name)

	t.Log("Removing quota to unblock batch 20")
	tc.deleteDAGQuota()

	t.Log("Waiting for DAG to converge (ProvisioningProgress=True)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	t.Log("Verifying canary resource was recreated by SSA (proves deploy ran)")
	tc.EnsureResourceExists(
		WithMinimalObject(canaryGVK, canaryNN),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Verifying PlatformReady=True on Kserve (gate lifted)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: kserveInstanceName}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			precondition.PlatformReadyConditionType, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Test passed: deploy was blocked while gated, and resources were applied after unblocking")
}

// ValidateDAGCleanup sets all components to Removed, waits for the DSC
// to reach Ready, and verifies that all component CRs are cleaned up
// with no orphans.
//
// NOTE: Reverse-batch cleanup ordering (ReverseBatches) is implemented
// in the modules controller path but not in the current mixed
// component/module environment where the DSC GC action deletes all
// stale component CRs in a single pass. Once the migration to modules
// is complete, this test should be extended to assert reverse-batch
// deletion ordering.
func (tc *DAGOrderingTestCtx) ValidateDAGCleanup(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	tc.setAllRemoved(t)

	t.Log("Verifying all component CRs are cleaned up (no orphans)")
	for _, batch := range dagBatches {
		for _, comp := range batch.components {
			instanceName := tc.GetInstanceName(comp.gvk)
			tc.EnsureResourceGone(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: instanceName}),
				WithEventuallyTimeout(5*time.Minute),
				WithEventuallyPollingInterval(10*time.Second),
			)
		}
	}
}

// ValidateRunlevelGatingAndRecovery applies a zero-pod quota so batch 20
// components cannot become ready, enables all components, asserts that
// no Extensions-batch CRs are created while gated, then removes the
// quota and verifies the DAG recovers.
func (tc *DAGOrderingTestCtx) ValidateRunlevelGatingAndRecovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	// Prior test (DAGCleanup) leaves all components Removed and
	// verifies all CRs are deleted, but double-check extension CRs
	// since the gating test depends on their absence.
	t.Log("Waiting for Extension CRs to be fully deleted before gating test")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		tc.EnsureResourceGone(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
			WithEventuallyTimeout(5*time.Minute),
			WithEventuallyPollingInterval(10*time.Second),
		)
	}

	t.Log("Creating zero-pod quota to block deployments")
	tc.createDAGQuota()

	t.Log("Enabling all components under quota")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(allComponentsTransform("Managed")),
	)

	t.Log("Waiting for ProvisioningProgress=False (deployments blocked by quota)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Verifying no Extensions CRs were created while gated")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		u := tc.FetchResource(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
		)
		require.Nil(t, u,
			"Extensions CR %s should not exist while batch 20 is stuck under quota", extGVK.Kind)
	}
	t.Log("Confirmed: no Extensions CRs exist while gated")

	t.Log("Removing quota to unblock deployments")
	tc.deleteDAGQuota()

	t.Log("Waiting for ProvisioningProgress=True after quota removal (DAG recovery)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	t.Log("Verifying Extensions CRs now exist after recovery")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		u := tc.FetchResource(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
		)
		require.NotNil(t, u,
			"Extensions CR %s should exist after recovery", extGVK.Kind)
	}

	t.Log("Cleaning up: setting all components to Removed")
	tc.setAllRemoved(t)
}

// ValidatePartialEnablement enables a subset of components spanning
// multiple batches and verifies that disabled components don't block
// the DAG.
func (tc *DAGOrderingTestCtx) ValidatePartialEnablement(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	// Prior test (AdminAckGates) leaves all components Removed.

	partialFields := []string{"dashboard", "kserve", "modelregistry"}

	t.Log("Enabling partial set: dashboard (batch 20), kserve (batch 31), modelregistry (batch 20)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform("Managed", partialFields)),
	)

	t.Log("Waiting for DAG to start processing (ProvisioningProgress=False)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Waiting for ProvisioningProgress=True (DAG walked all batches)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	enabledGVKs := []schema.GroupVersionKind{gvk.Dashboard, gvk.Kserve, gvk.ModelRegistry}
	for _, g := range enabledGVKs {
		instanceName := tc.GetInstanceName(g)
		u := tc.FetchResource(
			WithMinimalObject(g, types.NamespacedName{Name: instanceName}),
		)
		require.NotNil(t, u, "Enabled component CR %s should exist", g.Kind)
	}

	disabledGVKs := []schema.GroupVersionKind{
		gvk.Ray, gvk.FeastOperator, gvk.SparkOperator, gvk.TrustyAI,
	}
	for _, g := range disabledGVKs {
		instanceName := tc.GetInstanceName(g)
		tc.EnsureResourceGone(
			WithMinimalObject(g, types.NamespacedName{Name: instanceName}),
			WithEventuallyTimeout(3*time.Minute),
			WithEventuallyPollingInterval(10*time.Second),
			WithCustomErrorMsg("Disabled component CR %s should not exist", g.Kind),
		)
	}

	t.Log("Cleaning up: setting all components to Removed")
	tc.setAllRemoved(t)
}

// ValidateInTreeGates verifies that gate entries compiled into the
// operator binary (from pkg/controller/gates/resources/gates.yaml)
// block provisioning and can be acknowledged. The test self-discovers
// in-tree entries for the current version and skips if none exist.
func (tc *DAGOrderingTestCtx) ValidateInTreeGates(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	operatorVersion := tc.getDeployedVersion(t)

	intreeGates, err := gates.LoadInTreeGates(operatorVersion)
	require.NoError(t, err, "Failed to load in-tree gates")

	if len(intreeGates) == 0 {
		t.Skipf("No in-tree gates for version %s, skipping", operatorVersion)
	}

	t.Logf("Found %d in-tree gate(s) for version %s", len(intreeGates), operatorVersion)

	// Suite-level setup (or prior test) leaves all components Removed.

	gateKeys := make([]string, 0, len(intreeGates))
	for k := range intreeGates {
		gateKeys = append(gateKeys, k)
		t.Logf("  gate: %s = %q", k, intreeGates[k])
	}

	tc.clearStaleAcks(t, gateKeys...)

	t.Log("Enabling dashboard to trigger a reconcile (in-tree gates should be discovered)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform("Managed", []string{"dashboard"})),
	)

	t.Log("Waiting for ProvisioningProgress=False with reason AdminAckRequired")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s" and .reason == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse, status.AdminAckRequiredReason,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Confirmed: provisioning blocked by in-tree gates")

	t.Log("Verifying gate descriptions were written to odh-upgrade-acks")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      gates.AcksConfigMap,
		}),
		WithCondition(jq.Match(`.data["%s"] != null and .data["%s"] != "true"`, gateKeys[0], gateKeys[0])),
		WithEventuallyTimeout(30*time.Second),
		WithEventuallyPollingInterval(5*time.Second),
	)
	t.Log("Confirmed: in-tree gate descriptions present in odh-upgrade-acks ConfigMap")

	t.Log("Acknowledging all in-tree gates")
	for _, key := range gateKeys {
		tc.patchAcksConfigMap(t, key, "true")
	}

	t.Log("Waiting for provisioning to resume")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .reason != "%s")`,
			status.ConditionTypeProvisioningProgress, status.AdminAckRequiredReason,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Confirmed: provisioning resumed after in-tree gates acknowledged")

	t.Log("Cleaning up: setting all components to Removed")
	tc.setAllRemoved(t)
}

// ValidateAdminAckGates creates labeled source ConfigMaps (simulating
// gate declarations from component/module charts), enables a component,
// and verifies that the operator discovers them, writes descriptions
// to odh-upgrade-acks, and blocks provisioning with AdminAckRequired.
// It then partially acks (verifying blocking continues) and fully acks
// (verifying provisioning resumes).
func (tc *DAGOrderingTestCtx) ValidateAdminAckGates(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	// Prior test (InTreeGates) leaves all components Removed.

	operatorVersion := tc.getDeployedVersion(t)
	gateKey1 := "ack-" + operatorVersion + "-e2e-api-change"
	gateKey2 := "ack-" + operatorVersion + "-e2e-storage-migration"

	tc.clearStaleAcks(t, gateKey1, gateKey2)

	t.Log("Creating labeled gate source ConfigMaps (simulating component/module chart output)")

	source1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-gate-source-1",
			Namespace: tc.OperatorNamespace,
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			gateKey1: "API change: review migration guide before proceeding",
		},
	}
	source2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-gate-source-2",
			Namespace: tc.OperatorNamespace,
			Labels:    map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{
			gateKey2: "Storage migration: back up data before proceeding",
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(source1))
	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(source2))

	defer tc.deleteGateSourceCMs(t, "e2e-gate-source-1", "e2e-gate-source-2")

	t.Log("Enabling dashboard to trigger a reconcile")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform("Managed", []string{"dashboard"})),
	)

	t.Log("Waiting for ProvisioningProgress=False with reason AdminAckRequired")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s" and .reason == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse, status.AdminAckRequiredReason,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Confirmed: provisioning blocked by two unacknowledged gates (discovered from labeled CMs)")

	t.Logf("Acknowledging only the first gate: %s", gateKey1)
	tc.patchAcksConfigMap(t, gateKey1, "true")

	t.Log("Verifying provisioning remains blocked (second gate still unacknowledged)")
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s" and .reason == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse, status.AdminAckRequiredReason,
		)),
		WithConsistentlyDuration(20*time.Second),
		WithConsistentlyPollingInterval(5*time.Second),
	)
	t.Log("Confirmed: still blocked with one gate remaining")

	t.Logf("Acknowledging second gate: %s", gateKey2)
	tc.patchAcksConfigMap(t, gateKey2, "true")

	t.Log("Waiting for provisioning to resume (all gates acknowledged)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .reason != "%s")`,
			status.ConditionTypeProvisioningProgress, status.AdminAckRequiredReason,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Confirmed: provisioning resumed after all gates acknowledged")

	t.Log("Cleaning up: setting all components to Removed")
	tc.setAllRemoved(t)
}

func (tc *DAGOrderingTestCtx) clearStaleAcks(t *testing.T, keys ...string) {
	t.Helper()

	t.Logf("Clearing stale acks for keys %v (if present)", keys)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      gates.AcksConfigMap,
		}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
			if data != nil {
				for _, k := range keys {
					delete(data, k)
				}
			}
			return unstructured.SetNestedStringMap(obj.Object, data, "data")
		}),
	)
}

func (tc *DAGOrderingTestCtx) patchAcksConfigMap(t *testing.T, key, value string) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      gates.AcksConfigMap,
		}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, value, "data", key)
		}),
	)
}

func (tc *DAGOrderingTestCtx) deleteGateSourceCMs(t *testing.T, names ...string) {
	t.Helper()

	for _, name := range names {
		t.Logf("Removing gate source ConfigMap %s (if present)", name)
		tc.DeleteResource(
			WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
				Namespace: tc.OperatorNamespace,
				Name:      name,
			}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	}
}

// --- helpers ---

func allComponentsTransform(state string) func(*unstructured.Unstructured) error {
	return selectComponentsTransform(state, dscComponentFields)
}

func selectComponentsTransform(state string, fields []string) func(*unstructured.Unstructured) error {
	return func(obj *unstructured.Unstructured) error {
		for _, field := range fields {
			if err := unstructured.SetNestedField(
				obj.Object, state, "spec", "components", field, "managementState",
			); err != nil {
				return err
			}
		}
		return nil
	}
}

// setAllRemoved is a reusable helper that sets all components to Removed,
// waits for Ready=True, and verifies all component CRs are deleted.
func (tc *DAGOrderingTestCtx) setAllRemoved(t *testing.T) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(allComponentsTransform("Removed")),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeReady, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(10*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	for _, batch := range dagBatches {
		for _, comp := range batch.components {
			instanceName := tc.GetInstanceName(comp.gvk)
			tc.EnsureResourceGone(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: instanceName}),
				WithEventuallyTimeout(5*time.Minute),
				WithEventuallyPollingInterval(10*time.Second),
			)
		}
	}
}

func (tc *DAGOrderingTestCtx) createDAGQuota() {
	quota := &corev1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ResourceQuota.Version,
			Kind:       gvk.ResourceQuota.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dagQuotaName,
			Namespace: tc.AppsNamespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("0"),
			},
		},
	}

	tc.EventuallyResourceCreated(WithObjectToCreate(quota))

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceQuota, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      dagQuotaName,
		}),
		WithCondition(jq.Match(`.status.hard != null`)),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)
}

// getDeployedVersion reads the operator release version from the DSC
// status on the cluster. This avoids relying on cluster.GetRelease()
// which returns 0.0.0 when CI=true short-circuits version detection
// in the e2e test binary.
func (tc *DAGOrderingTestCtx) getDeployedVersion(t *testing.T) string {
	t.Helper()

	dsc := tc.FetchResource(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
	)
	require.NotNil(t, dsc, "DSC must exist to read deployed version")

	ver, _, _ := unstructured.NestedString(dsc.Object, "status", "release", "version")
	require.NotEmpty(t, ver, "DSC .status.release.version must be populated")

	t.Logf("Using deployed operator version from DSC status: %s", ver)

	return ver
}

func (tc *DAGOrderingTestCtx) deleteDAGQuota() {
	tc.DeleteResource(
		WithMinimalObject(
			gvk.ResourceQuota,
			types.NamespacedName{Namespace: tc.AppsNamespace, Name: dagQuotaName},
		),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
}

// rolloutRestartOperator patches the operator deployment's pod template
// with a restartedAt annotation, triggering a rolling restart (the same
// mechanism as `kubectl rollout restart`). It waits for the rollout to
// complete before returning.
func (tc *DAGOrderingTestCtx) rolloutRestartOperator(t *testing.T) {
	t.Helper()

	deployName := tc.getControllerDeploymentName()
	deployNN := types.NamespacedName{
		Namespace: tc.OperatorNamespace,
		Name:      deployName,
	}

	t.Logf("Rollout restarting operator deployment %s/%s", deployNN.Namespace, deployNN.Name)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Deployment, deployNN),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(
				obj.Object,
				time.Now().Format(time.RFC3339),
				"spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt",
			)
		}),
	)

	t.Log("Waiting for operator rollout to complete")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, deployNN),
		WithCondition(jq.Match(
			`.status.availableReplicas == .status.replicas and .status.updatedReplicas == .status.replicas and .status.unavailableReplicas == null`,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(5*time.Second),
	)
}

// deletePodsForComponent deletes all pods for a component's deployments
// in the apps namespace. When combined with a zero-pod quota, this makes
// the component's deployments unhealthy (0/N ready).
func (tc *DAGOrderingTestCtx) deletePodsForComponent(t *testing.T, componentName string) {
	t.Helper()

	labelKey := "app.opendatahub.io/" + componentName
	t.Logf("Deleting pods with label %s=true in %s", labelKey, tc.AppsNamespace)

	tc.DeleteResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{}),
		WithNamespaceFilter(tc.AppsNamespace),
		WithDeleteAllOfOptions(client.MatchingLabels{labelKey: "true"}),
	)
}
