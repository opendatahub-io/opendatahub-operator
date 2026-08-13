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
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
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
				Name:            moduleName,
				CRName:          crName,
				GVK:             gvk.Trainer,
				ManifestDir:     "trainer",
				ContextDir:      "default",
				ControllerImage: "RELATED_IMAGE_ODH_TRAINER_OPERATOR_IMAGE",
				DeploymentName:  "trainer-operator-controller-manager",
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_TRAINER_IMAGE",
					"RELATED_IMAGE_ODH_TH_TORCH_CUDA_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TH_TORCH_ROCM_PY312_IMAGE",
					"RELATED_IMAGE_ODH_TH_TORCH_CPU_PY312_IMAGE",
				},
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Trainer.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Trainer.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Trainer.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build Trainer CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dscCtx.DSC.Spec.Components.Trainer.TrainerCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert TrainerCommonSpec to unstructured: %w", err)
	}

	if cfg != nil {
		spec["appNamespace"] = cfg.ApplicationsNamespace
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
