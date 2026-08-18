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
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.SparkOperatorComponentName
	crName     = componentApi.SparkOperatorInstanceName

	deploymentName = "spark-operator-module-controller-manager"

	controllerImage = "RELATED_IMAGE_ODH_SPARK_OPERATOR_MODULE_IMAGE"

	// Module manifests are under spark-operator-module/config/default (see get_all_manifests.sh).
	// The legacy in-tree component used config/overlays/odh and config/overlays/rhoai for
	// platform-specific patches (e.g. NetworkPolicy). The module repo currently ships a single
	// default overlay; confirm RHOAI/ODH parity with the spark-operator-module maintainers.
	moduleSourcePath = "default"
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
				SourcePath:      moduleSourcePath,
				ControllerImage: controllerImage,
				RelatedImages:   relatedImages,
				DeploymentName:  deploymentName,
				GVK:             gvk.SparkOperator,
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.SparkOperator.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.SparkOperator.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.SparkOperator.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build SparkOperator CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dscCtx.DSC.Spec.Components.SparkOperator.SparkOperatorCommonSpec,
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
