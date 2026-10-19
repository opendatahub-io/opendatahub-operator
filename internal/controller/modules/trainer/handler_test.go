//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package trainer

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
)

const testAppsNS = "opendatahub"

func newDSC(mgmtState operatorv1.ManagementState) *dscApi.DataScienceCluster {
	dsc := &dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
	}
	dsc.Spec.Components.Trainer.ManagementState = mgmtState

	return dsc
}

func newModuleCRConfig() *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		ApplicationsNamespace: testAppsNS,
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	h := NewHandler()
	pm := &configApi.PlatformModules{
		Trainer: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}
	if !h.IsEnabled(pm) {
		t.Error("expected trainer to be enabled when ManagementState is Managed")
	}
}

func TestIsEnabled_Removed(t *testing.T) {
	h := NewHandler()
	pm := &configApi.PlatformModules{
		Trainer: common.ManagementSpec{ManagementState: operatorv1.Removed},
	}
	if h.IsEnabled(pm) {
		t.Error("expected trainer to be disabled when ManagementState is Removed")
	}
}

func TestIsEnabled_Empty(t *testing.T) {
	h := NewHandler()
	pm := &configApi.PlatformModules{
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

	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: newDSC(operatorv1.Managed)})

	if pm.Trainer.ManagementState != operatorv1.Managed {
		t.Fatalf("expected Managed, got %s", pm.Trainer.ManagementState)
	}
}

func TestPopulatePlatformModule_NilSafe(t *testing.T) {
	h := NewHandler()

	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, &modules.DSCContext{})
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

func TestGetName(t *testing.T) {
	h := NewHandler()
	if got := h.GetName(); got != componentApi.TrainerComponentName {
		t.Errorf("GetName: want %q, got %q", componentApi.TrainerComponentName, got)
	}
}

func TestDSCToPlatformModuleFlow(t *testing.T) {
	t.Run("DSC with trainer=Managed populates and enables the platform module", func(t *testing.T) {
		h := NewHandler()
		pm := &configApi.PlatformModules{}
		h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: newDSC(operatorv1.Managed)})

		if !h.IsEnabled(pm) {
			t.Fatal("IsEnabled should return true when managementState=Managed")
		}
	})
}
