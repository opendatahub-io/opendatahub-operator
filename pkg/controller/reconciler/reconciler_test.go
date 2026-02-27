//nolint:testpackage
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	gomegaTypes "github.com/onsi/gomega/types"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
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

func createReconciler(cli client.Client) *Reconciler {
	return &Reconciler{
		Client:   cli,
		Scheme:   cli.Scheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("test"),
		Release:  cluster.GetRelease(),
		Recorder: record.NewFakeRecorder(100),
		name:     "test",
		instanceFactory: func() (common.PlatformObject, error) {
			i := &componentApi.Dashboard{
				TypeMeta: ctrl.TypeMeta{
					APIVersion: gvk.Dashboard.GroupVersion().String(),
					Kind:       gvk.Dashboard.Kind,
				},
			}

			return i, nil
		},
		conditionsManagerFactory: func(accessor common.ConditionsAccessor) *conditions.Manager {
			return conditions.NewManager(accessor, status.ConditionTypeReady)
		},
	}
}

// startManager starts the manager in the background and waits for the cache to sync.
// It registers a cleanup function to stop the manager when the test completes.
func startManager(t *testing.T, g *WithT, mgr ctrl.Manager) {
	t.Helper()

	ctx := t.Context()
	mgrCtx, mgrCancel := context.WithCancel(ctx)

	go func() {
		_ = mgr.Start(mgrCtx)
	}()
	t.Cleanup(mgrCancel)

	// Wait for cache to sync
	g.Eventually(func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}).Should(BeTrue())
}

func TestConditions(t *testing.T) {
	ctx := t.Context()

	g := NewWithT(t)

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	cli := et.Client()

	dsci := resources.GvkToUnstructured(gvk.DSCInitialization)
	dsci.SetName(xid.New().String())
	dsci.SetGeneration(1)

	err = cli.Create(ctx, dsci)
	g.Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name    string
		err     error
		matcher gomegaTypes.GomegaMatcher
	}{
		{
			name: "ready",
			err:  nil,

			matcher: And(
				jq.Match(`all(.status.conditions[]?.type; . != "foo")`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
			),
		},
		{
			name: "stop",
			err:  odherrors.NewStopError("stop"),
			matcher: And(
				jq.Match(`all(.status.conditions[]?.type; . != "foo")`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
			),
		},
		{
			name: "failure",
			err:  errors.New("failure"),
			matcher: And(
				jq.Match(`all(.status.conditions[]?.type; . != "foo")`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dash := resources.GvkToUnstructured(gvk.Dashboard)
			dash.SetName(componentApi.DashboardInstanceName)
			dash.SetGeneration(1)

			err = cli.Create(ctx, dash)
			g.Expect(err).NotTo(HaveOccurred())

			st, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&common.Status{
				Conditions: []common.Condition{{
					Type:               "foo",
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.NewTime(time.Now()),
				}},
			})

			g.Expect(err).NotTo(HaveOccurred())

			err = unstructured.SetNestedField(dash.Object, st, "status")
			g.Expect(err).NotTo(HaveOccurred())

			err = cli.Status().Update(ctx, dash)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(dash).Should(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, "foo", metav1.ConditionFalse),
			)

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: componentApi.DashboardInstanceName,
				},
			}

			cc := createReconciler(cli)
			cc.AddAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
				return tt.err
			})

			result, err := cc.Reconcile(ctx, req)
			if tt.err == nil {
				g.Expect(err).ShouldNot(HaveOccurred())
			} else {
				g.Expect(err).Should(MatchError(tt.err))
			}

			g.Expect(result.RequeueAfter).Should(BeZero())

			di := resources.GvkToUnstructured(gvk.Dashboard)
			di.SetName(dash.GetName())

			err = cli.Get(ctx, client.ObjectKeyFromObject(di), di)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(di).Should(tt.matcher)

			err = cli.Delete(ctx, di, client.PropagationPolicy(metav1.DeletePropagationBackground))
			g.Expect(err).ShouldNot(HaveOccurred())

			g.Eventually(func() ([]componentApi.Dashboard, error) {
				l := componentApi.DashboardList{}
				if err := cli.List(ctx, &l, client.InNamespace("")); err != nil {
					return nil, err
				}

				return l.Items, nil
			}).WithTimeout(10 * time.Second).Should(BeEmpty())
		})
	}
}

// TestReconcilerBuilder_WatchMethods_UseUnstructured verifies that all watch
// registration methods (Owns, Watches, OwnsGVK, WatchesGVK) convert objects
// to unstructured. This prevents the stale cache bug where typed and
// unstructured informers can become out of sync.
func TestReconcilerBuilder_WatchMethods_UseUnstructured(t *testing.T) {
	g := NewWithT(t)

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme:     et.Scheme(),
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	g.Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name       string
		setupWatch func(*ReconcilerBuilder[*componentApi.Dashboard])
	}{
		{
			name: "Owns with typed object",
			setupWatch: func(b *ReconcilerBuilder[*componentApi.Dashboard]) {
				b.Owns(&corev1.ConfigMap{})
			},
		},
		{
			name: "Watches with typed object",
			setupWatch: func(b *ReconcilerBuilder[*componentApi.Dashboard]) {
				b.Watches(&corev1.Secret{})
			},
		},
		{
			name: "OwnsGVK",
			setupWatch: func(b *ReconcilerBuilder[*componentApi.Dashboard]) {
				b.OwnsGVK(gvk.Deployment)
			},
		},
		{
			name: "WatchesGVK",
			setupWatch: func(b *ReconcilerBuilder[*componentApi.Dashboard]) {
				b.WatchesGVK(gvk.Secret)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := ReconcilerFor(mgr, &componentApi.Dashboard{})
			tt.setupWatch(builder)

			g.Expect(builder.watches).To(HaveLen(1),
				"expected exactly one watch to be registered")

			_, isUnstructured := builder.watches[0].object.(*unstructured.Unstructured)
			g.Expect(isUnstructured).To(BeTrue(),
				"%s must use unstructured objects to prevent stale cache bugs", tt.name)
		})
	}
}

func TestNewReconciler_WithDynamicOwnership(t *testing.T) {
	g := NewWithT(t)

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme:     et.Scheme(),
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("dynamic ownership disabled by default", func(t *testing.T) {
		r, err := NewReconciler(mgr, "test", &componentApi.Dashboard{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(r.IsDynamicOwnershipEnabled()).To(BeFalse())
	})

	t.Run("dynamic ownership enabled with option", func(t *testing.T) {
		r, err := NewReconciler(mgr, "test", &componentApi.Dashboard{},
			WithDynamicOwnership(),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(r.IsDynamicOwnershipEnabled()).To(BeTrue())
	})

	t.Run("dynamic ownership with excluded GVKs", func(t *testing.T) {
		r, err := NewReconciler(mgr, "test", &componentApi.Dashboard{},
			WithDynamicOwnership(ExcludeGVKs(gvk.ConfigMap, gvk.Secret)),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(r.IsDynamicOwnershipEnabled()).To(BeTrue())
		g.Expect(r.IsExcludedFromOwnership(gvk.ConfigMap)).To(BeTrue())
		g.Expect(r.IsExcludedFromOwnership(gvk.Secret)).To(BeTrue())
		g.Expect(r.IsExcludedFromOwnership(gvk.Deployment)).To(BeFalse())
	})
}

func TestDynamicOwnership_DeployAction(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme:     et.Scheme(),
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	g.Expect(err).NotTo(HaveOccurred())

	cli := et.Client()

	// Create test namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	// Create Dashboard instance (owner)
	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	cmName := xid.New().String()
	secretName := xid.New().String()
	notManagedCMName := xid.New().String()

	// Create reconciler using builder pattern with dynamic ownership enabled
	rec, err := ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithDynamicOwnership(ExcludeGVKs(gvk.Secret)).
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			// Prepare a ConfigMap to deploy
			cm := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
				Data:       map[string]string{"key": "value"},
			}

			// Prepare a Secret to deploy (excluded from ownership)
			secret := &corev1.Secret{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: nsName},
				StringData: map[string]string{"key": "secret-value"},
			}

			// Prepare a ConfigMap with ManagedByODHOperator annotation set to "false"
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

	// Start manager after reconciler is built (watches are registered)
	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	// Verify ConfigMap was deployed with owner reference
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

	// Verify Secret was deployed WITHOUT owner reference
	deployedSecret := &corev1.Secret{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, deployedSecret)).To(Succeed())
	g.Expect(deployedSecret.GetOwnerReferences()).To(BeEmpty(), "Excluded resource should not have owner references")

	// Verify ConfigMap with ManagedByODHOperator=false was deployed without owner reference
	deployedNotManagedCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, deployedNotManagedCM)).To(Succeed())
	g.Expect(deployedNotManagedCM.GetOwnerReferences()).To(BeEmpty())

	t.Run("owned resource is restored after external modification", func(t *testing.T) {
		// Save the original state for patch base
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedConfigMap)).To(Succeed())
		original := deployedConfigMap.DeepCopy()

		// Modify the ConfigMap externally (simulating drift)
		deployedConfigMap.Data["key"] = "modified-externally"
		err := cli.Patch(ctx, deployedConfigMap, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		// Wait for watch-triggered reconciliation to restore the value
		g.Eventually(func(gg Gomega) {
			configMap := &corev1.ConfigMap{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, configMap)).To(Succeed())
			gg.Expect(configMap.Data).To(Equal(map[string]string{"key": "value"}))
			// ResourceVersion changes on any update, Generation only changes on spec changes
			gg.Expect(configMap.GetResourceVersion()).NotTo(Equal(original.GetResourceVersion()))
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	t.Run("excluded resource are not restored after external modification", func(t *testing.T) {
		// Wait for any pending reconciliations from previous subtest to complete
		// by verifying the Secret remains stable (ResourceVersion doesn't change)
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

		// Re-fetch the Secret to get current state
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, deployedSecret)).To(Succeed())

		// Save the original state for patch base
		original := deployedSecret.DeepCopy()

		// Modify the Secret externally (simulating drift)
		deployedSecret.StringData = map[string]string{"key": "modified-externally"}
		err := cli.Patch(ctx, deployedSecret, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		// First, wait for the modification to be visible (handles any pending reconciliation)
		g.Eventually(func(gg Gomega) {
			secret := &corev1.Secret{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, secret)).To(Succeed())
			gg.Expect(string(secret.Data["key"])).To(Equal("modified-externally"))
		}).WithTimeout(5*time.Second).Should(Succeed(), "Modification should be visible")

		// Then verify the modification persists (excluded resources should NOT be restored)
		g.Consistently(func(gg Gomega) {
			secret := &corev1.Secret{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nsName}, secret)).To(Succeed())
			gg.Expect(string(secret.Data["key"])).To(Equal("modified-externally"))
		}).WithTimeout(3 * time.Second).Should(Succeed())
	})

	t.Run("not managed resource are not restored after external modification", func(t *testing.T) {
		// Save the original state for patch base
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: notManagedCMName, Namespace: nsName}, deployedNotManagedCM)).To(Succeed())
		original := deployedNotManagedCM.DeepCopy()

		// Modify the ConfigMap externally (simulating drift)
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
			// ResourceVersion changes on any update, Generation only changes on spec changes
			gg.Expect(managedCM.GetResourceVersion()).NotTo(Equal(original.GetResourceVersion()))
		}).WithTimeout(3 * time.Second).Should(Succeed())
	})

	t.Run("not managed resource are restored if deleted", func(t *testing.T) {
		// Re-fetch to get current state after previous test modifications
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

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme:     et.Scheme(),
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	g.Expect(err).NotTo(HaveOccurred())

	cli := et.Client()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	// Create reconciler WITHOUT dynamic ownership (default)
	rec, err := ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			// Prepare a ConfigMap to deploy
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

	// Verify ConfigMap was deployed WITHOUT owner reference
	deployed := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: nsName}, deployed)).To(Succeed())
	g.Expect(deployed.GetOwnerReferences()).To(BeEmpty(), "Resource should not have owner references when dynamic ownership is disabled")
}

func TestDynamicOwnership_DeployAction_CRDAndCR(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme:     et.Scheme(),
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	g.Expect(err).NotTo(HaveOccurred())

	cli := et.Client()

	// Create test namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	// Create Dashboard instance (owner)
	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	// Define the CRD kind
	crdKind := "TestWidget"
	crd := createCRD(t, g, crdKind)

	// Define a CR instance of the CRD
	crName := xid.New().String()

	// To test the recreation of the CRD once deleted, we need to create a CRD without a CR.
	// This is because if we delete a CRD with a CR, also the CR will be deleted and this
	// will trigger a new reconciliation which will restore the CRD.
	crdKindWithoutCR := "WithoutCreatedCR"
	crdWithoutCreatedCR := createCRD(t, g, crdKindWithoutCR)

	// Also deploy a ConfigMap to verify mixed resource handling
	cmName := xid.New().String()

	// Create reconciler with dynamic ownership, excluding CRDs from ownership
	rec, err := ReconcilerFor(mgr, &componentApi.Dashboard{}).
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

	// Start manager
	startManager(t, g, mgr)

	// Run first reconciliation (may fail with NoKindMatchError for CR if RESTMapper hasn't refreshed)
	_, err = rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	if err != nil {
		var noKindMatchErr *meta.NoKindMatchError
		g.Expect(errors.As(err, &noKindMatchErr)).To(BeTrue(),
			"unexpected reconcile error: %v", err)
	}

	// Verify CRD was deployed WITHOUT owner reference (cluster-scoped, excluded)
	deployedCRD := &apiextensionsv1.CustomResourceDefinition{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: crd.GetName()}, deployedCRD)).To(Succeed())
	g.Expect(deployedCRD.GetOwnerReferences()).To(BeEmpty(), "CRD should not have owner references")

	// Verify CR was deployed WITH owner reference
	// The first reconciliation may fail with NoKindMatchError if the RESTMapper cache
	// hasn't refreshed yet. The CRD watch (registered by dynamic ownership action) should
	// trigger a new reconciliation once the CRD is established, which will deploy the CR.
	deployedCR := &unstructured.Unstructured{}
	deployedCR.SetAPIVersion("test.opendatahub.io/v1")
	deployedCR.SetKind("TestWidget")
	g.Eventually(func(gg Gomega) {
		gg.Expect(cli.Get(ctx, client.ObjectKey{Name: crName, Namespace: nsName}, deployedCR)).To(Succeed())
		gg.Expect(deployedCR.GetOwnerReferences()).To(HaveLen(1), "CR should have owner reference")
		gg.Expect(deployedCR.GetOwnerReferences()[0].Kind).To(Equal("Dashboard"))
	}).WithTimeout(30*time.Second).WithPolling(1*time.Second).Should(Succeed(), "CR should be deployed with owner reference")

	// Verify ConfigMap was deployed WITH owner reference
	deployedCM := &corev1.ConfigMap{}
	g.Eventually(func() error {
		return cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)
	}).WithTimeout(5*time.Second).Should(Succeed(), "ConfigMap should be deployed")
	g.Expect(deployedCM.GetOwnerReferences()).To(HaveLen(1), "ConfigMap should have owner reference")

	t.Run("CR is restored after external deletion", func(t *testing.T) {
		// Delete the CR externally
		err := cli.Delete(ctx, deployedCR)
		g.Expect(err).NotTo(HaveOccurred())

		// Verify CR is deleted
		g.Eventually(func() bool {
			err := cli.Get(ctx, client.ObjectKey{Name: crName, Namespace: nsName}, deployedCR)
			return err != nil && k8serr.IsNotFound(err)
		}).WithTimeout(5*time.Second).Should(BeTrue(), "CR should be deleted")

		// Wait for watch-triggered reconciliation to restore the CR
		g.Eventually(func(gg Gomega) {
			restored := &unstructured.Unstructured{}
			restored.SetAPIVersion("test.opendatahub.io/v1")
			restored.SetKind("TestWidget")
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: crName, Namespace: nsName}, restored)).To(Succeed())
			gg.Expect(restored.GetOwnerReferences()).To(HaveLen(1))
		}).WithTimeout(10*time.Second).Should(Succeed(), "CR should be restored after deletion")
	})

	t.Run("CRD deletion triggers reconciliation", func(t *testing.T) {
		// Get the current CRD resourceVersion before deletion
		currentCRD := &apiextensionsv1.CustomResourceDefinition{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: crdWithoutCreatedCR.GetName()}, currentCRD)).To(Succeed())
		oldUID := currentCRD.GetUID()

		// Delete the CRD externally
		err := cli.Delete(ctx, currentCRD)
		g.Expect(err).NotTo(HaveOccurred())

		// The dynamic ownership action watches CRDs by name, so deletion should trigger reconciliation
		// which will restore the CRD. The reconciliation may restore it before we can observe the
		// deletion, so we verify the CRD exists with a different resourceVersion (indicating recreation)
		g.Eventually(func(gg Gomega) {
			restored := &apiextensionsv1.CustomResourceDefinition{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: crdWithoutCreatedCR.GetName()}, restored)).To(Succeed())
			// The CRD should have been recreated (new uid)
			gg.Expect(restored.GetUID()).NotTo(Equal(oldUID), "CRD should have been recreated with new uid")
		}).WithTimeout(10*time.Second).Should(Succeed(), "CRD should be restored after deletion")
	})
}

func TestDynamicOwnership_DeployAction_WithGVKPredicates(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	nsName := xid.New().String()

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	mgr, err := ctrl.NewManager(et.Config(), ctrl.Options{
		Scheme:     et.Scheme(),
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	g.Expect(err).NotTo(HaveOccurred())

	cli := et.Client()

	// Create test namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	g.Expect(cli.Create(ctx, ns)).To(Succeed())

	// Create Dashboard instance (owner)
	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName, Generation: 1},
	}
	dashboard.SetGroupVersionKind(gvk.Dashboard)
	g.Expect(cli.Create(ctx, dashboard)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)).To(Succeed())

	deploymentName := xid.New().String()
	cmName := xid.New().String()

	// Custom predicate that reacts only to delete events
	deploymentPredicate := predicate.Funcs{
		CreateFunc: func(_ event.TypedCreateEvent[client.Object]) bool { return false },
		UpdateFunc: func(_ event.TypedUpdateEvent[client.Object]) bool { return false },
		DeleteFunc: func(_ event.TypedDeleteEvent[client.Object]) bool { return true },
	}

	// Create reconciler with dynamic ownership and custom GVK predicates
	rec, err := ReconcilerFor(mgr, &componentApi.Dashboard{}).
		WithInstanceName(xid.New().String()).
		WithDynamicOwnership(
			WithGVKPredicates(map[schema.GroupVersionKind][]predicate.Predicate{
				gvk.Deployment: {deploymentPredicate},
			}),
		).
		WithAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
			// Prepare a Deployment to deploy
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

			// Also deploy a ConfigMap (uses default predicate)
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

	// Start manager after reconciler is built
	startManager(t, g, mgr)

	res, err := rec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dashboard.GetName()}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeZero())

	// Verify Deployment was deployed with owner reference
	deployedDeployment := &appsv1.Deployment{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployedDeployment)).To(Succeed())
	g.Expect(deployedDeployment.GetOwnerReferences()).To(HaveLen(1))
	g.Expect(deployedDeployment.GetOwnerReferences()[0].Kind).To(Equal(gvk.Dashboard.Kind))

	// Verify ConfigMap was deployed with owner reference
	deployedCM := &corev1.ConfigMap{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)).To(Succeed())
	g.Expect(deployedCM.GetOwnerReferences()).To(HaveLen(1))

	t.Run("deployment is restored after external deletion", func(t *testing.T) {
		// Delete the Deployment externally
		err := cli.Delete(ctx, deployedDeployment)
		g.Expect(err).NotTo(HaveOccurred())

		// Verify Deployment is deleted
		g.Eventually(func() bool {
			err := cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, deployedDeployment)
			return err != nil && k8serr.IsNotFound(err)
		}).WithTimeout(5*time.Second).Should(BeTrue(), "Deployment should be deleted")

		// Wait for watch-triggered reconciliation to restore the Deployment
		g.Eventually(func(gg Gomega) {
			restored := &appsv1.Deployment{}
			gg.Expect(cli.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: nsName}, restored)).To(Succeed())
			gg.Expect(restored.GetOwnerReferences()).To(HaveLen(1))
		}).WithTimeout(10*time.Second).Should(Succeed(), "Deployment should be restored after deletion")
	})

	t.Run("deployment spec fields are not restored after external modification", func(t *testing.T) {
		// Re-fetch to get current state
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
		// Re-fetch to get current state
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nsName}, deployedCM)).To(Succeed())
		original := deployedCM.DeepCopy()

		// Modify the ConfigMap externally
		deployedCM.Data["key"] = "modified-externally"
		err := cli.Patch(ctx, deployedCM, client.MergeFrom(original))
		g.Expect(err).NotTo(HaveOccurred())

		// Wait for watch-triggered reconciliation to restore the data
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
