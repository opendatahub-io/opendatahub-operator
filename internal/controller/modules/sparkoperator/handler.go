package sparkoperator

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
	moduleName = componentApi.SparkOperatorComponentName
	crName     = componentApi.SparkOperatorInstanceName

	deploymentName = "spark-operator-module-controller-manager"

	controllerImage = "RELATED_IMAGE_ODH_SPARK_OPERATOR_MODULE_IMAGE"
)

var relatedImages = []string{
	"RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				ManifestDir:     "sparkoperator",
				SourcePath:      "default",
				ControllerImage: controllerImage,
				RelatedImages:   relatedImages,
				DeploymentName:  deploymentName,
				GVK:             gvk.SparkOperator,
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}

	if platform.DSC != nil {
		return platform.DSC.Spec.Components.SparkOperator.ManagementState == operatorv1.Managed
	}

	return false
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build SparkOperator CR")
	}

	if platform.DSC == nil {
		return nil, errors.New("DSC CR is nil, cannot build SparkOperator CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&platform.DSC.Spec.Components.SparkOperator.SparkOperatorCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert SparkOperatorCommonSpec to unstructured: %w", err)
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
