package kserve_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	kservegate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kserve"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type kserveGateTestCtx struct {
	cli client.Client
}

func TestKserveGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installKServeGateCRDs(t.Context(), te); err != nil {
		t.Fatalf("install KServe CRDs: %v", err)
	}

	tc := &kserveGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("serverless InferenceService blocks", tc.testServerlessInferenceServiceBlocks)
	t.Run("ModelMesh InferenceService blocks", tc.testModelMeshInferenceServiceBlocks)
	t.Run("multi-model ServingRuntime blocks", tc.testMultiModelServingRuntimeBlocks)
	t.Run("removed runtime InferenceService blocks", tc.testRemovedRuntimeInferenceServiceBlocks)
	t.Run("llm workload without Authorino blocks", tc.testLLMInferenceServiceWithoutAuthorinoBlocks)
}

func (tc *kserveGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "kserve-gate-clean"

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	g.Expect(tc.cli.Create(ctx, ns)).ToNot(HaveOccurred())
	err := kservegate.Check(ctx, tc.cli, "kserve", namespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *kserveGateTestCtx) testServerlessInferenceServiceBlocks(t *testing.T) {
	g := NewWithT(t)
	namespace := "kserve-gate-serverless"

	obj, err := tp.RenderObject(resourcesFS, "resources/inferenceservice.tmpl.yaml", map[string]any{
		"Name":           "serverless-isvc",
		"Namespace":      namespace,
		"DeploymentMode": "Serverless",
		"RuntimeName":    "",
	})
	g.Expect(err).ToNot(HaveOccurred())
	tc.assertBlockingCase(t, namespace, obj, func(g *WithT, blockingErr *kservegate.UpgradeBlockedError) {
		g.Expect(blockingErr.ServerlessInferenceServices).To(Equal(1))
		g.Expect(blockingErr.ModelMeshInferenceServices).To(Equal(0))
		g.Expect(blockingErr.MultiModelServingRuntimes).To(Equal(0))
		g.Expect(blockingErr.RemovedRuntimeInferenceServices).To(Equal(0))
	})
}

func (tc *kserveGateTestCtx) testModelMeshInferenceServiceBlocks(t *testing.T) {
	g := NewWithT(t)
	namespace := "kserve-gate-modelmesh"

	obj, err := tp.RenderObject(resourcesFS, "resources/inferenceservice.tmpl.yaml", map[string]any{
		"Name":           "modelmesh-isvc",
		"Namespace":      namespace,
		"DeploymentMode": "ModelMesh",
		"RuntimeName":    "",
	})
	g.Expect(err).ToNot(HaveOccurred())
	tc.assertBlockingCase(t, namespace, obj, func(g *WithT, blockingErr *kservegate.UpgradeBlockedError) {
		g.Expect(blockingErr.ServerlessInferenceServices).To(Equal(0))
		g.Expect(blockingErr.ModelMeshInferenceServices).To(Equal(1))
		g.Expect(blockingErr.MultiModelServingRuntimes).To(Equal(0))
		g.Expect(blockingErr.RemovedRuntimeInferenceServices).To(Equal(0))
	})
}

func (tc *kserveGateTestCtx) testMultiModelServingRuntimeBlocks(t *testing.T) {
	g := NewWithT(t)
	namespace := "kserve-gate-multimodel"

	obj, err := tp.RenderObject(resourcesFS, "resources/servingruntime.tmpl.yaml", map[string]any{
		"Name":       "mm-runtime",
		"Namespace":  namespace,
		"MultiModel": true,
	})
	g.Expect(err).ToNot(HaveOccurred())
	tc.assertBlockingCase(t, namespace, obj, func(g *WithT, blockingErr *kservegate.UpgradeBlockedError) {
		g.Expect(blockingErr.ServerlessInferenceServices).To(Equal(0))
		g.Expect(blockingErr.ModelMeshInferenceServices).To(Equal(0))
		g.Expect(blockingErr.MultiModelServingRuntimes).To(Equal(1))
		g.Expect(blockingErr.RemovedRuntimeInferenceServices).To(Equal(0))
	})
}

func (tc *kserveGateTestCtx) testRemovedRuntimeInferenceServiceBlocks(t *testing.T) {
	g := NewWithT(t)
	namespace := "kserve-gate-removed-runtime"

	obj, err := tp.RenderObject(resourcesFS, "resources/inferenceservice.tmpl.yaml", map[string]any{
		"Name":           "removed-runtime-isvc",
		"Namespace":      namespace,
		"DeploymentMode": "",
		"RuntimeName":    "ovms",
	})
	g.Expect(err).ToNot(HaveOccurred())
	tc.assertBlockingCase(t, namespace, obj, func(g *WithT, blockingErr *kservegate.UpgradeBlockedError) {
		g.Expect(blockingErr.ServerlessInferenceServices).To(Equal(0))
		g.Expect(blockingErr.ModelMeshInferenceServices).To(Equal(0))
		g.Expect(blockingErr.MultiModelServingRuntimes).To(Equal(0))
		g.Expect(blockingErr.RemovedRuntimeInferenceServices).To(Equal(1))
	})
}

func (tc *kserveGateTestCtx) testLLMInferenceServiceWithoutAuthorinoBlocks(t *testing.T) {
	namespace := "kserve-gate-llm-no-authorino"

	obj := renderLLMInferenceService(t)
	obj.SetNamespace(namespace)

	tc.assertBlockingCase(t, namespace, obj, func(g *WithT, blockingErr *kservegate.UpgradeBlockedError) {
		g.Expect(blockingErr.ServerlessInferenceServices).To(Equal(0))
		g.Expect(blockingErr.ModelMeshInferenceServices).To(Equal(0))
		g.Expect(blockingErr.MultiModelServingRuntimes).To(Equal(0))
		g.Expect(blockingErr.RemovedRuntimeInferenceServices).To(Equal(0))
		g.Expect(blockingErr.AuthorinoTLSNotReady).To(Equal(1))
	})
}

func (tc *kserveGateTestCtx) assertBlockingCase(
	t *testing.T,
	namespace string,
	obj client.Object,
	assertFn func(g *WithT, blockingErr *kservegate.UpgradeBlockedError),
) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, obj)).ToNot(HaveOccurred())
	envt.CleanupDelete(t, g, context.Background(), tc.cli, obj)

	err := kservegate.Check(ctx, tc.cli, "kserve", namespace)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *kservegate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	assertFn(g, blockingErr)
}

func installKServeGateCRDs(ctx context.Context, te *envt.EnvT) error {
	if _, err := te.RegisterCRD(
		ctx,
		gvk.InferenceServices,
		"inferenceservices",
		"inferenceservice",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	if _, err := te.RegisterCRD(
		ctx,
		gvk.ServingRuntime,
		"servingruntimes",
		"servingruntime",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	if _, err := te.RegisterCRD(
		ctx,
		gvk.LLMInferenceServiceV1Alpha2,
		"llminferenceservices",
		"llminferenceservice",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	if _, err := te.RegisterCRD(
		ctx,
		gvk.Authorinov1beta1,
		"authorinos",
		"authorino",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	return nil
}
