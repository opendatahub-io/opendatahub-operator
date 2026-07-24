package trainer_test

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/operator-framework/api/pkg/lib/version"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trainer"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{
				Version: semver.Version{Major: 2, Minor: 20, Patch: 0},
			},
		},
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

func TestIsEnabled_NilPlatform(t *testing.T) {
	h := trainer.NewHandler()
	if h.IsEnabled(nil) {
		t.Error("expected trainer to be disabled when platform is nil")
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

	if _, exists := spec["managementState"]; exists {
		t.Error("managementState is a DSC-level field and must not be projected into the module CR")
	}

	if ns, ok := spec["appNamespace"].(string); !ok || ns != "opendatahub" {
		t.Errorf("appNamespace: want %q, got %v", "opendatahub", spec["appNamespace"])
	}
}

func TestBuildModuleCR_NilPlatformReturnsError(t *testing.T) {
	h := trainer.NewHandler()

	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error when platform is nil")
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
		"RELATED_IMAGE_ODH_TRAINER_IMAGE":                        false,
		"RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE": false,
		"RELATED_IMAGE_ODH_TRAINING_ROCM64_TORCH29_PY312_IMAGE":  false,
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

// TestDSCToModuleCRFlow verifies the complete handler flow: DSC -> handler -> Module CR.
func TestDSCToModuleCRFlow(t *testing.T) {
	t.Run("DSC with trainer=Managed creates correct Module CR", func(t *testing.T) {
		platform := newPlatformCtx(operatorv1.Managed)
		h := trainer.NewHandler()

		if !h.IsEnabled(platform) {
			t.Fatal("IsEnabled should return true when managementState=Managed")
		}

		moduleCR, err := h.BuildModuleCR(context.TODO(), nil, platform)
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

		spec, ok := moduleCR.Object["spec"].(map[string]interface{})
		if !ok {
			t.Fatal("Module CR missing spec")
		}

		if _, exists := spec["managementState"]; exists {
			t.Error("managementState is a DSC-level field and must not be projected into the module CR")
		}
	})
}

func TestGetName(t *testing.T) {
	h := trainer.NewHandler()
	if got := h.GetName(); got != componentApi.TrainerComponentName {
		t.Errorf("GetName: want %q, got %q", componentApi.TrainerComponentName, got)
	}
}
