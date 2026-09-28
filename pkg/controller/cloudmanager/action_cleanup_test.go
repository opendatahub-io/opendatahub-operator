//nolint:testpackage // white-box tests for unexported cleanupExcludedCharts
package cloudmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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
