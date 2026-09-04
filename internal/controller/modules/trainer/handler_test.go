//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package trainer

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

const testAppsNS = "opendatahub"

func newDSCContext(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
	}
	dsc.Spec.Components.Trainer.ManagementState = mgmtState

	return &modules.DSCContext{DSC: dsc}
}

func newModuleCRConfig() *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		ApplicationsNamespace: testAppsNS,
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	h := NewHandler()
	pm := &configv1alpha1.PlatformModules{
		Trainer: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}
	if !h.IsEnabled(pm) {
		t.Error("expected trainer to be enabled when ManagementState is Managed")
	}
}

func TestIsEnabled_Removed(t *testing.T) {
	h := NewHandler()
	pm := &configv1alpha1.PlatformModules{
		Trainer: common.ManagementSpec{ManagementState: operatorv1.Removed},
	}
	if h.IsEnabled(pm) {
		t.Error("expected trainer to be disabled when ManagementState is Removed")
	}
}

func TestIsEnabled_Empty(t *testing.T) {
	h := NewHandler()
	pm := &configv1alpha1.PlatformModules{
		Trainer: common.ManagementSpec{ManagementState: ""},
	}
	if h.IsEnabled(pm) {
		t.Error("expected trainer to be disabled when ManagementState is empty")
	}
}

func TestIsEnabled_Nil(t *testing.T) {
	h := NewHandler()
	if h.IsEnabled(nil) {
		t.Error("expected trainer to be disabled when modules is nil")
	}
}

func TestPopulatePlatformModule(t *testing.T) {
	h := NewHandler()

	pm := &configv1alpha1.PlatformModules{}
	h.PopulatePlatformModule(pm, newDSCContext(operatorv1.Managed))

	if pm.Trainer.ManagementState != operatorv1.Managed {
		t.Fatalf("expected Managed, got %s", pm.Trainer.ManagementState)
	}
}

func TestPopulatePlatformModule_NilSafe(t *testing.T) {
	h := NewHandler()

	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, &modules.DSCContext{})
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	h := NewHandler()

	u, err := h.BuildModuleCR(t.Context(), nil, newDSCContext(operatorv1.Managed), newModuleCRConfig())
	if err != nil {
		t.Fatalf("BuildModuleCR returned error: %v", err)
	}

	if u.GetName() != componentApi.TrainerInstanceName {
		t.Errorf("name: want %q, got %q", componentApi.TrainerInstanceName, u.GetName())
	}
	if u.GetKind() != componentApi.TrainerKind {
		t.Errorf("kind: want %q, got %q", componentApi.TrainerKind, u.GetKind())
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}

	if _, exists := spec["managementState"]; exists {
		t.Error("managementState is a DSC-level field and must not be projected into the module CR")
	}

	if ns, ok := spec["appNamespace"].(string); !ok || ns != testAppsNS {
		t.Errorf("appNamespace: want %q, got %v", testAppsNS, spec["appNamespace"])
	}
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	h := NewHandler()

	_, err := h.BuildModuleCR(t.Context(), nil, nil, newModuleCRConfig())
	if err == nil {
		t.Error("expected error when DSCContext is nil")
	}
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	h := NewHandler()

	_, err := h.BuildModuleCR(t.Context(), nil, &modules.DSCContext{}, newModuleCRConfig())
	if err == nil {
		t.Error("expected error when DSC is nil")
	}
}

func TestGetRelatedImages(t *testing.T) {
	h := NewHandler()
	images := h.GetRelatedImages()

	want := map[string]bool{
		"RELATED_IMAGE_ODH_TRAINER_IMAGE":             false,
		"RELATED_IMAGE_ODH_TH_TORCH_CUDA_PY312_IMAGE": false,
		"RELATED_IMAGE_ODH_TH_TORCH_ROCM_PY312_IMAGE": false,
		"RELATED_IMAGE_ODH_TH_TORCH_CPU_PY312_IMAGE":  false,
		"RELATED_IMAGE_RHAII_MODEL_OPT_CUDA_IMAGE":    false,
		"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE":         false,
	}

	for _, img := range images {
		if _, ok := want[img]; ok {
			want[img] = true
		} else {
			t.Errorf("unexpected related image: %q", img)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing related image: %q", name)
		}
	}
}

// TestGetOperatorManifests_PlatformOverlay verifies the handler selects the
// platform-specific Kustomize overlay and resolves it under ManifestsBasePath.
func TestGetOperatorManifests_PlatformOverlay(t *testing.T) {
	h := NewHandler()

	cases := []struct {
		name     string
		platform common.Platform
		want     string
	}{
		{"xks-default", cluster.XKS, "/base/trainer/default"},
		{"odh", cluster.OpenDataHub, "/base/trainer/overlays/odh"},
		{"self-managed-rhoai", cluster.SelfManagedRhoai, "/base/trainer/overlays/rhoai"},
		{"managed-rhoai", cluster.ManagedRhoai, "/base/trainer/overlays/rhoai"},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &modules.PlatformContext{
				ApplicationsNamespace: testAppsNS,
				ManifestsBasePath:     "/base",
				Release:               common.Release{Name: tcase.platform},
			}

			m := h.GetOperatorManifests(ctx)
			g.Expect(m.HelmCharts).Should(BeEmpty())
			g.Expect(m.Manifests).Should(HaveLen(1))
			g.Expect(m.Manifests[0].String()).Should(Equal(tcase.want))
		})
	}
}

func TestGetName(t *testing.T) {
	h := NewHandler()
	if got := h.GetName(); got != componentApi.TrainerComponentName {
		t.Errorf("GetName: want %q, got %q", componentApi.TrainerComponentName, got)
	}
}

func TestDSCToModuleCRFlow(t *testing.T) {
	t.Run("DSC with trainer=Managed creates correct Module CR", func(t *testing.T) {
		h := NewHandler()
		dscCtx := newDSCContext(operatorv1.Managed)

		pm := &configv1alpha1.PlatformModules{}
		h.PopulatePlatformModule(pm, dscCtx)

		if !h.IsEnabled(pm) {
			t.Fatal("IsEnabled should return true when managementState=Managed")
		}

		moduleCR, err := h.BuildModuleCR(t.Context(), nil, dscCtx, newModuleCRConfig())
		if err != nil {
			t.Fatalf("BuildModuleCR failed: %v", err)
		}

		if moduleCR.GetName() != componentApi.TrainerInstanceName {
			t.Errorf("Expected CR name %q, got %q", componentApi.TrainerInstanceName, moduleCR.GetName())
		}

		gvk := moduleCR.GroupVersionKind()
		if gvk.Group != "components.platform.opendatahub.io" || gvk.Version != "v1alpha1" || gvk.Kind != "Trainer" {
			t.Errorf("Unexpected GVK: %s", gvk.String())
		}

		spec, ok := moduleCR.Object["spec"].(map[string]any)
		if !ok {
			t.Fatal("Module CR missing spec")
		}

		if _, exists := spec["managementState"]; exists {
			t.Error("managementState is a DSC-level field and must not be projected into the module CR")
		}
	})
}
