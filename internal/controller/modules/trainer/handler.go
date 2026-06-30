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
				},
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.Trainer.ManagementState == operatorv1.Managed
	}
	return false
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build Trainer CR")
	}
	if platform.DSC == nil {
		return nil, errors.New("DSC is not available, cannot build Trainer CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&platform.DSC.Spec.Components.Trainer.TrainerCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert TrainerCommonSpec to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}
