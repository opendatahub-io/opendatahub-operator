package trainer

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.TrainerComponentName
	crName     = componentApi.TrainerInstanceName
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:        moduleName,
				CRName:      crName,
				GVK:         gvk.Trainer,
				ManifestDir: "trainer",
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_TRAINER_IMAGE",
					"RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TRAINING_ROCM64_TORCH29_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TH06_CUDA130_TORCH210_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TH06_ROCM64_TORCH291_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TH06_CPU_TORCH210_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CUDA",
					"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_ROCM",
					"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CPU",
					"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CUDA_3_5",
					"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_ROCM_3_5",
					"RELATED_IMAGE_ODH_TRAINING_UNIVERSAL_WORKBENCH_IMAGE_CPU_3_5",
				},
			},
		},
	}
}

// IsEnabled reads the DSC trainer management state. Trainer is a
// component-type module: enablement lives on the DSC, not the DSCI.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform.DSC == nil {
		return false
	}
	return platform.DSC.Spec.Components.Trainer.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects DSC trainer configuration onto the module CR.
// Since DSCTrainer and the module's TrainerSpec share the same JSON
// schema (ManagementSpec + TrainerCommonSpec), we convert the DSC struct
// directly rather than mapping fields one-by-one.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build trainer CR")
	}

	dscTrainer := platform.DSC.Spec.Components.Trainer

	if dscTrainer.ManagementState == "" {
		dscTrainer.ManagementState = operatorv1.Managed
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dscTrainer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert DSCTrainer to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)
	u.Object["spec"] = spec

	return u, nil
}
