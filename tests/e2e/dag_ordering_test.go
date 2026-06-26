package e2e_test

import (
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
			{name: componentApi.AIGatewayComponentName, gvk: gvk.AIGateway, internal: true},
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
	"aigateway",
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
	//   PartialEnablement (→Removed) →
	//   RunlevelGating (→Removed, enables under quota, proves gating,
	//                    removes quota, converges → AllReady) →
	//   BatchOrdering (→AllReady, read-only) →
	//   PlatformReady (→AllReady, read-only) →
	//   DAGCleanup (→Removed)
	testCases := []TestCase{
		{"Validate in-tree gates block and unblock provisioning", ctx.ValidateInTreeGates},
		{"Validate admin ack gates block and unblock provisioning", ctx.ValidateAdminAckGates},
		{"Validate partial enablement", ctx.ValidatePartialEnablement},
		{"Validate runlevel gating and convergence", ctx.ValidateRunlevelGatingAndConvergence},
		{"Validate batch ordering", ctx.ValidateBatchOrdering},
		{"Validate PlatformReady condition on component CRs", ctx.ValidatePlatformReady},
		{"Validate DAG cleanup", ctx.ValidateDAGCleanup},
	}

	RunTestCases(t, testCases)
}

// ValidateRunlevelGatingAndConvergence combines gating proof with DAG
// convergence in a single enable cycle. Starting from a Removed state
// (all component CRs deleted), it applies a zero-pod quota, enables all
// components, and verifies that Extension CRs (batch 31+) are absent
// while batch 20 is stuck. After removing the quota it waits for full
// convergence (ProvisioningProgress=True), verifies Extension CRs were
// created, and leaves the cluster in an AllReady state for subsequent
// read-only tests.
//
// This avoids operator restarts (and associated stale-PlatformReady
// race conditions) by leveraging the fact that the RunlevelTracker is
// naturally empty for newly-created component CRs.
func (tc *DAGOrderingTestCtx) ValidateRunlevelGatingAndConvergence(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	t.Log("Waiting for Extension CRs to be fully deleted before gating test")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		tc.EnsureResourceGone(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
			WithEventuallyTimeout(5*time.Minute),
			WithEventuallyPollingInterval(10*time.Second),
		)
	}

	t.Log("Applying zero-pod quota to block batch 20 deployments")
	tc.createDAGQuota()

	t.Log("Enabling all components under quota")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(allComponentsTransform("Managed")),
	)

	t.Log("Waiting for ProvisioningProgress=False (batch 20 stuck under quota)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Verifying no Extension CRs were created while batch 20 is gated")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		u := tc.FetchResource(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
		)
		require.Nil(t, u,
			"Extension CR %s should not exist while batch 20 is stuck under quota", extGVK.Kind)
	}
	t.Log("Confirmed: no Extension CRs exist while gated")

	t.Log("Removing quota to unblock batch 20")
	tc.deleteDAGQuota()

	t.Log("Waiting for DAG convergence (ProvisioningProgress=True)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	t.Log("Verifying Extension CRs now exist after recovery")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		u := tc.FetchResource(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
		)
		require.NotNil(t, u,
			"Extension CR %s should exist after DAG convergence", extGVK.Kind)
	}
	t.Log("DAG converged and gating verified")
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

	t.Log("Enabling dashboard and aigateway to trigger a reconcile (in-tree gates should be discovered)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform("Managed", []string{"dashboard", "aigateway"})),
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
	t.Log("Confirmed: provisioning blocked by in-tree gates (components and modules)")

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

	t.Log("Enabling dashboard and aigateway to trigger a reconcile")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform("Managed", []string{"dashboard", "aigateway"})),
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
	t.Log("Confirmed: provisioning blocked by two unacknowledged gates (components and modules)")

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
