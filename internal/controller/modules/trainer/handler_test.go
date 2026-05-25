package trainer_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trainer"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Trainer: componentApi.DSCTrainer{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
					},
				},
			},
		},
		DSCI: &dsciv2.DSCInitialization{
			Spec: dsciv2.DSCInitializationSpec{
				ApplicationsNamespace: "opendatahub",
			},
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	h := trainer.NewHandler()
	if !h.IsEnabled(newPlatformCtx(operatorv1.Managed)) {
		t.Error("expected trainer to be enabled when ManagementState is Managed")
	}
}

func TestIsEnabled_Removed(t *testing.T) {
	h := trainer.NewHandler()
	if h.IsEnabled(newPlatformCtx(operatorv1.Removed)) {
		t.Error("expected trainer to be disabled when ManagementState is Removed")
	}
}

func TestIsEnabled_Empty(t *testing.T) {
	h := trainer.NewHandler()
	if h.IsEnabled(newPlatformCtx("")) {
		t.Error("expected trainer to be disabled when ManagementState is empty")
	}
}

func TestIsEnabled_NilDSC(t *testing.T) {
	h := trainer.NewHandler()
	ctx := &modules.PlatformContext{DSCI: &dsciv2.DSCInitialization{}}
	if h.IsEnabled(ctx) {
		t.Error("expected trainer to be disabled when DSC is nil")
	}
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	h := trainer.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
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

	if got := spec["managementState"]; got != "Managed" {
		t.Errorf("managementState: want %q, got %v", "Managed", got)
	}
}

func TestBuildModuleCR_EmptyManagementStateDefaultsToManaged(t *testing.T) {
	h := trainer.NewHandler()
	platform := newPlatformCtx("")

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err != nil {
		t.Fatalf("BuildModuleCR returned error: %v", err)
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}

	if got := spec["managementState"]; got != "Managed" {
		t.Errorf("managementState: want %q, got %v", "Managed", got)
	}
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	h := trainer.NewHandler()
	platform := &modules.PlatformContext{DSCI: &dsciv2.DSCInitialization{}}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err == nil {
		t.Error("expected error when DSC is nil")
	}
}

func TestGetRelatedImages(t *testing.T) {
	h := trainer.NewHandler()
	images := h.GetRelatedImages()

	want := map[string]bool{
		"RELATED_IMAGE_ODH_TRAINER_IMAGE":                               false,
		"RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE":        false,
		"RELATED_IMAGE_ODH_TRAINING_ROCM64_TORCH29_PY312_IMAGE":         false,
		"RELATED_IMAGE_ODH_TH06_CUDA130_TORCH210_PY312_IMAGE":           false,
		"RELATED_IMAGE_ODH_TH06_ROCM64_TORCH291_PY312_IMAGE":            false,
		"RELATED_IMAGE_ODH_TH06_CPU_TORCH210_PY312_IMAGE":               false,
		"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CUDA":     false,
		"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_ROCM":     false,
		"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CPU":      false,
		"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CUDA_3_5": false,
		"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_ROCM_3_5": false,
		"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CPU_3_5":  false,
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
	h := trainer.NewHandler()
	if got := h.GetName(); got != componentApi.TrainerComponentName {
		t.Errorf("GetName: want %q, got %q", componentApi.TrainerComponentName, got)
	}
}
