package kserve_test

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
	kservegate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kserve"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

const (
	testNamespace = "test-ns"
)

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKServeClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KserveComponentName, kservegate.Check)

	checkFn := provision.GetUpgradeCheck("kserve")
	g.Expect(checkFn).ToNot(BeNil())

	err = checkFn(ctx, cli, "kserve", testNamespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func newKServeClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.InferenceServices, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.ServingRuntime, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderServingRuntime(t *testing.T, name string, multiModel bool) *unstructured.Unstructured {
	t.Helper()
	g := NewWithT(t)

	obj, err := tp.RenderObject(resourcesFS, "resources/servingruntime.tmpl.yaml", map[string]any{
		"Name":       name,
		"Namespace":  testNamespace,
		"MultiModel": multiModel,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func renderInferenceService(
	t *testing.T,
	name string,
	deploymentMode string,
	runtimeName string,
) *unstructured.Unstructured {
	t.Helper()
	g := NewWithT(t)

	obj, err := tp.RenderObject(resourcesFS, "resources/inferenceservice.tmpl.yaml", map[string]any{
		"Name":           name,
		"Namespace":      testNamespace,
		"DeploymentMode": deploymentMode,
		"RuntimeName":    runtimeName,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func TestRegister_MultipleBlockingCategoriesAreAggregated(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	serverless := renderInferenceService(t, "serverless-isvc", "Serverless", "")
	modelMesh := renderInferenceService(t, "modelmesh-isvc", "ModelMesh", "")
	multiModel := renderServingRuntime(t, "mm-runtime", true)
	removedRuntime := renderInferenceService(t, "ovms-isvc", "", "ovms")

	cli, err := newKServeClient(serverless, modelMesh, multiModel, removedRuntime)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KserveComponentName, kservegate.Check)

	err = provision.GetUpgradeCheck("kserve")(ctx, cli, "kserve", testNamespace)
	g.Expect(err).To(HaveOccurred())
	var blockingErr *kservegate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.ServerlessInferenceServices).To(Equal(1))
	g.Expect(blockingErr.ModelMeshInferenceServices).To(Equal(1))
	g.Expect(blockingErr.MultiModelServingRuntimes).To(Equal(1))
	g.Expect(blockingErr.RemovedRuntimeInferenceServices).To(Equal(1))
}
