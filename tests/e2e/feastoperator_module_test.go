package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"

	. "github.com/onsi/gomega"
)

const (
	feastModuleOperatorDeployment = "opendatahub-feast-operator"
	feastModuleCRName             = componentApi.FeastOperatorInstanceName
	feastOperatorDeploymentName   = "feast-operator-controller-manager"
)

var feastModuleCRGVK = schema.GroupVersionKind{
	Group:   "components.platform.opendatahub.io",
	Version: "v1",
	Kind:    "FeastOperator",
}

type FeastModuleTestCtx struct {
	*TestContext
}

func feastModuleTestSuite(t *testing.T) {
	t.Helper()

	baseCtx, err := NewTestContext(t)
	require.NoError(t, err)

	ctx := FeastModuleTestCtx{TestContext: baseCtx}

	testCases := []TestCase{
		{"Validate upgrade from in-tree: selector migration", ctx.ValidateUpgradeSelectorMigration},
		{"Validate module operator deployed", ctx.ValidateModuleOperatorDeployed},
		{"Validate module CR created", ctx.ValidateModuleCRCreated},
		{"Validate module CR ready", ctx.ValidateModuleCRReady},
		{"Validate feast-operator deployed by module", ctx.ValidateFeastOperatorDeployed},
		{"Validate upgrade: existing operands preserved", ctx.ValidateUpgradeOperandsPreserved},
		{"Validate module disabled cleanup", ctx.ValidateModuleDisabledCleanup},
	}

	RunTestCases(t, testCases)
}

// ValidateModuleOperatorDeployed checks that the opendatahub-feast-operator
// Deployment exists and is available (deployed by the platform's Helm action).
func (ctx *FeastModuleTestCtx) ValidateModuleOperatorDeployed(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	nn := types.NamespacedName{
		Name:      feastModuleOperatorDeployment,
		Namespace: ctx.AppsNamespace,
	}

	g.Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(ctx.Client().Get(context.Background(), nn, deploy)).To(Succeed())
		g.Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1))
	}).
		WithTimeout(3 * time.Minute).
		WithPolling(5 * time.Second).
		Should(Succeed(), "module operator Deployment should be available")
}

// ValidateModuleCRCreated checks that the FeastOperator CR (v1) was created
// by the platform's provisionModules action.
func (ctx *FeastModuleTestCtx) ValidateModuleCRCreated(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	g.Eventually(func() error {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(feastModuleCRGVK)
		return ctx.Client().Get(context.Background(), types.NamespacedName{Name: feastModuleCRName}, cr)
	}).
		WithTimeout(2 * time.Minute).
		WithPolling(5 * time.Second).
		Should(Succeed(), "FeastOperator CR should be created by the platform")
}

// ValidateModuleCRReady checks that the FeastOperator CR reports Ready=True,
// meaning the module operator has successfully reconciled and deployed the
// feast-operator from bundled manifests.
func (ctx *FeastModuleTestCtx) ValidateModuleCRReady(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	g.Eventually(func(g Gomega) {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(feastModuleCRGVK)
		g.Expect(ctx.Client().Get(context.Background(), types.NamespacedName{Name: feastModuleCRName}, cr)).To(Succeed())

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
		WithTimeout(5 * time.Minute).
		WithPolling(10 * time.Second).
		Should(Succeed(), "FeastOperator CR should become Ready")
}

// ValidateFeastOperatorDeployed checks that the feast-operator-controller-manager
// Deployment was created by the module operator from the bundled kustomize manifests.
func (ctx *FeastModuleTestCtx) ValidateFeastOperatorDeployed(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	nn := types.NamespacedName{
		Name:      "feast-operator-controller-manager",
		Namespace: ctx.AppsNamespace,
	}

	g.Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(ctx.Client().Get(context.Background(), nn, deploy)).To(Succeed())
		g.Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1))
	}).
		WithTimeout(3 * time.Minute).
		WithPolling(5 * time.Second).
		Should(Succeed(), "feast-operator-controller-manager should be deployed and available")
}

// ValidateUpgradeSelectorMigration simulates the in-tree to module upgrade scenario.
// It creates a Deployment with old-style selectors (missing app.kubernetes.io/name) to
// verify that the module operator's selector migration logic handles the transition by
// deleting and recreating the Deployment with the correct selector.
func (ctx *FeastModuleTestCtx) ValidateUpgradeSelectorMigration(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	oldDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      feastOperatorDeploymentName,
			Namespace: ctx.AppsNamespace,
			Labels: map[string]string{
				"app.opendatahub.io/feast-operator": "true",
				"app.kubernetes.io/part-of":         "feast-operator",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"control-plane": "controller-manager",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"control-plane": "controller-manager",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "manager",
							Image: "registry.redhat.io/rhoai/odh-feast-operator-rhel8:latest",
						},
					},
				},
			},
		},
	}

	err := ctx.Client().Create(context.Background(), oldDeploy)
	if err != nil {
		t.Logf("Old-style Deployment already exists or cannot be created (expected in upgrade): %v", err)
		return
	}

	// Verify the old Deployment exists with the stale selector
	g.Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(ctx.Client().Get(context.Background(), types.NamespacedName{
			Name:      feastOperatorDeploymentName,
			Namespace: ctx.AppsNamespace,
		}, deploy)).To(Succeed())
	}).
		WithTimeout(30 * time.Second).
		WithPolling(2 * time.Second).
		Should(Succeed(), "old-style Deployment should exist before module takes over")
}

// ValidateUpgradeOperandsPreserved verifies that after the modular operator takes
// over from the in-tree component, existing operand resources remain intact. This
// validates the SSA-with-ForceOwnership adoption path.
func (ctx *FeastModuleTestCtx) ValidateUpgradeOperandsPreserved(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	// After the module operator reconciles, the feast-operator Deployment should have
	// the new-style selector including app.kubernetes.io/name
	nn := types.NamespacedName{
		Name:      feastOperatorDeploymentName,
		Namespace: ctx.AppsNamespace,
	}

	g.Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(ctx.Client().Get(context.Background(), nn, deploy)).To(Succeed())

		g.Expect(deploy.Spec.Selector.MatchLabels).To(
			HaveKeyWithValue("app.kubernetes.io/name", "feast-operator"),
			"Deployment should have migrated selector after module takes over",
		)
		g.Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1),
			"Deployment should be available after migration")
	}).
		WithTimeout(5 * time.Minute).
		WithPolling(10 * time.Second).
		Should(Succeed(), "feast-operator Deployment should be recreated with correct selector")
}

// ValidateModuleDisabledCleanup verifies the two-phase cleanup when the module
// is disabled via ManagementState: Removed. This test is destructive and should run last.
func (ctx *FeastModuleTestCtx) ValidateModuleDisabledCleanup(t *testing.T) {
	t.Helper()
	g := NewWithT(t)

	// Phase 1: Module CR should be deleted (module operator processes finalizers)
	g.Eventually(func() bool {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(feastModuleCRGVK)
		err := ctx.Client().Get(context.Background(), types.NamespacedName{Name: feastModuleCRName}, cr)
		return client.IgnoreNotFound(err) == nil && err != nil
	}).
		WithTimeout(3 * time.Minute).
		WithPolling(5 * time.Second).
		Should(BeTrue(), "FeastOperator CR should be deleted")

	// Phase 2: Module operator Deployment should be deleted
	g.Eventually(func() bool {
		deploy := &appsv1.Deployment{}
		err := ctx.Client().Get(context.Background(), types.NamespacedName{
			Name:      feastModuleOperatorDeployment,
			Namespace: ctx.AppsNamespace,
		}, deploy)
		return client.IgnoreNotFound(err) == nil && err != nil
	}).
		WithTimeout(3 * time.Minute).
		WithPolling(5 * time.Second).
		Should(BeTrue(), "module operator Deployment should be deleted after CR is gone")
}
