//nolint:testpackage // Starts the production controller and verifies its watches and action pipeline.
package platformmodules

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	frameworkreconciler "github.com/opendatahub-io/odh-platform-utilities/framework/controller/reconciler"
	frameworkmanager "github.com/opendatahub-io/odh-platform-utilities/framework/manager"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/spf13/viper"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// Read real module CR state so the tests exercise the controller watches.
type controllerTestHandler struct {
	stubModuleHandler

	observedLiveModuleCR atomic.Bool
}

func (*controllerTestHandler) IsEnabled(config *configApi.PlatformModules) bool {
	return config.Dashboard.ManagementState == operatorv1.Managed
}

func (h *controllerTestHandler) GetModuleStatus(ctx context.Context, cli client.Client) (*modules.ModuleStatus, error) {
	return h.BaseHandler.GetModuleStatus(ctx, cli)
}

func (h *controllerTestHandler) GetModuleCRState(ctx context.Context, cli client.Client) (modules.CRState, error) {
	state, err := h.BaseHandler.GetModuleCRState(ctx, cli)
	if err == nil {
		h.observedLiveModuleCR.Store(state == modules.CRStateAlive)
	}
	return state, err
}

// Start a fresh manager against the same API server on each restart.
func startPlatformModuleController(
	t *testing.T, et *envt.EnvT, manifestsRoot string, blockStatus, statusFailed *atomic.Bool,
	withPlatform bool, customize ...func(*interceptor.Funcs),
) func() {
	t.Helper()
	g := NewWithT(t)
	targetVersion := cluster.GetRelease().Version.String()
	faults := interceptor.Funcs{
		SubResourceApply: func(ctx context.Context, cli client.Client, subresource string,
			obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				return err
			}
			version, _, _ := unstructured.NestedString(u, "status", "release", "version")
			if blockStatus.Load() && version == targetVersion {
				statusFailed.Store(true)
				return k8serr.NewServiceUnavailable("status write interrupted")
			}
			return cli.SubResource(subresource).Apply(ctx, obj, opts...)
		},
	}
	for _, configure := range customize {
		configure(&faults)
	}
	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme: et.Scheme(), Metrics: metricsserver.Options{BindAddress: "0"},
		Cache:      cache.Options{DefaultTransform: cache.TransformStripManagedFields()},
		Controller: config.Controller{SkipNameValidation: new(true)},
		Client:     client.Options{Cache: &client.CacheOptions{Unstructured: true}},
		NewClient: func(cfg *rest.Config, opts client.Options) (client.Client, error) {
			c, err := client.NewWithWatch(cfg, opts)
			if err != nil {
				return nil, err
			}
			return interceptor.NewClient(c, faults), nil
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	wrapped := frameworkmanager.New(mgr, frameworkmanager.WithManifestsBasePath(manifestsRoot))
	if withPlatform {
		g.Expect(modules.NewModuleReconciler(t.Context(), wrapped)).To(Succeed())
	}
	g.Expect(NewPlatformModuleReconciler(t.Context(), wrapped)).To(Succeed())
	managerCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- wrapped.Start(managerCtx) }()
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			cancel()
			g.Eventually(done).WithTimeout(10 * time.Second).Should(Receive(Succeed()))
		})
	}
	t.Cleanup(stop)
	return stop
}

// Seed the persisted state of a healthy previous release.
func createPreviousModuleRelease(
	t *testing.T, cli client.Client, namespace string, platform *configApi.Platform,
) (*configApi.PlatformModule, *corev1.ConfigMap, *testscheme.TestPlatformObject) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{
		Name: "testmod", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
	}}
	g.Expect(cli.Create(ctx, pm)).To(Succeed())
	// Hold deletion of old-config, which is absent from the new manifests.
	stale := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "old-config", Namespace: namespace, Finalizers: []string{"test.io/hold"},
		OwnerReferences: []metav1.OwnerReference{platformModuleOwner(pm.UID)},
	}}
	g.Expect(cli.Create(ctx, stale)).To(Succeed())
	pm.Status.Release = testRelease("1.0.0")
	pm.Status.Resources = []configApi.ResourceRef{{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: stale.Name}}
	pm.Status.Conditions = []common.Condition{
		{Type: status.ConditionTypeProvisioningSucceeded, Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
		{Type: status.ConditionTypeModuleCRReady, Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
		{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
	}
	g.Expect(cli.Status().Update(ctx, pm)).To(Succeed())
	module := &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	g.Expect(cli.Create(ctx, module)).To(Succeed())
	module.Status.Conditions = []common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()}}
	g.Expect(cli.Status().Update(ctx, module)).To(Succeed())

	return pm, stale, module
}

func newPlatformModuleTestEnv(t *testing.T, namespace, version string, disableDAG bool) *envt.EnvT {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()
	withCleanRegistries(t)
	for key, value := range map[string]any{
		"rhai-version":                version,
		"rhai-applications-namespace": namespace,
		"disable-dag-ordering":        disableDAG,
	} {
		previous := viper.Get(key)
		viper.Set(key, value)
		t.Cleanup(func() { viper.Set(key, previous) })
	}
	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { g.Expect(et.Stop()).To(Succeed()) })
	initClient, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster.Init(ctx, initClient, operatorconfig.OperatorSettings{
		OperatorNamespace: namespace, PlatformType: string(cluster.XKS),
	})).To(Succeed())
	g.Expect(et.Client().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())
	return et
}

func registerTestModule(t *testing.T, et *envt.EnvT) *controllerTestHandler {
	t.Helper()
	g := NewWithT(t)
	handler := &controllerTestHandler{stubModuleHandler: stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{
		Name: "testmod", CRName: "default", GVK: testscheme.TestPlatformObjectGVK, ManifestDir: "completion",
	}}}}
	_, err := et.RegisterCRD(t.Context(), handler.GetGVK(),
		"testplatformobjects", "testplatformobject", apiextensionsv1.ClusterScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	modules.Add(handler)
	provision.DefaultRegistry().Add(handler.GetName(), provision.KindModule, dag.RL(20))
	return handler
}

// TestPlatformModuleFirstUpgradeCollectsLegacyResources covers adoption and cleanup on the first upgrade from 3.6.
func TestPlatformModuleFirstUpgradeCollectsLegacyResources(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	const namespace = "completion"
	et := newPlatformModuleTestEnv(t, namespace, "3.7.0", true)
	cli := et.Client()
	registerTestModule(t, et)
	_, err := et.RegisterCRD(ctx, gvk.CoreosPrometheusRule,
		"prometheusrules", "prometheusrule", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	module := &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	g.Expect(cli.Create(ctx, module)).To(Succeed())
	module.Status.Conditions = []common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()}}
	g.Expect(cli.Status().Update(ctx, module)).To(Succeed())

	// Seed resources retained and removed by the upgrade.
	platform := platformWithDAG(0)
	platform.Spec.Modules.Dashboard.ManagementState = operatorv1.Managed
	g.Expect(cli.Create(ctx, platform)).To(Succeed())
	legacyAnnotations := map[string]string{
		annotations.InstanceUID:     string(platform.UID),
		annotations.InstanceName:    platform.Name,
		annotations.PlatformVersion: "3.6.0",
		annotations.PlatformType:    string(cluster.XKS),
	}
	owner := *metav1.NewControllerRef(platform, gvk.Platform)
	retained := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "operator-config", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{owner}, Annotations: legacyAnnotations,
	}}
	obsoleteRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{
		Name: "obsolete-module-role", OwnerReferences: []metav1.OwnerReference{owner}, Annotations: legacyAnnotations,
	}}
	obsoleteRule := &unstructured.Unstructured{}
	obsoleteRule.SetGroupVersionKind(gvk.CoreosPrometheusRule)
	obsoleteRule.SetName("trainer-operator-kubeflow-trainer-telemetry")
	obsoleteRule.SetNamespace(namespace)
	obsoleteRule.SetOwnerReferences([]metav1.OwnerReference{owner})
	obsoleteRule.SetAnnotations(legacyAnnotations)
	obsolete := []client.Object{obsoleteRole, obsoleteRule}
	g.Expect(cli.Create(ctx, retained)).To(Succeed())
	for _, obj := range obsolete {
		g.Expect(cli.Create(ctx, obj)).To(Succeed())
	}
	retainedUID := retained.UID

	// Preserve obsolete resources until the tracker publishes its release.
	var blockStatus, statusFailed atomic.Bool
	blockStatus.Store(true)
	// Legacy cleanup must never delete a 3.6 resource before the tracker has
	// committed the target release: record any delete attempted against the
	// seeded 3.6 inventory and verify the release ordering afterwards.
	var legacyDeletedBeforeRelease atomic.Bool
	onLegacyDelete := func(obj client.Object) {
		if obj.GetAnnotations()[annotations.PlatformVersion] != "3.6.0" {
			return
		}
		tracker := &configApi.PlatformModule{}
		if err := cli.Get(ctx, client.ObjectKey{Name: "testmod"}, tracker); err != nil ||
			tracker.Status.Release.Version.String() != cluster.GetRelease().Version.String() ||
			tracker.Status.Release.Name != cluster.GetRelease().Name {
			legacyDeletedBeforeRelease.Store(true)
		}
	}
	startPlatformModuleController(t, et, filepath.Join(et.ProjectRoot(), "internal/controller/platformmodules/testdata"), &blockStatus, &statusFailed, true,
		func(faults *interceptor.Funcs) {
			faults.Delete = func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				onLegacyDelete(obj)
				return cli.Delete(ctx, obj, opts...)
			}
		})
	g.Eventually(statusFailed.Load).WithTimeout(20 * time.Second).Should(BeTrue())
	for _, obj := range obsolete {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		g.Expect(obj.GetDeletionTimestamp().IsZero()).To(BeTrue())
	}

	// Transfer the retained ConfigMap to the tracker and delete obsolete Platform-owned resources.
	blockStatus.Store(false)
	tracker := &configApi.PlatformModule{}
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: "testmod"}, tracker)).To(Succeed())
		g.Expect(tracker.Status.Release).To(Equal(cluster.GetRelease()))
		g.Expect(conditions.FindStatusCondition(tracker, status.ConditionTypeProvisioningSucceeded)).
			To(HaveValue(HaveField("Status", metav1.ConditionTrue)))
		g.Expect(conditions.FindStatusCondition(tracker, status.ConditionTypeModuleCRReady)).
			To(HaveValue(HaveField("Status", metav1.ConditionTrue)))
		g.Expect(conditions.FindStatusCondition(tracker, status.ConditionTypeReady)).
			To(HaveValue(HaveField("Status", metav1.ConditionFalse)))
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(retained), retained)).To(Succeed())
		g.Expect(retained.UID).To(Equal(retainedUID))
		g.Expect(metav1.GetControllerOf(retained)).To(HaveValue(And(
			HaveField("Kind", gvk.PlatformModule.Kind), HaveField("UID", tracker.UID),
		)))
		for _, obj := range obsolete {
			g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.GetFinalizers()).To(ContainElement(metav1.FinalizerDeleteDependents))
			g.Expect(obj.GetDeletionTimestamp().IsZero()).To(BeFalse())
		}
	}).WithTimeout(45 * time.Second).Should(Succeed())
	// Finish foreground deletion in envtest.
	for _, obj := range obsolete {
		obj.SetFinalizers(nil)
		g.Expect(cli.Update(ctx, obj)).To(Succeed())
		g.Expect(k8serr.IsNotFound(cli.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
	}
	g.Expect(legacyDeletedBeforeRelease.Load()).To(BeFalse(),
		"legacy 3.6 resources were deleted before the tracker committed release %s", cluster.GetRelease().Version.String())
}

func TestLegacyUpgradeKeepsOperatorUntilRemovedModuleCRIsGone(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	const namespace = "completion"
	et := newPlatformModuleTestEnv(t, namespace, "3.7.0", true)
	cli := et.Client()
	moduleHandler := registerTestModule(t, et)

	// Seed a removed 3.6 module with a terminating CR and a live operator.
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName}}
	platform.Spec.Modules.Dashboard.ManagementState = operatorv1.Removed
	g.Expect(cli.Create(ctx, platform)).To(Succeed())
	module := &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{
		Name: moduleHandler.GetCRName(), Finalizers: []string{"test.io/module-cleanup"},
	}}
	g.Expect(cli.Create(ctx, module)).To(Succeed())
	g.Expect(cli.Delete(ctx, module)).To(Succeed())

	operatorLabels := map[string]string{"app": "operator"}
	operator := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "operator", Namespace: namespace, Labels: operatorLabels,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
			Annotations: map[string]string{
				annotations.InstanceUID:     string(platform.UID),
				annotations.InstanceName:    platform.Name,
				annotations.PlatformVersion: "3.6.0",
				annotations.PlatformType:    string(cluster.XKS),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: operatorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: operatorLabels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: "example.com/operator"}}},
			},
		},
	}
	g.Expect(cli.Create(ctx, operator)).To(Succeed())

	var blockStatus, statusFailed atomic.Bool
	stop := startPlatformModuleController(t, et, "", &blockStatus, &statusFailed, true)
	// Keep the operator available while the module CR is terminating.
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(platform), platform)).To(Succeed())
		g.Expect(platform.Status.DAG.Release).To(Equal(cluster.GetRelease()))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(module), module)).To(Succeed())
	g.Expect(module.GetDeletionTimestamp().IsZero()).To(BeFalse())
	trackerKey := client.ObjectKey{Name: moduleHandler.GetName()}
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, trackerKey, &configApi.PlatformModule{}))).To(BeTrue())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(operator), operator)).To(Succeed())
	g.Expect(operator.DeletionTimestamp.IsZero()).To(BeTrue(), "the legacy operator must remain while the module CR finalizes")

	// Finish deleting the CR while the controllers are stopped.
	stop()
	module.SetFinalizers(nil)
	g.Expect(cli.Update(ctx, module)).To(Succeed())
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, client.ObjectKeyFromObject(module), module))).To(BeTrue())

	// Resume legacy operator cleanup after restart.
	startPlatformModuleController(t, et, "", &blockStatus, &statusFailed, true)
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(operator), operator)).To(Succeed())
		g.Expect(operator.DeletionTimestamp.IsZero()).To(BeFalse())
		g.Expect(operator.Finalizers).To(ContainElement(metav1.FinalizerDeleteDependents))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	// Finish foreground deletion in envtest.
	operator.Finalizers = nil
	g.Expect(cli.Update(ctx, operator)).To(Succeed())
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, client.ObjectKeyFromObject(operator), operator))).To(BeTrue())
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, trackerKey, &configApi.PlatformModule{}))).To(BeTrue())
}

func TestPlatformModuleRecoversInterruptedInventory(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	const namespace = "completion"
	et := newPlatformModuleTestEnv(t, namespace, "2.0.0", true)
	cli := et.Client()
	registerTestModule(t, et)
	g.Expect(cli.Create(ctx, platformWithDAG(0))).To(Succeed())
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}}
	g.Expect(cli.Create(ctx, pm)).To(Succeed())
	operatorKey := client.ObjectKey{Namespace: namespace, Name: "operator-config"}

	var blockCheckpoint, checkpointFailed, blockStatus, statusFailed atomic.Bool
	blockCheckpoint.Store(true)
	blockStatus.Store(true)
	manifestsRoot := filepath.Join(et.ProjectRoot(), "internal/controller/platformmodules/testdata")
	stop := startPlatformModuleController(t, et, manifestsRoot, &blockStatus, &statusFailed, false,
		func(faults *interceptor.Funcs) {
			faults.SubResourcePatch = func(ctx context.Context, cli client.Client, subresource string, obj client.Object,
				patch client.Patch, opts ...client.SubResourcePatchOption,
			) error {
				if _, ok := obj.(*configApi.PlatformModule); ok && blockCheckpoint.Load() {
					checkpointFailed.Store(true)
					return k8serr.NewServiceUnavailable("inventory write interrupted")
				}
				return cli.SubResource(subresource).Patch(ctx, obj, patch, opts...)
			}
		})
	g.Eventually(checkpointFailed.Load).WithTimeout(10 * time.Second).Should(BeTrue())
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, operatorKey, &corev1.ConfigMap{}))).To(BeTrue(), "apply must wait for the checkpoint")

	blockCheckpoint.Store(false)
	g.Eventually(statusFailed.Load).WithTimeout(15 * time.Second).Should(BeTrue())
	g.Expect(cli.Get(ctx, operatorKey, &corev1.ConfigMap{})).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	g.Expect(pm.Status.Release.Version.String()).NotTo(Equal("2.0.0"))
	g.Expect(pm.Status.Resources).To(ContainElement(configApi.ResourceRef{
		Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: operatorKey.Name,
	}))
	stop()

	smallerRoot := t.TempDir()
	g.Expect(os.Mkdir(filepath.Join(smallerRoot, "completion"), 0o750)).To(Succeed())
	smaller, err := os.OpenRoot(filepath.Join(smallerRoot, "completion"))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { g.Expect(smaller.Close()).To(Succeed()) })
	for _, name := range []string{"kustomization.yaml", "operator.yaml"} {
		data, err := os.ReadFile(filepath.Join(manifestsRoot, "completion", name))
		g.Expect(err).NotTo(HaveOccurred())
		if name == "operator.yaml" {
			_, remaining, found := strings.Cut(string(data), "---\n")
			g.Expect(found).To(BeTrue())
			data = []byte(remaining)
		}
		g.Expect(smaller.WriteFile(name, data, 0o600)).To(Succeed())
	}
	blockStatus.Store(false)
	startPlatformModuleController(t, et, smallerRoot, &blockStatus, &statusFailed, false)
	g.Eventually(func(g Gomega) {
		g.Expect(k8serr.IsNotFound(cli.Get(ctx, operatorKey, &corev1.ConfigMap{}))).To(BeTrue())
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(pm.Status.Release.Version.String()).To(Equal("2.0.0"))
	}).WithTimeout(45 * time.Second).Should(Succeed())
}

func TestPlatformModuleRetriesStaleRenderFailureWithoutLosingInventory(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	const namespace = "stale-cache"
	et := newPlatformModuleTestEnv(t, namespace, "2.0.0", true)
	cli := et.Client()
	registerTestModule(t, et)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", Finalizers: []string{frameworkreconciler.DefaultFinalizerName}}}
	g.Expect(cli.Create(ctx, pm)).To(Succeed())
	previousRef := configApi.ResourceRef{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: "previous"}
	pm.Status.Resources = []configApi.ResourceRef{previousRef}
	g.Expect(cli.Status().Update(ctx, pm)).To(Succeed())
	old := pm.DeepCopy()
	// Model a successful checkpoint that the first cached read has yet to see.
	ref := configApi.ResourceRef{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: "applied"}
	pm.Status.Resources = append(pm.Status.Resources, ref)
	pm.Status.Release = testRelease("1.0.0")
	pm.Status.ObservedGeneration = pm.Generation
	g.Expect(cli.Status().Update(ctx, pm)).To(Succeed())

	var blockStatus, statusFailed, returnedStale atomic.Bool
	oldObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(old)
	g.Expect(err).NotTo(HaveOccurred())
	startPlatformModuleController(t, et, t.TempDir(), &blockStatus, &statusFailed, false, func(faults *interceptor.Funcs) {
		faults.Get = func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if tracker, ok := obj.(*unstructured.Unstructured); ok && tracker.GroupVersionKind() == gvk.PlatformModule &&
				key.Name == old.Name && returnedStale.CompareAndSwap(false, true) {
				gvk := tracker.GroupVersionKind()
				tracker.SetUnstructuredContent(runtime.DeepCopyJSON(oldObject))
				tracker.SetGroupVersionKind(gvk)
				return nil
			}
			return cli.Get(ctx, key, obj, opts...)
		}
	})
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).To(HaveValue(
			HaveField("Status", metav1.ConditionFalse),
		))
	}).WithTimeout(15 * time.Second).Should(Succeed())
	g.Expect(returnedStale.Load()).To(BeTrue())
	g.Expect(pm.Status.Resources).To(ConsistOf(previousRef, ref))
	g.Expect(pm.Status.Release.Version.String()).To(Equal("1.0.0"))
	g.Expect(pm.Status.ObservedGeneration).To(Equal(pm.Generation))
}

// TestPlatformModuleControllerLifecycle covers upgrade, removal, and re-enable through API events.
func TestPlatformModuleControllerLifecycle(t *testing.T) {
	t.Run("removal", func(t *testing.T) { testPlatformModuleControllerLifecycle(t, false, "platform") })
	t.Run("reenable during removal", func(t *testing.T) { testPlatformModuleControllerLifecycle(t, true, "datasciencecluster") })
}

func testPlatformModuleControllerLifecycle(t *testing.T, reenable bool, previousFieldOwner string) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()
	const namespace = "completion"
	et := newPlatformModuleTestEnv(t, namespace, "2.0.0", false)
	cli := et.Client()
	moduleHandler := registerTestModule(t, et)
	release := cluster.GetRelease()

	// Seed a healthy 1.0.0 installation with the upgrade runlevel closed.
	platform := platformWithDAG(0)
	platform.Spec.Modules.Dashboard.ManagementState = operatorv1.Managed
	platform.Status.DAG.Release = release
	dagStatus := platform.Status.DAG
	g.Expect(cli.Create(ctx, platform)).To(Succeed())
	platform.Status.DAG = dagStatus
	g.Expect(cli.Status().Update(ctx, platform)).To(Succeed())
	pm, stale, module := createPreviousModuleRelease(t, cli, namespace, platform)
	previous := pm.DeepCopy()

	var blockStatus, statusFailed atomic.Bool
	manifestsRoot := filepath.Join(et.ProjectRoot(), "internal/controller/platformmodules/testdata")

	// Keep the old release Ready without applying the upgrade.
	stopController := startPlatformModuleController(t, et, manifestsRoot, &blockStatus, &statusFailed, false)
	operator := &corev1.ConfigMap{}
	operatorKey := client.ObjectKey{Namespace: namespace, Name: "operator-config"}
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(pm.Finalizers).NotTo(BeEmpty())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).To(HaveValue(And(
			HaveField("ObservedGeneration", pm.Generation), HaveField("Status", metav1.ConditionTrue),
		)))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Status).To(Equal(metav1.ConditionTrue))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(pm.Status.Release).To(Equal(previous.Status.Release))
	g.Expect(pm.Status.Resources).To(Equal(previous.Status.Resources))
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, operatorKey, operator))).To(BeTrue(), "closed runlevel must prevent apply")

	// Seed a Platform-owned ConfigMap for field ownership migration.
	legacyOperator := corev1apply.ConfigMap(operatorKey.Name, operatorKey.Namespace).
		WithOwnerReferences(metav1apply.OwnerReference().WithAPIVersion(gvk.Platform.GroupVersion().String()).
			WithKind(gvk.Platform.Kind).WithName(platform.Name).WithUID(platform.UID).WithController(true)).
		WithData(map[string]string{"obsolete": "legacy"})
	g.Expect(cli.Apply(ctx, legacyOperator, client.FieldOwner(previousFieldOwner))).To(Succeed())
	g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
	operatorUID := operator.UID
	// Publish the upgrade even if the user has yet to create a module CR.
	g.Expect(cli.Delete(ctx, module)).To(Succeed())

	// Open the runlevel and hold publication while the stale resource is deleting.
	platform.Status.DAG.ClearedRunlevel = 20
	g.Expect(cli.Status().Update(ctx, platform)).To(Succeed())
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(stale), stale)).To(Succeed())
		g.Expect(stale.DeletionTimestamp.IsZero()).To(BeFalse())
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).To(HaveValue(And(
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Message", And(
				ContainSubstring("operator resources are still deleting"),
				ContainSubstring("ConfigMap completion/old-config (finalizers: test.io/hold)"),
			)),
		)))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	inventory := []configApi.ResourceRef{
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: PlatformConfigName(pm.Name)},
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: operatorKey.Name},
		{Group: gvk.Lease.Group, Version: "v1", Kind: gvk.Lease.Kind, Namespace: namespace, Name: "operator-lock"},
		{Group: gvk.Deployment.Group, Version: "v1", Kind: gvk.Deployment.Kind, Namespace: namespace, Name: "operator"},
	}
	checkpoint := slices.Concat(previous.Status.Resources, inventory)
	g.Expect(pm.Status.Resources).To(ConsistOf(checkpoint))
	g.Expect(pm.Status.Release).To(Equal(previous.Status.Release))

	// Restart while cleanup is pending.
	stopController()
	startPlatformModuleController(t, et, manifestsRoot, &blockStatus, &statusFailed, false)

	// Keep the previous release and full checkpoint when the status write fails.
	blockStatus.Store(true)
	stale.Finalizers = nil
	g.Expect(cli.Update(ctx, stale)).To(Succeed())
	g.Eventually(statusFailed.Load).WithTimeout(20 * time.Second).Should(BeTrue())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	g.Expect(pm.Status.Resources).To(ConsistOf(checkpoint))
	g.Expect(pm.Status.Release).To(Equal(previous.Status.Release))

	// Publish the new release and inventory together on retry.
	blockStatus.Store(false)
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(pm.Status.Release).To(Equal(release))
		g.Expect(pm.Status.Resources).To(ConsistOf(inventory))
		for _, ref := range pm.Status.Resources {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(ref.GroupVersionKind())
			key := client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}
			g.Expect(cli.Get(ctx, key, obj)).To(Succeed(), "inventory resource %s %s", ref.Kind, key)
			g.Expect(obj.GetDeletionTimestamp().IsZero()).To(BeTrue(), "inventory resource %s %s is terminating", ref.Kind, key)
		}
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).
			To(HaveValue(HaveField("Status", metav1.ConditionTrue)))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)).
			To(HaveValue(HaveField("Status", Not(Equal(metav1.ConditionTrue)))))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
	g.Expect(operator.UID).To(Equal(operatorUID))
	g.Expect(operator.Data).To(Equal(map[string]string{"configured": "true"}))
	g.Expect(operator.ManagedFields).NotTo(ContainElement(And(
		HaveField("Manager", previousFieldOwner), HaveField("Operation", metav1.ManagedFieldsOperationApply),
	)))
	g.Expect(metav1.GetControllerOf(operator).UID).To(Equal(pm.UID))
	lease := &coordinationv1.Lease{}
	leaseKey := client.ObjectKey{Namespace: namespace, Name: "operator-lock"}
	g.Expect(cli.Get(ctx, leaseKey, lease)).To(Succeed())
	g.Expect(lease.OwnerReferences).To(BeEmpty())
	leaseUID := lease.UID

	// Keep the tracker unready while its Deployment rolls out.
	module = &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "default", Finalizers: []string{"test.io/module"}}}
	g.Expect(cli.Create(ctx, module)).To(Succeed())
	module.Status.Conditions = []common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue}}
	g.Expect(cli.Status().Update(ctx, module)).To(Succeed())
	assertReadiness := func(moduleReady, trackerReady metav1.ConditionStatus) {
		t.Helper()
		g.Eventually(func(g Gomega) {
			g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)).
				To(HaveValue(HaveField("Status", moduleReady)))
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).Status).To(Equal(metav1.ConditionTrue))
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Status).To(Equal(trackerReady))
			g.Expect(pm.Status.Release).To(Equal(release))
			g.Expect(pm.Status.Resources).To(ConsistOf(inventory))
		}).WithTimeout(20 * time.Second).Should(Succeed())
	}
	assertReadiness(metav1.ConditionTrue, metav1.ConditionFalse)

	// Complete this module's rollout while another module remains unready.
	deployment := &appsv1.Deployment{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "operator"}, deployment)).To(Succeed())
	// An extra Deployment outside this module's inventory must not affect
	// its availability. It shares the operator's labels and never reports a
	// rollout: only the inventory-based check can ignore it.
	g.Expect(cli.Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-operator", Namespace: namespace, Labels: maps.Clone(deployment.Labels),
		},
		Spec: *deployment.Spec.DeepCopy(),
	})).To(Succeed())
	// Envtest has no Deployment controller to report rollout status.
	deployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration: deployment.Generation, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
	}
	g.Expect(cli.Status().Update(ctx, deployment)).To(Succeed())
	assertReadiness(metav1.ConditionTrue, metav1.ConditionTrue)

	// Follow module CR readiness without changing the committed release or inventory.
	module.Status.Conditions[0].Status = metav1.ConditionFalse
	g.Expect(cli.Status().Update(ctx, module)).To(Succeed())
	assertReadiness(metav1.ConditionFalse, metav1.ConditionFalse)

	// Recreate a deleted owned resource through its watch.
	originalUID := operator.UID
	g.Expect(cli.Delete(ctx, operator)).To(Succeed())
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
		g.Expect(operator.UID).NotTo(Equal(originalUID))
		g.Expect(metav1.GetControllerOf(operator).UID).To(Equal(pm.UID))
	}).WithTimeout(20*time.Second).Should(Succeed(), "owned-resource deletion must trigger reconciliation")

	testPlatformModuleRemoval(t, cli, platform, pm, module, operator, moduleHandler, reenable)
	g.Expect(cli.Get(ctx, leaseKey, lease)).To(Succeed())
	g.Expect(lease.UID).To(Equal(leaseUID), "protected resources must survive tracker removal")
}

func testPlatformModuleRemoval(
	t *testing.T, cli client.Client, platform *configApi.Platform, pm *configApi.PlatformModule,
	module *testscheme.TestPlatformObject, operator *corev1.ConfigMap, moduleHandler *controllerTestHandler, reenable bool,
) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()
	operatorKey := client.ObjectKeyFromObject(operator)
	trackerKey, trackerUID := client.ObjectKeyFromObject(pm), pm.UID

	// Keep the operator running while the module CR finalizes.
	platform.Spec.Modules.Dashboard.ManagementState = operatorv1.Removed
	g.Expect(cli.Update(ctx, platform)).To(Succeed())
	operator.Finalizers = []string{"test.io/operator"}
	g.Expect(cli.Update(ctx, operator)).To(Succeed())
	g.Expect(cli.Delete(ctx, pm)).To(Succeed())
	g.Expect(cli.Delete(ctx, module)).To(Succeed())
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, trackerKey, pm)).To(Succeed())
		g.Expect(pm.Finalizers).NotTo(BeEmpty())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady)).NotTo(BeNil())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Message).To(ContainSubstring("module CR"))
		g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
		g.Expect(operator.DeletionTimestamp.IsZero()).To(BeTrue())
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(module), module)).To(Succeed())

	// Keep the tracker until its operator resources are deleted.
	module.Finalizers = nil
	g.Expect(cli.Update(ctx, module)).To(Succeed())
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
		g.Expect(operator.DeletionTimestamp.IsZero()).To(BeFalse())
		g.Expect(cli.Get(ctx, trackerKey, pm)).To(Succeed())
		g.Expect(pm.Finalizers).NotTo(BeEmpty())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Message).
			To(ContainSubstring("ConfigMap completion/operator-config (finalizers: test.io/operator)"))
	}).WithTimeout(20*time.Second).Should(Succeed(), "tracker must wait for actual operator-resource deletion")

	if reenable {
		// Re-enable while the original tracker is still terminating.
		platform.Spec.Modules.Dashboard.ManagementState = operatorv1.Managed
		g.Expect(cli.Update(ctx, platform)).To(Succeed())
		replacement := &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: module.Name}}
		g.Expect(cli.Create(ctx, replacement)).To(Succeed())
		g.Expect(replacement.UID).NotTo(Equal(module.UID))
		g.Eventually(moduleHandler.observedLiveModuleCR.Load).WithTimeout(20*time.Second).
			Should(BeTrue(), "the finalizer must observe the replacement CR before cleanup resumes")
		g.Expect(cli.Get(ctx, trackerKey, pm)).To(Succeed())
		g.Expect(pm.UID).To(Equal(trackerUID))
		g.Expect(pm.DeletionTimestamp.IsZero()).To(BeFalse(), "the original tracker must still be terminating after re-enable")
		g.Expect(pm.Finalizers).NotTo(BeEmpty())
	}

	// Finish deleting the original tracker.
	operator.Finalizers = nil
	g.Expect(cli.Update(ctx, operator)).To(Succeed())
	g.Eventually(func() bool { return k8serr.IsNotFound(cli.Get(ctx, trackerKey, pm)) }).
		WithTimeout(20*time.Second).Should(BeTrue(), "the old tracker must finish removal")
	if reenable {
		// Simulate Platform recreating the tracker with a new UID.
		fresh := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{
			Name: pm.Name, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
		}}
		g.Expect(cli.Create(ctx, fresh)).To(Succeed())
		g.Expect(fresh.UID).NotTo(Equal(trackerUID))
		g.Eventually(func(g Gomega) {
			g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
			g.Expect(metav1.GetControllerOf(operator)).NotTo(BeNil())
			g.Expect(metav1.GetControllerOf(operator).UID).To(Equal(fresh.UID))
		}).WithTimeout(20 * time.Second).Should(Succeed())
	}
}
