package monitoring

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	componentMonitoring "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
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
	templateData["AcceleratorMetrics"] = monitoring.Spec.Metrics != nil
	templateData["ApplicationNamespace"] = rr.DSCI.Spec.ApplicationsNamespace

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

		// - if user explicitly set replicas, use their value
		// - if metrics is configured (storage or resources) but no explicit replicas, use SNO-aware defaults
		// - otherwise, rely on MonitoringStack CRD defaults
		allowedByConfig := metrics.Storage != nil || metrics.Resources != nil
		isSNO := isSingleNodeCluster(ctx, rr)

		switch {
		case metrics.Replicas != 0 && allowedByConfig:
			templateData["Replicas"] = strconv.Itoa(int(metrics.Replicas))
		case allowedByConfig:
			if isSNO {
				templateData["Replicas"] = "1"
			} else {
				templateData["Replicas"] = "2"
			}
		default:
		}

		// Handle custom metrics exporters
		if metrics.Exporters != nil {
			validExporters := make(map[string]interface{})
			var exporterNames []string

			for name, configYAML := range metrics.Exporters {
				if isReservedExporterName(name) {
					return nil, fmt.Errorf("exporter name '%s' is reserved and cannot be used", name)
				}

				// Trim and parse YAML configuration string
				cfgStr := strings.TrimSpace(configYAML)
				var cfg interface{}
				if err := yaml.Unmarshal([]byte(cfgStr), &cfg); err != nil {
					return nil, fmt.Errorf("invalid YAML configuration for exporter '%s': %w", name, err)
				}

				// Require a mapping/object to avoid invalid collector config
				cfgMap, ok := cfg.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("exporter '%s' configuration must be a YAML mapping/object", name)
				}

				validExporters[name] = cfgMap
				exporterNames = append(exporterNames, name)
			}

			// Ensure deterministic order in templates/pipelines
			sort.Strings(exporterNames)
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

// isSingleNodeCluster determines if the cluster is a single-node cluster by counting the actual nodes.
func isSingleNodeCluster(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
	nodeList := &corev1.NodeList{}
	if err := rr.Client.List(ctx, nodeList); err != nil {
		logf.FromContext(ctx).Info("could not list nodes, defaulting to multi-node behavior", "error", err)
		return false
	}

	// Count only nodes that are ready and not marked for deletion
	// We only need to know if there are more than 1 ready nodes, so we can break early
	var readyNodeCount int
	for _, node := range nodeList.Items {
		if node.DeletionTimestamp == nil {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					readyNodeCount++
					break
				}
			}
		}
		if readyNodeCount > 1 {
			break
		}
	}

	logf.FromContext(ctx).V(1).Info("detected cluster size", "totalNodes", len(nodeList.Items), "readyNodes", readyNodeCount)
	return readyNodeCount <= 1
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

func addPrometheusRules(componentName string, rr *odhtypes.ReconciliationRequest) error {
	componentRules := fmt.Sprintf("%s/monitoring/%s-prometheusrules.tmpl.yaml", componentName, componentName)

	if !common.FileExists(componentMonitoring.ComponentRulesFS, componentRules) {
		return fmt.Errorf("prometheus rules file for component %s not found", componentName)
	}

	rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
		FS:   componentMonitoring.ComponentRulesFS,
		Path: componentRules,
	})

	return nil
}

// if a component is disabled, we need to delete the prometheus rules. If the DSCI is deleted
// the rules will be gc'd automatically.
func cleanupPrometheusRules(ctx context.Context, componentName string, rr *odhtypes.ReconciliationRequest) error {
	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(gvk.PrometheusRule)
	pr.SetName(fmt.Sprintf("%s-prometheusrules", componentName))
	pr.SetNamespace(rr.DSCI.Spec.Monitoring.Namespace)

	if err := rr.Client.Delete(ctx, pr); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete prometheus rule for component %s: %w", componentName, err)
	}

	return nil
}

// isReservedExporterName checks if an exporter name conflicts with built-in exporters.
func isReservedExporterName(name string) bool {
	switch name {
	case "prometheus", "otlp/tempo":
		return true
	default:
		return false
	}
}
