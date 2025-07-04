package monitoring

import (
	"context"
	"embed"
	"errors"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate = "resources/monitoring-stack.tmpl.yaml"
	TempoMonolithicTemplate = "resources/tempo-monolithic.tmpl.yaml"
	TempoStackTemplate      = "resources/tempo-stack.tmpl.yaml"
	ManagedStackName        = "rhoai-monitoringstack"
	OpenDataHubStackName    = "odh-monitoringstack"
	TempoName               = "tempo"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type *services.Monitoring")
	}

	templateData := map[string]any{
		"Namespace": monitoring.Spec.Namespace,
		"TempoName": TempoName,
	}

	// Add metrics data if metrics are configured
	if monitoring.Spec.Metrics != nil {
		var monitoringStackName string
		switch rr.Release.Name {
		case cluster.ManagedRhoai:
			monitoringStackName = ManagedStackName
		case cluster.SelfManagedRhoai:
			monitoringStackName = ManagedStackName
		case cluster.OpenDataHub:
			monitoringStackName = OpenDataHubStackName
		default:
			monitoringStackName = OpenDataHubStackName
		}

		metrics := monitoring.Spec.Metrics
		templateData["CPULimit"] = metrics.Resources.CPULimit
		templateData["MemoryLimit"] = metrics.Resources.MemoryLimit
		templateData["CPURequest"] = metrics.Resources.CPURequest
		templateData["MemoryRequest"] = metrics.Resources.MemoryRequest
		templateData["StorageSize"] = metrics.Storage.Size
		templateData["StorageRetention"] = metrics.Storage.Retention
		templateData["MonitoringStackName"] = monitoringStackName
	}

	// Add traces data if traces are configured
	if monitoring.Spec.Traces != nil {
		traces := monitoring.Spec.Traces.Storage
		templateData["Backend"] = traces.Backend
		templateData["Secret"] = traces.Secret
		templateData["Size"] = traces.Size
	}

	return templateData, nil
}
