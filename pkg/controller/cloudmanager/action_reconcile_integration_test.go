package cloudmanager_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/blang/semver/v4"
	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

const (
	testReleaseName = "test"
	testResourceID  = "testresourceid"
)

func newTestReconcileAction(t *testing.T) func(context.Context, *types.ReconciliationRequest) error {
	t.Helper()
	g := NewWithT(t)
	action, err := cloudmanager.NewReconcileAction(
		testResourceID,
		cloudmanager.WithDeployOptions(),
		cloudmanager.WithHelmOptions(helm.WithCache(false)),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return action
}

// newTestReconciliationRequest creates a ReconciliationRequest backed by a real envtest
// cluster so that all actions — including cleanupOwnership — have access to working
// discovery and dynamic clients.
func newTestReconciliationRequest(
	et *envt.EnvT,
	charts []types.HelmChartInfo,
) *types.ReconciliationRequest {
	instance := &ccmv1alpha1.AzureKubernetesEngine{}

	rr := &types.ReconciliationRequest{
		Client:   et.Client(),
		Instance: instance,
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
			m.On("GetClient").Return(et.Client())
			m.On("GetDiscoveryClient").Return(et.DiscoveryClient())
			m.On("GetDynamicClient").Return(et.DynamicClient())
			m.On("IsDynamicOwnershipEnabled").Return(false)
		}),
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 0, Patch: 0,
			}},
		},
		HelmCharts: charts,
	}

	rr.Conditions = conditions.NewManager(instance, status.ConditionTypeReady)

	return rr
}

// newEnvT creates a fresh envtest environment for a test and registers Stop as cleanup.
func newEnvT(t *testing.T) *envt.EnvT {
	t.Helper()
	g := NewWithT(t)
	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })
	return et
}

// createTestNS creates a namespace in the cluster and registers deletion as cleanup.
func createTestNS(t *testing.T, g *WithT, ctx context.Context, cli client.Client) string {
	t.Helper()
	ns := xid.New().String()
	g.Expect(cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).
		NotTo(HaveOccurred())
	t.Cleanup(func() {
		_ = cli.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	})
	return ns
}

func checkTestChartDeployedResources(t *testing.T, g *WithT, ctx context.Context, cli client.Client, ns, releaseName string) {
	t.Helper()

	cm := &corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{Namespace: ns, Name: fmt.Sprintf("%s-config", releaseName)}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.Data).Should(HaveKeyWithValue("key", "value"))
}

func TestNewReconcileAction_RendersAndDeploys(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
	}})

	err := action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(1))

	checkTestChartDeployedResources(t, g, ctx, et.Client(), ns, testReleaseName)
}

func TestNewReconcileAction_ExecutesPreApplyHooks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	var preHookCalled bool
	var resourceCountAtPreHook int
	var resourceNotDeployedAtPreHook bool

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
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

	err := action(ctx, rr)

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
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
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

	err := action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestNewReconcileAction_PreApplyHookCanModifyResources(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
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

	err := action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(2))

	checkTestChartDeployedResources(t, g, ctx, et.Client(), ns, testReleaseName)

	// Verify the extra resource was deployed
	cm2 := &corev1.ConfigMap{}
	err = et.Client().Get(ctx, client.ObjectKey{Namespace: ns, Name: "hook-added"}, cm2)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestNewReconcileAction_PreApplyHookErrorStopsPipeline(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	hookErr := errors.New("pre-apply failed")

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PreApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
			return hookErr
		}},
	}})

	err := action(ctx, rr)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(errors.Is(err, hookErr)).Should(BeTrue())

	// Resource should NOT have been deployed since pre-apply failed
	cm := &corev1.ConfigMap{}
	err = et.Client().Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-config"}, cm)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(k8serr.IsNotFound(err)).Should(BeTrue())
}

func TestNewReconcileAction_PostApplyHookErrorPropagates(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	hookErr := errors.New("post-apply failed")

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
		PostApply: []types.HookFn{func(_ context.Context, _ *types.ReconciliationRequest) error {
			return hookErr
		}},
	}})

	err := action(ctx, rr)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(errors.Is(err, hookErr)).Should(BeTrue())

	checkTestChartDeployedResources(t, g, ctx, et.Client(), ns, testReleaseName)
}

func TestNewReconcileAction_SetsInfrastructureLabel(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
	}})

	err := action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(1))

	for _, res := range rr.Resources {
		g.Expect(resources.GetLabel(&res, labels.InfrastructurePartOf)).
			Should(Equal(testResourceID))
	}
}

func TestNewReconcileAction_SetsInfrastructureDependencyLabel(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns := createTestNS(t, g, ctx, et.Client())

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{{
		Source: helmRenderer.Source{
			Chart:       filepath.Join("testdata", "test-chart"),
			ReleaseName: testReleaseName,
			Values:      helmRenderer.Values(map[string]any{"namespace": ns}),
		},
	}})

	err := action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(1))

	// Each resource must carry the InfrastructureDependency label whose value matches
	// the chart's ReleaseName. The GC predicate uses this label to derive Managed/Unmanaged
	// state from rr.Resources.
	for _, res := range rr.Resources {
		g.Expect(resources.GetLabel(&res, labels.InfrastructureDependency)).
			Should(Equal(testReleaseName))
	}
}

func TestNewReconcileAction_MultipleCharts(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	et := newEnvT(t)
	ns1 := createTestNS(t, g, ctx, et.Client())
	ns2 := createTestNS(t, g, ctx, et.Client())
	releaseName1 := "chart-one"
	releaseName2 := "chart-two"

	var hookOrder []string

	action := newTestReconcileAction(t)
	rr := newTestReconciliationRequest(et, []types.HelmChartInfo{
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

	err := action(ctx, rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(2))

	checkTestChartDeployedResources(t, g, ctx, et.Client(), ns1, releaseName1)
	checkTestChartDeployedResources(t, g, ctx, et.Client(), ns2, releaseName2)

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
