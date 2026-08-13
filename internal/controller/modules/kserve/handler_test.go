package kserve_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Kserve: componentApi.DSCKserve{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
					},
				},
			},
		},
	}
}

func newPlatformModePlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					Kserve: common.ManagementSpec{
						ManagementState: mgmtState,
					},
				},
			},
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC_NilPlatform(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	ctx := &modules.PlatformContext{}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_PlatformMode_Removed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Empty(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(""))).Should(BeFalse())
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCNilPlatformReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := &modules.PlatformContext{}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.Kserve.KserveCommonSpec = componentApi.KserveCommonSpec{
		RawDeploymentServiceConfig: componentApi.KserveRawHeaded,
		NIM: componentApi.NimSpec{
			ManagementState: operatorv1.Managed,
			AirGapped:       true,
		},
		WVA: componentApi.WVASpec{
			ManagementState: operatorv1.Removed,
		},
	}

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.KserveInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.KserveKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"))
	g.Expect(spec["rawDeploymentServiceConfig"]).Should(Equal("Headed"))

	nim, ok := spec["nim"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.nim missing")
	g.Expect(nim["managementState"]).Should(Equal("Managed"))
	g.Expect(nim["airGapped"]).Should(BeTrue())

	wva, ok := spec["wva"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.wva missing")
	g.Expect(wva["managementState"]).Should(Equal("Removed"))

	mr, ok := spec["modelRegistry"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.modelRegistry missing")
	g.Expect(mr["managementState"]).Should(Equal("Removed"))
}

func TestBuildModuleCR_PlatformMode_ReturnsNil(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformModePlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u).Should(BeNil(), "xKS module CR is externally managed, BuildModuleCR must return nil")
}

func TestBuildModuleCR_HeadedRawServiceConfig(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.Kserve.RawDeploymentServiceConfig = componentApi.KserveRawHeaded

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["rawDeploymentServiceConfig"]).Should(Equal("Headed"))
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ContainElements(
		"RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_WORKLOAD_VARIANT_AUTOSCALER_CONTROLLER_IMAGE",
		"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE",
	))
	g.Expect(images).ShouldNot(ContainElement(h.GetControllerImage()))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.KserveComponentName))
}

func TestStripLLMISvcConfigFinalizers_RemovesFinalizers(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	ns := "test-namespace"
	finalizer := "serving.kserve.io/llmisvcconfig-finalizer"

	// Create LLMInferenceServiceConfig resources with the finalizer for both API versions.
	v1alpha1Obj := &unstructured.Unstructured{}
	v1alpha1Obj.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha1)
	v1alpha1Obj.SetName("config-v1alpha1")
	v1alpha1Obj.SetNamespace(ns)
	v1alpha1Obj.SetFinalizers([]string{finalizer})

	v1alpha2Obj := &unstructured.Unstructured{}
	v1alpha2Obj.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha2)
	v1alpha2Obj.SetName("config-v1alpha2")
	v1alpha2Obj.SetNamespace(ns)
	v1alpha2Obj.SetFinalizers([]string{finalizer})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(v1alpha1Obj, v1alpha2Obj),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.LLMInferenceServiceConfigV1Alpha1, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.LLMInferenceServiceConfigV1Alpha2, Scope: meta.RESTScopeNamespace},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = kserve.StripLLMISvcConfigFinalizers(ctx, cli, ns)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify finalizers were removed from v1alpha1 resource.
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha1)
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(v1alpha1Obj), updated)).Should(Succeed())
	g.Expect(updated.GetFinalizers()).Should(BeEmpty())

	// Verify finalizers were removed from v1alpha2 resource.
	updated = &unstructured.Unstructured{}
	updated.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha2)
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(v1alpha2Obj), updated)).Should(Succeed())
	g.Expect(updated.GetFinalizers()).Should(BeEmpty())
}

func TestStripLLMISvcConfigFinalizers_CRDNotInstalled(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	// Create a client without registering the LLMInferenceServiceConfig GVKs.
	// This simulates the CRD not being installed — List should return a
	// NoMatchError which the function handles gracefully.
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = kserve.StripLLMISvcConfigFinalizers(ctx, cli, "some-namespace")
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestStripLLMISvcConfigFinalizers_NoFinalizer(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	ns := "test-namespace"

	// Create a resource WITHOUT the target finalizer.
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha1)
	obj.SetName("config-no-finalizer")
	obj.SetNamespace(ns)
	obj.SetFinalizers([]string{"some-other-finalizer"})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(obj),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.LLMInferenceServiceConfigV1Alpha1, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.LLMInferenceServiceConfigV1Alpha2, Scope: meta.RESTScopeNamespace},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = kserve.StripLLMISvcConfigFinalizers(ctx, cli, ns)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify the original finalizer is still present (was not touched).
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha1)
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(obj), updated)).Should(Succeed())
	g.Expect(updated.GetFinalizers()).Should(ConsistOf("some-other-finalizer"))
}

func TestDeleteOperatorResources_StripsFinalizers(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	ns := "opendatahub"
	finalizer := "serving.kserve.io/llmisvcconfig-finalizer"

	// Create a LLMInferenceServiceConfig with the finalizer.
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha1)
	obj.SetName("config-test")
	obj.SetNamespace(ns)
	obj.SetFinalizers([]string{finalizer})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(obj),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.LLMInferenceServiceConfigV1Alpha1, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.LLMInferenceServiceConfigV1Alpha2, Scope: meta.RESTScopeNamespace},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.ApplicationsNamespace = ns

	// DeleteOperatorResources calls StripLLMISvcConfigFinalizers internally,
	// then delegates to BaseHandler.DeleteOperatorResources. The base handler
	// may return an error because it tries to render/delete operator manifests
	// that are not set up in this test — we only care that finalizers were
	// stripped before that point.
	_ = h.DeleteOperatorResources(ctx, cli, platform)

	// Verify the finalizer was removed.
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(gvk.LLMInferenceServiceConfigV1Alpha1)
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(obj), updated)).Should(Succeed())
	g.Expect(updated.GetFinalizers()).Should(BeEmpty())
}
