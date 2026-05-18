package reconciler_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

//nolint:gochecknoinits
func init() {
	log.SetLogger(zap.New(zap.WriteTo(io.Discard)))
}

// startManager starts the manager in the background and waits for the cache to sync.
func startManager(t *testing.T, g *WithT, mgr ctrl.Manager) {
	t.Helper()

	ctx := t.Context()
	mgrCtx, mgrCancel := context.WithCancel(ctx)

	go func() {
		_ = mgr.Start(mgrCtx)
	}()
	t.Cleanup(mgrCancel)

	g.Eventually(func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}).Should(BeTrue())
}

func TestPreConditions_StopReconciliation(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	dash := resources.GvkToUnstructured(gvk.Dashboard)
	dash.SetName(componentApi.DashboardInstanceName)
	dash.SetGeneration(1)

	err = cli.Create(ctx, dash)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() {
		_ = cli.Delete(ctx, dash, client.PropagationPolicy(metav1.DeletePropagationBackground))
	})

	actionExecuted := false

	cc, err := reconciler.NewReconciler(mgr, "test", &componentApi.Dashboard{},
		reconciler.WithPreConditions([]precondition.PreCondition{
			precondition.MonitorCRD(
				schema.GroupVersionKind{Group: "fake.opendatahub.io", Version: "v1", Kind: "FakeResource"},
				precondition.WithStopReconciliation(),
			),
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())

	cc.AddAction(func(_ context.Context, _ *odhtype.ReconciliationRequest) error {
		actionExecuted = true
		return nil
	})

	startManager(t, g, mgr)

	result, err := cc.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: componentApi.DashboardInstanceName},
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result.RequeueAfter).Should(BeZero())
	g.Expect(actionExecuted).To(BeFalse())

	di := resources.GvkToUnstructured(gvk.Dashboard)
	di.SetName(componentApi.DashboardInstanceName)

	err = cli.Get(ctx, client.ObjectKeyFromObject(di), di)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(di).Should(And(
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, status.ConditionTypeProvisioningSucceeded, "PreConditionFailed"),
	))
}

func TestNewReconciler_WithDynamicOwnership(t *testing.T) {
	g := NewWithT(t)

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()

	t.Run("dynamic ownership disabled by default", func(t *testing.T) {
		r, err := reconciler.NewReconciler(mgr, "test", &componentApi.Dashboard{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(r.IsDynamicOwnershipEnabled()).To(BeFalse())
	})

	t.Run("dynamic ownership enabled with option", func(t *testing.T) {
		r, err := reconciler.NewReconciler(mgr, "test", &componentApi.Dashboard{},
			reconciler.WithDynamicOwnership(),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(r.IsDynamicOwnershipEnabled()).To(BeTrue())
	})

	t.Run("dynamic ownership with excluded GVKs", func(t *testing.T) {
		r, err := reconciler.NewReconciler(mgr, "test", &componentApi.Dashboard{},
			reconciler.WithDynamicOwnership(reconciler.ExcludeGVKs(gvk.ConfigMap, gvk.Secret)),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(r.IsDynamicOwnershipEnabled()).To(BeTrue())
		g.Expect(r.IsExcludedFromDynamicOwnership(gvk.ConfigMap)).To(BeTrue())
		g.Expect(r.IsExcludedFromDynamicOwnership(gvk.Secret)).To(BeTrue())
		g.Expect(r.IsExcludedFromDynamicOwnership(gvk.Deployment)).To(BeFalse())
	})

	t.Run("Owns returns true after AddDynamicOwnedType", func(t *testing.T) {
		g := NewWithT(t)
		r, err := reconciler.NewReconciler(mgr, "test", &componentApi.Dashboard{},
			reconciler.WithDynamicOwnership(),
		)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(r.Owns(gvk.ConfigMap)).To(BeFalse())
		r.AddDynamicOwnedType(gvk.ConfigMap)
		g.Expect(r.Owns(gvk.ConfigMap)).To(BeTrue())
		g.Expect(r.Owns(gvk.Secret)).To(BeFalse())
	})

	t.Run("Owns returns true for both static and dynamic ownership", func(t *testing.T) {
		g := NewWithT(t)
		r, err := reconciler.NewReconciler(mgr, "test", &componentApi.Dashboard{},
			reconciler.WithDynamicOwnership(),
		)
		g.Expect(err).NotTo(HaveOccurred())

		r.AddOwnedType(gvk.ConfigMap)
		r.AddDynamicOwnedType(gvk.Secret)

		g.Expect(r.Owns(gvk.ConfigMap)).To(BeTrue(), "Static ownership should be recognized")
		g.Expect(r.Owns(gvk.Secret)).To(BeTrue(), "Dynamic ownership should be recognized")
		g.Expect(r.Owns(gvk.Deployment)).To(BeFalse(), "Unregistered GVK should not be owned")
	})
}

func TestBuild_SetsControllerField(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName("build-controller-test").
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rec.Controller).NotTo(BeNil(),
		"Build() must assign the built controller to Reconciler.Controller")
}

func TestBuild_NamedAllowsMultipleControllersForSameGVK(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	et, err := envt.New(envt.WithManager(ctrl.Options{}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()

	rec1, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName("controller-alpha").
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rec1.Controller).NotTo(BeNil())

	rec2, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName("controller-beta").
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred(),
		"Build() with Named() must allow two controllers for the same GVK with different names")
	g.Expect(rec2.Controller).NotTo(BeNil())
}

func TestDynamicOwnership_DeployAction(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	cmName := xid.New().String()
	secretName := xid.New().String()
	notManagedCMName := xid.New().String()

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithDynamicOwnership(reconciler.ExcludeGVKs(gvk.Secret)).
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}

			secret := &corev1.Secret{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: nsName},
				StringData: map[string]string{"key": "secret-value"},
			}

			notManagedCM := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      notManagedCMName,
					Namespace: nsName,
					Annotations: map[string]string{
						annotations.ManagedByODHOperator: "false",
					},
				},
				Data: map[string]string{"managed": "false"},
			}

			return rr.AddResources(cm, secret, notManagedCM)
		}).
		WithAction(deploy.NewAction()).
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rec.IsDynamicOwnershipEnabled()).To(BeTrue())

	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	deployedConfigMap := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedConfigMap)).To(Succeed())

	ownerRefs := deployedConfigMap.GetOwnerReferences()
	g.Expect(ownerRefs).To(HaveLen(1), "Resource should have exactly one owner reference")
	g.Expect(ownerRefs[0]).To(Equal(metav1.OwnerReference{
		APIVersion:         gvk.Dashboard.GroupVersion().String(),
		Kind:               gvk.Dashboard.Kind,
		Name:               componentApi.DashboardInstanceName,
		UID:                dashboard.GetUID(),
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}))

	deployedSecret := &corev1.Secret{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, deployedSecret)).To(Succeed())
	g.Expect(deployedSecret.GetOwnerReferences()).To(BeEmpty(), "Excluded resource should not have owner references")

	deployedNotManagedCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, deployedNotManagedCM)).To(Succeed())
	g.Expect(deployedNotManagedCM.GetOwnerReferences()).To(BeEmpty())

	t.Run("owned resource is restored after external modification", func(t *testing.T) {
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedConfigMap)).To(Succeed())
		original := deployedConfigMap.DeepCopy()

		deployedConfigMap.Data["key"] = "modified-externally"
		err := cli.Patch(ctx, deployedConfigMap, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			configMap := &corev1.ConfigMap{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, configMap)).To(Succeed())
			gg.Expect(configMap.Data).To(Equal(map[string]string{"key": "value"}))
			gg.Expect(configMap.GetResourceVersion()).NotTo(Equal(original.GetResourceVersion()))
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	t.Run("excluded resource are not restored after external modification", func(t *testing.T) {
		var stableResourceVersion string
		g.Eventually(func(gg Gomega) {
			secret := &corev1.Secret{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, secret)).To(Succeed())
			stableResourceVersion = secret.GetResourceVersion()
		}).WithTimeout(5 * time.Second).Should(Succeed())

		g.Consistently(func(gg Gomega) {
			secret := &corev1.Secret{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, secret)).To(Succeed())
			gg.Expect(secret.GetResourceVersion()).To(Equal(stableResourceVersion))
		}).WithTimeout(2*time.Second).Should(Succeed(), "Secret should be stable before modification")

		g.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, deployedSecret)).To(Succeed())
		original := deployedSecret.DeepCopy()

		deployedSecret.StringData = map[string]string{"key": "modified-externally"}
		err := cli.Patch(ctx, deployedSecret, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			secret := &corev1.Secret{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, secret)).To(Succeed())
			gg.Expect(string(secret.Data["key"])).To(Equal("modified-externally"))
		}).WithTimeout(5*time.Second).Should(Succeed(), "Modification should be visible")

		g.Consistently(func(gg Gomega) {
			secret := &corev1.Secret{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, secret)).To(Succeed())
			gg.Expect(string(secret.Data["key"])).To(Equal("modified-externally"))
		}).WithTimeout(3 * time.Second).Should(Succeed())
	})

	t.Run("not managed resource are not restored after external modification", func(t *testing.T) {
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, deployedNotManagedCM)).To(Succeed())
		original := deployedNotManagedCM.DeepCopy()

		deployedNotManagedCM.Data = map[string]string{"key": "modified-externally"}
		err := cli.Patch(ctx, deployedNotManagedCM, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			managedCM := &corev1.ConfigMap{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, managedCM)).To(Succeed())
			gg.Expect(managedCM.Data).To(Equal(map[string]string{"key": "modified-externally"}))
			gg.Expect(managedCM.GetResourceVersion()).NotTo(Equal(original.GetResourceVersion()))
		}).WithTimeout(5 * time.Second).Should(Succeed())

		g.Consistently(func(gg Gomega) {
			managedCM := &corev1.ConfigMap{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, managedCM)).To(Succeed())
			gg.Expect(managedCM.Data).To(Equal(map[string]string{"key": "modified-externally"}))
			gg.Expect(managedCM.GetResourceVersion()).NotTo(Equal(original.GetResourceVersion()))
		}).WithTimeout(3 * time.Second).Should(Succeed())
	})

	t.Run("not managed resource are restored if deleted", func(t *testing.T) {
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, deployedNotManagedCM)).To(Succeed())

		err := cli.Delete(ctx, deployedNotManagedCM)
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			managedCM := &corev1.ConfigMap{}
			err := cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, managedCM)
			gg.Expect(err).To(Not(HaveOccurred()))
			gg.Expect(managedCM.Data).To(Equal(map[string]string{"managed": "false"}))
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
}

func TestDynamicOwnership_DisabledByDefault(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()
	configMapName := xid.New().String()

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}

			return rr.AddResources(cm)
		}).
		WithAction(deploy.NewAction()).
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rec.IsDynamicOwnershipEnabled()).To(BeFalse(), "Dynamic ownership should be disabled by default")

	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	deployed := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: nsName}, deployed)).To(Succeed())
	g.Expect(deployed.GetOwnerReferences()).To(BeEmpty(), "Resource should not have owner references when dynamic ownership is disabled")
}

func TestDynamicOwnership_DeployAction_CRDAndCR(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	crdKind := "TestWidget"
	crd := createCRD(t, g, crdKind)

	crName := xid.New().String()

	crdKindWithoutCR := "WithoutCreatedCR"
	crdWithoutCreatedCR := createCRD(t, g, crdKindWithoutCR)

	cmName := xid.New().String()

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithDynamicOwnership().
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}
			cr := &unstructured.Unstructured{}
			cr.SetAPIVersion("test.opendatahub.io/v1")
			cr.SetKind(crdKind)
			cr.SetName(crName)
			cr.SetNamespace(nsName)

			return rr.AddResources(crd.DeepCopy(), cr, crdWithoutCreatedCR.DeepCopy(), cm)
		}).
		WithAction(deploy.NewAction()).
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rec.IsDynamicOwnershipEnabled()).To(BeTrue())

	startManager(t, g, mgr)

	_, err = rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	if err != nil {
		var noKindMatchErr *meta.NoKindMatchError
		g.Expect(errors.As(err, &noKindMatchErr)).To(BeTrue(),
			"unexpected reconcile error: %v", err)
	}

	deployedCRD := &apiextensionsv1.CustomResourceDefinition{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: crd.GetName()}, deployedCRD)).To(Succeed())
	g.Expect(deployedCRD.GetOwnerReferences()).To(BeEmpty(), "CRD should not have owner references")

	deployedCR := &unstructured.Unstructured{}
	deployedCR.SetAPIVersion("test.opendatahub.io/v1")
	deployedCR.SetKind("TestWidget")
	g.Eventually(func(gg Gomega) {
		gg.Expect(cli.Get(ctx, client.ObjectKey{Name: crName, Namespace: nsName}, deployedCR)).To(Succeed())
		gg.Expect(deployedCR.GetOwnerReferences()).To(HaveLen(1), "CR should have owner reference")
		gg.Expect(deployedCR.GetOwnerReferences()[0].Kind).To(Equal("Dashboard"))
	}).WithTimeout(30*time.Second).WithPolling(1*time.Second).Should(Succeed(), "CR should be deployed with owner reference")

	deployedCM := &corev1.ConfigMap{}
	g.Eventually(func() error {
		return cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)
	}).WithTimeout(5*time.Second).Should(Succeed(), "ConfigMap should be deployed")
	g.Expect(deployedCM.GetOwnerReferences()).To(HaveLen(1), "ConfigMap should have owner reference")

	t.Run("CR is restored after external deletion", func(t *testing.T) {
		err := cli.Delete(ctx, deployedCR)
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func() bool {
			err := cli.Get(ctx, client.ObjectKey{Name: crName, Namespace: nsName}, deployedCR)
			return err != nil && k8serr.IsNotFound(err)
		}).WithTimeout(5*time.Second).Should(BeTrue(), "CR should be deleted")

		g.Eventually(func(gg Gomega) {
			restored := &unstructured.Unstructured{}
			restored.SetAPIVersion("test.opendatahub.io/v1")
			restored.SetKind("TestWidget")
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: crName, Namespace: nsName}, restored)).To(Succeed())
			gg.Expect(restored.GetOwnerReferences()).To(HaveLen(1))
		}).WithTimeout(10*time.Second).Should(Succeed(), "CR should be restored after deletion")
	})

	t.Run("CRD deletion triggers reconciliation", func(t *testing.T) {
		currentCRD := &apiextensionsv1.CustomResourceDefinition{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: crdWithoutCreatedCR.GetName()}, currentCRD)).To(Succeed())
		oldUID := currentCRD.GetUID()

		err := cli.Delete(ctx, currentCRD)
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			restored := &apiextensionsv1.CustomResourceDefinition{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: crdWithoutCreatedCR.GetName()}, restored)).To(Succeed())
			gg.Expect(restored.GetUID()).NotTo(Equal(oldUID), "CRD should have been recreated with new uid")
		}).WithTimeout(10*time.Second).Should(Succeed(), "CRD should be restored after deletion")
	})
}

func TestDynamicOwnership_DeployAction_WithGVKPredicates(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	deploymentName := xid.New().String()
	cmName := xid.New().String()

	deploymentPredicate := predicate.Funcs{
		CreateFunc: func(_ event.TypedCreateEvent[client.Object]) bool { return false },
		UpdateFunc: func(_ event.TypedUpdateEvent[client.Object]) bool { return false },
		DeleteFunc: func(_ event.TypedDeleteEvent[client.Object]) bool { return true },
	}

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithDynamicOwnership(
			reconciler.WithGVKPredicates(map[schema.GroupVersionKind][]predicate.Predicate{
				gvk.Deployment: {deploymentPredicate},
			}),
		).
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			deployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: nsName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Replicas: ptr.To(int32(1)),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "test",
								Image: "busybox",
							}},
						},
					},
				},
			}

			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}

			return rr.AddResources(deployment, cm)
		}).
		WithAction(deploy.NewAction()).
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rec.IsDynamicOwnershipEnabled()).To(BeTrue())

	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	deployedDeployment := &appsv1.Deployment{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployedDeployment)).To(Succeed())
	g.Expect(deployedDeployment.GetOwnerReferences()).To(HaveLen(1))
	g.Expect(deployedDeployment.GetOwnerReferences()[0].Kind).To(Equal(gvk.Dashboard.Kind))

	deployedCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)).To(Succeed())
	g.Expect(deployedCM.GetOwnerReferences()).To(HaveLen(1))

	t.Run("deployment is restored after external deletion", func(t *testing.T) {
		err := cli.Delete(ctx, deployedDeployment)
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func() bool {
			err := cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployedDeployment)
			return err != nil && k8serr.IsNotFound(err)
		}).WithTimeout(5*time.Second).Should(BeTrue(), "Deployment should be deleted")

		g.Eventually(func(gg Gomega) {
			restored := &appsv1.Deployment{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, restored)).To(Succeed())
			gg.Expect(restored.GetOwnerReferences()).To(HaveLen(1))
		}).WithTimeout(10*time.Second).Should(Succeed(), "Deployment should be restored after deletion")
	})

	t.Run("deployment spec fields are not restored after external modification", func(t *testing.T) {
		g.Consistently(func(gg Gomega) {
			deployment := &appsv1.Deployment{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployment)).To(Succeed())
			gg.Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		}).WithTimeout(3*time.Second).WithPolling(200*time.Millisecond).Should(Succeed(), "Deployment should be stable before modification")

		g.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployedDeployment)).To(Succeed())
		original := deployedDeployment.DeepCopy()

		deployedDeployment.Spec.Replicas = ptr.To(int32(2))
		err := cli.Patch(ctx, deployedDeployment, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			deployment := &appsv1.Deployment{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployment)).To(Succeed())
			gg.Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
		}).WithTimeout(5*time.Second).Should(Succeed(), "Modification should be visible")

		g.Consistently(func(gg Gomega) {
			deployment := &appsv1.Deployment{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployment)).To(Succeed())
			gg.Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
		}).WithTimeout(3*time.Second).Should(Succeed(), "Replicas should not be restored since it's not in the manifest")
	})

	t.Run("configmap is restored after external modification", func(t *testing.T) {
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)).To(Succeed())
		original := deployedCM.DeepCopy()

		deployedCM.Data["key"] = "modified-externally"
		err := cli.Patch(ctx, deployedCM, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		g.Eventually(func(gg Gomega) {
			restored := &corev1.ConfigMap{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, restored)).To(Succeed())
			gg.Expect(restored.Data).To(Equal(map[string]string{"key": "value"}))
		}).WithTimeout(5*time.Second).Should(Succeed(), "ConfigMap should be restored to original data")
	})
}

func createCRD(t *testing.T, g Gomega, kind string) *unstructured.Unstructured {
	t.Helper()

	lowerKind := strings.ToLower(kind)
	plural := fmt.Sprintf("%ss", lowerKind)
	group := "test.opendatahub.io"

	name := fmt.Sprintf("%s.%s", plural, group)

	crd := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   plural,
				Singular: lowerKind,
				Kind:     kind,
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {Type: "object"},
						},
					},
				},
			}},
		},
	}

	obj, err := resources.ToUnstructured(crd)
	g.Expect(err).ShouldNot(HaveOccurred())

	return obj
}

func TestDynamicOwnership_GCWithDynamicAction(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	cmName := xid.New().String()
	orphanCMName := xid.New().String()

	var deployOrphan atomic.Bool
	deployOrphan.Store(true)

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithDynamicOwnership().
		WithAction(func(_ context.Context, rr *odhtype.ReconciliationRequest) error {
			rr.Generated = true

			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}
			objs := []client.Object{cm}

			if deployOrphan.Load() {
				orphanCM := &corev1.ConfigMap{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
					ObjectMeta: metav1.ObjectMeta{Name: orphanCMName, Namespace: nsName},
					Data:       map[string]string{"orphan": "true"},
				}
				objs = append(objs, orphanCM)
			}

			return rr.AddResources(objs...)
		}).
		WithAction(deploy.NewAction()).
		WithAction(gc.NewAction(
			gc.WithTypePredicate(func(rr *odhtype.ReconciliationRequest, objGVK schema.GroupVersionKind) (bool, error) {
				return rr.Controller.Owns(objGVK), nil
			}),
			gc.WithObjectPredicate(func(rr *odhtype.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
				for i := range rr.Resources {
					if rr.Resources[i].GroupVersionKind() == obj.GroupVersionKind() &&
						rr.Resources[i].GetNamespace() == obj.GetNamespace() &&
						rr.Resources[i].GetName() == obj.GetName() {
						return false, nil
					}
				}
				return true, nil
			}),
			gc.InNamespace(nsName),
			gc.WithDeletePropagationPolicy(metav1.DeletePropagationBackground),
		)).
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	deployedCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)).To(Succeed())
	g.Expect(deployedCM.GetOwnerReferences()).To(HaveLen(1))

	deployedOrphanCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: orphanCMName, Namespace: nsName}, deployedOrphanCM)).To(Succeed())
	g.Expect(deployedOrphanCM.GetOwnerReferences()).To(HaveLen(1))

	g.Expect(rec.Owns(gvk.ConfigMap)).To(BeTrue(),
		"Owns(ConfigMap) should return true after dynamic ownership action registers the type")

	deployOrphan.Store(false)

	g.Eventually(func(innerG Gomega) {
		_, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
		innerG.Expect(err).NotTo(HaveOccurred())

		innerG.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, &corev1.ConfigMap{})).To(Succeed())

		orphanErr := cli.Get(ctx, client.ObjectKey{Name: orphanCMName, Namespace: nsName}, &corev1.ConfigMap{})
		innerG.Expect(k8serr.IsNotFound(orphanErr)).To(BeTrue(),
			"Orphaned ConfigMap should be deleted by GC because Owns(ConfigMap) returns true for dynamically owned types")
	}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
}

func TestDynamicOwnership_StaticOwnershipPrecedence(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New(envt.WithManager(ctrl.Options{
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr := et.Manager()
	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	cmName := xid.New().String()

	rec, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		OwnsGVK(gvk.ConfigMap).
		WithDynamicOwnership(reconciler.ExcludeGVKs(gvk.ConfigMap)).
		WithAction(func(_ context.Context, rr *odhtype.ReconciliationRequest) error {
			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}
			return rr.AddResources(cm)
		}).
		WithAction(deploy.NewAction()).
		Build(ctx)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(rec.Owns(gvk.ConfigMap)).To(BeTrue(),
		"Owns should return true for statically owned GVK")
	g.Expect(rec.IsExcludedFromDynamicOwnership(gvk.ConfigMap)).To(BeTrue(),
		"ConfigMap should be excluded from dynamic ownership")

	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	deployedCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)).To(Succeed())

	ownerRefs := deployedCM.GetOwnerReferences()
	g.Expect(ownerRefs).To(HaveLen(1), "Static ownership should set owner reference despite dynamic exclusion")
	g.Expect(ownerRefs[0]).To(Equal(metav1.OwnerReference{
		APIVersion:         gvk.Dashboard.GroupVersion().String(),
		Kind:               gvk.Dashboard.Kind,
		Name:               componentApi.DashboardInstanceName,
		UID:                dashboard.GetUID(),
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}))
}
