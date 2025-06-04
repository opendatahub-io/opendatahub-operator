package monitoring

import (
	"context"
	"embed"
	"fmt"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate = "resources/monitoring-stack.tmpl.yaml"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	mon, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, fmt.Errorf("resource instance %v is not a servicesAPI.Monitoring)", rr.Instance)
	}

	var monitoringStackName string
	switch rr.Release.Name {
	case cluster.ManagedRhoai:
		monitoringStackName = "rhoai-monitoringstack"
	case cluster.SelfManagedRhoai:
		monitoringStackName = "rhoai-monitoringstack"
	case cluster.OpenDataHub:
		monitoringStackName = "odh-monitoringstack"
	default:
		monitoringStackName = "odh-monitoringstack"
	}

	return map[string]any{
		"Monitoring":          mon.Spec,
		"MonitoringStackName": monitoringStackName,
	}, nil
}
