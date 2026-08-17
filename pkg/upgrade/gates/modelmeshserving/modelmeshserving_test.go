package modelmeshserving_test

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
	modelmeshgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/modelmeshserving"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newModelMeshServingClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.ModelMeshServingComponentName, modelmeshgate.Check)

	err = provision.GetUpgradeCheck(componentApi.ModelMeshServingComponentName)(ctx, cli, componentApi.ModelMeshServingComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_ModelMeshServingCRBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderModelMeshServing(t)

	cli, err := newModelMeshServingClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.ModelMeshServingComponentName, modelmeshgate.Check)

	err = provision.GetUpgradeCheck(componentApi.ModelMeshServingComponentName)(ctx, cli, componentApi.ModelMeshServingComponentName, "")
	g.Expect(err).To(HaveOccurred())
	var blockingErr *modelmeshgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
}

func newModelMeshServingClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.ModelMeshServing, Scope: meta.RESTScopeRoot},
		),
	)
}

func renderModelMeshServing(t *testing.T) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/modelmeshserving.tmpl.yaml", map[string]any{
		"Name": componentApi.ModelMeshServingInstanceName,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}
