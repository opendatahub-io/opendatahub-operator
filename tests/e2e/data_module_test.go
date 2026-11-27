package e2e_test

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dataModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/data"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	dataModuleOperatorDeployment = "opendatahub-feast-operator"
	dataModuleCRName             = componentApi.FeastOperatorInstanceName
	feastOperatorDeploymentName  = "feast-operator-controller-manager"
	dataReadyCondition           = "DataReady"
)

var dataModuleCRGVK = gvk.FeastOperator

type DataModuleTestCtx struct {
	*ComponentTestCtx
}

func dataModuleTestSuite(t *testing.T) {
	t.Helper()

	baseCtx, err := NewModuleTestCtx(t, dataModule.NewHandler())
	require.NoError(t, err)

	ctx := DataModuleTestCtx{ComponentTestCtx: baseCtx}

	testCases := []TestCase{
		{"Validate component enabled", ctx.ValidateComponentEnabled},
		{"Validate module enabled", ctx.ValidateModuleEnabled},
		{"Validate module operator deployed", ctx.ValidateModuleOperatorDeployed},
		{"Validate module CR created", ctx.ValidateModuleCRCreated},
		{"Validate module CR ready", ctx.ValidateModuleCRReady},
		{"Validate DataReady condition", ctx.ValidateDataReadyCondition},
		{"Validate v2 DSC Data selection", ctx.ValidateV2DSCDataSelection},
		{"Validate Data Registry enables module independently", ctx.ValidateDataRegistryAloneEnablesModule},
		{"Validate DCH shared-module projection", ctx.ValidateDataConnectHubProjection},
		{"Validate module disabled cleanup", ctx.ValidateModuleDisabledCleanup},
		{"Validate module disabled", ctx.ValidateModuleDisabled},
	}

	RunTestCases(t, testCases)
}

// ValidateDataConnectHubProjection exercises only the DSC -> Platform -> module
// CR contract. The Data module owns application rendering and operand lifecycle.
func (ctx *DataModuleTestCtx) ValidateDataConnectHubProjection(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier3)
	if ctx.IsXKS() {
		t.Skip("DCH DSC projection requires the OpenShift platform projection")
	}
	const (
		gatewayName = "dch-e2e-gateway"
		audience    = "https://dch-e2e.example"
	)
	moduleNN := types.NamespacedName{Name: dataModuleCRName}
	snapshotDSCFields(t, ctx.TestContext, gvk.DataScienceCluster, []string{"spec", "components", "data"})

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.data.featureStore.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataRegistry.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataConnectHub = {
				"managementState":"Managed",
				"gateway":{"name":%q,"namespace":%q},
				"tokenReviewAudiences":[%q]
			}`, gatewayName, ctx.AppsNamespace, audience),
		)),
		WithCondition(jq.Match(`.spec.components.data.dataConnectHub.managementState == "Managed"`)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, ctx.PlatformNamespacedName),
		WithCondition(jq.Match(`.spec.modules.data.managementState == "Managed"`)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(dataModuleCRGVK, moduleNN),
		WithCondition(And(
			jq.Match(`.spec.capabilities.featureStore.managementState == "Removed"`),
			jq.Match(`.spec.capabilities.dataRegistry.managementState == "Removed"`),
			jq.Match(`.spec.capabilities.dataConnectHub.managementState == "Managed"`),
			jq.Match(`.spec.capabilities.dataConnectHub.namespace == %q`, componentApi.DefaultDataConnectHubNamespace),
			jq.Match(`.spec.capabilities.dataConnectHub.gateway.name == %q`, gatewayName),
			jq.Match(`.spec.capabilities.dataConnectHub.gateway.namespace == %q`, ctx.AppsNamespace),
			jq.Match(`.spec.capabilities.dataConnectHub.tokenReviewAudiences == [%q]`, audience),
		)),
	)
	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.data.featureStore.managementState = "Managed"`),
			testf.Transform(`.spec.components.data.dataConnectHub.managementState = "Removed"`),
		)),
		WithCondition(jq.Match(`.spec.components.data.dataConnectHub.managementState == "Removed"`)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(dataModuleCRGVK, moduleNN),
		WithCondition(And(
			jq.Match(`.spec.capabilities.featureStore.managementState == "Managed"`),
			jq.Match(`.spec.capabilities.dataConnectHub.managementState == "Removed"`),
		)),
	)
	ctx.ValidateModuleOperatorDeployed(t)
}

// ValidateV2DSCDataSelection proves that the v2 FeastOperator stanza still
// drives the v3 FeatureStore runtime and maps DataRegistry into the v3 DSC.
func (ctx *DataModuleTestCtx) ValidateV2DSCDataSelection(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier3)
	if ctx.IsXKS() {
		t.Skip("v2 DSC conversion smoke is not supported on XKS")
	}

	snapshotDSCFields(t, ctx.TestContext, gvk.DataScienceClusterV2, []string{"spec", "components", "feastoperator"})
	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceClusterV2, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.feastoperator.managementState = "Managed"`),
			testf.Transform(`.spec.components.feastoperator.dataRegistry.managementState = "Managed"`),
		)),
		WithCondition(And(
			jq.Match(`.spec.components.feastoperator.managementState == "Managed"`),
			jq.Match(`.spec.components.feastoperator.dataRegistry.managementState == "Managed"`),
		)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.spec.components.data.featureStore.managementState == "Managed"`),
			jq.Match(`.spec.components.data.dataRegistry.managementState == "Managed"`),
		)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(dataModuleCRGVK, types.NamespacedName{Name: dataModuleCRName}),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.spec.capabilities.featureStore.managementState == "Managed"`),
			jq.Match(`.spec.capabilities.dataRegistry.managementState == "Managed"`),
		)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace, Name: dataModuleOperatorDeployment}),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.components.data.managementState == "Managed"`)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceClusterV2, ctx.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.spec.components.feastoperator.managementState == "Managed"`),
			jq.Match(`.spec.components.feastoperator.dataRegistry.managementState == "Managed"`),
			jq.Match(`.status.components.feastoperator.managementState == "Managed"`),
		)),
	)
}

// ValidateComponentEnabled patches the DSC to set the Feature Store to Managed,
// triggering the module controller to deploy the feast module operator.
func (ctx *DataModuleTestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.data.featureStore.managementState = "Managed"`)),
		WithCondition(jq.Match(`.spec.components.data.featureStore.managementState == "Managed"`)),
	)
}

// ValidateModuleOperatorDeployed checks that the opendatahub-feast-operator
// Deployment exists and is available (deployed by the platform's Helm action).
func (ctx *DataModuleTestCtx) ValidateModuleOperatorDeployed(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	nn := types.NamespacedName{
		Name:      dataModuleOperatorDeployment,
		Namespace: ctx.AppsNamespace,
	}

	g.Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(ctx.Client().Get(context.Background(), nn, deploy)).To(Succeed())
		g.Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1))
	}).
		WithTimeout(3*time.Minute).
		WithPolling(5*time.Second).
		Should(Succeed(), "module operator Deployment should be available")
}

// ValidateModuleCRCreated checks that the FeastOperator CR (v1alpha1) was created
// by the platform's provisionModules action.
func (ctx *DataModuleTestCtx) ValidateModuleCRCreated(t *testing.T) {
	t.Helper()
	g := NewWithT(t)
	dsc := ctx.EnsureResourceExists(WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName))

	g.Eventually(func(g Gomega) {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(dataModuleCRGVK)
		g.Expect(ctx.Client().Get(t.Context(), types.NamespacedName{Name: dataModuleCRName}, cr)).To(Succeed())
		owner := metav1.GetControllerOf(cr)
		g.Expect(owner).NotTo(BeNil())
		g.Expect(owner.Kind).To(Equal(gvk.DataScienceCluster.Kind))
		g.Expect(owner.Name).To(Equal(dsc.GetName()))
		g.Expect(owner.UID).To(Equal(dsc.GetUID()))
	}).
		WithTimeout(2*time.Minute).
		WithPolling(5*time.Second).
		Should(Succeed(), "FeastOperator CR should be created by the platform")
}

// ValidateModuleCRReady checks that the FeastOperator CR reports Ready=True,
// allowing the platform to aggregate the module's readiness contract.
func (ctx *DataModuleTestCtx) ValidateModuleCRReady(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	g.Eventually(func(g Gomega) {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(dataModuleCRGVK)
		g.Expect(ctx.Client().Get(context.Background(), types.NamespacedName{Name: dataModuleCRName}, cr)).To(Succeed())
		observed, found, err := unstructured.NestedInt64(cr.Object, "status", "observedGeneration")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue(), "module status must identify its observed generation")
		g.Expect(observed).To(Equal(cr.GetGeneration()))

		conditions, found, err := unstructured.NestedSlice(cr.Object, "status", "conditions")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue(), ".status.conditions should exist")

		readyFound := false
		for _, c := range conditions {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if cm["type"] == "Ready" {
				g.Expect(cm["status"]).To(Equal("True"), "FeastOperator CR should be Ready")
				readyFound = true
				break
			}
		}
		g.Expect(readyFound).To(BeTrue(), "Ready condition should exist")
	}).
		WithTimeout(5*time.Minute).
		WithPolling(10*time.Second).
		Should(Succeed(), "FeastOperator CR should become Ready")
}

// ValidateDataReadyCondition checks that the enabled Data module reports its
// v3 readiness condition on the DataScienceCluster.
func (ctx *DataModuleTestCtx) ValidateDataReadyCondition(t *testing.T) {
	t.Helper()

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, dataReadyCondition)),
		WithCustomErrorMsg("DataScienceCluster should have %s condition set to True", dataReadyCondition),
	)
}

// ValidateDataRegistryAloneEnablesModule verifies how Feature Store and Data
// Registry management states are projected to Platform, DSC status, and the
// FeastOperator capabilities.
func (ctx *DataModuleTestCtx) ValidateDataRegistryAloneEnablesModule(t *testing.T) {
	t.Helper()
	snapshotDSCFields(t, ctx.TestContext, gvk.DataScienceCluster, []string{"spec", "components", "data"})

	setStatesAndCheckProjection := func(featureStoreState, dataRegistryState, expectedState operatorv1.ManagementState) {
		ctx.EventuallyResourcePatched(
			WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.TransformPipeline(
				testf.Transform(`.spec.components.data.featureStore.managementState = %q`, featureStoreState),
				testf.Transform(`.spec.components.data.dataRegistry.managementState = %q`, dataRegistryState),
				testf.Transform(`.spec.components.data.dataConnectHub.managementState = "Removed"`),
			)),
			WithCondition(And(
				jq.Match(`(.spec.components.data.featureStore.managementState // "") == %q`, featureStoreState),
				jq.Match(`(.spec.components.data.dataRegistry.managementState // "") == %q`, dataRegistryState),
				jq.Match(`.spec.components.data.dataConnectHub.managementState == "Removed"`),
			)),
		)

		ctx.EnsureResourceExists(
			WithMinimalObject(gvk.Platform, ctx.PlatformNamespacedName),
			WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
			WithCondition(jq.Match(`.spec.modules.data.managementState == %q`, expectedState)),
		)
		ctx.EnsureResourceExists(
			WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
			WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
			WithCondition(jq.Match(`.status.components.data.managementState == %q`, expectedState)),
		)
		if expectedState == operatorv1.Managed {
			ctx.EnsureResourceExists(
				WithMinimalObject(dataModuleCRGVK, types.NamespacedName{Name: dataModuleCRName}),
				WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.spec.capabilities.featureStore.managementState == %q`, featureStoreState),
					jq.Match(`.spec.capabilities.dataRegistry.managementState == %q`, dataRegistryState),
				)),
				WithCustomErrorMsg("FeastOperator capabilities should match DSC Data management states"),
			)
		}
	}

	steps := []struct {
		name                string
		featureStoreState   operatorv1.ManagementState
		dataRegistryState   operatorv1.ManagementState
		expectedModuleState operatorv1.ManagementState
	}{
		{name: "both removed", featureStoreState: operatorv1.Removed, dataRegistryState: operatorv1.Removed, expectedModuleState: operatorv1.Removed},
		{name: "enable Feast", featureStoreState: operatorv1.Managed, dataRegistryState: operatorv1.Removed, expectedModuleState: operatorv1.Managed},
		{name: "disable Feast", featureStoreState: operatorv1.Removed, dataRegistryState: operatorv1.Removed, expectedModuleState: operatorv1.Removed},
		{name: "enable Data Registry", featureStoreState: operatorv1.Removed, dataRegistryState: operatorv1.Managed, expectedModuleState: operatorv1.Managed},
		{name: "disable Data Registry", featureStoreState: operatorv1.Removed, dataRegistryState: operatorv1.Removed, expectedModuleState: operatorv1.Removed},
		{name: "enable both", featureStoreState: operatorv1.Managed, dataRegistryState: operatorv1.Managed, expectedModuleState: operatorv1.Managed},
		{name: "disable both", featureStoreState: operatorv1.Removed, dataRegistryState: operatorv1.Removed, expectedModuleState: operatorv1.Removed},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			t.Helper()
			setStatesAndCheckProjection(step.featureStoreState, step.dataRegistryState, step.expectedModuleState)
		})
	}
}

// ValidateModuleDisabledCleanup verifies the two-phase cleanup when the module
// is disabled via ManagementState: Removed. This test is destructive and should run last.
func (ctx *DataModuleTestCtx) ValidateModuleDisabledCleanup(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	moduleCRNN := types.NamespacedName{Name: dataModuleCRName}
	controllerNN := types.NamespacedName{
		Namespace: ctx.AppsNamespace,
		Name:      dataModuleOperatorDeployment,
	}
	operandNN := types.NamespacedName{Namespace: ctx.AppsNamespace, Name: feastOperatorDeploymentName}
	// Projection cases (including their cleanup hooks) can leave every Data
	// capability Removed. Establish an active module before testing deletion;
	// otherwise absence alone would let this lifecycle assertion pass vacuously.
	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.data.featureStore.managementState = "Managed"`),
			testf.Transform(`.spec.components.data.dataRegistry.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataConnectHub.managementState = "Removed"`),
		)),
		WithCondition(jq.Match(`.spec.components.data.featureStore.managementState == "Managed"`)),
	)
	module := ctx.EnsureResourceExists(
		WithMinimalObject(dataModuleCRGVK, moduleCRNN),
		WithCondition(And(
			jq.Match(`.status.observedGeneration == .metadata.generation`),
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)),
	)
	controller := ctx.EnsureResourceExists(WithMinimalObject(gvk.Deployment, controllerNN))
	// Observe an actual operand before removal, so the later absence check is
	// evidence of module teardown rather than an object that never existed.
	ctx.EnsureResourceExists(WithMinimalObject(gvk.Deployment, operandNN))

	heldUID, err := addModuleCleanupFinalizer(t.Context(), ctx.Client(), module)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), ctx.TestTimeouts.shortEventuallyTimeout)
		defer cancel()
		require.NoError(t, releaseModuleCleanupFinalizer(cleanupCtx, ctx.Client(), module, heldUID))
	})
	require.NoError(t, err)

	// Transition FeastOperator to Removed via DSC patch
	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.data.featureStore.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataRegistry.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataConnectHub.managementState = "Removed"`),
		)),
		WithCondition(And(
			jq.Match(`.spec.components.data.featureStore.managementState == "Removed"`),
			jq.Match(`.spec.components.data.dataRegistry.managementState == "Removed"`),
			jq.Match(`.spec.components.data.dataConnectHub.managementState == "Removed"`),
		)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(dataModuleCRGVK, moduleCRNN),
		WithCondition(jq.Match(`.metadata.deletionTimestamp != null`)),
	)
	// This is the platform's finalizer boundary: its module operator must stay
	// alive while the module CR is terminating. DCH child cleanup/RBAC details
	// are covered by the Feast module's integration suite instead.
	g.Consistently(func(g Gomega) {
		freshModule := &unstructured.Unstructured{}
		freshModule.SetGroupVersionKind(dataModuleCRGVK)
		g.Expect(ctx.Client().Get(t.Context(), moduleCRNN, freshModule)).To(Succeed())
		g.Expect(freshModule.GetUID()).To(Equal(heldUID))
		g.Expect(freshModule.GetDeletionTimestamp()).NotTo(BeNil())
		freshController := &appsv1.Deployment{}
		g.Expect(ctx.Client().Get(t.Context(), controllerNN, freshController)).To(Succeed())
		g.Expect(freshController.GetUID()).To(Equal(controller.GetUID()))
		g.Expect(freshController.GetDeletionTimestamp()).To(BeNil())
	}).WithTimeout(10 * time.Second).WithPolling(time.Second).Should(Succeed())
	require.NoError(t, releaseModuleCleanupFinalizer(t.Context(), ctx.Client(), module, heldUID))
	// The module must finish its own finalizers and operand teardown before
	// the platform can remove its operator resources.
	ctx.EnsureResourceGone(WithMinimalObject(dataModuleCRGVK, moduleCRNN))
	ctx.EnsureResourceGone(WithMinimalObject(gvk.Deployment, operandNN))

	// Phase 2: Module operator Deployment should be deleted
	ctx.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))
}
