//nolint:testpackage // Exercises package-private actions directly.
package platformmodules

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func withCleanRegistries(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		modules.DefaultRegistry().Reset()
		provision.DefaultRegistry().Reset()
		provision.GetRunlevelTracker().Reset()
	})
	modules.DefaultRegistry().Reset()
	provision.DefaultRegistry().Reset()
	provision.GetRunlevelTracker().Reset()
}

type stubModuleHandler struct {
	modules.BaseHandler

	enabled         bool
	moduleStatus    *modules.ModuleStatus
	moduleStatusErr error
	crState         modules.CRState
}

func (s stubModuleHandler) IsEnabled(_ *configApi.PlatformModules) bool {
	return s.enabled
}

func (s stubModuleHandler) GetModuleStatus(_ context.Context, _ client.Client) (*modules.ModuleStatus, error) {
	return s.moduleStatus, s.moduleStatusErr
}

func (s stubModuleHandler) GetModuleCRState(_ context.Context, _ client.Client) (modules.CRState, error) {
	return s.crState, nil
}

func (s stubModuleHandler) PopulatePlatformModule(_ *configApi.PlatformModules, _ *modules.DSCContext) {
}

func (s stubModuleHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *modules.DSCContext, _ *modules.ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func testRelease(v string) common.Release {
	sv, _ := semver.Parse(v)
	return common.Release{
		Version: ofversion.OperatorVersion{Version: sv},
	}
}

func TestCheckPlatformModuleRunlevelGate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cleared int
		version string
		pending bool
	}{
		{name: "cleared", cleared: 20, version: "1.0.0"},
		{name: "runlevel not cleared", cleared: 0, version: "1.0.0", pending: true},
		{name: "previous release", cleared: 20, version: "0.9.0", pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			withCleanRegistries(t)
			provision.DefaultRegistry().Add("testmod", provision.KindModule, dag.RL(20))
			platform := platformWithDAG(tc.cleared)
			platform.Status.DAG.Release = testRelease(tc.version)
			cli, err := fakeclient.New(fakeclient.WithObjects(platform))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtype.ReconciliationRequest{
				Client: cli, Instance: &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}},
				Release: testRelease("1.0.0"),
			}

			err = checkPlatformModuleRunlevelGate(t.Context(), rr)
			g.Expect(rr.SkipDeploy).To(Equal(tc.pending))
			if tc.pending {
				var pending odherrors.RequeueAfterError
				g.Expect(errors.As(err, &pending)).To(BeTrue(), "%v", err)
				g.Expect(pending.After).To(Equal(30 * time.Second))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestUpdatePlatformModuleStatusSkipsDeferredDeployment(t *testing.T) {
	g := NewWithT(t)
	pm, desired, stale := completionFixture(t)
	cli, err := fakeclient.New(fakeclient.WithObjects(stale))
	g.Expect(err).NotTo(HaveOccurred())
	rr := completionRequest(pm, cli, desired)
	rr.SkipDeploy = true
	before := pm.DeepCopy()
	g.Expect(updatePlatformModuleStatus(t.Context(), rr)).To(Succeed())
	g.Expect(pm.Status.Release).To(Equal(before.Status.Release))
	g.Expect(pm.Status.Resources).To(Equal(before.Status.Resources))
	moduleCRReady := conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)
	g.Expect(moduleCRReady).NotTo(BeNil())
	g.Expect(moduleCRReady.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(stale), stale)).To(Succeed())
	g.Expect(stale.DeletionTimestamp.IsZero()).To(BeTrue(), "deferred deployment must not start cleanup")
}

func TestUpdatePlatformModuleStatusKeepsCommittedAvailabilityWhileGated(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	modules.Add(&stubModuleHandler{
		BaseHandler:  modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}},
		moduleStatus: &modules.ModuleStatus{},
	})
	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}}
	// The committed release is old; the inventory union carries a Deployment
	// from a failed attempt at the new release that never rolled out.
	pm.Status.Release = testRelease("1.0.0")
	pm.Status.Resources = []configApi.ResourceRef{{
		Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "apps", Name: "operator-b",
	}}
	pm.SetConditions([]common.Condition{{Type: status.ConditionDeploymentsAvailable, Status: metav1.ConditionTrue}})
	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: pm,
		Release:  testRelease("2.0.0"),
		Conditions: conditionstest.NewManager(pm, status.ConditionTypeReady, status.ConditionTypeProvisioningSucceeded,
			status.ConditionTypeModuleCRReady, status.ConditionDeploymentsAvailable),
	}
	rr.SkipDeploy = true
	g.Expect(updatePlatformModuleStatus(t.Context(), rr)).To(Succeed())
	condition := conditions.FindStatusCondition(pm, status.ConditionDeploymentsAvailable)
	g.Expect(condition).ToNot(BeNil())
	g.Expect(condition.Status).To(Equal(metav1.ConditionTrue),
		"a pending Deployment from a failed release must not mark the running operator unhealthy")
}

func TestHandlePlatformModuleDeletionModuleCRState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state modules.CRState
	}{
		{name: "absent", state: modules.CRStateAbsent},
		{name: "deleting", state: modules.CRStateDeleting},
		{name: "alive", state: modules.CRStateAlive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			withCleanRegistries(t)
			modules.Add(&stubModuleHandler{
				BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}},
				crState:     tc.state,
			})
			cli, err := fakeclient.New()
			g.Expect(err).NotTo(HaveOccurred())
			err = handlePlatformModuleDeletion(t.Context(), &odhtype.ReconciliationRequest{
				Client: cli, Instance: &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}},
			})
			if tc.state == modules.CRStateAbsent {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring("still exists")))
			}
		})
	}
}

func TestHandlePlatformModuleDeletion_TwoPhaseCleanup(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)

	handler := stubModuleHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:   "testmod",
				CRName: "default",
				GVK:    schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestModule"},
			},
		},
		crState: modules.CRStateDeleting,
	}
	modules.Add(&handler)

	const namespace = "opendatahub"
	const trackerUID = k8stypes.UID("tracker-uid")
	owner := platformModuleOwner(trackerUID)
	trackedObjects := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "testmod-controller-manager", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{owner}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "odh-testmod-config", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{owner}}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "testmod-manager-role", OwnerReferences: []metav1.OwnerReference{owner}}},
	}
	trackedRefs := []configApi.ResourceRef{
		{Group: "apps", Version: "v1", Kind: gvk.Deployment.Kind, Namespace: namespace, Name: "testmod-controller-manager"},
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: namespace, Name: "odh-testmod-config"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: gvk.ClusterRole.Kind, Name: "testmod-manager-role"},
	}

	s, err := testscheme.New()
	g.Expect(err).ToNot(HaveOccurred())
	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(trackedObjects...))
	g.Expect(err).ToNot(HaveOccurred())

	pm := &configApi.PlatformModule{
		ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: trackerUID},
		Status:     configApi.PlatformModuleStatus{Resources: trackedRefs},
	}
	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: pm,
		Release:  testRelease("1.0.0"),
	}

	// Phase 1: the module CR is still deleting, so the tracker finalizer must
	// keep all operator resources available for the module operator.
	err = handlePlatformModuleDeletion(context.Background(), rr)
	g.Expect(err).To(HaveOccurred())
	for _, ref := range trackedRefs {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(ref.GroupVersionKind())
		err = cli.Get(context.Background(), client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, obj)
		g.Expect(err).ToNot(HaveOccurred(), "resource %s/%s should remain while the module CR is deleting", ref.Kind, ref.Name)
	}

	// Phase 2: once the module CR is gone, the tracker finalizer deletes the
	// resources recorded in PlatformModule.Status.Resources.
	handler.crState = modules.CRStateAbsent
	err = handlePlatformModuleDeletion(context.Background(), rr)
	var pending odherrors.RequeueAfterError
	g.Expect(errors.As(err, &pending)).To(BeTrue())
	g.Expect(handlePlatformModuleDeletion(context.Background(), rr)).To(Succeed())
	for _, ref := range trackedRefs {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(ref.GroupVersionKind())
		err = cli.Get(context.Background(), client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, obj)
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "resource %s/%s should be deleted after the module CR is gone; got %v", ref.Kind, ref.Name, err)
	}
}

func TestTrackerDeletionPersistsWaitingStatus(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	modules.Add(&stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}}, crState: modules.CRStateAlive})
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
	pm.SetConditions([]common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue}})
	cli, err := fakeclient.New(fakeclient.WithObjects(pm), fakeclient.WithStatusSubresources(pm))
	g.Expect(err).NotTo(HaveOccurred())
	err = handlePlatformModuleDeletion(t.Context(), &odhtype.ReconciliationRequest{Client: cli, Instance: pm})
	g.Expect(err).To(HaveOccurred(), "operator cleanup must wait for module CR deletion")
	current := &configApi.PlatformModule{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(pm), current)).To(Succeed())
	ready := conditions.FindStatusCondition(current, status.ConditionTypeReady)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(status.DeletingReason))
	g.Expect(ready.Message).To(ContainSubstring("still exists"))
}

func TestHandlePlatformModuleDeletion_MissingHandlerRetainsFinalizer(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)

	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "missing", UID: "tracker-uid", Finalizers: []string{"test.io/tracker"}},
		Status: configApi.PlatformModuleStatus{
			Release:   testRelease("1.0.0"),
			Resources: []configApi.ResourceRef{{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: "operator-config"}},
		},
	}
	owner := platformModuleOwner(pm.UID)
	owner.Name = pm.Name
	resource := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "operator-config", Namespace: "apps", OwnerReferences: []metav1.OwnerReference{owner}}}
	cli, err := fakeclient.New(fakeclient.WithObjects(pm, resource), fakeclient.WithStatusSubresources(pm))
	g.Expect(err).NotTo(HaveOccurred())
	before := pm.DeepCopy()
	err = handlePlatformModuleDeletion(t.Context(), &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: pm,
	})
	g.Expect(err).To(MatchError(ContainSubstring("cannot establish module CR absence")))
	var stop odherrors.StopError
	g.Expect(errors.As(err, &stop)).To(BeFalse(),
		"the finalizer must retain the shared deletion error instead of stopping reconciliation")
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(resource), resource)).To(Succeed())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	g.Expect(pm.Finalizers).To(Equal(before.Finalizers))
	g.Expect(pm.Status.Release).To(Equal(before.Status.Release))
	g.Expect(pm.Status.Resources).To(Equal(before.Status.Resources))
}

func TestCleanupStalePlatformModuleResourcesSafeInventoryDiff(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)

	const trackerUID = k8stypes.UID("tracker-uid")
	owner := platformModuleOwner(trackerUID)
	externalOwner := platformModuleOwner("replacement-uid")

	ownedStale := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "owned-stale", Namespace: "test", OwnerReferences: []metav1.OwnerReference{owner},
	}}
	externalStale := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "external-stale", Namespace: "test", OwnerReferences: []metav1.OwnerReference{externalOwner},
	}}
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "desired", Namespace: "test", OwnerReferences: []metav1.OwnerReference{owner},
	}}

	s, err := testscheme.New()
	g.Expect(err).ToNot(HaveOccurred())
	pm := &configApi.PlatformModule{
		ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: trackerUID},
		Status: configApi.PlatformModuleStatus{Resources: []configApi.ResourceRef{
			{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "owned-stale"},
			{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "external-stale"},
			// API version is not object identity; version migration must not
			// classify the still-desired resource as stale.
			{Version: "v1beta1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "desired"},
			{Group: "apiextensions.k8s.io", Version: "v1", Kind: gvk.CustomResourceDefinition.Kind, Name: "kept.example.io"},
			{Version: "v1", Kind: gvk.Namespace.Kind, Name: "kept-namespace"},
			{Group: "coordination.k8s.io", Version: "v1", Kind: gvk.Lease.Kind, Namespace: "test", Name: "kept-lease"},
			{Group: "missing.example.io", Version: "v1", Kind: "Missing", Name: "gone"},
		}},
	}
	var desiredGets []string
	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(ownedStale, externalStale, desired, pm),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Namespace == "test" && key.Name == "desired" {
					desiredGets = append(desiredGets, key.String())
				}
				return cli.Get(ctx, key, obj, opts...)
			},
		}))
	g.Expect(err).ToNot(HaveOccurred())

	desiredUnstructured := &unstructured.Unstructured{}
	desiredUnstructured.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(gvk.ConfigMap.Kind))
	desiredUnstructured.SetNamespace("test")
	desiredUnstructured.SetName("desired")

	rr := &odhtype.ReconciliationRequest{Client: cli, Instance: pm, Resources: []unstructured.Unstructured{*desiredUnstructured}}
	var pending odherrors.RequeueAfterError
	err = cleanupStalePlatformModuleResources(context.Background(), rr.Client, pm, resourceRefs(rr.Resources))
	g.Expect(errors.As(err, &pending)).To(BeTrue())
	g.Expect(cleanupStalePlatformModuleResources(context.Background(), rr.Client, pm, resourceRefs(rr.Resources))).To(Succeed())
	// Snapshot before the verification loop performs its own reads.
	desiredGetsDuringCleanup := len(desiredGets)

	for _, name := range []string{"desired", "external-stale"} {
		g.Expect(cli.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: name}, &corev1.ConfigMap{})).To(Succeed())
	}
	g.Expect(k8serr.IsNotFound(cli.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "owned-stale"}, &corev1.ConfigMap{}))).To(BeTrue())
	// Cleanup must not touch desired resources: no Get for test/desired was
	// issued while cleaning up stale inventory.
	g.Expect(desiredGetsDuringCleanup).To(BeZero())
}

func TestDeletePlatformModuleResourcesUsesReverseOrder(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)

	const trackerUID = k8stypes.UID("tracker-uid")
	owner := platformModuleOwner(trackerUID)
	objects := []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "test", OwnerReferences: []metav1.OwnerReference{owner}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "second", Namespace: "test", OwnerReferences: []metav1.OwnerReference{owner}}},
	}
	deleted := make([]string, 0, 2)
	cli, err := fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deleted = append(deleted, obj.GetName())
				return cli.Delete(ctx, obj, opts...)
			},
		}),
	)
	g.Expect(err).ToNot(HaveOccurred())

	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: trackerUID}}
	refs := []configApi.ResourceRef{
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "first"},
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "second"},
	}
	var pending odherrors.RequeueAfterError
	err = deletePlatformModuleResources(context.Background(), cli, pm, refs)
	g.Expect(errors.As(err, &pending)).To(BeTrue())
	g.Expect(err).To(MatchError(ContainSubstring("ConfigMap test/second; ConfigMap test/first")))
	g.Expect(deletePlatformModuleResources(context.Background(), cli, pm, refs)).To(Succeed())
	g.Expect(deleted).To(Equal([]string{"second", "first"}))
}

func TestUnremovablePlatformModuleResources(t *testing.T) {
	g := NewWithT(t)
	for _, kind := range []string{gvk.CustomResourceDefinition.Kind, gvk.Namespace.Kind, gvk.Lease.Kind} {
		g.Expect(isUnremovablePlatformModuleResource(configApi.ResourceRef{Kind: kind})).To(BeTrue(), kind)
	}
	g.Expect(isUnremovablePlatformModuleResource(configApi.ResourceRef{Kind: gvk.Deployment.Kind})).To(BeFalse())
}

func TestResourceRefsAreCanonical(t *testing.T) {
	g := NewWithT(t)

	first := makeUnstructuredResource(gvk.ConfigMap.Kind, "", "config")
	duplicate := makeUnstructuredResource(gvk.ConfigMap.Kind, "", "config")
	deployment := makeUnstructuredResource(gvk.Deployment.Kind, "apps", "operator")

	refs := resourceRefs([]unstructured.Unstructured{deployment, duplicate, first})
	g.Expect(refs).To(HaveLen(2))
	g.Expect(refs[0].Kind).To(Equal(gvk.ConfigMap.Kind))
	g.Expect(refs[1].Kind).To(Equal(gvk.Deployment.Kind))
}

func makeUnstructuredResource(kind, group, name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1", Kind: kind})
	u.SetNamespace("apps")
	u.SetName(name)
	return u
}

func platformModuleOwner(uid k8stypes.UID) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: configApi.GroupVersion.String(),
		Kind:       configApi.PlatformModuleKind,
		Name:       "testmod",
		UID:        uid,
		Controller: &controller,
	}
}

func TestDeletePlatformModuleResourcesSharedClusterObjects(t *testing.T) {
	const trackerUID = k8stypes.UID("tracker-uid")
	for _, tc := range []struct {
		name        string
		owner       *metav1.OwnerReference
		unmanaged   bool
		lookupError bool
		wantDeleted bool
	}{
		{name: "current tracker", owner: new(platformModuleOwner(trackerUID)), wantDeleted: true},
		{name: "standalone install"},
		{name: "unmanaged object", owner: new(platformModuleOwner(trackerUID)), unmanaged: true},
		{name: "recreated tracker", owner: new(platformModuleOwner("previous-tracker"))},
		{name: "Platform controller owner", owner: &metav1.OwnerReference{
			APIVersion: configApi.GroupVersion.String(), Kind: "Platform", Name: configApi.PlatformInstanceName, UID: "platform-uid", Controller: new(true),
		}},
		{name: "ownership lookup fails closed", owner: new(platformModuleOwner(trackerUID)), lookupError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			objects := make([]client.Object, 0, 2)
			refs := make([]configApi.ResourceRef, 0, 2)
			for _, resourceGVK := range []schema.GroupVersionKind{gvk.SecurityContextConstraints, gvk.ClusterRole} {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(resourceGVK)
				obj.SetName("shared-resource")
				if tc.unmanaged {
					obj.SetAnnotations(map[string]string{annotations.ManagedByODHOperator: "false"})
				}
				if tc.owner != nil {
					obj.SetOwnerReferences([]metav1.OwnerReference{*tc.owner})
				}
				objects = append(objects, obj)
				refs = append(refs, configApi.ResourceRef{Group: resourceGVK.Group, Version: resourceGVK.Version, Kind: resourceGVK.Kind, Name: obj.GetName()})
			}
			deleted := 0
			cli, err := fakeclient.New(fakeclient.WithObjects(objects...), fakeclient.WithGVKs(
				fakeclient.GVKMapping{GVK: gvk.SecurityContextConstraints, Scope: meta.RESTScopeRoot},
				fakeclient.GVKMapping{GVK: gvk.ClusterRole, Scope: meta.RESTScopeRoot},
			), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if tc.lookupError {
						return errors.New("ownership lookup failed")
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleted++
					return c.Delete(ctx, obj, opts...)
				},
			}))
			g.Expect(err).NotTo(HaveOccurred())
			pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: trackerUID}}
			err = deletePlatformModuleResources(t.Context(), cli, pm, refs)
			switch {
			case tc.lookupError:
				g.Expect(err).To(HaveOccurred())
			case tc.wantDeleted:
				var pending odherrors.RequeueAfterError
				g.Expect(errors.As(err, &pending)).To(BeTrue())
				g.Expect(deletePlatformModuleResources(t.Context(), cli, pm, refs)).To(Succeed())
			default:
				g.Expect(err).NotTo(HaveOccurred())
			}
			if tc.wantDeleted {
				g.Expect(deleted).To(Equal(2))
			} else {
				g.Expect(deleted).To(BeZero())
			}
			if tc.unmanaged {
				for _, obj := range objects {
					g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)).To(Succeed())
					g.Expect(obj.GetOwnerReferences()).To(BeEmpty(), "unmanaged objects must also survive tracker garbage collection")
				}
			}
		})
	}
}

func TestCleanupProtectedResourcesDetachesTrackerOwner(t *testing.T) {
	g := NewWithT(t)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
	lease := &coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: "operator-lock", Namespace: "apps", OwnerReferences: []metav1.OwnerReference{platformModuleOwner(pm.UID)}}}
	cli, err := fakeclient.New(fakeclient.WithObjects(lease))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deletePlatformModuleResources(t.Context(), cli, pm, []configApi.ResourceRef{{
		Group: "coordination.k8s.io", Version: "v1", Kind: gvk.Lease.Kind, Namespace: lease.Namespace, Name: lease.Name,
	}})).To(Succeed())
	current := &coordinationv1.Lease{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(lease), current)).To(Succeed())
	g.Expect(current.OwnerReferences).To(BeEmpty(), "a retained Lease must survive Kubernetes garbage collection after tracker deletion")
}

func TestCleanupRetainedResourcesDetachesStaleOwnerRegardlessOfController(t *testing.T) {
	g := NewWithT(t)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
	// Controller is a replacement tracker; a stale non-controller reference
	// still points at the current tracker UID.
	replacement := platformModuleOwner("replacement-uid")
	stale := platformModuleOwner(pm.UID)
	stale.Controller = nil
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:            "operator",
		Namespace:       "apps",
		Annotations:     map[string]string{annotations.ManagedByODHOperator: "false"},
		OwnerReferences: []metav1.OwnerReference{replacement, stale},
	}}
	cli, err := fakeclient.New(fakeclient.WithObjects(deployment))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deletePlatformModuleResources(t.Context(), cli, pm, []configApi.ResourceRef{{
		Group: "apps", Version: "v1", Kind: "Deployment", Namespace: deployment.Namespace, Name: deployment.Name,
	}})).To(Succeed())
	current := &appsv1.Deployment{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(deployment), current)).To(Succeed())
	g.Expect(current.OwnerReferences).To(ConsistOf(replacement),
		"only the current tracker's references must be detached from a retained object")
}

// A reference recorded at a no-longer-served API version must still resolve
// to the live object at a served version of the same kind.
// A live resource must not be skipped because an alternate-version read
// failed with a transient or authorization error (CWE-459).
func TestDeletePlatformModuleResourcesPropagatesAlternateReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "forbidden", err: k8serr.NewForbidden(schema.GroupResource{Group: "test.opendatahub.io", Resource: "testresources"}, "recorded", errors.New("denied"))},
		{name: "transient", err: errors.New("connection refused")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
			served := schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1", Kind: "TestResource"}
			unserved := schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1beta1", Kind: "TestResource"}
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(served)
			obj.SetNamespace("apps")
			obj.SetName("recorded")
			obj.SetOwnerReferences([]metav1.OwnerReference{platformModuleOwner(pm.UID)})
			s := runtime.NewScheme()
			// Register the served version first: the fake mapper prefers the
			// first observed version per group, mirroring discovery.
			s.AddKnownTypeWithName(served, &unstructured.Unstructured{})
			s.AddKnownTypeWithName(unserved, &unstructured.Unstructured{})
			cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(obj), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, getObj client.Object, opts ...client.GetOption) error {
					if getObj.GetObjectKind().GroupVersionKind().Version != served.Version {
						// The recorded, no-longer-served version.
						return k8serr.NewNotFound(schema.GroupResource{Group: "test.opendatahub.io", Resource: "testresources"}, key.Name)
					}
					return tc.err
				},
			}))
			g.Expect(err).NotTo(HaveOccurred())

			ref := configApi.ResourceRef{
				Group: unserved.Group, Version: unserved.Version, Kind: unserved.Kind,
				Namespace: obj.GetNamespace(), Name: obj.GetName(),
			}
			err = deletePlatformModuleResources(t.Context(), cli, pm, []configApi.ResourceRef{ref})
			g.Expect(err).To(MatchError(ContainSubstring("before deletion")), "lookup failure must propagate, got: %v", err)
			g.Expect(errors.Is(err, tc.err) || strings.Contains(err.Error(), tc.err.Error())).To(BeTrue(), "expected %v in: %v", tc.err, err)
		})
	}
}

func TestDeletePlatformModuleResourcesPropagatesMapperErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		wantError bool
	}{
		{name: "discovery unavailable", err: errors.New("discovery unavailable"), wantError: true},
		{name: "kind gone", err: &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "test.opendatahub.io", Kind: "TestResource"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
			unserved := schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1beta1", Kind: "TestResource"}
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(unserved, &unstructured.Unstructured{})
			cli := clientFake.NewClientBuilder().
				WithScheme(s).
				WithRESTMapper(failingRESTMapper{err: tc.err}).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
						return k8serr.NewNotFound(schema.GroupResource{Group: "test.opendatahub.io", Resource: "testresources"}, "recorded")
					},
				}).
				Build()
			ref := configApi.ResourceRef{Group: unserved.Group, Version: unserved.Version, Kind: unserved.Kind, Namespace: "apps", Name: "recorded"}
			err := deletePlatformModuleResources(t.Context(), cli, pm, []configApi.ResourceRef{ref})
			if tc.wantError {
				g.Expect(err).To(MatchError(tc.err), "mapper failure must propagate")
			} else {
				g.Expect(err).To(Succeed(), "NoMatch means the kind is gone: treated as absent")
			}
		})
	}
}

type failingRESTMapper struct {
	meta.RESTMapper

	err error
}

func (m failingRESTMapper) RESTMappings(schema.GroupKind, ...string) ([]*meta.RESTMapping, error) {
	return nil, m.err
}

func TestDeletePlatformModuleResourcesResolvesServedVersion(t *testing.T) {
	g := NewWithT(t)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
	served := schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1", Kind: "TestResource"}
	unserved := schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1beta1", Kind: "TestResource"}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(served)
	obj.SetNamespace("apps")
	obj.SetName("recorded")
	obj.SetOwnerReferences([]metav1.OwnerReference{platformModuleOwner(pm.UID)})
	s := runtime.NewScheme()
	// Register the served version first: the fake mapper prefers the first
	// observed version per group, mirroring discovery-based preferred versions.
	s.AddKnownTypeWithName(served, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(unserved, &unstructured.Unstructured{})
	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(obj))
	g.Expect(err).NotTo(HaveOccurred())

	ref := configApi.ResourceRef{
		Group: unserved.Group, Version: unserved.Version, Kind: unserved.Kind,
		Namespace: obj.GetNamespace(), Name: obj.GetName(),
	}
	var pending odherrors.RequeueAfterError
	err = deletePlatformModuleResources(t.Context(), cli, pm, []configApi.ResourceRef{ref})
	g.Expect(errors.As(err, &pending)).To(BeTrue(), "resource must be selected for deletion, got: %v", err)
	g.Expect(deletePlatformModuleResources(t.Context(), cli, pm, []configApi.ResourceRef{ref})).To(Succeed())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)).To(HaveOccurred(),
		"a resource recorded at a no-longer-served version must still be deleted")
}

func TestCleanupStaleResourcesPreservesConcurrentOwnershipChange(t *testing.T) {
	g := NewWithT(t)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
	stale := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "stale", Namespace: "apps", UID: "object-uid", OwnerReferences: []metav1.OwnerReference{platformModuleOwner(pm.UID)},
	}}
	pm.Status.Resources = []configApi.ResourceRef{{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: stale.Namespace, Name: stale.Name}}
	cli, err := fakeclient.New(fakeclient.WithObjects(stale, pm), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			current := &corev1.ConfigMap{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
				return err
			}
			current.SetOwnerReferences([]metav1.OwnerReference{platformModuleOwner("new-owner")})
			if err := c.Update(ctx, current); err != nil {
				return err
			}
			return c.Delete(ctx, obj, opts...)
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	wantInventory := append([]configApi.ResourceRef(nil), pm.Status.Resources...)
	err = cleanupStalePlatformModuleResources(t.Context(), cli, pm, nil)
	g.Expect(k8serr.IsConflict(err)).To(BeTrue(), "concurrent owner change must fail deletion: %v", err)
	g.Expect(pm.Status.Resources).To(Equal(wantInventory))
	current := &corev1.ConfigMap{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(stale), current)).To(Succeed())
	g.Expect(metav1.GetControllerOf(current).UID).To(Equal(k8stypes.UID("new-owner")))
}

// Availability must track the Deployments recorded in the tracker inventory,
// not every Deployment with the module label: only the operator that should
// be installed counts.
func TestOperatorDeploymentConditionChecksInventoriedDeployments(t *testing.T) {
	g := NewWithT(t)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}}
	pm.Status.Resources = []configApi.ResourceRef{{
		Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "apps", Name: "operator",
	}}
	ready := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: "apps"}}
	ready.Status.Replicas = 1
	ready.Status.ReadyReplicas = 1
	// Not in the inventory: an extra Deployment must not affect readiness.
	extra := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "extra", Namespace: "apps"}}
	extra.Status.Replicas = 0
	cli, err := fakeclient.New(fakeclient.WithObjects(ready, extra))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: pm,
		Conditions: conditionstest.NewManager(pm, status.ConditionDeploymentsAvailable),
	}
	updateOperatorDeploymentCondition(t.Context(), rr, pm)
	condition := conditions.FindStatusCondition(pm, status.ConditionDeploymentsAvailable)
	g.Expect(condition).ToNot(BeNil())
	g.Expect(condition.Status).To(Equal(metav1.ConditionTrue),
		"a Deployment outside the inventory must not affect availability")
}

// A missing or unavailable inventoried Deployment must block readiness.
func TestOperatorDeploymentConditionBlocksOnMissingInventoriedDeployment(t *testing.T) {
	g := NewWithT(t)
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}}
	pm.Status.Resources = []configApi.ResourceRef{{
		Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "apps", Name: "operator",
	}}
	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: pm,
		Conditions: conditionstest.NewManager(pm, status.ConditionDeploymentsAvailable),
	}
	updateOperatorDeploymentCondition(t.Context(), rr, pm)
	condition := conditions.FindStatusCondition(pm, status.ConditionDeploymentsAvailable)
	g.Expect(condition).ToNot(BeNil())
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse),
		"a missing inventoried Deployment must block availability")
}

// Lifecycle state must come from the same snapshot as the Ready condition
// (CWE-367): a terminating CR must not publish ModuleCRReady=True.
func TestModuleCRReadinessUsesSingleSnapshot(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	handler := &stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}}, moduleStatus: &modules.ModuleStatus{
		Deleting:   true,
		Generation: 2, ObservedGeneration: 2,
		Conditions: []common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue}},
	}}
	modules.Add(handler)
	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}}
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: pm,
		Conditions: conditionstest.NewManager(pm, status.ConditionTypeProvisioningSucceeded, status.ConditionTypeModuleCRReady),
	}
	updateModuleCRCondition(t.Context(), rr, handler)
	ready := conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(status.DeletingReason))
}

func TestModuleCRReadinessRejectsStaleGeneration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		observed int64
		want     metav1.ConditionStatus
	}{
		{name: "stale", observed: 1, want: metav1.ConditionFalse},
		{name: "current", observed: 2, want: metav1.ConditionTrue},
		// BaseHandler reads a missing top-level status.observedGeneration as zero;
		// operators that omit it still report readiness through their conditions.
		{name: "without observedGeneration", observed: 0, want: metav1.ConditionTrue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			withCleanRegistries(t)
			handler := &stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}}, moduleStatus: &modules.ModuleStatus{
				Generation: 2, ObservedGeneration: tc.observed, Conditions: []common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue}},
			}}
			modules.Add(handler)
			cli, err := fakeclient.New()
			g.Expect(err).NotTo(HaveOccurred())
			pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}}
			rr := &odhtype.ReconciliationRequest{
				Client: cli, Instance: pm,
				Conditions: conditionstest.NewManager(pm, status.ConditionTypeProvisioningSucceeded, status.ConditionTypeModuleCRReady),
			}
			updateModuleCRCondition(t.Context(), rr, handler)
			ready := conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(tc.want))
			if tc.observed == 1 {
				g.Expect(ready.Message).To(Equal("Module status is stale (observedGeneration < generation)"))
			}
		})
	}
}

func TestModuleCRReadinessRejectsDegradedOrDeletingModules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  modules.CRState
		reason string
	}{
		{name: "degraded", state: modules.CRStateAlive, reason: "RuntimeFailure"},
		{name: "deleting", state: modules.CRStateDeleting, reason: status.DeletingReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			pm, desired, stale := completionFixture(t)
			handler, ok := modules.DefaultRegistry().Lookup("testmod").(*stubModuleHandler)
			g.Expect(ok).To(BeTrue())
			if tc.name == "deleting" {
				handler.moduleStatus.Deleting = true
			}
			if tc.name == "degraded" {
				handler.moduleStatus.Conditions = append(handler.moduleStatus.Conditions,
					common.Condition{Type: status.ConditionTypeDegraded, Status: metav1.ConditionTrue, Reason: tc.reason, Message: "runtime failed"})
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(stale, pm))
			g.Expect(err).NotTo(HaveOccurred())
			var pending odherrors.StopError
			g.Expect(errors.As(updatePlatformModuleStatus(t.Context(), completionRequest(pm, cli, desired)), &pending)).To(BeTrue())
			g.Expect(updatePlatformModuleStatus(t.Context(), completionRequest(pm, cli, desired))).To(Succeed())
			ready := conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Reason).To(Equal(tc.reason))
			g.Expect(pm.Status.Release.Version.String()).To(Equal("2.0.0"))
		})
	}
}

func completionFixture(t *testing.T) (*configApi.PlatformModule, unstructured.Unstructured, *corev1.ConfigMap) {
	t.Helper()
	withCleanRegistries(t)
	modules.Add(&stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}}, moduleStatus: &modules.ModuleStatus{
		Conditions: []common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue}},
	}})
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}, Status: configApi.PlatformModuleStatus{
		Release: testRelease("1.0.0"), Resources: []configApi.ResourceRef{{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: "stale"}},
	}}
	deployment := makeUnstructuredResource(gvk.Deployment.Kind, "apps", "operator")
	deployment.SetOwnerReferences([]metav1.OwnerReference{platformModuleOwner(pm.UID)})
	stale := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "stale", Namespace: "apps", OwnerReferences: []metav1.OwnerReference{platformModuleOwner(pm.UID)}}}
	return pm, deployment, stale
}

func completionRequest(pm *configApi.PlatformModule, cli client.Client, desired unstructured.Unstructured) *odhtype.ReconciliationRequest {
	return &odhtype.ReconciliationRequest{Instance: pm, Client: cli, Release: testRelease("2.0.0"), Resources: []unstructured.Unstructured{desired},
		Conditions: conditionstest.NewManager(pm, status.ConditionTypeReady, status.ConditionTypeProvisioningSucceeded,
			status.ConditionTypeModuleCRReady, status.ConditionDeploymentsAvailable),
	}
}

func TestCompletionChecksDeletionOnNextReconcile(t *testing.T) {
	g := NewWithT(t)
	pm, desired, stale := completionFixture(t)
	reads := 0
	cli, err := fakeclient.New(fakeclient.WithObjects(stale, pm), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key == client.ObjectKeyFromObject(stale) {
				reads++
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	rr := completionRequest(pm, cli, desired)
	before := pm.DeepCopy()
	err = updatePlatformModuleStatus(t.Context(), rr)
	g.Expect(reads).To(Equal(1), "deletion is checked on the next reconciliation")
	var pending odherrors.StopError
	g.Expect(errors.As(err, &pending)).To(BeTrue())
	g.Expect(pending.RequeueAfter()).To(Equal(30 * time.Second))
	var delayed odherrors.RequeueAfterError
	g.Expect(errors.As(err, &delayed)).To(BeFalse(), "the framework must see a stop, not a continue-and-requeue marker")
	g.Expect(pm.Status.Release).To(Equal(before.Status.Release))
	g.Expect(pm.Status.Resources).To(Equal(before.Status.Resources))
	g.Expect(updatePlatformModuleStatus(t.Context(), rr)).To(Succeed())
	g.Expect(reads).To(Equal(2))
	g.Expect(pm.Status.Release.Version.String()).To(Equal("2.0.0"))
	g.Expect(pm.Status.Resources).To(Equal([]configApi.ResourceRef{{Group: "apps", Version: "v1", Kind: gvk.Deployment.Kind, Namespace: "apps", Name: "operator"}}))
}

func TestCompletionSurvivesOperatorStatusReadError(t *testing.T) {
	g := NewWithT(t)
	pm, desired, stale := completionFixture(t)
	handler, ok := modules.DefaultRegistry().Lookup("testmod").(*stubModuleHandler)
	g.Expect(ok).To(BeTrue())
	handler.moduleStatus.Conditions[0].Status = metav1.ConditionFalse
	cli, err := fakeclient.New(fakeclient.WithObjects(stale, pm), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, isDeployment := obj.(*appsv1.Deployment); isDeployment {
				return errors.New("deployment status temporarily unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	var pending odherrors.StopError
	g.Expect(errors.As(updatePlatformModuleStatus(t.Context(), completionRequest(pm, cli, desired)), &pending)).To(BeTrue())
	g.Expect(updatePlatformModuleStatus(t.Context(), completionRequest(pm, cli, desired))).To(Succeed())
	g.Expect(pm.Status.Release.Version.String()).To(Equal("2.0.0"))
	g.Expect(pm.Status.Resources).To(Equal(resourceRefs([]unstructured.Unstructured{desired})))
	g.Expect(conditions.FindStatusCondition(pm, status.ConditionDeploymentsAvailable)).To(HaveValue(And(
		HaveField("Status", metav1.ConditionUnknown),
		HaveField("Message", ContainSubstring("deployment status temporarily unavailable")),
	)))
	g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady).Status).To(Equal(metav1.ConditionFalse))
	g.Expect(k8serr.IsNotFound(cli.Get(t.Context(), client.ObjectKeyFromObject(stale), stale))).To(BeTrue())
}

func TestTrackerCleanupReturnsAPIErrorsBeforePendingAndCanRetry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		action      func(context.Context, *odhtype.ReconciliationRequest) error
		wantRelease string
	}{
		{name: "completion", action: updatePlatformModuleStatus, wantRelease: "2.0.0"},
		{name: "finalization", action: handlePlatformModuleDeletion, wantRelease: "1.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			pm, desired, stale := completionFixture(t)
			handler, ok := modules.DefaultRegistry().Lookup("testmod").(*stubModuleHandler)
			g.Expect(ok).To(BeTrue())
			handler.crState = modules.CRStateAbsent
			stale.Finalizers = []string{"test.io/hold"}
			failed := stale.DeepCopy()
			failed.Name = "failed"
			failed.Finalizers = nil
			pm.Status.Resources = append(pm.Status.Resources, configApi.ResourceRef{
				Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: failed.Name,
			})
			wantErr := errors.New("delete denied")
			fail := true
			cli, err := fakeclient.New(fakeclient.WithObjects(pm, stale, failed), fakeclient.WithStatusSubresources(pm),
				fakeclient.WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						if fail && obj.GetName() == failed.Name {
							return wantErr
						}
						return c.Delete(ctx, obj, opts...)
					},
				}))
			g.Expect(err).NotTo(HaveOccurred())
			before := pm.DeepCopy()
			rr := completionRequest(pm, cli, desired)
			err = tc.action(t.Context(), rr)
			g.Expect(errors.Is(err, wantErr)).To(BeTrue(), "%v", err)
			var pending odherrors.RequeueAfterError
			g.Expect(errors.As(err, &pending)).To(BeFalse(), "API failures must not become successful delayed requeues")
			g.Expect(pm.Status.Release).To(Equal(before.Status.Release))
			g.Expect(pm.Status.Resources).To(Equal(before.Status.Resources))

			var stopped odherrors.StopError
			g.Expect(errors.As(err, &stopped)).To(BeFalse(), "API failures must not become scheduled stops")
			fail = false
			err = tc.action(t.Context(), rr)
			g.Expect(errors.As(err, &stopped)).To(Equal(tc.name == "completion"), "only the apply action may return StopError")
			g.Expect(errors.As(err, &pending)).To(Equal(tc.name == "finalization"), "finalization must keep its deletion barrier")
			g.Expect(err).To(MatchError(ContainSubstring("ConfigMap apps/stale (finalizers: test.io/hold)")))
			delay := pending.After
			if tc.name == "completion" {
				delay = stopped.RequeueAfter()
			}
			g.Expect(delay).To(Equal(30 * time.Second))
			g.Expect(pm.Status.Release).To(Equal(before.Status.Release))
			g.Expect(pm.Status.Resources).To(Equal(before.Status.Resources))
			g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(stale), stale)).To(Succeed())
			stale.Finalizers = nil
			g.Expect(cli.Update(t.Context(), stale)).To(Succeed())
			g.Expect(tc.action(t.Context(), rr)).To(Succeed())
			g.Expect(pm.Status.Release.Version.String()).To(Equal(tc.wantRelease))
		})
	}
}

func TestCompletionDoesNotDependOnXKSModuleCRReadiness(t *testing.T) {
	g := NewWithT(t)
	pm, desired, stale := completionFixture(t)
	modules.DefaultRegistry().Reset()
	modules.Add(&stubModuleHandler{
		BaseHandler:     modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}},
		moduleStatusErr: k8serr.NewNotFound(schema.GroupResource{Group: "test.io", Resource: "modules"}, "missing"),
	})
	cli, err := fakeclient.New(fakeclient.WithObjects(&desired, stale, pm))
	g.Expect(err).NotTo(HaveOccurred())
	rr := completionRequest(pm, cli, desired)
	rr.Release.Name = cluster.XKS
	var pending odherrors.StopError
	g.Expect(errors.As(updatePlatformModuleStatus(t.Context(), rr), &pending)).To(BeTrue())
	g.Expect(updatePlatformModuleStatus(t.Context(), rr)).To(Succeed())
	g.Expect(pm.Status.Release.Version.String()).To(Equal("2.0.0"))
	g.Expect(pm.Status.Release.Name).To(Equal(cluster.XKS))
	g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady).Status).NotTo(Equal(metav1.ConditionTrue))
}

func TestCompletionPreservesCommitOnLaterRuntimeFailure(t *testing.T) {
	g := NewWithT(t)
	pm, desired, stale := completionFixture(t)
	cli, err := fakeclient.New(fakeclient.WithObjects(&desired, stale, pm))
	g.Expect(err).NotTo(HaveOccurred())
	rr := completionRequest(pm, cli, desired)
	var pending odherrors.StopError
	g.Expect(errors.As(updatePlatformModuleStatus(t.Context(), rr), &pending)).To(BeTrue())
	g.Expect(updatePlatformModuleStatus(t.Context(), rr)).To(Succeed())
	committed := pm.DeepCopy()
	handler, ok := modules.DefaultRegistry().Lookup("testmod").(*stubModuleHandler)
	g.Expect(ok).To(BeTrue())
	handler.moduleStatus.Conditions[0].Status = metav1.ConditionFalse
	g.Expect(updatePlatformModuleStatus(t.Context(), rr)).To(Succeed())
	g.Expect(pm.Status.Release).To(Equal(committed.Status.Release))
	g.Expect(pm.Status.Resources).To(Equal(committed.Status.Resources))
	g.Expect(conditions.FindStatusCondition(pm, status.ConditionTypeModuleCRReady).Status).To(Equal(metav1.ConditionFalse))
}

func TestRecordIntendedInventory(t *testing.T) {
	g := NewWithT(t)
	newResource := makeUnstructuredResource(gvk.ConfigMap.Kind, "", "new")

	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: "tracker-uid"}}
	// The instance starts each reconcile carrying the last persisted
	// inventory; the pre-deploy write must merge into it, not replace it.
	pm.Status.Resources = []configApi.ResourceRef{
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: "old"},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(pm), fakeclient.WithStatusSubresources(pm))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	rr := &odhtype.ReconciliationRequest{Client: cli, Instance: pm, Resources: []unstructured.Unstructured{newResource}}
	g.Expect(recordIntendedInventory(t.Context(), rr)).To(Succeed())
	persisted := &configApi.PlatformModule{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(pm), persisted)).To(Succeed())
	g.Expect(persisted.Status.Resources).To(HaveLen(2), "the cleanup inventory must already be durable before deploy")
	g.Expect(pm.Status.Resources).To(ConsistOf(
		configApi.ResourceRef{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: "old"},
		configApi.ResourceRef{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: "new"},
	), "previously recorded entries must survive so a failed deploy cannot orphan them")

	// Re-recording the same desired set is idempotent.
	g.Expect(recordIntendedInventory(t.Context(), rr)).To(Succeed())
	g.Expect(pm.Status.Resources).To(HaveLen(2))

	// A deferred deployment must keep the last recorded inventory: resources
	// may not be rendered while the runlevel gate holds the module back.
	rr.SkipDeploy = true
	g.Expect(recordIntendedInventory(t.Context(), rr)).To(Succeed())
	g.Expect(pm.Status.Resources).To(HaveLen(2))
}

func TestPendingCleanupKeepsCheckpointedInventory(t *testing.T) {
	g := NewWithT(t)
	pm, desired, stale := completionFixture(t)
	stale.Finalizers = []string{"test.io/hold"}
	cli, err := fakeclient.New(fakeclient.WithObjects(pm, stale, &desired), fakeclient.WithStatusSubresources(pm))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(pm), pm)).To(Succeed())
	rr := completionRequest(pm, cli, desired)
	g.Expect(recordIntendedInventory(t.Context(), rr)).To(Succeed())

	var pending odherrors.StopError
	g.Expect(errors.As(updatePlatformModuleStatus(t.Context(), rr), &pending)).To(BeTrue())
	g.Expect(pm.Status.Resources).To(ConsistOf(
		configApi.ResourceRef{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "apps", Name: "stale"},
		configApi.ResourceRef{Group: "apps", Version: "v1", Kind: gvk.Deployment.Kind, Namespace: "apps", Name: "operator"},
	))
	g.Expect(pm.Status.Release.Version.String()).To(Equal("1.0.0"))
}

func TestCleanupCollectsCommittedAndInterruptedInventories(t *testing.T) {
	// A deploy failed after the pre-deploy merge was persisted: the recorded
	// inventory holds both the previous commit ("old", no longer desired)
	// and the interrupted attempt's intended set ("leftover", applied but
	// dropped from the next render). The next successful deploy must clean
	// both — neither may be orphaned by the inventory rewrite.
	g := NewWithT(t)
	withCleanRegistries(t)

	const trackerUID = k8stypes.UID("tracker-uid")
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod", UID: trackerUID}}
	pm.Status.Resources = []configApi.ResourceRef{
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "old"},
		{Version: "v1", Kind: gvk.ConfigMap.Kind, Namespace: "test", Name: "leftover"},
	}
	oldObject := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "old", Namespace: "test", OwnerReferences: []metav1.OwnerReference{platformModuleOwner(trackerUID)},
	}}
	leftoverObject := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "leftover", Namespace: "test", OwnerReferences: []metav1.OwnerReference{platformModuleOwner(trackerUID)},
	}}

	s, err := testscheme.New()
	g.Expect(err).ToNot(HaveOccurred())
	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(oldObject, leftoverObject))
	g.Expect(err).ToNot(HaveOccurred())

	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(gvk.ConfigMap.Kind))
	desired.SetNamespace("test")
	desired.SetName("new")

	desiredRefs := resourceRefs([]unstructured.Unstructured{*desired})
	var pending odherrors.RequeueAfterError
	err = cleanupStalePlatformModuleResources(t.Context(), cli, pm, desiredRefs)
	g.Expect(errors.As(err, &pending)).To(BeTrue(), "foreground deletion must be awaited")
	err = cleanupStalePlatformModuleResources(t.Context(), cli, pm, desiredRefs)
	g.Expect(err).To(Succeed())
	for _, name := range []string{"old", "leftover"} {
		g.Expect(k8serr.IsNotFound(cli.Get(t.Context(), client.ObjectKey{Namespace: "test", Name: name}, &corev1.ConfigMap{}))).To(BeTrue(),
			"%s must be deleted by the post-failure cleanup", name)
	}
}
