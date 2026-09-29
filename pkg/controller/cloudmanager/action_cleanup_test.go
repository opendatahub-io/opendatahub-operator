//nolint:testpackage // white-box tests for unexported cleanupExcludedCharts
package cloudmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	ctypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// testCleanupChart returns a HelmChartInfo pointing to the shared test chart.
// The chart renders a ConfigMap named "{releaseName}-config" in the "default"
// namespace (from the chart's values.yaml defaults, since cleanupExcludedCharts
// always renders with empty values).
func testCleanupChart(releaseName string) ctypes.HelmChartInfo {
	return ctypes.HelmChartInfo{
		Source: helmRenderer.Source{
			Chart:       "testdata/test-chart",
			ReleaseName: releaseName,
			Values:      helmRenderer.Values(map[string]any{}),
		},
	}
}

// testOperatorCleanupChart renders ServiceAccount, Role, RoleBinding, and
// Deployment — enough to assert access-control is deferred while workloads live.
func testOperatorCleanupChart(releaseName string) ctypes.HelmChartInfo {
	return ctypes.HelmChartInfo{
		Source: helmRenderer.Source{
			Chart:       "testdata/operator-chart",
			ReleaseName: releaseName,
			Values:      helmRenderer.Values(map[string]any{}),
		},
	}
}

func TestIsAccessControlKind(t *testing.T) {
	g := NewWithT(t)

	g.Expect(isAccessControlKind(gvk.ServiceAccount.Kind)).To(BeTrue())
	g.Expect(isAccessControlKind(gvk.Role.Kind)).To(BeTrue())
	g.Expect(isAccessControlKind(gvk.RoleBinding.Kind)).To(BeTrue())
	g.Expect(isAccessControlKind(gvk.ClusterRole.Kind)).To(BeTrue())
	g.Expect(isAccessControlKind(gvk.ClusterRoleBinding.Kind)).To(BeTrue())
	g.Expect(isAccessControlKind(gvk.Deployment.Kind)).To(BeFalse())
	g.Expect(isAccessControlKind(gvk.ConfigMap.Kind)).To(BeFalse())
}

func TestIsWorkloadKind(t *testing.T) {
	g := NewWithT(t)

	g.Expect(isWorkloadKind(gvk.Deployment.Kind)).To(BeTrue())
	g.Expect(isWorkloadKind(gvk.StatefulSet.Kind)).To(BeTrue())
	g.Expect(isWorkloadKind(gvk.DaemonSet.Kind)).To(BeTrue())
	g.Expect(isWorkloadKind("ReplicaSet")).To(BeTrue())
	g.Expect(isWorkloadKind(gvk.ServiceAccount.Kind)).To(BeFalse())
	g.Expect(isWorkloadKind(gvk.ConfigMap.Kind)).To(BeFalse())
}

func newCleanupRR(cl client.Client, generated bool) *ctypes.ReconciliationRequest {
	instance := &ccmv1alpha1.AzureKubernetesEngine{}
	instance.SetUID("owner-uid-1234")

	return &ctypes.ReconciliationRequest{
		Client:    cl,
		Instance:  instance,
		Generated: generated,
	}
}

func makeOwnerRef(uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: gvk.AzureKubernetesEngine.GroupVersion().String(),
		Kind:       gvk.AzureKubernetesEngine.Kind,
		Name:       "test",
		UID:        uid,
	}
}

func makeTestConfigMap(ownerUID types.UID) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
		},
	}
	if ownerUID != "" {
		cm.SetOwnerReferences([]metav1.OwnerReference{makeOwnerRef(ownerUID)})
	}

	return cm
}

// TestCleanupExcludedChartsLogFields verifies that every cleanup log line
// identifying the chart resource emits the structured "child"/"childNamespace"
// keys and never the bare "name"/"namespace" keys (which collide with the
// reconciler's reserved logger keys). Cleanup logs through the context logger, so
// the capture logger is injected via logf.IntoContext.
func TestCleanupExcludedChartsLogFields(t *testing.T) {
	ctx := context.Background()

	const (
		instanceUID = types.UID("owner-uid-1234")
		releaseName = "test"
		cmName      = releaseName + "-config"
		cmNS        = "default"
	)

	charts := []ctypes.HelmChartInfo{testCleanupChart(releaseName)}

	// captureCtx returns a context carrying a logger that records everything at
	// V(1) so both Info and V(1).Info lines are captured, plus its buffer.
	captureCtx := func() (context.Context, *strings.Builder) {
		var buf strings.Builder
		logger := funcr.New(func(_, args string) {
			buf.WriteString(args)
			buf.WriteByte('\n')
		}, funcr.Options{Verbosity: 1})

		return logf.IntoContext(ctx, logger), &buf
	}

	assertChildKeys := func(g *WithT, out string) {
		g.Expect(out).To(ContainSubstring(`"child"="`+cmName+`"`), "expected structured child key, got: %s", out)
		g.Expect(out).To(ContainSubstring(`"childNamespace"="`+cmNS+`"`), "expected structured childNamespace key, got: %s", out)
		g.Expect(out).NotTo(ContainSubstring(`"name"=`), "old name key must not be emitted, got: %s", out)
		g.Expect(out).NotTo(ContainSubstring(`"namespace"=`), "old namespace key must not be emitted, got: %s", out)
	}

	t.Run("owned resource deletion is logged with child keys", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New(fakeclient.WithObjects(makeTestConfigMap(instanceUID)))
		g.Expect(err).NotTo(HaveOccurred())

		lctx, buf := captureCtx()
		g.Expect(cleanupExcludedCharts(lctx, newCleanupRR(cl, true), charts)).To(Succeed())
		assertChildKeys(g, buf.String())
	})

	t.Run("not-owned resource skip is logged with child keys", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New(fakeclient.WithObjects(makeTestConfigMap("other-uid")))
		g.Expect(err).NotTo(HaveOccurred())

		lctx, buf := captureCtx()
		g.Expect(cleanupExcludedCharts(lctx, newCleanupRR(cl, true), charts)).To(Succeed())
		assertChildKeys(g, buf.String())
	})

	t.Run("get error is logged with child keys", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return errors.New("transient api error")
			},
		}))
		g.Expect(err).NotTo(HaveOccurred())

		lctx, buf := captureCtx()
		g.Expect(cleanupExcludedCharts(lctx, newCleanupRR(cl, true), charts)).To(HaveOccurred())
		out := buf.String()
		assertChildKeys(g, out)
		g.Expect(out).To(ContainSubstring(`"resourceKind"="ConfigMap"`), "expected structured resourceKind key, got: %s", out)
	})

	t.Run("delete error is logged with child keys", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New(
			fakeclient.WithObjects(makeTestConfigMap(instanceUID)),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return errors.New("transient api error")
				},
			}),
		)
		g.Expect(err).NotTo(HaveOccurred())

		lctx, buf := captureCtx()
		g.Expect(cleanupExcludedCharts(lctx, newCleanupRR(cl, true), charts)).To(HaveOccurred())
		out := buf.String()
		assertChildKeys(g, out)
		g.Expect(out).To(ContainSubstring(`"resourceKind"="ConfigMap"`), "expected structured resourceKind key, got: %s", out)
	})
}

func TestCleanupExcludedCharts(t *testing.T) {
	ctx := context.Background()

	const (
		instanceUID = types.UID("owner-uid-1234")
		releaseName = "test"
		cmName      = releaseName + "-config"
		cmNS        = "default"
	)

	charts := []ctypes.HelmChartInfo{testCleanupChart(releaseName)}
	cmKey := client.ObjectKey{Namespace: cmNS, Name: cmName}

	t.Run("no-op when Generated is false", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New()
		g.Expect(err).NotTo(HaveOccurred())

		cm := makeTestConfigMap(instanceUID)
		g.Expect(cl.Create(ctx, cm)).To(Succeed())

		rr := newCleanupRR(cl, false)
		g.Expect(cleanupExcludedCharts(ctx, rr, charts)).To(Succeed())

		got := &corev1.ConfigMap{}
		g.Expect(cl.Get(ctx, cmKey, got)).To(Succeed())
	})

	t.Run("no-op when charts is empty", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New()
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		g.Expect(cleanupExcludedCharts(ctx, rr, nil)).To(Succeed())
	})

	t.Run("owned resource is deleted", func(t *testing.T) {
		g := NewWithT(t)

		cm := makeTestConfigMap(instanceUID)
		cl, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		g.Expect(cleanupExcludedCharts(ctx, rr, charts)).To(Succeed())

		got := &corev1.ConfigMap{}
		g.Expect(cl.Get(ctx, cmKey, got)).To(MatchError(ContainSubstring("not found")))
	})

	t.Run("not-owned resource is skipped", func(t *testing.T) {
		g := NewWithT(t)

		cm := makeTestConfigMap("other-uid")
		cl, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		g.Expect(cleanupExcludedCharts(ctx, rr, charts)).To(Succeed())

		got := &corev1.ConfigMap{}
		g.Expect(cl.Get(ctx, cmKey, got)).To(Succeed())
	})

	t.Run("resource without owner refs is skipped", func(t *testing.T) {
		g := NewWithT(t)

		cm := makeTestConfigMap("")
		cl, err := fakeclient.New(fakeclient.WithObjects(cm))
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		g.Expect(cleanupExcludedCharts(ctx, rr, charts)).To(Succeed())

		got := &corev1.ConfigMap{}
		g.Expect(cl.Get(ctx, cmKey, got)).To(Succeed())
	})

	t.Run("absent resource is no-op", func(t *testing.T) {
		g := NewWithT(t)

		cl, err := fakeclient.New()
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		g.Expect(cleanupExcludedCharts(ctx, rr, charts)).To(Succeed())
	})

	t.Run("Get error is collected and returned", func(t *testing.T) {
		g := NewWithT(t)

		getErr := errors.New("transient api error")
		cl, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return getErr
			},
		}))
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		err = cleanupExcludedCharts(ctx, rr, charts)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("transient api error"))
	})
}

func TestCleanupExcludedChartsAccessControlOrdering(t *testing.T) {
	ctx := context.Background()

	const (
		instanceUID = types.UID("owner-uid-1234")
		releaseName = "op"
		ns          = "default"
	)

	charts := []ctypes.HelmChartInfo{testOperatorCleanupChart(releaseName)}

	deployKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-operator"}
	saKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-sa"}
	roleKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-role"}
	rbKey := client.ObjectKey{Namespace: ns, Name: releaseName + "-rb"}

	makeOwnedOperatorObjects := func(ownerUID types.UID) []client.Object {
		owner := []metav1.OwnerReference{makeOwnerRef(ownerUID)}
		replicas := int32(1)

		return []client.Object{
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
	}

	t.Run("retains access-control while owned Deployment still exists", func(t *testing.T) {
		g := NewWithT(t)
		t.Cleanup(func() { deferredAccessControlCleanup.Delete(string(instanceUID)) })

		// Fake clients remove objects on Delete immediately. Intercept Delete so
		// workloads stay present (like a terminating Deployment) and record kinds.
		var deletedKinds []string
		cl, err := fakeclient.New(
			fakeclient.WithObjects(makeOwnedOperatorObjects(instanceUID)...),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					deletedKinds = append(deletedKinds, obj.GetObjectKind().GroupVersionKind().Kind)

					return nil
				},
			}),
		)
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		err = cleanupExcludedCharts(ctx, rr, charts)
		g.Expect(err).To(HaveOccurred())

		var requeueErr odherrors.RequeueAfterError
		g.Expect(errors.As(err, &requeueErr)).To(BeTrue())
		g.Expect(requeueErr.After).To(Equal(accessControlCleanupRequeueAfter))
		g.Expect(isAccessControlCleanupDeferred(rr)).To(BeTrue())

		g.Expect(deletedKinds).To(ContainElement("Deployment"))
		g.Expect(deletedKinds).NotTo(ContainElement("ServiceAccount"))
		g.Expect(deletedKinds).NotTo(ContainElement("Role"))
		g.Expect(deletedKinds).NotTo(ContainElement("RoleBinding"))

		g.Expect(cl.Get(ctx, saKey, &corev1.ServiceAccount{})).To(Succeed())
		g.Expect(cl.Get(ctx, roleKey, &rbacv1.Role{})).To(Succeed())
		g.Expect(cl.Get(ctx, rbKey, &rbacv1.RoleBinding{})).To(Succeed())
		g.Expect(cl.Get(ctx, deployKey, &appsv1.Deployment{})).To(Succeed())
	})

	t.Run("deletes access-control after owned Deployment is gone", func(t *testing.T) {
		g := NewWithT(t)
		t.Cleanup(func() { deferredAccessControlCleanup.Delete(string(instanceUID)) })

		objs := makeOwnedOperatorObjects(instanceUID)
		// Drop the Deployment — workloads already terminated.
		objs = objs[1:]

		cl, err := fakeclient.New(fakeclient.WithObjects(objs...))
		g.Expect(err).NotTo(HaveOccurred())

		rr := newCleanupRR(cl, true)
		g.Expect(cleanupExcludedCharts(ctx, rr, charts)).To(Succeed())

		g.Expect(cl.Get(ctx, saKey, &corev1.ServiceAccount{})).To(MatchError(ContainSubstring("not found")))
		g.Expect(cl.Get(ctx, roleKey, &rbacv1.Role{})).To(MatchError(ContainSubstring("not found")))
		g.Expect(cl.Get(ctx, rbKey, &rbacv1.RoleBinding{})).To(MatchError(ContainSubstring("not found")))
		g.Expect(isAccessControlCleanupDeferred(rr)).To(BeFalse())
	})

	t.Run("finishes deferred access-control cleanup when Generated is false", func(t *testing.T) {
		g := NewWithT(t)

		const deferredUID = types.UID("deferred-cleanup-uid")
		t.Cleanup(func() { deferredAccessControlCleanup.Delete(string(deferredUID)) })

		// Pass A deferred while Deployment remains (Delete is a no-op).
		cl1, err := fakeclient.New(
			fakeclient.WithObjects(makeOwnedOperatorObjects(deferredUID)...),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					return nil
				},
			}),
		)
		g.Expect(err).NotTo(HaveOccurred())

		rr1 := newCleanupRR(cl1, true)
		rr1.Instance.SetUID(deferredUID)
		err = cleanupExcludedCharts(ctx, rr1, charts)
		g.Expect(err).To(HaveOccurred())
		var requeueErr odherrors.RequeueAfterError
		g.Expect(errors.As(err, &requeueErr)).To(BeTrue())

		// Workloads gone; Generated stays false (hash unchanged) but deferred
		// flag must still allow Pass B to delete SA/RBAC.
		objs := makeOwnedOperatorObjects(deferredUID)[1:]
		cl2, err := fakeclient.New(fakeclient.WithObjects(objs...))
		g.Expect(err).NotTo(HaveOccurred())

		rr2 := newCleanupRR(cl2, false)
		rr2.Instance.SetUID(deferredUID)
		g.Expect(cleanupExcludedCharts(ctx, rr2, charts)).To(Succeed())

		g.Expect(cl2.Get(ctx, saKey, &corev1.ServiceAccount{})).To(MatchError(ContainSubstring("not found")))
		g.Expect(cl2.Get(ctx, roleKey, &rbacv1.Role{})).To(MatchError(ContainSubstring("not found")))
		g.Expect(cl2.Get(ctx, rbKey, &rbacv1.RoleBinding{})).To(MatchError(ContainSubstring("not found")))
		g.Expect(isAccessControlCleanupDeferred(rr2)).To(BeFalse())
	})
}
