package cloudmanager_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

const testReleaseName = "test"

func newTestReconcileAction() func(context.Context, *types.ReconciliationRequest) error {
	return cloudmanager.NewReconcileAction(
		cloudmanager.WithDeployOptions(),
		cloudmanager.WithHelmOptions(helm.WithCache(false)),
	)
}

func newTestReconciliationRequest(cl client.Client, charts []types.HelmChartInfo) *types.ReconciliationRequest {
	return &types.ReconciliationRequest{
		Client:   cl,
		Instance: &ccmv1alpha1.AzureKubernetesEngine{},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 0, Patch: 0,
			}},
		},
		HelmCharts: charts,
	}
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

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{{
		Chart:       filepath.Join("testdata", "test-chart"),
		ReleaseName: testReleaseName,
		Values:      map[string]any{"namespace": ns},
	}})

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

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{{
		Chart:       filepath.Join("testdata", "test-chart"),
		ReleaseName: testReleaseName,
		Values:      map[string]any{"namespace": ns},
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

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{{
		Chart:       filepath.Join("testdata", "test-chart"),
		ReleaseName: testReleaseName,
		Values:      map[string]any{"namespace": ns},
		PostApply: []types.HookFn{func(ctx context.Context, rr *types.ReconciliationRequest) error {
			// Verify the resource was already deployed before post-apply runs
			checkTestChartDeployedResources(t, g, ctx, rr.Client, ns, testReleaseName)
			return nil
		}},
	}})

	err = action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestNewReconcileAction_PreApplyHookCanModifyResources(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{{
		Chart:       filepath.Join("testdata", "test-chart"),
		ReleaseName: testReleaseName,
		Values:      map[string]any{"namespace": ns},
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

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{{
		Chart:       filepath.Join("testdata", "test-chart"),
		ReleaseName: testReleaseName,
		Values:      map[string]any{"namespace": ns},
		PreApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
			return hookErr
		}},
	}})

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

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{{
		Chart:       filepath.Join("testdata", "test-chart"),
		ReleaseName: testReleaseName,
		Values:      map[string]any{"namespace": ns},
		PostApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
			return hookErr
		}},
	}})

	err = action(ctx, rr)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(errors.Is(err, hookErr)).Should(BeTrue())

	checkTestChartDeployedResources(t, g, ctx, cl, ns, testReleaseName)
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

	action := newTestReconcileAction()
	rr := newTestReconciliationRequest(cl, []types.HelmChartInfo{
		{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: releaseName1,
			Values:      map[string]any{"namespace": ns1},
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
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: releaseName2,
			Values:      map[string]any{"namespace": ns2},
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
