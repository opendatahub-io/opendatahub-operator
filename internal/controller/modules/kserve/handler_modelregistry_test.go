package kserve_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"

	. "github.com/onsi/gomega"
)

func TestBuildModuleCR_ModelRegistryStateManaged(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")

	mr, ok := spec["modelRegistry"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.modelRegistry missing")
	g.Expect(mr["managementState"]).Should(Equal("Managed"))
}

func TestBuildModuleCR_ModelRegistryStateRemoved(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.ModelRegistry.ManagementState = operatorv1.Removed

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	mr, ok := spec["modelRegistry"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(mr["managementState"]).Should(Equal("Removed"))
}

func TestBuildModuleCR_ModelRegistryStateDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	// ModelRegistry management state not set (empty string)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	mr, ok := spec["modelRegistry"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.modelRegistry must be present even when DSC MR state is empty")
	g.Expect(mr["managementState"]).Should(Equal("Removed"))
}
