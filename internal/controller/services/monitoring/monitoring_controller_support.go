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
	ManagedStackName        = "rhoai-monitoringstack"
	OpenDataHubStackName    = "odh-monitoringstack"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		return nil, nil
	}

	var monitoringStackName string
	switch rr.Release.Name {
	case cluster.ManagedRhoai:
		monitoringStackName = ManagedStackName
	case cluster.SelfManagedRhoai:
		monitoringStackName = ManagedStackName
	default:
		monitoringStackName = OpenDataHubStackName
	}

	defaultIfEmpty := func(value, defaultVal string) string {
		if value == "" {
			return defaultVal
		}
		return value
	}

	metrics := monitoring.Spec.Metrics
	return map[string]any{
		"CPULimit":            defaultIfEmpty(metrics.Resources.CPULimit, "500m"),
		"MemoryLimit":         defaultIfEmpty(metrics.Resources.MemoryLimit, "512Mi"),
		"CPURequest":          defaultIfEmpty(metrics.Resources.CPURequest, "100m"),
		"MemoryRequest":       defaultIfEmpty(metrics.Resources.MemoryRequest, "256Mi"),
		"StorageSize":         defaultIfEmpty(metrics.Storage.Size, "5Gi"),
		"StorageRetention":    defaultIfEmpty(metrics.Storage.Retention, "1d"),
		"MonitoringStackName": monitoringStackName,
		"Namespace":           monitoring.Spec.Namespace,
	}, nil
}
