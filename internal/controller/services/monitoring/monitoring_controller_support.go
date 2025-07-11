package monitoring

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate = "resources/monitoring-stack.tmpl.yaml"
	InstrumentationTemplate = "resources/instrumentation.tmpl.yaml"
	ManagedStackName        = "rhoai-monitoringstack"
	OpenDataHubStackName    = "odh-monitoringstack"
	InstrumentationName     = "opendatahub-instrumentation"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type *services.Monitoring")
	}

	// Return early only if both metrics and traces are nil
	if monitoring.Spec.Metrics == nil && monitoring.Spec.Traces == nil {
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

	templateData := map[string]any{
		"Namespace": monitoring.Spec.Namespace,
	}

	// Add metrics-related data if metrics are configured
	if metrics := monitoring.Spec.Metrics; metrics != nil {
		// Handle Resources fields - provide defaults if Resources is nil
		if metrics.Resources != nil {
			templateData["CPULimit"] = defaultIfEmpty(metrics.Resources.CPULimit.String(), "500m")
			templateData["MemoryLimit"] = defaultIfEmpty(metrics.Resources.MemoryLimit.String(), "512Mi")
			templateData["CPURequest"] = defaultIfEmpty(metrics.Resources.CPURequest.String(), "100m")
			templateData["MemoryRequest"] = defaultIfEmpty(metrics.Resources.MemoryRequest.String(), "256Mi")
		} else {
			// Use defaults when Resources is nil
			templateData["CPULimit"] = "500m"
			templateData["MemoryLimit"] = "512Mi"
			templateData["CPURequest"] = "100m"
			templateData["MemoryRequest"] = "256Mi"
		}

		// Handle Storage fields - provide defaults if Storage is nil
		if metrics.Storage != nil {
			templateData["StorageSize"] = defaultIfEmpty(metrics.Storage.Size.String(), "5Gi")
			templateData["StorageRetention"] = defaultIfEmpty(metrics.Storage.Retention, "1d")
		} else {
			// Use defaults when Storage is nil
			templateData["StorageSize"] = "5Gi"
			templateData["StorageRetention"] = "1d"
		}

		templateData["MonitoringStackName"] = monitoringStackName

		// only when either storage or resources is set, we take replicas into account
		// - if user did not set it / zero-value "0", we use default value of 2
		// - if user set it to Y, we pass Y to template
		var replicas int32 = 2 // default value to match monitoringstack CRD's default
		if (metrics.Storage != nil || metrics.Resources != nil) && metrics.Replicas != 0 {
			replicas = metrics.Replicas
		}
		templateData["Replicas"] = strconv.Itoa(int(replicas))
	}

	// Add traces-related data if traces are configured
	if traces := monitoring.Spec.Traces; traces != nil {
		templateData["InstrumentationName"] = InstrumentationName
		templateData["OtlpEndpoint"] = fmt.Sprintf("http://otel-collector.%s.svc.cluster.local:4317", monitoring.Spec.Namespace)
		sampleRatio := "0.1"
		if traces.SampleRatio != "" {
			sampleRatio = traces.SampleRatio
		}
		templateData["SampleRatio"] = sampleRatio
	}

	return templateData, nil
}

func ifGVKInstalled(kvg schema.GroupVersionKind) func(context.Context, *odhtypes.ReconciliationRequest) bool {
	return func(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
		hasCRD, err := cluster.HasCRD(ctx, rr.Client, kvg)
		if err != nil {
			ctrl.Log.Error(err, "error checking if CRD installed", "GVK", kvg)
			return false
		}
		return hasCRD
	}
}
