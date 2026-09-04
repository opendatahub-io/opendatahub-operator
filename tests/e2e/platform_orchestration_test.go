package e2e_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type platformComponent struct {
	componentGVK   schema.GroupVersionKind
	dscFieldName   string
	conditionKind  string
	crdName        string
	releasesField  string
	deploymentName string
	longReadiness  bool
	// enableField overrides dscFieldName for enable/disable operations.
	// Used by implicitly-managed components (e.g. ModelController is controlled by Kserve).
	enableField string
}

func allPlatformComponents() []platformComponent {
	return []platformComponent{
		{
			componentGVK:   gvk.Dashboard,
			dscFieldName:   componentApi.DashboardComponentName,
			crdName:        "dashboards.components.platform.opendatahub.io",
			releasesField:  componentApi.DashboardComponentName,
			deploymentName: "dashboard-operator",
		},
		{
			componentGVK:   gvk.Workbenches,
			dscFieldName:   componentApi.WorkbenchesComponentName,
			crdName:        "workbenches.components.platform.opendatahub.io",
			releasesField:  componentApi.WorkbenchesComponentName,
			deploymentName: "workbenches-operator",
		},
		{
			componentGVK:  gvk.DataSciencePipelines,
			dscFieldName:  aiPipelinesFieldName,
			conditionKind: componentApi.AIPipelinesKind,
			crdName:       "datasciencepipelines.components.platform.opendatahub.io",
			releasesField: aiPipelinesFieldName,
		},
		{
			componentGVK:   gvk.Kserve,
			dscFieldName:   componentApi.KserveComponentName,
			crdName:        "kserves.components.platform.opendatahub.io",
			releasesField:  componentApi.KserveComponentName,
			deploymentName: "kserve-module-controller-manager",
			longReadiness:  true,
		},
		{
			componentGVK:  gvk.Kueue,
			dscFieldName:  componentApi.KueueComponentName,
			crdName:       "kueues.components.platform.opendatahub.io",
			releasesField: componentApi.KueueComponentName,
		},
		{
			componentGVK:  gvk.Ray,
			dscFieldName:  componentApi.RayComponentName,
			crdName:       "rays.components.platform.opendatahub.io",
			releasesField: componentApi.RayComponentName,
		},
		{
			componentGVK:  gvk.TrustyAI,
			dscFieldName:  componentApi.TrustyAIComponentName,
			crdName:       "trustyais.components.platform.opendatahub.io",
			releasesField: componentApi.TrustyAIComponentName,
		},
		{
			componentGVK:   gvk.AIHub,
			dscFieldName:   componentApi.ModelRegistryComponentName,
			conditionKind:  componentApi.ModelRegistryKind,
			releasesField:  componentApi.ModelRegistryComponentName,
			deploymentName: "aihub-controller-manager",
		},
		{
			componentGVK:   gvk.FeastOperator,
			dscFieldName:   componentApi.FeastOperatorComponentName,
			crdName:        "feastoperators.components.platform.opendatahub.io",
			releasesField:  componentApi.FeastOperatorComponentName,
			deploymentName: "opendatahub-feast-operator",
		},
		{
			componentGVK:   gvk.OGX,
			dscFieldName:   componentApi.OGXComponentName,
			crdName:        "ogxs.components.platform.opendatahub.io",
			releasesField:  componentApi.OGXComponentName,
			deploymentName: "opendatahub-ogx-operator",
		},
		{
			componentGVK:   gvk.MLflowOperator,
			dscFieldName:   componentApi.MLflowOperatorComponentName,
			crdName:        "mlflowoperators.components.platform.opendatahub.io",
			releasesField:  componentApi.MLflowOperatorComponentName,
			deploymentName: "mlflow-operator-controller-manager",
		},
		{
			componentGVK:  gvk.Trainer,
			dscFieldName:  componentApi.TrainerComponentName,
			crdName:       "trainers.components.platform.opendatahub.io",
			releasesField: componentApi.TrainerComponentName,
		},
		{
			componentGVK:  gvk.SparkOperator,
			dscFieldName:  componentApi.SparkOperatorComponentName,
			crdName:       "sparkoperators.components.platform.opendatahub.io",
			releasesField: componentApi.SparkOperatorComponentName,
		},
	}
}

type PlatformOrchestrationTestCtx struct {
	*TestContext

	componentGVK   schema.GroupVersionKind
	componentNN    types.NamespacedName
	dscFieldName   string
	conditionKind  string
	crdName        string
	releasesField  string
	deploymentName string
	enableField    string
	longReadiness  bool
}

func newPlatformOrchestrationTestCtx(t *testing.T, tc *TestContext, pc platformComponent) *PlatformOrchestrationTestCtx {
	t.Helper()

	conditionKind := pc.conditionKind
	if conditionKind == "" {
		conditionKind = pc.componentGVK.Kind
	}

	return &PlatformOrchestrationTestCtx{
		TestContext:    tc,
		componentGVK:   pc.componentGVK,
		componentNN:    types.NamespacedName{Name: tc.GetInstanceName(pc.componentGVK)},
		dscFieldName:   pc.dscFieldName,
		conditionKind:  conditionKind,
		crdName:        pc.crdName,
		releasesField:  pc.releasesField,
		deploymentName: pc.deploymentName,
		enableField:    pc.enableField,
		longReadiness:  pc.longReadiness,
	}
}

func (ctx *PlatformOrchestrationTestCtx) readinessTimeout() time.Duration {
	if ctx.longReadiness {
		return ctx.TestTimeouts.longEventuallyTimeout
	}
	return ctx.TestTimeouts.componentReadinessTimeout
}

// operatorResourceListOpts returns ListOptions to find resources belonging to
// this component's operator.
func (ctx *PlatformOrchestrationTestCtx) operatorResourceListOpts() *client.ListOptions {
	if ctx.deploymentName != "" {
		return &client.ListOptions{
			Namespace:     ctx.AppsNamespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", ctx.deploymentName),
		}
	}
	return &client.ListOptions{
		Namespace: ctx.AppsNamespace,
		LabelSelector: k8slabels.Set{
			labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
		}.AsSelector(),
	}
}

func (ctx *PlatformOrchestrationTestCtx) effectiveEnableField() string {
	if ctx.enableField != "" {
		return ctx.enableField
	}
	return ctx.dscFieldName
}

func (ctx *PlatformOrchestrationTestCtx) isImplicitlyManaged() bool {
	return ctx.enableField != ""
}

func platformOrchestrationTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	if tc.IsXKS() {
		tc.EnsurePlatformCR(t)
	}

	components, err := filterPlatformComponents(allPlatformComponents(), platformComponentFlags)
	require.NoError(t, err)

	if !tc.IsXKS() {
		// Dynamic check: verify allPlatformComponents stays in sync with dscv2.Components.
		// Keep this list next to allPlatformComponents() when adding/removing DSC fields.
		allComponents := allPlatformComponents()
		dscComponentCount := reflect.TypeFor[dscv2.Components]().NumField()
		excludedFromOrchestration := []string{
			"LlamaStackOperator",   // deprecated
			"TrainingOperator",     // deprecated — handler removed, CEL blocks re-enablement
			"AIGateway",            // module
			"MCPLifecycleOperator", // module
		}
		expectedOrchestrationCount := dscComponentCount - len(excludedFromOrchestration)
		require.Len(t, allComponents, expectedOrchestrationCount,
			"allPlatformComponents() is out of sync with dscv2.Components struct. "+
				"Expected %d testable components but found %d. (Total DSC components: %d, Excluded: %d — %s)",
			expectedOrchestrationCount, len(allComponents), dscComponentCount, len(excludedFromOrchestration),
			strings.Join(excludedFromOrchestration, ", "))
	}

	for _, pc := range components {
		ctx := newPlatformOrchestrationTestCtx(t, tc, pc)
		name := ctx.dscFieldName

		t.Run(name, func(t *testing.T) {
			testCases := []TestCase{
				{"Bootstrap: component CR created when enabled", ctx.TestComponentCRCreatedWhenEnabled},
				{"Bootstrap: operator deployment exists and is available", ctx.TestOperatorDeploymentAvailable},
				{"Bootstrap: RBAC resources exist", ctx.TestRBACResourcesExist},
				{"Bootstrap: APPLICATIONS_NAMESPACE env var injected", ctx.TestApplicationsNamespaceInjected},
				{"Bootstrap: RELATED_IMAGE env vars injected", ctx.TestRelatedImageEnvVarsInjected},
				{"Lifecycle: component CR owned by orchestrator", ctx.TestComponentCROwnership},
				{"Spec projection: managementState reflected on component CR", ctx.TestSpecProjectionManagementState},
				{"Spec projection: tampered spec field restored by reconciler", ctx.TestSpecProjectionDSCPatchPropagated},
				{"Spec projection: SSA idempotency", ctx.TestSSAIdempotency},
				{"Contract: observedGeneration matches generation", ctx.TestObservedGenerationMatchesGeneration},
				{"Contract: singleton enforcement rejects duplicate CR", ctx.TestSingletonEnforcement},
				{"Status: component Ready propagated to orchestrator", ctx.TestStatusReadyPropagated},
				{"Status: ProvisioningSucceeded condition set", ctx.TestProvisioningSucceededCondition},
				{"Status: component releases populated on DSC", ctx.TestComponentReleasesPopulated},
				{"Resilience: deleted component CR is recreated", ctx.TestDeletedComponentCRRecreated},
				{"Resilience: deleted operator deployment is recreated", ctx.TestDeletedDeploymentRecreated},
				{"Disable lifecycle: CR deleted, CRD preserved, status propagated, deployments cleaned", ctx.TestDisableLifecycle},
			}

			RunTestCases(t, testCases)
		})
	}
}

func (ctx *PlatformOrchestrationTestCtx) TestComponentCRCreatedWhenEnabled(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestOperatorDeploymentAvailable(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(ctx.operatorResourceListOpts()),
		WithCondition(HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
		)),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestRBACResourcesExist(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourcesExist(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(ctx.operatorResourceListOpts()),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestApplicationsNamespaceInjected(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	deployments := ctx.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(ctx.operatorResourceListOpts()),
	)

	require.NotEmpty(t, deployments, "expected at least one operator deployment for %s",
		ctx.dscFieldName)

	for _, dep := range deployments {
		if !deploymentHasEnvVar(dep, "APPLICATIONS_NAMESPACE") {
			continue
		}

		ctx.EnsureResourceExists(
			WithMinimalObject(gvk.Deployment, types.NamespacedName{
				Name:      dep.GetName(),
				Namespace: dep.GetNamespace(),
			}),
			WithCondition(
				jq.Match(`.spec.template.spec.containers[].env[]? | select(.name == "APPLICATIONS_NAMESPACE") | .value == "%s"`, ctx.AppsNamespace),
			),
			WithCustomErrorMsg("Deployment %s should have APPLICATIONS_NAMESPACE=%s", dep.GetName(), ctx.AppsNamespace),
		)

		return
	}

	t.Skip("No deployments with APPLICATIONS_NAMESPACE env var found")
}

func (ctx *PlatformOrchestrationTestCtx) TestRelatedImageEnvVarsInjected(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	deployments := ctx.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(ctx.operatorResourceListOpts()),
	)

	require.NotEmpty(t, deployments, "expected at least one operator deployment for %s",
		ctx.dscFieldName)

	dep := ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      deployments[0].GetName(),
			Namespace: deployments[0].GetNamespace(),
		}),
	)

	if !deploymentHasEnvVarPrefix(dep, "RELATED_IMAGE_") {
		t.Skip("No RELATED_IMAGE_* env vars found (non-CI environment)")
	}
}

func (ctx *PlatformOrchestrationTestCtx) TestComponentCROwnership(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ownerKind := gvk.DataScienceCluster.Kind
	if ctx.IsXKS() {
		ownerKind = gvk.Platform.Kind
	}

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, ownerKind),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestDisableLifecycle(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	if ctx.isImplicitlyManaged() {
		t.Skipf("Skipping disable lifecycle for implicitly-managed component %s (controlled via %s)", ctx.dscFieldName, ctx.enableField)
	}

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)

	ctx.setComponentManagementState(operatorv1.Removed)

	ctx.EnsureResourceGone(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)

	if ctx.crdName != "" {
		ctx.EnsureResourceExists(
			WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: ctx.crdName}),
			WithCustomErrorMsg("CRD %s should NOT be deleted when component is disabled", ctx.crdName),
		)
	}

	ctx.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(ctx.operatorResourceListOpts()),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)

	if !ctx.IsXKS() {
		conditionType := ctx.conditionKind + "Ready"

		ctx.EnsureResourceExists(
			WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
			WithCondition(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
					conditionType, metav1.ConditionFalse),
			),
		)
	}

	ctx.ensureComponentEnabled(t)
}

func (ctx *PlatformOrchestrationTestCtx) TestSpecProjectionManagementState(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	if ctx.IsXKS() {
		t.Skip("DSC spec projection not applicable on xKS (uses Platform CR)")
	}

	if ctx.isImplicitlyManaged() {
		t.Skipf("Skipping spec projection for implicitly-managed component %s (no direct DSC field)", ctx.dscFieldName)
	}

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.spec.components.%s.managementState == "%s"`, ctx.dscFieldName, operatorv1.Managed),
		),
	)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestSpecProjectionDSCPatchPropagated(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	if ctx.IsXKS() {
		t.Skip("DSC spec projection not applicable on xKS (uses Platform CR)")
	}

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.spec.managementState == "%s"`, operatorv1.Managed),
		),
	)

	ctx.EventuallyResourcePatched(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithMutateFunc(testf.Transform(`.spec.managementState = "%s"`, operatorv1.Removed)),
		WithCondition(
			jq.Match(`.spec.managementState == "%s"`, operatorv1.Removed),
		),
	)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.spec.managementState == "%s"`, operatorv1.Managed),
		),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestSSAIdempotency(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	cr := ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)

	gen := cr.GetGeneration()

	triggerGVK := gvk.DataScienceCluster
	triggerNN := ctx.DataScienceClusterNamespacedName
	if ctx.IsXKS() {
		triggerGVK = gvk.Platform
		triggerNN = ctx.PlatformNamespacedName
	}

	ctx.EventuallyResourcePatched(
		WithMinimalObject(triggerGVK, triggerNN),
		WithMutateFunc(testf.Transform(`.metadata.annotations["ssa-idempotency-trigger"] = "%d"`, gen)),
		WithCondition(
			jq.Match(`.metadata.annotations["ssa-idempotency-trigger"] == "%d"`, gen),
		),
	)

	ctx.EnsureResourceExistsConsistently(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.metadata.generation == %d`, gen),
		),
		WithConsistentlyDuration(ctx.TestTimeouts.defaultConsistentlyTimeout),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestStatusReadyPropagated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	if ctx.IsXKS() {
		t.Skip("DSC status aggregation not applicable on xKS")
	}

	ctx.ensureComponentEnabled(t)

	conditionType := ctx.conditionKind + "Ready"

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				conditionType, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestProvisioningSucceededCondition(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestComponentReleasesPopulated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke)

	if ctx.IsXKS() {
		t.Skip("DSC releases status not applicable on xKS")
	}

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, ctx.dscFieldName, operatorv1.Managed),
			jq.Match(`.status.components.%s.releases | length > 0`, ctx.releasesField),
			jq.Match(`.status.components.%s.releases[].name != ""`, ctx.releasesField),
			jq.Match(`.status.components.%s.releases[].version != ""`, ctx.releasesField),
			jq.Match(`[.status.components.%s.releases[] | select(.name == "platform")] | length > 0`, ctx.releasesField),
		)),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestObservedGenerationMatchesGeneration(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.observedGeneration == .metadata.generation`),
		),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestSingletonEnforcement(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	duplicate := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": ctx.componentGVK.GroupVersion().String(),
			"kind":       ctx.componentGVK.Kind,
			"metadata":   map[string]any{"name": "duplicate-" + strings.ToLower(ctx.componentGVK.Kind)},
			"spec":       map[string]any{},
		},
	}

	err := ctx.Client().Create(ctx.Context(), duplicate)
	require.Error(t, err, "Creating a duplicate %s CR should be rejected by admission", ctx.componentGVK.Kind)
}

func (ctx *PlatformOrchestrationTestCtx) TestDeletedComponentCRRecreated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	savedOpts := ctx.DefaultResourceOpts
	ctx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ctx.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(ctx.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { ctx.DefaultResourceOpts = savedOpts }()

	ctx.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestDeletedDeploymentRecreated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	savedOpts := ctx.DefaultResourceOpts
	ctx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ctx.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(ctx.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { ctx.DefaultResourceOpts = savedOpts }()

	deployments := ctx.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(ctx.operatorResourceListOpts()),
	)

	require.NotEmpty(t, deployments, "expected at least one operator deployment for %s",
		ctx.dscFieldName)

	ctx.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      deployments[0].GetName(),
			Namespace: deployments[0].GetNamespace(),
		}),
	)
}

func deploymentHasEnvVar(dep unstructured.Unstructured, envName string) bool {
	return deploymentHasEnvVarFunc(&dep, func(name string) bool { return name == envName })
}

func deploymentHasEnvVarPrefix(dep *unstructured.Unstructured, prefix string) bool {
	return deploymentHasEnvVarFunc(dep, func(name string) bool { return strings.HasPrefix(name, prefix) })
}

func deploymentHasEnvVarFunc(dep *unstructured.Unstructured, match func(string) bool) bool {
	containers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
	for _, c := range containers {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		envList, ok := cm["env"].([]any)
		if !ok {
			continue
		}
		for _, e := range envList {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			name, _ := em["name"].(string)
			if match(name) {
				return true
			}
		}
	}
	return false
}

func filterPlatformComponents(all []platformComponent, flags []string) ([]platformComponent, error) {
	if len(flags) == 0 {
		flags = []string{componentApi.DashboardComponentName}
	}

	valid := make(map[string]bool, len(all))
	for _, pc := range all {
		valid[pc.dscFieldName] = true
	}

	selected := make(map[string]bool, len(flags))
	for _, f := range flags {
		name := strings.ToLower(strings.TrimSpace(f))
		if !valid[name] {
			validNames := make([]string, 0, len(all))
			for _, pc := range all {
				validNames = append(validNames, pc.dscFieldName)
			}

			return nil, fmt.Errorf("unsupported --test-platform-component value %q, valid values are: %s", f, strings.Join(validNames, ", "))
		}

		selected[name] = true
	}

	var filtered []platformComponent
	for _, pc := range all {
		if selected[pc.dscFieldName] {
			filtered = append(filtered, pc)
		}
	}

	return filtered, nil
}

func (ctx *PlatformOrchestrationTestCtx) ensureComponentEnabled(t *testing.T) {
	t.Helper()

	field := ctx.effectiveEnableField()

	if ctx.IsXKS() {
		ctx.SetModuleStateInPlatformCR(t, field, operatorv1.Managed)

		ctx.EnsureResourceExists(
			WithMinimalObject(ctx.componentGVK, ctx.componentNN),
			WithCondition(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
					status.ConditionTypeReady, metav1.ConditionTrue),
			),
			WithEventuallyTimeout(ctx.readinessTimeout()),
		)

		return
	}

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, field, operatorv1.Managed)),
		WithCondition(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, field, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
				ctx.conditionKind, metav1.ConditionTrue),
		)),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)
}

func (ctx *PlatformOrchestrationTestCtx) setComponentManagementState(state operatorv1.ManagementState) {
	field := ctx.effectiveEnableField()

	if ctx.IsXKS() {
		ctx.EventuallyResourcePatched(
			WithMinimalObject(gvk.Platform, ctx.PlatformNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.modules.%s.managementState = "%s"`, field, state)),
			WithCondition(
				jq.Match(`.spec.modules.%s.managementState == "%s"`, field, state),
			),
			WithEventuallyTimeout(ctx.readinessTimeout()),
		)

		return
	}

	readyStatus := metav1.ConditionFalse
	if state == operatorv1.Managed {
		readyStatus = metav1.ConditionTrue
	}

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, field, state)),
		WithCondition(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, field, state),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
				ctx.conditionKind, readyStatus),
		)),
		WithEventuallyTimeout(ctx.readinessTimeout()),
	)
}
