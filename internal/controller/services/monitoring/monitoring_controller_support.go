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
	MonitoringStackTemplate        = "resources/monitoring-stack.tmpl.yaml"
	OpenTelemetryCollectorTemplate = "resources/opentelemetry-collector.tmpl.yaml"
	CollectorRBACTemplate          = "resources/collector-rbac.tmpl.yaml"
	PrometheusRouteTemplate        = "resources/prometheus-route.tmpl.yaml"
	ManagedStackName               = "rhoai-monitoringstack"
	OpenDataHubStackName           = "odh-monitoringstack"
	OpendatahubPipelineName        = "odh-prometheus-collector"
	ManagedPipelineName            = "rhoai-prometheus-collector"
	OpendatahubCollectorName       = "odh-collector"
	ManagedCollectorName           = "rhoai-collector"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		return nil, errors.New("monitoring metrics are not set")
	}

	var monitoringStackName string
	var promPipelineName string
	var otcName string
	switch rr.Release.Name {
	case cluster.ManagedRhoai:
		monitoringStackName = ManagedStackName
		promPipelineName = ManagedPipelineName
		otcName = ManagedCollectorName
	case cluster.SelfManagedRhoai:
		monitoringStackName = ManagedStackName
		promPipelineName = ManagedPipelineName
		otcName = ManagedCollectorName
	case cluster.OpenDataHub:
		monitoringStackName = OpenDataHubStackName
		promPipelineName = OpendatahubPipelineName
		otcName = OpendatahubCollectorName
	default:
		monitoringStackName = OpenDataHubStackName
		promPipelineName = OpendatahubPipelineName
		otcName = OpendatahubCollectorName
	}

	defaultIfEmpty := func(value, defaultVal string) string {
		if value == "" {
			return defaultVal
		}
		return value
	}

	defaultIfZero := func(value, defaultVal int) int {
		if value == 0 {
			return defaultVal
		}
		return value
	}

	metrics := monitoring.Spec.Metrics
	// Handle nil traces pointer
	tracesEnabled := monitoring.Spec.Traces != nil

	return map[string]any{
		"CPULimit":                   defaultIfEmpty(metrics.Resources.CPULimit, "500"),
		"MemoryLimit":                defaultIfEmpty(metrics.Resources.MemoryLimit, "512"),
		"CPURequest":                 defaultIfEmpty(metrics.Resources.CPURequest, "100"),
		"MemoryRequest":              defaultIfEmpty(metrics.Resources.MemoryRequest, "256"),
		"StorageSize":                defaultIfZero(metrics.Storage.Size, 5),
		"StorageRetention":           defaultIfZero(metrics.Storage.Retention, 1),
		"MonitoringStackName":        monitoringStackName,
		"Namespace":                  monitoring.Spec.Namespace,
		"PromPipelineName":           promPipelineName,
		"OpenTelemetryCollectorName": otcName,
		"Traces":                     tracesEnabled,
	}, nil
}
