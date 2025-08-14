package monitoring

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

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
		// - if user did not set it / zero-value "0", we choose default based on cluster size (1 for SNO, else 2)
		// - if user set it to Y, we pass Y to template
		allowedByConfig := metrics.Storage != nil || metrics.Resources != nil
		isSNO := isSingleNodeCluster(ctx, rr)

		switch {
		case metrics.Replicas != 0 && allowedByConfig:
			templateData["Replicas"] = strconv.Itoa(int(metrics.Replicas))
		case isSNO: // enforce 1 replica on single-node even if Storage/Resources are not set
			templateData["Replicas"] = "1"
		case allowedByConfig: // multi-node and config provided but no explicit replicas -> default to 2
			templateData["Replicas"] = "2"
		default:
			// Do not set .Replicas; rely on MonitoringStack CRD defaults (2) when nothing configured
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

func isSingleNodeCluster(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
	cm := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Name:      "cluster-config-v1",
		Namespace: "kube-system",
	}
	if err := rr.Client.Get(ctx, namespacedName, cm); err != nil {
		logf.FromContext(ctx).Info("could not read cluster-config-v1, defaulting to multi-node behavior", "error", err)
		return false
	}

	installConfigStr := cm.Data["install-config"]
	if installConfigStr == "" {
		logf.FromContext(ctx).Info("install-config not found in cluster-config-v1, defaulting to multi-node behavior")
		return false
	}

	type replicaGroup struct {
		Replicas int32 `json:"replicas"`
	}
	type minimalInstallConfig struct {
		ControlPlane replicaGroup   `json:"controlPlane"`
		Compute      []replicaGroup `json:"compute"`
	}

	var ic minimalInstallConfig
	if err := yaml.Unmarshal([]byte(installConfigStr), &ic); err != nil {
		logf.FromContext(ctx).Info("failed to parse install-config, defaulting to multi-node behavior", "error", err)
		return false
	}

	totalNodes := ic.ControlPlane.Replicas
	for i := range ic.Compute {
		totalNodes += ic.Compute[i].Replicas
	}

	return totalNodes <= 1
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
