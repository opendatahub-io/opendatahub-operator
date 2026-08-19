package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const (
	modelRegistryModuleOperatorDeployment = "aihub-controller-manager"
	modelRegistryModuleCRName             = "default"
	modelRegistryChildDeployment          = "model-registry-operator-controller-manager"
	catalogChildDeployment                = "catalog-controller-manager"
	modelRegistryTestNamespace            = "e2e-model-registries"
)

var modelRegistryModuleCRGVK = gvk.AIHub

type ModelRegistryModuleTestCtx struct {
	*TestContext

	originalRegistriesNamespace string
}

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	baseCtx, err := NewTestContext(t)
	require.NoError(t, err)

	ctx := ModelRegistryModuleTestCtx{TestContext: baseCtx}

	testCases := []TestCase{
		{"Validate component enabled", ctx.ValidateComponentEnabled},
		{"Validate module operator deployed", ctx.ValidateModuleOperatorDeployed},
		{"Validate module CR created", ctx.ValidateModuleCRCreated},
		{"Validate module CR spec", ctx.ValidateModuleCRSpec},
		{"Validate registries namespace created", ctx.ValidateRegistriesNamespaceCreated},
		{"Validate module CR ready", ctx.ValidateModuleCRReady},
		{"Validate child operators deployed", ctx.ValidateChildOperatorsDeployed},
		{"Validate DSC ModelRegistry ready", ctx.ValidateDSCModelRegistryReady},
		{"Validate module disabled cleanup", ctx.ValidateModuleDisabledCleanup},
	}

	RunTestCases(t, testCases)
}

// ValidateComponentEnabled patches the DSC to set modelregistry to Managed
// and configures a non-default registriesNamespace to exercise namespace auto-creation.
func (ctx *ModelRegistryModuleTestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	// Capture the original registriesNamespace so cleanup can restore it (shared &ctx).
	if dsc := ctx.FetchDataScienceCluster(); dsc != nil {
		ctx.originalRegistriesNamespace = dsc.Spec.Components.ModelRegistry.RegistriesNamespace
	}

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			if err := unstructured.SetNestedField(obj.Object, "Managed", "spec", "components", "modelregistry", "managementState"); err != nil {
				return err
			}
			return unstructured.SetNestedField(obj.Object, modelRegistryTestNamespace, "spec", "components", "modelregistry", "registriesNamespace")
		}),
		WithCondition(And(
			jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
			jq.Match(`.spec.components.modelregistry.registriesNamespace == "%s"`, modelRegistryTestNamespace),
		)),
	)
}

// ValidateModuleOperatorDeployed checks that the aihub-controller-manager
// Deployment exists and is available.
func (ctx *ModelRegistryModuleTestCtx) ValidateModuleOperatorDeployed(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	nn := types.NamespacedName{
		Name:      modelRegistryModuleOperatorDeployment,
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

// ValidateModuleCRCreated checks that the AIHub CR was created
// by the platform's provisionModules action.
func (ctx *ModelRegistryModuleTestCtx) ValidateModuleCRCreated(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	g.Eventually(func() error {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(modelRegistryModuleCRGVK)
		return ctx.Client().Get(context.Background(), types.NamespacedName{Name: modelRegistryModuleCRName}, cr)
	}).
		WithTimeout(2*time.Minute).
		WithPolling(5*time.Second).
		Should(Succeed(), "AIHub CR should be created by the platform")
}

// ValidateModuleCRSpec asserts that the AIHub CR spec contains the expected
// applicationNamespace and instancesNamespace values.
func (ctx *ModelRegistryModuleTestCtx) ValidateModuleCRSpec(t *testing.T) {
	t.Helper()

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.AIHub, types.NamespacedName{Name: modelRegistryModuleCRName}),
		WithCondition(And(
			jq.Match(`.spec.applicationNamespace == "%s"`, ctx.AppsNamespace),
			jq.Match(`.spec.instancesNamespace == "%s"`, modelRegistryTestNamespace),
		)),
	)
}

// ValidateRegistriesNamespaceCreated checks that the non-default registries
// namespace is auto-created by the operator.
func (ctx *ModelRegistryModuleTestCtx) ValidateRegistriesNamespaceCreated(t *testing.T) {
	t.Helper()

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: modelRegistryTestNamespace}),
	)
}

// ValidateModuleCRReady checks that the AIHub CR reports Ready=True,
// meaning the module operator has successfully reconciled.
func (ctx *ModelRegistryModuleTestCtx) ValidateModuleCRReady(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	g.Eventually(func(g Gomega) {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(modelRegistryModuleCRGVK)
		g.Expect(ctx.Client().Get(context.Background(), types.NamespacedName{Name: modelRegistryModuleCRName}, cr)).To(Succeed())

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
				g.Expect(cm["status"]).To(Equal("True"), "AIHub CR should be Ready")
				readyFound = true
				break
			}
		}
		g.Expect(readyFound).To(BeTrue(), "Ready condition should exist")
	}).
		WithTimeout(5*time.Minute).
		WithPolling(10*time.Second).
		Should(Succeed(), "AIHub CR should become Ready")
}

// ValidateChildOperatorsDeployed checks that both the model-registry-operator
// and catalog-controller-manager Deployments are available.
func (ctx *ModelRegistryModuleTestCtx) ValidateChildOperatorsDeployed(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	childDeployments := []string{modelRegistryChildDeployment, catalogChildDeployment}

	for _, name := range childDeployments {
		nn := types.NamespacedName{
			Name:      name,
			Namespace: ctx.AppsNamespace,
		}

		g.Eventually(func(g Gomega) {
			deploy := &appsv1.Deployment{}
			g.Expect(ctx.Client().Get(context.Background(), nn, deploy)).To(Succeed())
			g.Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1))
		}).
			WithTimeout(3*time.Minute).
			WithPolling(5*time.Second).
			Should(Succeed(), "%s should be deployed and available", name)
	}
}

// ValidateDSCModelRegistryReady checks that the DSC reports ModelRegistryReady
// condition as True and the component status reflects Managed state.
func (ctx *ModelRegistryModuleTestCtx) ValidateDSCModelRegistryReady(t *testing.T) {
	t.Helper()

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type=="ModelRegistryReady") | .status == "True"`),
			jq.Match(`.status.components.modelregistry.managementState == "Managed"`),
		)),
	)
}

// ValidateModuleDisabledCleanup verifies the two-phase cleanup when the module
// is disabled via ManagementState: Removed. This test is destructive and should run last.
func (ctx *ModelRegistryModuleTestCtx) ValidateModuleDisabledCleanup(t *testing.T) {
	t.Helper()

	moduleCRNN := types.NamespacedName{Name: modelRegistryModuleCRName}
	controllerNN := types.NamespacedName{
		Namespace: ctx.AppsNamespace,
		Name:      modelRegistryModuleOperatorDeployment,
	}

	// Transition ModelRegistry to Removed and restore registriesNamespace in one patch.
	// The CEL immutability rule allows changing registriesNamespace when managementState
	// becomes Removed in the same mutation.
	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			if err := unstructured.SetNestedField(obj.Object, "Removed", "spec", "components", "modelregistry", "managementState"); err != nil {
				return err
			}
			return unstructured.SetNestedField(obj.Object, ctx.originalRegistriesNamespace, "spec", "components", "modelregistry", "registriesNamespace")
		}),
		WithCondition(jq.Match(`.spec.components.modelregistry.managementState == "Removed"`)),
	)

	// Phase 1: Module CR should be deleted
	ctx.EnsureResourceGone(WithMinimalObject(modelRegistryModuleCRGVK, moduleCRNN))

	// Phase 2: Module operator Deployment should be deleted
	ctx.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

	// Remove the namespace created for this suite (owner-ref GC may already be deleting it).
	ctx.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: modelRegistryTestNamespace}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
}
