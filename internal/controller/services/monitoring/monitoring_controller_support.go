package monitoring

import (
	"context"
	"embed"
	"errors"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate                   = "resources/monitoring-stack.tmpl.yaml"
	OpenTelemetryCollectorTemplate            = "resources/opentelemetry-collector.tmpl.yaml"
	CollectorRBACTemplate                     = "resources/collector-rbac.tmpl.yaml"
	PrometheusRouteTemplate                   = "resources/prometheus-route.tmpl.yaml"
	MonitoringStackName                       = "monitoringstack"
	PrometheusPipelineName                    = "odh-prometheus-collector"
	CollectorName                             = "otel"
	MonitoringStackCRDAvailable               = "MonitoringStackCRDAvailable"
	MonitoringStackCRDNotFoundReason          = "MonitoringStack CRD Not Found"
	MonitoringStackCRDNotFoundMessage         = "MonitoringStack CRD not found. Dependent operator missing."
	MonitoringStackCRDAvailableReason         = "MonitoringStack CRD Found"
	MonitoringStackCRDAvailableMessage        = "MonitoringStack CRD found"
	OpenTelemetryCollectorCRDAvailable        = "OpenTelemetryCollectorCRDAvailable"
	OpenTelemetryCollectorCRDNotFoundReason   = "OpenTelemetryCollector CRD Not Found"
	OpenTelemetryCollectorCRDNotFoundMessage  = "OpenTelemetryCollector CRD not found. Dependent operator missing."
	OpenTelemetryCollectorCRDAvailableReason  = "OpenTelemetryCollector CRD Found"
	OpenTelemetryCollectorCRDAvailableMessage = "OpenTelemetryCollector CRD found"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		return nil, errors.New("monitoring metrics are not set")
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
		"MonitoringStackName":        MonitoringStackName,
		"Namespace":                  monitoring.Spec.Namespace,
		"PromPipelineName":           PrometheusPipelineName,
		"OpenTelemetryCollectorName": CollectorName,
		"Traces":                     tracesEnabled,
	}, nil
}
