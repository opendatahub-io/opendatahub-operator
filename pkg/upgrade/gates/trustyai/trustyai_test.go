package trustyai_test

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
	trustyaigate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/trustyai"
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

	cli, err := newTrustyAIClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.TrustyAIComponentName, trustyaigate.Check)

	err = provision.GetUpgradeCheck(componentApi.TrustyAIComponentName)(ctx, cli, componentApi.TrustyAIComponentName, testNamespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_TrustyAIServiceWithPVCStorageBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderTrustyAIService(t, map[string]any{
		"APIVersion":    "trustyai.opendatahub.io/v1",
		"Name":          "trustyai-pvc",
		"Namespace":     testNamespace,
		"StorageFormat": "PVC",
	})

	cli, err := newTrustyAIClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.TrustyAIComponentName, trustyaigate.Check)

	err = provision.GetUpgradeCheck(componentApi.TrustyAIComponentName)(ctx, cli, componentApi.TrustyAIComponentName, testNamespace)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *trustyaigate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.PVCStorageTrustyAIServices).To(Equal(1))
}

func TestRegister_TrustyAIServiceWithDatabaseStoragePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderTrustyAIService(t, map[string]any{
		"APIVersion":    "trustyai.opendatahub.io/v1",
		"Name":          "trustyai-db",
		"Namespace":     testNamespace,
		"StorageFormat": "DATABASE",
	})

	cli, err := newTrustyAIClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.TrustyAIComponentName, trustyaigate.Check)

	err = provision.GetUpgradeCheck(componentApi.TrustyAIComponentName)(ctx, cli, componentApi.TrustyAIComponentName, testNamespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func newTrustyAIClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.TrustyAIServiceV1, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.TrustyAIServiceV1Alpha1, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderTrustyAIService(t *testing.T, data map[string]any) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/trustyaiservice.tmpl.yaml", data)
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}
