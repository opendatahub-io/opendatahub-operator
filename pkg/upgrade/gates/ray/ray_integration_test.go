package ray_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	raygate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/ray"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type rayGateTestCtx struct {
	cli client.Client
}

func TestRayGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installRayGateCRD(t.Context(), te); err != nil {
		t.Fatalf("install RayCluster CRD: %v", err)
	}

	tc := &rayGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("CodeFlare-managed RayCluster blocks", tc.testCodeFlareManagedRayClusterBlocks)
	t.Run("RayCluster without CodeFlare finalizer passes", tc.testRayClusterWithoutCodeFlareFinalizerPasses)
}

func (tc *rayGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := raygate.Check(t.Context(), tc.cli, componentApi.RayComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *rayGateTestCtx) testCodeFlareManagedRayClusterBlocks(t *testing.T) {
	g := NewWithT(t)
	namespace := "ray-gate-codeflare-managed"

	obj, err := tp.RenderObject(resourcesFS, "resources/raycluster.tmpl.yaml", map[string]any{
		"APIVersion": "ray.io/v1",
		"Name":       "codeflare-raycluster",
		"Namespace":  namespace,
		"Finalizers": []string{"ray.openshift.ai/oauth-finalizer"},
	})
	g.Expect(err).ToNot(HaveOccurred())

	tc.assertBlockingCase(t, namespace, obj, 1)
}

func (tc *rayGateTestCtx) testRayClusterWithoutCodeFlareFinalizerPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "ray-gate-no-codeflare-finalizer"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	obj, err := tp.RenderObject(resourcesFS, "resources/raycluster.tmpl.yaml", map[string]any{
		"APIVersion": "ray.io/v1",
		"Name":       "non-codeflare-raycluster",
		"Namespace":  namespace,
		"Finalizers": []string{"some-other-finalizer"},
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, obj)).ToNot(HaveOccurred())
	cleanupRayCluster(t, g, tc.cli, obj)

	err = raygate.Check(ctx, tc.cli, componentApi.RayComponentName, namespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *rayGateTestCtx) assertBlockingCase(
	t *testing.T,
	namespace string,
	obj client.Object,
	expectedBlocked int,
) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, obj)).ToNot(HaveOccurred())
	cleanupRayCluster(t, g, tc.cli, obj)

	err := raygate.Check(ctx, tc.cli, componentApi.RayComponentName, namespace)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *raygate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.CodeFlareManagedRayClusters).To(Equal(expectedBlocked))
}

func installRayGateCRD(ctx context.Context, te *envt.EnvT) error {
	_, err := te.RegisterCRD(
		ctx,
		gvk.RayClusterV1,
		"rayclusters",
		"raycluster",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	)
	return err
}

func cleanupRayCluster(t *testing.T, g *WithT, cli client.Client, obj client.Object) {
	t.Helper()

	t.Cleanup(func() {
		key := client.ObjectKeyFromObject(obj)
		current, ok := obj.DeepCopyObject().(client.Object)
		g.Expect(ok).To(BeTrue())

		err := cli.Get(context.Background(), key, current)
		if k8serr.IsNotFound(err) {
			return
		}
		g.Expect(err).ToNot(HaveOccurred())
		current.SetFinalizers(nil)
		g.Expect(cli.Update(context.Background(), current)).ToNot(HaveOccurred())
		err = cli.Delete(context.Background(), current)
		if err != nil && !k8serr.IsNotFound(err) {
			g.Expect(err).ToNot(HaveOccurred())
		}
		g.Eventually(func() error {
			return cli.Get(context.Background(), key, current)
		}).WithTimeout(envt.DefaultMaxWait).WithPolling(envt.DefaultPollInterval).Should(
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		)
	})
}
