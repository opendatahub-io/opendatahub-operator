package monitoring

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// Dependent operators names. match the one in the operatorcondition..
	opentelemetryOperator        = "opentelemetry-operator"
	clusterObservabilityOperator = "cluster-observability-operator"
	tempoOperator                = "tempo-operator"
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

	// Always set metrics exporters data (even if empty to allow clean template logic)
	templateData["MetricsExporters"] = make(map[string]interface{})
	templateData["MetricsExporterNames"] = []string{}

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
				retention = "90d"
			}
			templateData["StorageRetention"] = retention
		} else {
			// Use defaults when Storage is nil
			templateData["StorageSize"] = "5Gi"
			templateData["StorageRetention"] = "90d"
		}

		// only when either storage or resources is set, we take replicas into account
		// - if user did not set it / zero-value "0", we use default value of 2
		// - if user set it to Y, we pass Y to template
		var replicas int32 = 2 // default value to match monitoringstack CRD's default
		if (metrics.Storage != nil || metrics.Resources != nil) && metrics.Replicas != 0 {
			replicas = metrics.Replicas
		}
		templateData["Replicas"] = strconv.Itoa(int(replicas))

		// Handle custom metrics exporters
		if metrics.Exporters != nil {
			validExporters := make(map[string]interface{})
			var exporterNames []string

			for name, configYAML := range metrics.Exporters {
				if isReservedExporterName(name) {
					return nil, fmt.Errorf("exporter name '%s' is reserved and cannot be used", name)
				}

				// Parse YAML configuration string
				var config interface{}
				if err := yaml.Unmarshal([]byte(configYAML), &config); err != nil {
					return nil, fmt.Errorf("invalid YAML configuration for exporter '%s': %w", name, err)
				}

				validExporters[name] = config
				exporterNames = append(exporterNames, name)
			}

			templateData["MetricsExporters"] = validExporters
			templateData["MetricsExporterNames"] = exporterNames
		}
	}

	// Add traces-related data if traces are configured
	if traces := monitoring.Spec.Traces; traces != nil {
		templateData["OtlpEndpoint"] = fmt.Sprintf("http://data-science-collector.%s.svc.cluster.local:4317", monitoring.Spec.Namespace)
		templateData["SampleRatio"] = traces.SampleRatio
		templateData["Backend"] = traces.Storage.Backend // backend has default "pv" set in API

		// Add retention for all backends (both TempoMonolithic and TempoStack)
		templateData["TracesRetention"] = traces.Storage.Retention.Duration.String()

		// Add tempo-related data from traces.Storage fields (Storage is a struct, not a pointer)
		switch traces.Storage.Backend {
		case "pv":
			templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempomonolithic.%s.svc.cluster.local:4317", monitoring.Spec.Namespace)
			templateData["Size"] = traces.Storage.Size
		case "s3", "gcs":
			templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempostack-gateway.%s.svc.cluster.local:4317", monitoring.Spec.Namespace)
			templateData["Secret"] = traces.Storage.Secret
		}
	}

	return templateData, nil
}

func addMonitoringCapability(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	// Set initial condition state
	rr.Conditions.MarkUnknown(status.ConditionMonitoringAvailable)

	if err := checkMonitoringPreconditions(ctx, rr); err != nil {
		log.Error(err, "Monitoring preconditions failed")

		rr.Conditions.MarkFalse(
			status.ConditionMonitoringAvailable,
			cond.WithReason(status.MissingOperatorReason),
			cond.WithMessage("Monitoring preconditions failed: %s", err.Error()),
		)

		return err
	}

	rr.Conditions.MarkTrue(status.ConditionMonitoringAvailable)

	return nil
}

func checkMonitoringPreconditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type services.Monitoring")
	}
	var allErrors *multierror.Error

	// Check for opentelemetry-product operator if either metrics or traces are enabled
	if monitoring.Spec.Metrics != nil || monitoring.Spec.Traces != nil {
		if found, err := cluster.OperatorExists(ctx, rr.Client, opentelemetryOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
			allErrors = multierror.Append(allErrors, odherrors.NewStopError(status.OpenTelemetryCollectorOperatorMissingMessage))
		}
	}

	// Check for cluster-observability-operator if metrics are enabled
	if monitoring.Spec.Metrics != nil {
		if found, err := cluster.OperatorExists(ctx, rr.Client, clusterObservabilityOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
			allErrors = multierror.Append(allErrors, odherrors.NewStopError(status.COOMissingMessage))
		}
	}

	// Check for tempo-product operator if traces are enabled
	if monitoring.Spec.Traces != nil {
		if found, err := cluster.OperatorExists(ctx, rr.Client, tempoOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
			allErrors = multierror.Append(allErrors, odherrors.NewStopError(status.TempoOperatorMissingMessage))
		}
	}

	return allErrors.ErrorOrNil()
}

// isReservedExporterName checks if an exporter name conflicts with built-in exporters.
func isReservedExporterName(name string) bool {
	reservedNames := map[string]bool{
		"prometheus": true,
		// Add other reserved names as needed
	}
	return reservedNames[name]
}
