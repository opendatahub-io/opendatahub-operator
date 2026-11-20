//nolint:testpackage // Starts the production controller and verifies its watches and action pipeline.
package platformmodules

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	frameworkmanager "github.com/opendatahub-io/odh-platform-utilities/framework/manager"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/spf13/viper"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

// Use the real module CR readers so lifecycle and readiness changes reach the
// controller through its watches, rather than through mutable stub state.
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

// EnvT provides the API server, CRDs, and client. Its manager helpers create one
// manager; this test needs a fresh manager on the same API server after shutdown.
func startPlatformModuleController(t *testing.T, et *envt.EnvT, manifestsRoot string, blockStatus, statusFailed *atomic.Bool) func() {
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
	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme: et.Scheme(), Metrics: metricsserver.Options{BindAddress: "0"},
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

// Seed API state left by a healthy previous release; no previous operator image
// runs here. Nonzero transition times also make these conditions valid SSA data.
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
	// old-config belongs to the previous inventory but is absent from the new
	// manifests. The test finalizer lets us hold its deletion after Delete succeeds.
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

// TestPlatformModuleControllerLifecycle runs the production controller against
// envtest's API server and etcd. The test drives API events, never Reconcile.
// ConfigMap/Lease fixtures stand in for operator resources; the test writes DAG
// and module CR status because their controllers are not running here.
func TestPlatformModuleControllerLifecycle(t *testing.T) {
	t.Run("removal", func(t *testing.T) { testPlatformModuleControllerLifecycle(t, false) })
	t.Run("reenable during removal", func(t *testing.T) { testPlatformModuleControllerLifecycle(t, true) })
}

func testPlatformModuleControllerLifecycle(t *testing.T, reenable bool) {
	t.Helper()
	// Phase 1: seed a healthy 1.0.0 installation, not a fresh install. The running
	// operator targets 2.0.0, but Platform has not opened this module's runlevel yet.
	g := NewWithT(t)
	ctx := t.Context()
	withCleanRegistries(t)
	const namespace = "completion"
	for key, value := range map[string]any{
		"rhai-version":                "2.0.0",
		"rhai-applications-namespace": namespace,
		"disable-dag-ordering":        false,
	} {
		previous := viper.Get(key)
		viper.Set(key, value)
		t.Cleanup(func() { viper.Set(key, previous) })
	}
	root, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())
	// Standalone Kubernetes uses Platform directly; the DSCI CRD is intentionally absent.
	et, err := envt.New(envt.WithCRDPaths(
		filepath.Join(root, "config/crd/bases/config.opendatahub.io_platforms.yaml"),
		filepath.Join(root, "config/crd/bases/config.opendatahub.io_platformmodules.yaml"),
	))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { g.Expect(et.Stop()).To(Succeed()) })
	cli := et.Client()
	initClient, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster.Init(ctx, initClient, operatorconfig.OperatorSettings{
		OperatorNamespace: namespace, PlatformType: string(cluster.XKS),
	})).To(Succeed())
	release := cluster.GetRelease()
	_, err = et.RegisterCRD(ctx, testscheme.TestPlatformObjectGVK,
		"testplatformobjects", "testplatformobject", apiextensionsv1.ClusterScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	moduleHandler := &controllerTestHandler{stubModuleHandler: stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{
		Name: "testmod", CRName: "default", GVK: testscheme.TestPlatformObjectGVK, ManifestDir: "completion",
	}}}}
	modules.Add(moduleHandler)
	provision.DefaultRegistry().Add("testmod", provision.KindModule, dag.RL(20))
	g.Expect(cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())
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
	manifestsRoot := filepath.Join(root, "internal/controller/platformmodules/testdata")

	// Phase 2: runlevel 20 is closed (only 0 is cleared). The existing module stays
	// Ready; its old committed release keeps the DAG from treating 2.0.0 as applied.
	stopController := startPlatformModuleController(t, et, manifestsRoot, &blockStatus, &statusFailed)
	operator := &corev1.ConfigMap{}
	operatorKey := client.ObjectKey{Namespace: namespace, Name: "operator-config"}
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(pm.Finalizers).NotTo(BeEmpty())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).NotTo(BeNil())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).ObservedGeneration).To(Equal(pm.Generation))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).Status).To(Equal(metav1.ConditionTrue))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Status).To(Equal(metav1.ConditionTrue))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(pm.Status.Release).To(Equal(previous.Status.Release))
	g.Expect(pm.Status.Resources).To(Equal(previous.Status.Resources))
	g.Expect(k8serr.IsNotFound(cli.Get(ctx, operatorKey, operator))).To(BeTrue(), "closed runlevel must prevent apply")
	// Exercise the remaining upgrade without a user-created module CR. Its
	// absence affects runtime readiness, but must not prevent release publication.
	g.Expect(cli.Delete(ctx, module)).To(Succeed())

	// Phase 3: a Platform DAG status event opens the gate; no direct Reconcile call.
	// Applying the new manifests makes old-config stale. Cleanup requests its
	// deletion; test.io/hold keeps it present with a deletionTimestamp. Accepting
	// Delete is insufficient: provisioning and publication wait until it is gone.
	platform.Status.DAG.ClearedRunlevel = 20
	g.Expect(cli.Status().Update(ctx, platform)).To(Succeed())
	g.Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(stale), stale)).To(Succeed())
		g.Expect(stale.DeletionTimestamp.IsZero()).To(BeFalse())
		g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).NotTo(BeNil())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).Status).To(Equal(metav1.ConditionFalse))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).Message).To(And(
			ContainSubstring("operator resources are still deleting"),
			ContainSubstring("ConfigMap completion/old-config (finalizers: test.io/hold)"),
		))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	g.Expect(pm.Status.Resources).To(Equal(previous.Status.Resources))
	g.Expect(pm.Status.Release).To(Equal(previous.Status.Release))

	// Phase 4: restart while cleanup is pending; recovery uses persisted state.
	stopController()
	startPlatformModuleController(t, et, manifestsRoot, &blockStatus, &statusFailed)

	// Phase 5: cleanup finishes, but a failed status apply preserves the previous commit.
	blockStatus.Store(true)
	stale.Finalizers = nil
	g.Expect(cli.Update(ctx, stale)).To(Succeed())
	g.Eventually(statusFailed.Load).WithTimeout(20 * time.Second).Should(BeTrue())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	g.Expect(pm.Status.Resources).To(Equal(previous.Status.Resources))
	g.Expect(pm.Status.Release).To(Equal(previous.Status.Release))

	// Phase 6: retry publishes release and inventory together without a module CR.
	// On standalone Kubernetes, users create the module CR after its operator is installed.
	blockStatus.Store(false)
	inventory := []configApi.ResourceRef{
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: PlatformConfigName(pm.Name)},
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: operatorKey.Name},
		{Group: gvk.Lease.Group, Version: "v1", Kind: gvk.Lease.Kind, Namespace: namespace, Name: "operator-lock"},
	}
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
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded)).NotTo(BeNil())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)).NotTo(BeNil())
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).Status).To(Equal(metav1.ConditionTrue))
		g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady).Status).NotTo(Equal(metav1.ConditionTrue))
	}).WithTimeout(20 * time.Second).Should(Succeed())
	g.Expect(cli.Get(ctx, operatorKey, operator)).To(Succeed())
	g.Expect(metav1.GetControllerOf(operator).UID).To(Equal(pm.UID))
	lease := &coordinationv1.Lease{}
	leaseKey := client.ObjectKey{Namespace: namespace, Name: "operator-lock"}
	g.Expect(cli.Get(ctx, leaseKey, lease)).To(Succeed())
	g.Expect(lease.OwnerReferences).To(BeEmpty())
	leaseUID := lease.UID

	// Phase 7: a user creates the module CR; its status changes runtime readiness
	// without changing the committed operator release or inventory.
	module = &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "default", Finalizers: []string{"test.io/module"}}}
	g.Expect(cli.Create(ctx, module)).To(Succeed())
	for _, ready := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse} {
		module.Status.Conditions = []common.Condition{{Type: status.ConditionTypeReady, Status: ready}}
		g.Expect(cli.Status().Update(ctx, module)).To(Succeed())
		g.Eventually(func(g Gomega) {
			g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(pm), pm)).To(Succeed())
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)).NotTo(BeNil())
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady).Status).To(Equal(ready))
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeProvisioningSucceeded).Status).To(Equal(metav1.ConditionTrue))
			g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeReady).Status).To(Equal(ready))
			g.Expect(pm.Status.Release).To(Equal(release))
			g.Expect(pm.Status.Resources).To(ConsistOf(inventory))
		}).WithTimeout(20 * time.Second).Should(Succeed())
	}

	// Phase 8: an ownership watch recreates a deleted resource under the same tracker.
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

	// Phase 9: tracker deletion keeps the operator available for module CR finalizers.
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

	// Phase 10: once the module CR is gone, cleanup waits for operator-resource deletion.
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

	// Phase 11: re-enable before releasing the last operator finalizer.
	if reenable {
		platform.Spec.Modules.Dashboard.ManagementState = operatorv1.Managed
		g.Expect(cli.Update(ctx, platform)).To(Succeed())
		replacement := &testscheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: module.Name}}
		g.Expect(cli.Create(ctx, replacement)).To(Succeed())
		g.Expect(replacement.UID).NotTo(Equal(module.UID))
		g.Eventually(moduleHandler.observedLiveModuleCR.Load).WithTimeout(20*time.Second).
			Should(BeTrue(), "the finalizer must observe the replacement CR before cleanup resumes")
		// The same tracker must still be terminating after Platform returns to Managed.
		g.Expect(cli.Get(ctx, trackerKey, pm)).To(Succeed())
		g.Expect(pm.UID).To(Equal(trackerUID))
		g.Expect(pm.DeletionTimestamp.IsZero()).To(BeFalse(), "the original tracker must still be terminating after re-enable")
		g.Expect(pm.Finalizers).NotTo(BeEmpty())
	}
	operator.Finalizers = nil
	g.Expect(cli.Update(ctx, operator)).To(Succeed())
	g.Eventually(func() bool { return k8serr.IsNotFound(cli.Get(ctx, trackerKey, pm)) }).
		WithTimeout(20*time.Second).Should(BeTrue(), "the old tracker must finish removal")
	if reenable {
		// Simulate the Platform controller creating a tracker with the same name and a new UID.
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
