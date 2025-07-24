package monitoring

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate          = "resources/monitoring-stack.tmpl.yaml"
	TempoMonolithicTemplate          = "resources/tempo-monolithic.tmpl.yaml"
	TempoStackTemplate               = "resources/tempo-stack.tmpl.yaml"
	MSName                           = "data-science-monitoringstack"
	OpenTelemetryCollectorTemplate   = "resources/opentelemetry-collector.tmpl.yaml"
	CollectorServiceMonitorsTemplate = "resources/collector-servicemonitors.tmpl.yaml"
	CollectorRBACTemplate            = "resources/collector-rbac.tmpl.yaml"
	PrometheusRouteTemplate          = "resources/prometheus-route.tmpl.yaml"
	PrometheusPipelineName           = "odh-prometheus-collector"
	opentelemetryOperator            = "opentelemetry-product"
	clusterObservabilityOperator     = "cluster-observability-operator"
	tempoOperator                    = "tempo-product"
	InstrumentationTemplate          = "resources/instrumentation.tmpl.yaml"
	InstrumentationName              = "data-science-instrumentation"
	DefaultSamplerType               = "traceidratio"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type services.Monitoring")
	}

	templateData := map[string]any{
		"Namespace": monitoring.Spec.Namespace,
	}

	templateData["Traces"] = monitoring.Spec.Traces != nil
	templateData["Metrics"] = monitoring.Spec.Metrics != nil

	// Add metrics-related data if metrics are configured
	if metrics := monitoring.Spec.Metrics; metrics != nil {
		// Handle Resources fields - provide defaults if Resources is nil
		if metrics.Resources != nil {
			cpuLimit := metrics.Resources.CPULimit.String()
			if cpuLimit == "" || cpuLimit == "0" {
				cpuLimit = "500m"
			}
			templateData["CPULimit"] = cpuLimit

			memoryLimit := metrics.Resources.MemoryLimit.String()
			if memoryLimit == "" || memoryLimit == "0" {
				memoryLimit = "512Mi"
			}
			templateData["MemoryLimit"] = memoryLimit

			cpuRequest := metrics.Resources.CPURequest.String()
			if cpuRequest == "" || cpuRequest == "0" {
				cpuRequest = "100m"
			}
			templateData["CPURequest"] = cpuRequest

			memoryRequest := metrics.Resources.MemoryRequest.String()
			if memoryRequest == "" || memoryRequest == "0" {
				memoryRequest = "256Mi"
			}
			templateData["MemoryRequest"] = memoryRequest
		} else {
			// Use defaults when Resources is nil
			templateData["CPULimit"] = "500m"
			templateData["MemoryLimit"] = "512Mi"
			templateData["CPURequest"] = "100m"
			templateData["MemoryRequest"] = "256Mi"
		}

		// Handle Storage fields - provide defaults if Storage is nil
		if metrics.Storage != nil {
			storageSize := metrics.Storage.Size.String()
			if storageSize == "" || storageSize == "0" {
				storageSize = "5Gi"
			}
			templateData["StorageSize"] = storageSize

			retention := metrics.Storage.Retention
			if retention == "" {
				retention = "1d"
			}
			templateData["StorageRetention"] = retention
		} else {
			// Use defaults when Storage is nil
			templateData["StorageSize"] = "5Gi"
			templateData["StorageRetention"] = "1d"
		}

		// only when either storage or resources is set, we take replicas into account
		// - if user did not set it / zero-value "0", we use default value of 2
		// - if user set it to Y, we pass Y to template
		var replicas int32 = 2 // default value to match monitoringstack CRD's default
		if (metrics.Storage != nil || metrics.Resources != nil) && metrics.Replicas != 0 {
			replicas = metrics.Replicas
		}
		templateData["Replicas"] = strconv.Itoa(int(replicas))
		templateData["PromPipelineName"] = PrometheusPipelineName
	}

	// Add traces-related data if traces are configured
	if traces := monitoring.Spec.Traces; traces != nil {
		templateData["InstrumentationName"] = InstrumentationName
		templateData["OtlpEndpoint"] = fmt.Sprintf("http://data-science-collector.%s.svc.cluster.local:4317", monitoring.Spec.Namespace)
		templateData["SampleRatio"] = traces.SampleRatio
		templateData["SamplerType"] = DefaultSamplerType

		// Add tempo-related data from traces.Storage fields (Storage is a struct, not a pointer)
		if traces.Storage.Backend != "" {
			templateData["Backend"] = traces.Storage.Backend
			templateData["Secret"] = traces.Storage.Secret
			templateData["Size"] = traces.Storage.Size
		}
	}

	return templateData, nil
}

func addMonitoringCapability(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	// Set initial condition state
	rr.Conditions.MarkUnknown("MonitoringConfigured")

	if err := checkMonitoringPreconditions(ctx, rr); err != nil {
		log.Error(err, "Monitoring preconditions failed")

		rr.Conditions.MarkFalse(
			"MonitoringConfigured",
			cond.WithReason(status.MissingOperatorReason),
			cond.WithMessage("Monitoring preconditions failed: %s", err.Error()),
		)

		return err
	}

	rr.Conditions.MarkTrue("MonitoringConfigured")

	return nil
}

func checkOperatorSubscription(ctx context.Context, cli client.Client, operatorName string) error {
	if found, err := cluster.SubscriptionExists(ctx, cli, operatorName); !found || err != nil {
		return fmt.Errorf("failed to find the pre-requisite operator subscription %q,"+
			" please ensure operator is installed: %w", operatorName, err)
	}
	return nil
}

func checkMonitoringPreconditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	var allErrors []error

	// Check for opentelemetry-product operator if either metrics or traces are enabled
	if rr.DSCI.Spec.Monitoring.Metrics != nil || rr.DSCI.Spec.Monitoring.Traces != nil {
		err := checkOperatorSubscription(ctx, rr.Client, opentelemetryOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	// Check for cluster-observability-operator if metrics are enabled
	if rr.DSCI.Spec.Monitoring.Metrics != nil {
		err := checkOperatorSubscription(ctx, rr.Client, clusterObservabilityOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	// Check for tempo-product operator if traces are enabled
	if rr.DSCI.Spec.Monitoring.Traces != nil {
		err := checkOperatorSubscription(ctx, rr.Client, tempoOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	if len(allErrors) > 0 {
		var errorMessages []string
		for _, err := range allErrors {
			errorMessages = append(errorMessages, err.Error())
		}
		return fmt.Errorf("monitoring preconditions failed: %s", strings.Join(errorMessages, "; "))
	}

	return nil
}
