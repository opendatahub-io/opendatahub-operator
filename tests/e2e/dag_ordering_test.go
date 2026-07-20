package e2e_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
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
			{name: componentApi.MCPLifecycleOperatorComponentName, gvk: gvk.MCPLifecycleOperator, internal: true},
		},
	},
	{
		name:     "Batch31",
		runlevel: 31,
		components: []componentEntry{
			{name: componentApi.KserveComponentName, gvk: gvk.Kserve},
			{name: componentApi.ModelControllerComponentName, gvk: gvk.ModelController},
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
			{name: componentApi.AIGatewayComponentName, gvk: gvk.AIGateway, internal: true},
		},
	},
	{
		name:     "Batch33",
		runlevel: 33,
		components: []componentEntry{
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
	"mcplifecycleoperator",
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
	ctx.removeOperatorEnvVars(t, "RHAI_VERSION", "CI")

	// Tests are ordered to minimise expensive enable/disable cycles.
	// Each test's end state feeds the next test's start state.
	//
	// Phase 1: Gate tests (no upgrade simulation needed)
	//   InTreeGates (→Removed) → AdminAckGates (→Removed)
	//
	// Phase 2: Deploy + gating with version handshake
	//   PartialEnablement (→Removed) →
	//   RunlevelGating (enables, converges, then upgrade + quota proves
	//     gating and batch-sequential ordering, converges at new version
	//     → AllReady)
	//
	// Phase 3: Steady-state tests (original version restored)
	//   PlatformReady (→AllReady, read-only) →
	//   ComponentStability (→AllReady, toggles KServe) →
	//   DAGCleanup (→Removed)

	// Phase 1: gate tests
	RunTestCases(t, []TestCase{
		{"Validate in-tree gates block and unblock provisioning", ctx.ValidateInTreeGates},
		{"Validate admin ack gates block and unblock provisioning", ctx.ValidateAdminAckGates},
	})

	// Phase 2: deploy + gating with version upgrade
	RunTestCases(t, []TestCase{
		{"Validate partial enablement", ctx.ValidatePartialEnablement},
		{"Validate runlevel gating and convergence", ctx.ValidateRunlevelGatingAndConvergence},
	})

	// Restore operator to original version after Phase 2 upgrade.
	// Components have platform release = 99.0.0. Restoring triggers
	// another gating cycle (version mismatch) until components reconcile.
	t.Log("Restoring operator to original version for Phase 3")
	ctx.removeOperatorEnvVars(t, "RHAI_VERSION", "CI")

	// Wait for the operator to acquire the leader lease and complete its
	// first reconciliation at the restored version. This ensures the
	// platform config ConfigMaps are updated with the correct
	// platformVersion BEFORE module operators are restarted. Without
	// this, module operators may read a stale ConfigMap (still containing
	// the Phase 2 version) because they only read platformVersion at
	// startup and never re-read it.
	t.Log("Waiting for operator to reconcile at restored version (ProvisioningProgress=False)")
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, ctx.PlatformNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	// Module operators are separate deployments that don't restart when
	// the main operator version changes. They still report the old
	// platform version, blocking the DAG readiness checker.
	// Force a rollout restart so they re-read the updated platform config.
	// TODO(dag): the operator should handle this automatically by
	// annotating module Deployments with a version hash so that a
	// platform version change triggers a pod restart.
	ctx.restartModuleOperators(t)

	t.Log("Waiting for convergence at original version")
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, ctx.PlatformNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	// Phase 3: steady-state tests
	RunTestCases(t, []TestCase{
		{"Validate PlatformReady condition on component CRs", ctx.ValidatePlatformReady},
		{"Validate component stability across enable/disable cycles (RHOAIENG-73142)", ctx.ValidateComponentStability},
		{"Validate DAG cleanup", ctx.ValidateDAGCleanup},
	})

	// Final cleanup: ensure cluster is left clean regardless of test outcomes
	t.Cleanup(func() {
		t.Log("Cleanup: setting all components to Removed")
		ctx.setAllRemoved(t)
		ctx.deleteDAGQuota()
	})
}

// ValidateRunlevelGatingAndConvergence proves that the DAG readiness
// gating mechanism works end-to-end. It first deploys all components at
// the original version, then simulates a version upgrade under a
// zero-pod quota. The version change makes the readiness checker detect
// a platform release mismatch on component CRs (old version != new
// operator version), blocking DAG advancement. The quota prevents
// batch-20 components from reconciling at the new version, keeping them
// stuck. Extension controllers (batch 31+) should be gated
// (PlatformReady=False) while batch 20 is blocked. Removing the quota
// lets components reconcile, proving convergence.
//
// The test leaves the operator at version 99.0.0-dag-test for
// subsequent tests (BatchOrdering). The suite restores the original
// version after Phase 2.
func (tc *DAGOrderingTestCtx) ValidateRunlevelGatingAndConvergence(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	// Guard against leftover quota from a prior crashed/interrupted run.
	tc.deleteDAGQuota()

	// Step 1: Deploy all components at the original version.
	// This establishes platform release version in component CR status.
	t.Log("Enabling all components for initial deployment and waiting for initial convergence (ComponentsReady=True and ModulesReady=True on DSC)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(allComponentsTransform("Managed")),
		WithCondition(And(
			jq.Match(
				`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue,
			),
			jq.Match(
				`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
				status.ConditionTypeModulesReady, metav1.ConditionTrue,
			),
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	// Step 2: Apply quota and simulate version upgrade.
	// Quota blocks new pod creation. Version change triggers readiness
	// gating: component CRs have the old platform release version which
	// no longer matches the new operator version.
	t.Log("Applying zero-pod quota to block deployments")
	tc.createDAGQuota()

	t.Log("Simulating version upgrade")
	tc.setOperatorEnvVars(t,
		corev1.EnvVar{Name: "CI", Value: "true"},
		corev1.EnvVar{Name: "RHAI_VERSION", Value: "99.0.0-dag-test"},
	)

	// Step 3: Verify gating is active.
	t.Log("Waiting for ProvisioningProgress=False on Platform CR (version mismatch + quota)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse,
		)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Verifying Extension controllers are gated (PlatformReady=False) while prior batches are stuck")
	for _, extGVK := range extensionGVKs {
		instanceName := tc.GetInstanceName(extGVK)
		tc.EnsureResourceExists(
			WithMinimalObject(extGVK, types.NamespacedName{Name: instanceName}),
			WithCondition(jq.Match(
				`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
				precondition.PlatformReadyConditionType, metav1.ConditionFalse,
			)),
			WithCustomErrorMsg("Extension %s should have PlatformReady=False while prior batches are gated", extGVK.Kind),
		)
	}
	t.Log("Confirmed: Extension controllers are gated")

	// Step 4: Remove quota and verify convergence at new version.
	t.Log("Removing quota to unblock deployments")
	tc.deleteDAGQuota()

	t.Log("Waiting for DAG convergence (ProvisioningProgress=True on Platform CR)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	t.Log("Verifying component controllers are unblocked (PlatformReady=True)")
	for _, batch := range dagBatches {
		for _, comp := range batch.components {
			if comp.internal {
				continue
			}
			tc.EnsureResourceExists(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: tc.GetInstanceName(comp.gvk)}),
				WithCondition(jq.Match(
					`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
					precondition.PlatformReadyConditionType, metav1.ConditionTrue,
				)),
				WithEventuallyTimeout(5*time.Minute),
				WithEventuallyPollingInterval(10*time.Second),
				WithCustomErrorMsg("%s should have PlatformReady=True after convergence", comp.name),
			)
		}
	}

	t.Log("Verifying ModulesReady=True on Platform CR")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeModulesReady, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(2*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Verifying module CRs are Ready")
	for _, batch := range dagBatches {
		for _, comp := range batch.components {
			if !comp.internal {
				continue
			}
			instanceName := tc.GetInstanceName(comp.gvk)
			if instanceName == "" {
				continue
			}
			u := tc.FetchResource(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: instanceName}),
			)
			if u == nil {
				continue
			}
			tc.EnsureResourceExists(
				WithMinimalObject(comp.gvk, types.NamespacedName{Name: instanceName}),
				WithCondition(jq.Match(
					`any(.status.conditions[]; .type == "Ready" and .status == "True")`,
				)),
				WithEventuallyTimeout(2*time.Minute),
				WithEventuallyPollingInterval(10*time.Second),
				WithCustomErrorMsg("module %s should have Ready=True after convergence", comp.name),
			)
		}
	}
	t.Log("DAG gating and version handshake verified")
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

// ValidateComponentStability verifies that toggling one component's
// ManagementState does not cause unrelated higher-RL components to be
// recreated (RHOAIENG-73142). Starting from AllReady state, it records
// SparkOperator CR UID, sets KServe to Removed then back to Managed,
// and asserts SparkOperator CR UID is unchanged.
func (tc *DAGOrderingTestCtx) ValidateComponentStability(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2, Tier3)

	t.Log("Ensuring KServe and SparkOperator are Managed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform(string(operatorv1.Managed), []string{"kserve", "sparkoperator"})),
	)

	sparkInstanceName := tc.GetInstanceName(gvk.SparkOperator)

	t.Log("Waiting for SparkOperator CR to exist and be Ready")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.SparkOperator, types.NamespacedName{Name: sparkInstanceName}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "Ready" and .status == "True")`,
		)),
		WithEventuallyTimeout(10*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	t.Log("Recording SparkOperator CR UID before toggle")
	sparkBefore := tc.FetchResource(
		WithMinimalObject(gvk.SparkOperator, types.NamespacedName{Name: sparkInstanceName}),
	)
	require.NotNil(t, sparkBefore, "SparkOperator CR should exist in AllReady state")
	uidBefore := sparkBefore.GetUID()
	require.NotEmpty(t, uidBefore, "SparkOperator CR should have a UID")

	t.Log("Setting KServe to Removed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform(string(operatorv1.Removed), []string{"kserve"})),
	)

	t.Log("Waiting for KServe CR to be deleted")
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: tc.GetInstanceName(gvk.Kserve)}),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	t.Log("Setting KServe back to Managed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(selectComponentsTransform(string(operatorv1.Managed), []string{"kserve"})),
		WithCondition(jq.Match(
			`.status.components.kserve.managementState == "%s"`,
			string(operatorv1.Managed),
		)),
	)

	t.Log("Waiting for KServe CR to be recreated and Ready")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: tc.GetInstanceName(gvk.Kserve)}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "Ready" and .status == "True")`,
		)),
		WithEventuallyTimeout(10*time.Minute),
		WithEventuallyPollingInterval(15*time.Second),
	)

	t.Log("Verifying SparkOperator CR UID is unchanged")
	sparkAfter := tc.FetchResource(
		WithMinimalObject(gvk.SparkOperator, types.NamespacedName{Name: sparkInstanceName}),
	)
	require.NotNil(t, sparkAfter, "SparkOperator CR should still exist")
	require.Equal(t, uidBefore, sparkAfter.GetUID(),
		"SparkOperator CR was recreated (UID changed) — RHOAIENG-73142 regression")
	t.Log("SparkOperator CR UID stable across KServe toggle")
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

	t.Log("Verifying enabled component CRs are created")
	enabledGVKs := []schema.GroupVersionKind{gvk.Dashboard, gvk.Kserve, gvk.ModelRegistry}
	for _, g := range enabledGVKs {
		instanceName := tc.GetInstanceName(g)
		tc.EnsureResourceExists(
			WithMinimalObject(g, types.NamespacedName{Name: instanceName}),
			WithEventuallyTimeout(5*time.Minute),
			WithEventuallyPollingInterval(10*time.Second),
			WithCustomErrorMsg("Enabled component CR %s should exist", g.Kind),
		)
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

	t.Log("Waiting for ProvisioningProgress=False with reason AdminAckRequired on Platform CR")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s" and .reason == "%s")`,
			status.ConditionTypeProvisioningProgress, metav1.ConditionFalse, status.AdminAckRequiredReason,
		)),
		WithEventuallyTimeout(3*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("Confirmed: provisioning blocked by two unacknowledged gates")

	t.Logf("Acknowledging only the first gate: %s", gateKey1)
	tc.patchAcksConfigMap(t, gateKey1, "true")

	t.Log("Verifying provisioning remains blocked (second gate still unacknowledged)")
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
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
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
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

// setOperatorEnvVars sets environment variables on the operator, triggering
// a single pod restart. For OLM installs it patches the CSV (OLM propagates to
// the Deployment). For non-OLM installs it patches the Deployment directly.
func (tc *DAGOrderingTestCtx) setOperatorEnvVars(t *testing.T, envs ...corev1.EnvVar) {
	t.Helper()

	if tc.patchSubscriptionEnvVars(t, envs) {
		t.Logf("Patched Subscription to set env vars: %v", envVarNames(envs))
	} else {
		t.Logf("No OLM Subscription found, patching Deployment directly")
		tc.patchDeploymentEnvVars(t, envs, nil)
	}

	tc.waitForOperatorReady(t)
}

// removeOperatorEnvVars removes environment variables from the operator
// and waits for the pod to restart.
func (tc *DAGOrderingTestCtx) removeOperatorEnvVars(t *testing.T, envNames ...string) {
	t.Helper()

	if tc.patchSubscriptionEnvVarRemovals(t, envNames) {
		t.Logf("Patched Subscription to remove env vars: %v", envNames)
	} else {
		t.Logf("No OLM Subscription found, patching Deployment directly")
		tc.patchDeploymentEnvVars(t, nil, envNames)
	}

	tc.waitForOperatorReady(t)
}

// patchSubscriptionEnvVars patches the operator Subscription to set env vars
// via spec.config.env. OLM propagates this to the CSV and Deployment.
// Returns false if no Subscription is found (non-OLM install).
func (tc *DAGOrderingTestCtx) patchSubscriptionEnvVars(t *testing.T, envs []corev1.EnvVar) bool {
	t.Helper()

	sub := tc.findSubscription(t)
	if sub == nil {
		return false
	}

	if sub.Spec.Config == nil {
		sub.Spec.Config = &ofapi.SubscriptionConfig{}
	}

	for _, env := range envs {
		sub.Spec.Config.Env = slices.DeleteFunc(sub.Spec.Config.Env, func(e corev1.EnvVar) bool {
			return e.Name == env.Name
		})
		sub.Spec.Config.Env = append(sub.Spec.Config.Env, env)
	}

	err := tc.Client().Update(tc.Context(), sub)
	require.NoError(t, err, "Failed to update Subscription %s", sub.Name)

	return true
}

// patchSubscriptionEnvVarRemovals patches the operator Subscription to remove
// env vars. Returns false if no Subscription is found (non-OLM install).
func (tc *DAGOrderingTestCtx) patchSubscriptionEnvVarRemovals(t *testing.T, envNames []string) bool {
	t.Helper()

	sub := tc.findSubscription(t)
	if sub == nil {
		return false
	}

	if sub.Spec.Config == nil {
		return true
	}

	for _, name := range envNames {
		sub.Spec.Config.Env = slices.DeleteFunc(sub.Spec.Config.Env, func(e corev1.EnvVar) bool {
			return e.Name == name
		})
	}

	err := tc.Client().Update(tc.Context(), sub)
	require.NoError(t, err, "Failed to update Subscription %s", sub.Name)

	return true
}

// findSubscription returns the operator Subscription, or nil if not found.
func (tc *DAGOrderingTestCtx) findSubscription(t *testing.T) *ofapi.Subscription {
	t.Helper()

	subList := &ofapi.SubscriptionList{}
	err := tc.Client().List(tc.Context(), subList, client.InNamespace(tc.OperatorNamespace))
	if err != nil {
		return nil
	}

	subIdx := slices.IndexFunc(subList.Items, func(sub ofapi.Subscription) bool {
		return strings.Contains(sub.Name, "opendatahub") || strings.Contains(sub.Name, "rhods")
	})
	if subIdx == -1 {
		return nil
	}

	return &subList.Items[subIdx]
}

// patchDeploymentEnvVars patches the operator Deployment to add/remove env vars
// in a single mutation. toSet are added/updated, toRemove are removed by name.
func (tc *DAGOrderingTestCtx) patchDeploymentEnvVars(t *testing.T, toSet []corev1.EnvVar, toRemove []string) {
	t.Helper()

	removeSet := make(map[string]bool, len(toRemove))
	for _, name := range toRemove {
		removeSet[name] = true
	}
	for _, env := range toSet {
		removeSet[env.Name] = true
	}

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Deployment, tc.operatorDeploymentNN()),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			if len(containers) == 0 {
				return nil
			}
			container, ok := containers[0].(map[string]any)
			if !ok {
				return nil
			}
			env, _ := container["env"].([]any)

			var filtered []any
			for _, e := range env {
				if entry, ok := e.(map[string]any); ok {
					if name, _ := entry["name"].(string); removeSet[name] {
						continue
					}
				}
				filtered = append(filtered, e)
			}

			for _, ev := range toSet {
				filtered = append(filtered, map[string]any{"name": ev.Name, "value": ev.Value})
			}

			container["env"] = filtered
			containers[0] = container
			return unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")
		}),
	)
}

func envVarNames(envs []corev1.EnvVar) []string {
	names := make([]string, 0, len(envs))
	for _, e := range envs {
		names = append(names, e.Name)
	}
	return names
}

// waitForOperatorReady waits for the operator deployment to have all replicas ready.
func (tc *DAGOrderingTestCtx) waitForOperatorReady(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, tc.operatorDeploymentNN()),
		WithCondition(jq.Match(`.status.readyReplicas == .status.replicas and .status.updatedReplicas == .status.replicas`)),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
}

func (tc *DAGOrderingTestCtx) operatorDeploymentName() string {
	if cluster.GetRelease().Name == cluster.SelfManagedRhoai || cluster.GetRelease().Name == cluster.ManagedRhoai {
		return controllerDeploymentRhoai
	}
	return controllerDeploymentODH
}

func (tc *DAGOrderingTestCtx) operatorDeploymentNN() types.NamespacedName {
	return types.NamespacedName{Name: tc.operatorDeploymentName(), Namespace: tc.OperatorNamespace}
}

// restartModuleOperators forces a restart of module operator pods in
// the applications namespace. Module operators are separate processes that
// don't automatically detect platform version changes; a restart causes
// them to re-read the platform config ConfigMap and report the updated
// version in their module CR status.
func (tc *DAGOrderingTestCtx) restartModuleOperators(t *testing.T) {
	t.Helper()

	deps := tc.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.SelectorFromSet(k8slabels.Set{
				labels.PlatformPartOf: labels.Platform,
			}),
		}),
	)

	for _, dep := range deps {
		name := dep.GetName()

		matchLabels, _, _ := unstructured.NestedStringMap(dep.Object, "spec", "selector", "matchLabels")
		if len(matchLabels) == 0 {
			t.Logf("Deployment %s has no selector matchLabels, skipping", name)
			continue
		}

		pods := tc.FetchResources(
			WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.AppsNamespace}),
			WithListOptions(&client.ListOptions{
				Namespace:     tc.AppsNamespace,
				LabelSelector: k8slabels.SelectorFromSet(matchLabels),
			}),
		)

		for _, pod := range pods {
			t.Logf("Deleting pod %s (module operator %s)", pod.GetName(), name)
			tc.DeleteResource(
				WithMinimalObject(gvk.Pod, types.NamespacedName{
					Namespace: pod.GetNamespace(),
					Name:      pod.GetName(),
				}),
			)
		}
	}
}
