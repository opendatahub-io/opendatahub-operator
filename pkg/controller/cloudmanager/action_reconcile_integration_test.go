package cloudmanager_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	fwapi "github.com/opendatahub-io/odh-platform-utilities/framework/api"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	ccmcharts "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

const testReleaseName = "test"

// only azurekubernetesengine is used as resourceID for the test suite as coreweave setup is analogous to azure at the moment
//
// in case more CCM controllers with their own configuration options were added in the future,
// the test suite should be expanded upon accordingly.
var testResourceID = labels.NormalizePartOfValue(ccmv1alpha1.AzureKubernetesEngineKind)

func newTestReconcileAction(t *testing.T, charts []types.HelmChartInfo) func(context.Context, *types.ReconciliationRequest) error {
	t.Helper()
	return newTestReconcileActionWithResult(t, ccmcharts.BuildResult{Charts: charts})
}

// newTestReconcileActionWithResult builds a reconcile action whose chart discovery
// returns the given BuildResult (Charts, CleanupCharts, etc.).
func newTestReconcileActionWithResult(t *testing.T, result ccmcharts.BuildResult) func(context.Context, *types.ReconciliationRequest) error {
	t.Helper()
	g := NewWithT(t)
	action, err := cloudmanager.NewReconcileAction(
		testResourceID,
		cloudmanager.WithDeployOptions(),
		cloudmanager.WithHelmOptions(helm.WithCache(false)),
		cloudmanager.WithBuildChartsFn(func(_ context.Context, _ *types.ReconciliationRequest) (ccmcharts.BuildResult, error) {
			return result, nil
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return action
}

func newTestReconciliationRequest(t *testing.T, cl client.Client) *types.ReconciliationRequest {
	t.Helper()
	return newTestReconciliationRequestWithUID(t, cl, "")
}

func newTestReconciliationRequestWithUID(t *testing.T, cl client.Client, uid k8stypes.UID) *types.ReconciliationRequest {
	t.Helper()
	instance := &ccmv1alpha1.AzureKubernetesEngine{}
	if uid != "" {
		instance.SetUID(uid)
	}

	rr := &types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
		Release: fwapi.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 0, Patch: 0,
			}},
		},
	}

	rr.Conditions = conditionstest.NewManager(instance, status.ConditionTypeReady)

	return rr
}

func checkTestChartDeployedResources(t *testing.T, g *WithT, ctx context.Context, cl client.Client, ns, releaseName string) {
	t.Helper()

	cm := &corev1.ConfigMap{}
	err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: fmt.Sprintf("%s-config", releaseName)}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.Data).Should(HaveKeyWithValue("key", "value"))
}

func TestNewReconcileAction_RendersAndDeploys(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(1))

	checkTestChartDeployedResources(t, g, ctx, cl, ns, testReleaseName)
}

func TestNewReconcileAction_ExecutesPreApplyHooks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	var preHookCalled bool
	var resourceCountAtPreHook int
	var resourceNotDeployedAtPreHook bool

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PreApply: []types.HookFn{func(ctx context.Context, rr *types.ReconciliationRequest) error {
			preHookCalled = true
			resourceCountAtPreHook = len(rr.Resources)

			// Verify the resource has NOT been deployed yet
			cm := &corev1.ConfigMap{}
			if err := rr.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-config"}, cm); k8serr.IsNotFound(err) {
				resourceNotDeployedAtPreHook = true
			}

			return nil
		}},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(preHookCalled).Should(BeTrue())
	// Pre-apply hook runs after helm render, so resources should be populated
	g.Expect(resourceCountAtPreHook).Should(Equal(1))
	// Pre-apply hook runs before deploy, so resource should not exist in the cluster yet
	g.Expect(resourceNotDeployedAtPreHook).Should(BeTrue())
}

func TestNewReconcileAction_ExecutesPostApplyHooks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PostApply: []types.HookFn{func(ctx context.Context, rr *types.ReconciliationRequest) error {
			// Verify the resource was already deployed before post-apply runs
			checkTestChartDeployedResources(t, g, ctx, rr.Client, ns, testReleaseName)
			return nil
		}},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestNewReconcileAction_PreApplyHookCanModifyResources(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PreApply: []types.HookFn{func(_ context.Context, rr *types.ReconciliationRequest) error {
			// Add an extra ConfigMap via the hook
			extra := unstructured.Unstructured{}
			extra.SetAPIVersion("v1")
			extra.SetKind("ConfigMap")
			extra.SetName("hook-added")
			extra.SetNamespace(ns)
			rr.Resources = append(rr.Resources, extra)
			return nil
		}},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(2))

	checkTestChartDeployedResources(t, g, ctx, cl, ns, testReleaseName)

	// Verify the extra resource was deployed
	cm2 := &corev1.ConfigMap{}
	err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "hook-added"}, cm2)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestNewReconcileAction_PreApplyHookErrorStopsPipeline(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	hookErr := errors.New("pre-apply failed")

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PreApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
			return hookErr
		}},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(errors.Is(err, hookErr)).Should(BeTrue())

	// Resource should NOT have been deployed since pre-apply failed
	cm := &corev1.ConfigMap{}
	err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-config"}, cm)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(k8serr.IsNotFound(err)).Should(BeTrue())
}

func TestNewReconcileAction_PostApplyHookErrorPropagates(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	hookErr := errors.New("post-apply failed")

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PostApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
			return hookErr
		}},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(errors.Is(err, hookErr)).Should(BeTrue())

	checkTestChartDeployedResources(t, g, ctx, cl, ns, testReleaseName)
}

func TestNewReconcileAction_SetsInfrastructureLabel(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := newTestReconcileAction(t, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
	}})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(1))

	cm := &corev1.ConfigMap{}
	err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: fmt.Sprintf("%s-config", testReleaseName)}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.Labels).Should(HaveKeyWithValue(labels.InfrastructurePartOf, "azurekubernetesengine"))
	g.Expect(cm.Labels).ShouldNot(HaveKey(labels.PlatformPartOf))
}

func TestNewReconcileAction_MultipleCharts(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns1 := xid.New().String()
	ns2 := xid.New().String()
	releaseName1 := "chart-one"
	releaseName2 := "chart-two"

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	var hookOrder []string

	action := newTestReconcileAction(t, []types.HelmChartInfo{
		{
			Source: helmRenderer.Source{
				Chart:       filepath.Join("testdata", "test-chart"),
				ReleaseName: releaseName1,
				Values:      helmRenderer.Values(map[string]any{"namespace": ns1}),
			},
			PreApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
				hookOrder = append(hookOrder, "chart-one-pre")
				return nil
			}},
			PostApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
				hookOrder = append(hookOrder, "chart-one-post")
				return nil
			}},
		},
		{
			Source: helmRenderer.Source{
				Chart:       filepath.Join("testdata", "test-chart"),
				ReleaseName: releaseName2,
				Values:      helmRenderer.Values(map[string]any{"namespace": ns2}),
			},
			PreApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
				hookOrder = append(hookOrder, "chart-two-pre")
				return nil
			}},
			PostApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
				hookOrder = append(hookOrder, "chart-two-post")
				return nil
			}},
		},
	})
	rr := newTestReconciliationRequest(t, cl)

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(2))

	checkTestChartDeployedResources(t, g, ctx, cl, ns1, releaseName1)

	checkTestChartDeployedResources(t, g, ctx, cl, ns2, releaseName2)

	// Verify hooks executed in chart order
	g.Expect(hookOrder).Should(Equal([]string{
		"chart-one-pre", "chart-two-pre",
		"chart-one-post", "chart-two-post",
	}))
}

func TestNewReconcileAction_RejectsEmptyResourceID(t *testing.T) {
	g := NewWithT(t)
	action, err := cloudmanager.NewReconcileAction("   ")
	g.Expect(err).To(MatchError(ContainSubstring("resourceID is required")))
	g.Expect(action).To(BeNil())
}

// TestNewReconcileAction_AccessControlCleanupOrdering exercises Phase-2 cleanup
// through the full NewReconcileAction pipeline: SA/RBAC stay while an owned
// Deployment is still present, then are deleted once the workload is gone.
func TestNewReconcileAction_AccessControlCleanupOrdering(t *testing.T) {
	ctx := t.Context()

	const (
		releaseName = "op"
		ns          = "default"
	)

	instanceUID := k8stypes.UID("integration-cleanup-" + xid.New().String())

	operatorChart := types.HelmChartInfo{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "operator-chart"),
			ReleaseName: releaseName,
			Values:      helmRenderer.Values(map[string]any{}),
		},
	}

	action := newTestReconcileActionWithResult(t, ccmcharts.BuildResult{
		CleanupCharts: []types.HelmChartInfo{operatorChart},
	})

	deployKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-operator"}
	saKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-sa"}
	roleKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-role"}
	rbKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-rb"}

	owner := []metav1.OwnerReference{{
		APIVersion: ccmv1alpha1.GroupVersion.String(),
		Kind:       ccmv1alpha1.AzureKubernetesEngineKind,
		Name:       "test",
		UID:        instanceUID,
	}}
	replicas := int32(1)

	makeOwnedObjects := func(includeDeployment bool) []client.Object {
		objs := []client.Object{
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:            saKey.Name,
					Namespace:       ns,
					OwnerReferences: owner,
				},
			},
			&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:            roleKey.Name,
					Namespace:       ns,
					OwnerReferences: owner,
				},
			},
			&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            rbKey.Name,
					Namespace:       ns,
					OwnerReferences: owner,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     roleKey.Name,
				},
				Subjects: []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      saKey.Name,
					Namespace: ns,
				}},
			},
		}
		if includeDeployment {
			objs = append([]client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            deployKey.Name,
						Namespace:       ns,
						OwnerReferences: owner,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: &replicas,
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": releaseName}},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": releaseName}},
							Spec: corev1.PodSpec{
								ServiceAccountName: saKey.Name,
								Containers: []corev1.Container{{
									Name:  "operator",
									Image: "example.com/operator:test",
								}},
							},
						},
					},
				},
			}, objs...)
		}

		return objs
	}

	t.Run("defers access-control while owned Deployment still exists", func(t *testing.T) {
		g := NewWithT(t)

		var deletedKinds []string
		cl, err := fakeclient.New(
			fakeclient.WithObjects(makeOwnedObjects(true)...),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					deletedKinds = append(deletedKinds, obj.GetObjectKind().GroupVersionKind().Kind)
					// Leave objects in place so the Deployment stays "terminating".
					return nil
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := newTestReconciliationRequestWithUID(t, cl, instanceUID)
		rr.Generated = true

		err = action(ctx, rr)
		g.Expect(err).Should(HaveOccurred())

		var requeueErr odherrors.RequeueAfterError
		g.Expect(errors.As(err, &requeueErr)).Should(BeTrue())
		g.Expect(requeueErr.After).Should(Equal(5 * time.Second))

		g.Expect(deletedKinds).Should(ContainElement("Deployment"))
		g.Expect(deletedKinds).ShouldNot(ContainElement("ServiceAccount"))
		g.Expect(deletedKinds).ShouldNot(ContainElement("Role"))
		g.Expect(deletedKinds).ShouldNot(ContainElement("RoleBinding"))

		g.Expect(cl.Get(ctx, saKey, &corev1.ServiceAccount{})).Should(Succeed())
		g.Expect(cl.Get(ctx, roleKey, &rbacv1.Role{})).Should(Succeed())
		g.Expect(cl.Get(ctx, rbKey, &rbacv1.RoleBinding{})).Should(Succeed())
		g.Expect(cl.Get(ctx, deployKey, &appsv1.Deployment{})).Should(Succeed())
	})

	t.Run("deletes access-control after owned Deployment is gone", func(t *testing.T) {
		g := NewWithT(t)

		// First reconcile: defer Pass B while the Deployment remains.
		cl1, err := fakeclient.New(
			fakeclient.WithObjects(makeOwnedObjects(true)...),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					return nil
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		rr1 := newTestReconciliationRequestWithUID(t, cl1, instanceUID)
		rr1.Generated = true
		err = action(ctx, rr1)
		g.Expect(err).Should(HaveOccurred())
		var requeueErr odherrors.RequeueAfterError
		g.Expect(errors.As(err, &requeueErr)).Should(BeTrue())

		// Second reconcile: workload gone, Generated false — deferred cleanup
		// must still finish Pass B.
		cl2, err := fakeclient.New(fakeclient.WithObjects(makeOwnedObjects(false)...))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr2 := newTestReconciliationRequestWithUID(t, cl2, instanceUID)
		rr2.Generated = false
		g.Expect(action(ctx, rr2)).Should(Succeed())

		g.Expect(cl2.Get(ctx, saKey, &corev1.ServiceAccount{})).Should(MatchError(ContainSubstring("not found")))
		g.Expect(cl2.Get(ctx, roleKey, &rbacv1.Role{})).Should(MatchError(ContainSubstring("not found")))
		g.Expect(cl2.Get(ctx, rbKey, &rbacv1.RoleBinding{})).Should(MatchError(ContainSubstring("not found")))
	})
}
