package ray_test

import (
	"embed"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	raygate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/ray"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

const testNamespace = "test-ns"

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newRayClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.RayComponentName, raygate.Check)

	err = provision.GetUpgradeCheck(componentApi.RayComponentName)(ctx, cli, componentApi.RayComponentName, testNamespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_CodeFlareManagedRayClusterBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderRayCluster(t, map[string]any{
		"APIVersion": "ray.io/v1",
		"Name":       "codeflare-raycluster",
		"Namespace":  testNamespace,
		"Finalizers": []string{"ray.openshift.ai/oauth-finalizer"},
	})

	cli, err := newRayClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.RayComponentName, raygate.Check)

	err = provision.GetUpgradeCheck(componentApi.RayComponentName)(ctx, cli, componentApi.RayComponentName, testNamespace)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *raygate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.CodeFlareManagedRayClusters).To(Equal(1))
}

func TestRegister_RayClusterWithoutCodeFlareFinalizerPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderRayCluster(t, map[string]any{
		"APIVersion": "ray.io/v1",
		"Name":       "non-codeflare-raycluster",
		"Namespace":  testNamespace,
		"Finalizers": []string{"some-other-finalizer"},
	})

	cli, err := newRayClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.RayComponentName, raygate.Check)

	err = provision.GetUpgradeCheck(componentApi.RayComponentName)(ctx, cli, componentApi.RayComponentName, testNamespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func newRayClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.RayClusterV1, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.RayClusterV1Alpha1, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderRayCluster(t *testing.T, data map[string]any) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/raycluster.tmpl.yaml", data)
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}
