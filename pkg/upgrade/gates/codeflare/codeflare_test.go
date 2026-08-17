package codeflare_test

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
	codeflaregate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/codeflare"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newCodeFlareClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.CodeFlareComponentName, codeflaregate.Check)

	err = provision.GetUpgradeCheck(componentApi.CodeFlareComponentName)(ctx, cli, componentApi.CodeFlareComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_CodeFlareCRBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderCodeFlare(t)

	cli, err := newCodeFlareClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.CodeFlareComponentName, codeflaregate.Check)

	err = provision.GetUpgradeCheck(componentApi.CodeFlareComponentName)(ctx, cli, componentApi.CodeFlareComponentName, "")
	g.Expect(err).To(HaveOccurred())
	var blockingErr *codeflaregate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.CodeFlareCRPresent).To(BeTrue())
	g.Expect(blockingErr.AppWrappers).To(Equal(0))
}

func TestRegister_AppWrapperBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	obj := renderAppWrapper(t, "test-appwrapper", "test-ns")

	cli, err := newCodeFlareClient(obj)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.CodeFlareComponentName, codeflaregate.Check)

	err = provision.GetUpgradeCheck(componentApi.CodeFlareComponentName)(ctx, cli, componentApi.CodeFlareComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *codeflaregate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.CodeFlareCRPresent).To(BeFalse())
	g.Expect(blockingErr.AppWrappers).To(Equal(1))
}

func newCodeFlareClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.CodeFlare, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: gvk.AppWrapper, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderCodeFlare(t *testing.T) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/codeflare.tmpl.yaml", map[string]any{
		"Name": componentApi.CodeFlareInstanceName,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func renderAppWrapper(t *testing.T, name string, namespace string) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/appwrapper.tmpl.yaml", map[string]any{
		"Name":      name,
		"Namespace": namespace,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}
