package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
					"RELATED_IMAGE_CLI_IMAGE",
					"RELATED_IMAGE_PERSES_IMAGE",
				},
			},
		},
	}
}

// IsEnabled checks whether the monitoring module should be deployed.
// In DSC mode (DSCI present), reads DSCI.Spec.Monitoring.ManagementState.
// In Platform mode (xKS), the module stanza's presence is the signal.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSCI != nil {
		return platform.DSCI.Spec.Monitoring.ManagementState == operatorv1.Managed
	}
	if platform.Platform != nil {
		return platform.Platform.Spec.Modules.Monitoring != nil
	}
	return false
}

// BuildModuleCR projects platform monitoring configuration onto the module CR.
// In DSC mode, MonitoringCommonSpec is projected from DSCI (managementState
// is excluded — the CR's existence implies Managed).
// In Platform mode, an empty spec is used since there is no DSCI config.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build monitoring CR")
	}

	var spec map[string]any

	switch {
	case platform.DSCI != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSCI.Spec.Monitoring.MonitoringCommonSpec)
		if err != nil {
			return nil, fmt.Errorf("failed to convert MonitoringCommonSpec to unstructured: %w", err)
		}
	case platform.Platform != nil:
		spec = map[string]any{}
	default:
		return nil, errors.New("neither DSCI nor Platform is available, cannot build monitoring CR")
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
