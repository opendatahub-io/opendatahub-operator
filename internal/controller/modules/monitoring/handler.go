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

// IsEnabled reads the DSCI monitoring management state. Monitoring is a
// service-type module: enablement lives on the DSCI, not the DSC.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform.DSCI == nil {
		return false
	}
	return platform.DSCI.Spec.Monitoring.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects DSCI monitoring configuration onto the module CR.
// Since DSCIMonitoring and the module's MonitoringSpec share the same JSON
// schema (ManagementSpec + MonitoringCommonSpec), we convert the DSCI struct
// directly rather than mapping fields one-by-one.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform.DSCI == nil {
		return nil, errors.New("DSCI is nil, cannot build monitoring CR")
	}

	dsciMon := platform.DSCI.Spec.Monitoring

	if dsciMon.ManagementState == "" {
		dsciMon.ManagementState = operatorv1.Managed
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dsciMon)
	if err != nil {
		return nil, fmt.Errorf("failed to convert DSCIMonitoring to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)
	u.Object["spec"] = spec

	return u, nil
}
