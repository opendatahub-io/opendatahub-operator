package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = serviceApi.MonitoringServiceName
	crName     = serviceApi.MonitoringInstanceName
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:              moduleName,
				CRName:            crName,
				ReleaseName:       "odh-observability",
				ChartDir:          "odh-observability",
				NamespaceValueKey: "operatorNamespace",
				GVK:               gvk.Monitoring,
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
					"RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE",
					"RELATED_IMAGE_PERSES_IMAGE",
				},
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if dscCtx == nil || dscCtx.DSCI == nil {
		return
	}
	pm.Monitoring.ManagementState = dscCtx.DSCI.Spec.Monitoring.ManagementState
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil || platform.Platform == nil {
		return false
	}
	return platform.Platform.Spec.Modules.Monitoring.ManagementState == operatorv1.Managed
}

// BuildModuleCR constructs the Monitoring CR from DSCI spec with
// conditional field projection matching the monitoring domain rules.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSCI == nil {
		return nil, errors.New("DSCI is nil, cannot build monitoring CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dscCtx.DSCI.Spec.Monitoring.MonitoringCommonSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to convert MonitoringSpec to unstructured: %w", err)
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
